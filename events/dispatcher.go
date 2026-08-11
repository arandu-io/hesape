package events

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/str"
)

// Listener is one prepared listener: the closure MakeListener returns.
//
// The PHP has no name for this shape because a Closure is anonymous there --
// makeListener() documents its return as "\Closure" and every prepared listener
// is called as $listener($event, $payload). Go needs a name for the type, and
// this is it. The two arguments are the same two: the event name, and the
// payload the event was dispatched with.
type Listener func(event string, payload []any) any

// ClassListener is the [class, method] pair the PHP writes as an array literal.
//
//	$events->listen('invoice.paid', [Notifier::class, 'whenPaid']);
//	dispatcher.Listen("invoice.paid", events.ClassListener{Class: notifier, Method: "WhenPaid"})
//
// Go has no array literal that mixes a class and a method name, and it has no
// class-string at all: a type is not addressable by name at run time. So the
// first element is the listener value itself rather than the name of its class,
// which is also what removes the container from CreateClassListener (ADR 0001).
type ClassListener struct {
	// Class is the listener. An empty Method calls its Handle method.
	Class any
	// Method is the method to call. Empty means Handle, which is the default
	// parseClassCallable() applies in the PHP.
	Method string
}

// Subscriber is an object that registers its own listeners.
//
// It answers Illuminate\Contracts\Events\Dispatcher::subscribe. The map it
// returns is the array the PHP subscriber returns -- event name to listeners --
// and a nil map means the subscriber registered everything itself through the
// dispatcher it was handed.
type Subscriber interface {
	Subscribe(dispatcher *Dispatcher) map[string][]any
}

// ShouldQueue marks a listener that is handled on the queue.
//
// In the PHP this is an empty marker interface and the dispatcher asks
// implementsInterface(ShouldQueue::class). An empty interface in Go is
// satisfied by every value, so the marker needs a method -- and Laravel already
// has the method, because handlerWantsToBeQueued() calls shouldQueue($event)
// when the listener defines it. The two are folded into one here: returning
// false is the listener declining the queue for this event.
type ShouldQueue interface {
	ShouldQueue(event any) bool
}

// ShouldDispatchAfterCommit marks an event that waits for the transaction.
//
// Illuminate\Contracts\Events\ShouldDispatchAfterCommit, with a method for the
// same reason ShouldQueue has one.
type ShouldDispatchAfterCommit interface {
	ShouldDispatchAfterCommit() bool
}

// ShouldHandleEventsAfterCommit marks a listener that waits for the transaction.
//
// Illuminate\Contracts\Events\ShouldHandleEventsAfterCommit. It replaces both
// that interface and the $afterCommit property the PHP also accepts, because a
// property is not something Go can ask a value for.
type ShouldHandleEventsAfterCommit interface {
	ShouldHandleEventsAfterCommit() bool
}

// Queue is the little of Illuminate\Contracts\Queue\Factory the dispatcher
// needs to put a listener on the queue.
//
// It is declared here rather than imported so this package does not depend on
// the queue implementation, which is the same reason DB is declared here.
type Queue interface {
	// Connection returns the named connection. An empty name is the default.
	Connection(name string) Connection
}

// Connection is the two calls queueHandler() makes on a queue connection.
type Connection interface {
	PushOn(queue string, job any) error
	LaterOn(queue string, delay time.Duration, job any) error
}

// TransactionManager is what the dispatcher needs from
// Illuminate\Database\DatabaseTransactionsManager: somewhere to hang work that
// only runs if the transaction commits.
type TransactionManager interface {
	AddCallback(callback func())
}

// Dispatcher is Illuminate\Events\Dispatcher.
//
// It registers listeners by event name, fires them in registration order, and
// stops early on request. The outbox in this package is a different thing and
// they meet in one place: Publish hands a stored event to the listeners
// registered for its name, so the relay delivers through this registry.
//
// Unlike the PHP, it is safe for concurrent use: one process serves many
// requests here, and a listener registered while another goroutine dispatches
// is a data race rather than a curiosity. Defer is the exception and says so.
type Dispatcher struct {
	mu sync.Mutex

	// listeners are the registered listeners, unprepared, by event name.
	listeners map[string][]any
	// wildcards are the listeners registered against a pattern.
	wildcards map[string][]any
	// wildcardsCache is the prepared wildcard listeners, by event name.
	wildcardsCache map[string][]Listener

	queueResolver              func() Queue
	transactionManagerResolver func() TransactionManager

	deferredEvents  []deferredEvent
	deferringEvents bool
	eventsToDefer   []string
}

// deferredEvent is one dispatch held back by Defer. The PHP keeps
// func_get_args() and spreads it again; the three arguments are named here.
type deferredEvent struct {
	event   any
	payload []any
	halt    bool
}

// NewDispatcher returns a dispatcher with nothing registered.
//
// The PHP constructor takes the container, which is what resolves a class
// listener and a subscriber given by name. There is no container here (ADR
// 0001), so there is nothing to take: a class listener is the value itself.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		listeners:      map[string][]any{},
		wildcards:      map[string][]any{},
		wildcardsCache: map[string][]Listener{},
	}
}

// Listen registers an event listener with the dispatcher.
//
//	d.Listen("invoice.paid", func(e Stored) error { ... })
//	d.Listen([]string{"invoice.paid", "invoice.voided"}, listener)
//	d.Listen("invoice.*", func(name string, payload []any) any { ... })
//	d.Listen(func(e InvoicePaid) { ... })
//
// The last form is the PHP's one-argument call: a closure registers itself
// against the type of its first parameter, which the PHP reads with
// ReflectsClosures and this reads with reflect. When the first parameter is a
// context.Context the second one names the event, because a listener in Go
// takes the context it will need.
//
// PHP declares this as listen($events, $listener = null); Go has no default
// argument, so the second one is variadic and the one-argument call is the
// closure form.
func (d *Dispatcher) Listen(events any, listener ...any) {
	var l any
	if len(listener) > 0 {
		l = listener[0]
	}

	if l == nil {
		if queued, ok := events.(*QueuedClosure); ok {
			for _, name := range firstParameterTypes(queued.Closure) {
				d.Listen(name, queued.Resolve(d))
			}
			return
		}
		if names := firstParameterTypes(events); len(names) > 0 {
			for _, name := range names {
				d.Listen(name, events)
			}
			return
		}
	}

	if queued, ok := l.(*QueuedClosure); ok {
		l = queued.Resolve(d)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for _, event := range eventNames(events) {
		if strings.Contains(event, "*") {
			d.setupWildcardListen(event, l)
			continue
		}
		d.listeners[event] = append(d.listeners[event], l)
	}
}

// setupWildcardListen records a pattern listener and drops the prepared cache.
// The caller holds the lock.
func (d *Dispatcher) setupWildcardListen(event string, listener any) {
	d.wildcards[event] = append(d.wildcards[event], listener)
	d.wildcardsCache = map[string][]Listener{}
}

// HasListeners reports whether a given event has listeners.
func (d *Dispatcher) HasListeners(eventName string) bool {
	d.mu.Lock()
	_, direct := d.listeners[eventName]
	_, pattern := d.wildcards[eventName]
	d.mu.Unlock()
	return direct || pattern || d.HasWildcardListeners(eventName)
}

// HasWildcardListeners reports whether the given event has any wildcard
// listeners.
func (d *Dispatcher) HasWildcardListeners(eventName string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.wildcards {
		if str.Is([]string{key}, eventName, false) {
			return true
		}
	}
	return false
}

// Push registers an event and payload to be fired later.
//
// It is Flush that fires it, and the mechanism is the PHP's: a listener is
// registered against the event name with "_pushed" appended, and dispatching
// that name dispatches the real one.
func (d *Dispatcher) Push(event string, payload ...any) {
	d.Listen(event+"_pushed", func(...any) any {
		d.Dispatch(event, payload...)
		return nil
	})
}

// Flush fires a set of pushed events.
func (d *Dispatcher) Flush(event string) {
	d.Dispatch(event + "_pushed")
}

// Subscribe registers an event subscriber with the dispatcher.
//
// A string in the returned map is the name of a method on the subscriber, which
// is the same shorthand the PHP accepts; anything else is a listener.
func (d *Dispatcher) Subscribe(subscriber Subscriber) {
	for event, listeners := range subscriber.Subscribe(d) {
		for _, listener := range listeners {
			if method, ok := listener.(string); ok && hasMethod(subscriber, method) {
				d.Listen(event, ClassListener{Class: subscriber, Method: method})
				continue
			}
			d.Listen(event, listener)
		}
	}
}

// Until fires an event until the first non-nil response is returned.
//
// It is the PHP's dispatch($event, $payload, halt: true), which is the only
// thing until() does there. Go has no default argument for $halt, so the two
// calls are the two methods, which is how the PHP is written anyway.
func (d *Dispatcher) Until(event any, payload ...any) any {
	return d.dispatch(event, payload, true)
}

// Dispatch fires an event and calls the listeners.
//
// It returns what each listener returned, in order. A listener that returns
// false stops the ones after it, exactly as in the PHP -- and a listener that
// returns nothing returns nil here, which is the PHP's null.
//
// When event is not a string it is the event itself: the name is its type and
// the payload is the value, which is get_class($event) with the only naming Go
// offers.
func (d *Dispatcher) Dispatch(event any, payload ...any) []any {
	responses, _ := d.dispatch(event, payload, false).([]any)
	return responses
}

// dispatch is the PHP's dispatch($event, $payload, $halt): one body, two public
// names, because Go cannot default the third argument.
func (d *Dispatcher) dispatch(event any, payload []any, halt bool) any {
	// The PHP asks is_object($event); here the question is the same one the
	// other way round, because a name is a string and an event is anything else.
	_, named := event.(string)
	isEventObject := !named

	parsedEvent, parsedPayload := parseEventAndPayload(event, payload)

	d.mu.Lock()
	deferring := d.deferringEvents && (d.eventsToDefer == nil || contains(d.eventsToDefer, parsedEvent))
	if deferring {
		d.deferredEvents = append(d.deferredEvents, deferredEvent{event: event, payload: payload, halt: halt})
		d.mu.Unlock()
		return nil
	}
	transactions := d.transactionManagerResolver
	d.mu.Unlock()

	// An event that is not meant to be dispatched unless the transaction
	// commits is hung on the transaction manager instead of being fired now.
	if isEventObject && len(parsedPayload) > 0 && transactions != nil {
		if after, ok := parsedPayload[0].(ShouldDispatchAfterCommit); ok && after.ShouldDispatchAfterCommit() {
			if manager := transactions(); manager != nil {
				manager.AddCallback(func() { d.invokeListeners(parsedEvent, parsedPayload, halt) })
				return nil
			}
		}
	}

	return d.invokeListeners(parsedEvent, parsedPayload, halt)
}

// invokeListeners calls every listener for the event, in order.
//
// The PHP broadcasts here as well, through the BroadcastFactory it pulls out of
// the container. That is the container path (ADR 0001) and it is not here: an
// event that has to leave the process goes through the outbox, which is the one
// delivery this package owns.
func (d *Dispatcher) invokeListeners(event string, payload []any, halt bool) any {
	var responses []any

	for _, listener := range d.GetListeners(event) {
		response := listener(event, payload)

		// A response with halting enabled is the answer, and the listeners
		// after it are not called.
		if halt && response != nil {
			return response
		}

		// A listener that returns false stops the event travelling further
		// down the chain.
		if stop, ok := response.(bool); ok && !stop {
			break
		}

		responses = append(responses, response)
	}

	if halt {
		return nil
	}
	return responses
}

// parseEventAndPayload prepares the event and payload for dispatching.
func parseEventAndPayload(event any, payload []any) (string, []any) {
	if name, ok := event.(string); ok {
		return name, payload
	}
	return typeName(event), []any{event}
}

// GetListeners returns all of the listeners for a given event name, prepared.
//
// The PHP also merges the listeners registered against the interfaces the event
// class implements. Go has no way to ask "which interfaces does this name
// implement" -- an interface is structural and a name is not a type at run
// time -- so addInterfaceListeners has no equivalent and there is none here.
func (d *Dispatcher) GetListeners(eventName string) []Listener {
	d.mu.Lock()
	defer d.mu.Unlock()

	prepared := make([]Listener, 0, len(d.listeners[eventName]))
	for _, listener := range d.listeners[eventName] {
		prepared = append(prepared, d.MakeListener(listener, false))
	}
	return append(prepared, d.getWildcardListeners(eventName)...)
}

// getWildcardListeners returns the pattern listeners for the event, from the
// cache when it is warm. The caller holds the lock.
func (d *Dispatcher) getWildcardListeners(eventName string) []Listener {
	if cached, ok := d.wildcardsCache[eventName]; ok {
		return cached
	}

	var wildcards []Listener
	for key, listeners := range d.wildcards {
		if !str.Is([]string{key}, eventName, false) {
			continue
		}
		for _, listener := range listeners {
			wildcards = append(wildcards, d.MakeListener(listener, true))
		}
	}
	d.wildcardsCache[eventName] = wildcards
	return wildcards
}

// GetRawListeners returns the raw, unprepared listeners.
func (d *Dispatcher) GetRawListeners() map[string][]any {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make(map[string][]any, len(d.listeners))
	for event, listeners := range d.listeners {
		out[event] = append([]any(nil), listeners...)
	}
	return out
}

// MakeListener prepares a registered listener for calling.
//
// PHP declares this as makeListener($listener, $wildcard = false); Go has no
// default argument, so the flag is always written out.
//
// A wildcard listener is handed the event name and the whole payload, because
// it answered a pattern and has to be told which event arrived. Every other
// listener is handed the payload spread across its parameters, which is the
// PHP's $listener(...array_values($payload)).
func (d *Dispatcher) MakeListener(listener any, wildcard bool) Listener {
	switch l := listener.(type) {
	case Listener:
		return l
	case func(event string, payload []any) any:
		return l
	case ClassListener:
		return d.CreateClassListener(l, wildcard)
	case *QueuedClosure:
		return l.Resolve(d)
	}

	// A value that is not a function is the PHP's class-string listener: the
	// object whose handle() answers the event.
	if listener != nil && reflect.TypeOf(listener).Kind() != reflect.Func {
		return d.CreateClassListener(listener, wildcard)
	}

	return func(event string, payload []any) any {
		if wildcard {
			return callFunc(listener, []any{event, payload})
		}
		return callFunc(listener, payload)
	}
}

// CreateClassListener creates a listener out of an object and a method.
//
// The PHP resolves the class out of the IoC container; here the value is
// already the value, which is the whole of what dropping the container changes
// (ADR 0001). The rest is the same: the default method is Handle, a listener
// that wants the queue is pushed onto it instead of being called, and a
// listener that waits for the transaction is hung on the transaction manager.
func (d *Dispatcher) CreateClassListener(listener any, wildcard bool) Listener {
	return func(event string, payload []any) any {
		callable := d.createClassCallable(listener)
		if wildcard {
			return callable([]any{event, payload})
		}
		return callable(payload)
	}
}

// createClassCallable turns a class listener into the function that runs it.
func (d *Dispatcher) createClassCallable(listener any) func(args []any) any {
	class, method := listener, ""
	if pair, ok := listener.(ClassListener); ok {
		class, method = pair.Class, pair.Method
	}
	if method == "" {
		method = "Handle"
	}
	if !hasMethod(class, method) {
		// The PHP falls back to __invoke, which Go does not have: a value is
		// not callable. The nearest thing is a method named for it.
		method = "Invoke"
	}

	if d.handlerShouldBeQueued(class) {
		return func(args []any) any {
			return d.createQueuedHandlerCallable(class, method, args)
		}
	}

	if d.handlerShouldBeDispatchedAfterDatabaseTransactions(class) {
		return func(args []any) any {
			d.resolveTransactionManager().AddCallback(func() {
				callMethod(class, method, args)
			})
			return nil
		}
	}

	return func(args []any) any { return callMethod(class, method, args) }
}

// handlerShouldBeQueued reports whether the listener class goes on the queue.
func (d *Dispatcher) handlerShouldBeQueued(class any) bool {
	_, ok := class.(ShouldQueue)
	d.mu.Lock()
	resolver := d.queueResolver
	d.mu.Unlock()
	return ok && resolver != nil
}

// handlerShouldBeDispatchedAfterDatabaseTransactions reports whether the
// listener waits for the current transaction to commit.
func (d *Dispatcher) handlerShouldBeDispatchedAfterDatabaseTransactions(class any) bool {
	after, ok := class.(ShouldHandleEventsAfterCommit)
	return ok && after.ShouldHandleEventsAfterCommit() && d.resolveTransactionManager() != nil
}

// createQueuedHandlerCallable puts the listener on the queue.
func (d *Dispatcher) createQueuedHandlerCallable(class any, method string, args []any) any {
	if wants, ok := class.(ShouldQueue); ok {
		var event any
		if len(args) > 0 {
			event = args[0]
		}
		if !wants.ShouldQueue(event) {
			return nil
		}
	}
	return d.queueHandler(class, method, args)
}

// queueHandler pushes the listener's job onto the queue.
//
// The PHP acquires a UniqueLock through the cache before pushing a unique job.
// There is no cache here -- this package would have to depend on it -- so the
// job carries what it knows about uniqueness and the queue is what enforces it.
func (d *Dispatcher) queueHandler(class any, method string, args []any) any {
	return d.push(d.propagateListenerOptions(class, NewCallQueuedListener(class, method, args)))
}

// push sends a job to the connection and queue it asked for.
//
// It is the tail of the PHP's queueHandler(), pulled out because QueuedClosure
// needs the same three lines and reaches them there through the global
// dispatch() helper, which does not exist here (ADR 0002).
func (d *Dispatcher) push(job *CallQueuedListener) any {
	queue := d.resolveQueue()
	if queue == nil {
		return nil
	}

	connection := queue.Connection(job.Connection)
	if connection == nil {
		return nil
	}

	if job.Delay <= 0 {
		return errorOrNil(connection.PushOn(job.Queue, job))
	}
	return errorOrNil(connection.LaterOn(job.Queue, job.Delay, job))
}

// propagateListenerOptions copies what the listener declares onto the job.
//
// The PHP reads a dozen optional methods and properties off the listener. Go
// has no properties, so each one that survives is an optional interface, and
// the ones that only exist to carry a PHP attribute do not survive.
func (d *Dispatcher) propagateListenerOptions(listener any, job *CallQueuedListener) *CallQueuedListener {
	if v, ok := listener.(interface{ ViaConnection() string }); ok {
		job.Connection = v.ViaConnection()
	}
	if v, ok := listener.(interface{ ViaQueue() string }); ok {
		job.Queue = v.ViaQueue()
	}
	if v, ok := listener.(interface{ WithDelay() time.Duration }); ok {
		job.Delay = v.WithDelay()
	}
	if v, ok := listener.(interface{ Tries() int }); ok {
		job.Tries = v.Tries()
	}
	if v, ok := listener.(interface{ Backoff() time.Duration }); ok {
		job.Backoff = v.Backoff()
	}
	if v, ok := listener.(interface{ RetryUntil() time.Time }); ok {
		job.RetryUntil = v.RetryUntil()
	}
	if v, ok := listener.(interface{ MessageGroup() string }); ok {
		job.MessageGroup = v.MessageGroup()
	}
	if v, ok := listener.(interface{ Deduplicator() string }); ok {
		job.WithDeduplicator(v.Deduplicator)
	}
	if v, ok := listener.(interface{ ShouldBeUnique() bool }); ok {
		job.shouldBeUnique = v.ShouldBeUnique()
	}
	if v, ok := listener.(interface{ ShouldBeUniqueUntilProcessing() bool }); ok {
		job.shouldBeUniqueUntilProcessing = v.ShouldBeUniqueUntilProcessing()
	}
	if job.shouldBeUnique {
		if v, ok := listener.(interface{ UniqueID() any }); ok {
			job.uniqueID = v.UniqueID()
		}
		if v, ok := listener.(interface{ UniqueFor() time.Duration }); ok {
			job.uniqueFor = v.UniqueFor()
		}
	}
	if v, ok := listener.(ShouldHandleEventsAfterCommit); ok {
		job.AfterCommit = v.ShouldHandleEventsAfterCommit()
	}
	return job
}

// Forget removes a set of listeners from the dispatcher.
func (d *Dispatcher) Forget(event string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.forget(event)
}

// forget is Forget with the lock already held.
func (d *Dispatcher) forget(event string) {
	if strings.Contains(event, "*") {
		delete(d.wildcards, event)
	} else {
		delete(d.listeners, event)
	}

	for key := range d.wildcardsCache {
		if str.Is([]string{event}, key, false) {
			delete(d.wildcardsCache, key)
		}
	}
}

// ForgetPushed forgets all of the pushed listeners.
func (d *Dispatcher) ForgetPushed() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.listeners {
		if strings.HasSuffix(key, "_pushed") {
			d.forget(key)
		}
	}
}

// resolveQueue returns the queue from the resolver, or nil when there is none.
func (d *Dispatcher) resolveQueue() Queue {
	d.mu.Lock()
	resolver := d.queueResolver
	d.mu.Unlock()
	if resolver == nil {
		return nil
	}
	return resolver()
}

// SetQueueResolver sets the queue implementation the dispatcher pushes through.
func (d *Dispatcher) SetQueueResolver(resolver func() Queue) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queueResolver = resolver
	return d
}

// resolveTransactionManager returns the transaction manager from the resolver.
func (d *Dispatcher) resolveTransactionManager() TransactionManager {
	d.mu.Lock()
	resolver := d.transactionManagerResolver
	d.mu.Unlock()
	if resolver == nil {
		return nil
	}
	return resolver()
}

// SetTransactionManagerResolver sets the database transaction manager the
// dispatcher hangs after-commit work on.
func (d *Dispatcher) SetTransactionManagerResolver(resolver func() TransactionManager) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transactionManagerResolver = resolver
	return d
}

// Defer runs callback while holding events back, then dispatches them.
//
// Passing event names defers only those; passing none defers every event, which
// is the PHP's $events = null.
//
// PHP is generic in the callback's result. A Go method cannot have a type
// parameter, so the result is any -- the change ADR 0044 allows where the PHP
// is generic, said here because it is the one place the signature differs.
//
// This is the one method on the dispatcher that is not safe to call from two
// goroutines at once: deferring is a mode the whole dispatcher is in, and two
// callers would take each other's events.
func (d *Dispatcher) Defer(callback func() any, events ...string) any {
	d.mu.Lock()
	wasDeferring := d.deferringEvents
	previousDeferredEvents := d.deferredEvents
	previousEventsToDefer := d.eventsToDefer

	d.deferringEvents = true
	d.deferredEvents = nil
	d.eventsToDefer = events
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.deferringEvents = wasDeferring
		d.deferredEvents = previousDeferredEvents
		d.eventsToDefer = previousEventsToDefer
		d.mu.Unlock()
	}()

	result := callback()

	d.mu.Lock()
	d.deferringEvents = false
	held := d.deferredEvents
	d.mu.Unlock()

	for _, held := range held {
		d.dispatch(held.event, held.payload, held.halt)
	}

	return result
}

// Publish hands a stored event to the listeners registered for its name.
//
// This is where the mechanism and the surface meet: the outbox stores, the
// relay reads, and the listeners were registered with Listen. A Dispatcher is
// therefore a Publisher, and NewRelay takes one directly.
//
// The payload is the context and the row, in that order, so a listener reads
//
//	d.Listen("invoice.paid", func(ctx context.Context, e events.Stored) error { ... })
//
// A listener that returns an error fails the delivery, which is what puts the
// event back in the outbox for the next pass. Nothing else is treated as a
// failure: the PHP has no error return and a listener that answers with a value
// is answering, not failing.
//
// Every listener runs, including the ones behind the one that failed, because
// Dispatch is what runs them and Dispatch does not stop for a response. The
// retry then runs all of them again, which is at-least-once arriving where it
// always arrives: a listener is idempotent on Stored.ID. A listener that does
// want to stop the ones behind it returns false, which is the PHP's own way of
// saying it.
func (d *Dispatcher) Publish(ctx context.Context, e Stored) error {
	for _, response := range d.Dispatch(e.Name, ctx, e) {
		if err, ok := response.(error); ok && err != nil {
			return err
		}
	}
	return nil
}

// eventNames reads the event names out of what Listen was given.
func eventNames(events any) []string {
	switch e := events.(type) {
	case string:
		return []string{e}
	case []string:
		return e
	case nil:
		return nil
	}
	return []string{typeName(events)}
}

// contains reports whether the list holds the value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()

// firstParameterTypes names the event a closure listens for, from the type of
// its first parameter.
//
// It answers ReflectsClosures::firstClosureParameterTypes. A leading
// context.Context is skipped: the PHP has no context and a Go listener that
// takes one still listens for what comes after it.
func firstParameterTypes(fn any) []string {
	if fn == nil {
		return nil
	}
	t := reflect.TypeOf(fn)
	if t.Kind() != reflect.Func || t.NumIn() == 0 {
		return nil
	}

	first := 0
	if t.In(0) == contextType {
		if t.NumIn() == 1 {
			return nil
		}
		first = 1
	}
	in := t.In(first)
	if in.Kind() == reflect.Interface || in.Kind() == reflect.Slice || in.Kind() == reflect.String {
		// A listener that takes any, a payload slice or a name is the prepared
		// shape rather than a typed event: it names no event of its own.
		return nil
	}
	return []string{typeNameOf(in)}
}

// typeName is get_class($event) with the only naming Go offers: the import path
// and the type name, which is as unique as a PHP fully qualified class name.
func typeName(v any) string {
	if v == nil {
		return ""
	}
	return typeNameOf(reflect.TypeOf(v))
}

func typeNameOf(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.String()
	}
	return t.PkgPath() + "." + t.Name()
}

// hasMethod reports whether the value has an exported method of that name.
func hasMethod(v any, name string) bool {
	if v == nil || name == "" {
		return false
	}
	rv := reflect.ValueOf(v)
	if rv.MethodByName(name).IsValid() {
		return true
	}
	// A method with a pointer receiver is not on the value, and a listener
	// registered by value would silently stop answering.
	if rv.Kind() != reflect.Pointer && rv.CanAddr() {
		return rv.Addr().MethodByName(name).IsValid()
	}
	return false
}

// callMethod calls the named method with the payload spread across its
// parameters.
func callMethod(v any, name string, args []any) any {
	if v == nil {
		return nil
	}
	method := reflect.ValueOf(v).MethodByName(name)
	if !method.IsValid() {
		return nil
	}
	return callValue(method, args)
}

// callFunc calls a listener function with the payload spread across its
// parameters, which is the PHP's $listener(...array_values($payload)).
func callFunc(fn any, args []any) any {
	switch f := fn.(type) {
	case nil:
		return nil
	case func():
		f()
		return nil
	case func(...any):
		f(args...)
		return nil
	case func(...any) any:
		return f(args...)
	case func(...any) error:
		return errorOrNil(f(args...))
	}

	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return nil
	}
	return callValue(v, args)
}

// callValue spreads args across the function's parameters and calls it.
//
// PHP raises ArgumentCountError when the payload is short and ignores it when
// the payload is long. Neither is available in Go: a reflect.Call with the
// wrong arity panics. So a missing argument is the zero value of its parameter
// and a surplus one is dropped, which keeps a mismatched listener quiet rather
// than taking the process down with it.
//
// One binding rule is not positional: a leading context.Context argument is
// dropped when the function does not take one. It is what lets the same
// listener be registered for an in-process dispatch and for an outbox delivery,
// which carries the context of the pass that read the row.
func callValue(v reflect.Value, args []any) any {
	t := v.Type()

	if len(args) > 0 && isContext(args[0]) && !(t.NumIn() > 0 && t.In(0) == contextType) {
		args = args[1:]
	}

	var in []reflect.Value
	if t.IsVariadic() {
		fixed := t.NumIn() - 1
		in = make([]reflect.Value, 0, max(fixed, len(args)))
		for i := range fixed {
			in = append(in, argumentFor(args, i, t.In(i)))
		}
		elem := t.In(fixed).Elem()
		for i := fixed; i < len(args); i++ {
			in = append(in, argumentFor(args, i, elem))
		}
	} else {
		in = make([]reflect.Value, 0, t.NumIn())
		for i := range t.NumIn() {
			in = append(in, argumentFor(args, i, t.In(i)))
		}
	}

	out := v.Call(in)
	if len(out) == 0 {
		return nil
	}
	return out[0].Interface()
}

// argumentFor prepares one argument for the parameter it is about to fill.
func argumentFor(args []any, i int, want reflect.Type) reflect.Value {
	if i >= len(args) || args[i] == nil {
		return reflect.Zero(want)
	}
	v := reflect.ValueOf(args[i])
	if v.Type().AssignableTo(want) {
		return v
	}
	return reflect.Zero(want)
}

func isContext(v any) bool {
	_, ok := v.(context.Context)
	return ok
}

// errorOrNil keeps a nil error out of the response list, where it would read as
// a listener that answered.
func errorOrNil(err error) any {
	if err == nil {
		return nil
	}
	return err
}
