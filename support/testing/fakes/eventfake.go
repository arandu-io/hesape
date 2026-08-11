package fakes

import (
	"sync"
)

// Dispatcher is the little of Illuminate\Contracts\Events\Dispatcher that an
// EventFake needs from the dispatcher it stands in for.
//
// The fake forwards to it whatever it does not intercept, and asks it for the
// listeners AssertListening reads.
type Dispatcher interface {
	// Listen answers Dispatcher::listen.
	Listen(events any, listener any)
	// HasListeners answers Dispatcher::hasListeners.
	HasListeners(eventName string) bool
	// Subscribe answers Dispatcher::subscribe.
	Subscribe(subscriber any)
	// Dispatch answers Dispatcher::dispatch.
	Dispatch(event any, payload []any, halt bool) []any
	// Until answers Dispatcher::until.
	Until(event any, payload []any) any
	// Push answers Dispatcher::push.
	Push(event string, payload []any)
	// Flush answers Dispatcher::flush.
	Flush(event string)
	// Forget answers Dispatcher::forget.
	Forget(event string)
	// ForgetPushed answers Dispatcher::forgetPushed.
	ForgetPushed()
	// GetListeners answers Dispatcher::getListeners, which is what
	// assertListening reads to find the listener it was asked about.
	GetListeners(eventName string) []any
}

// dispatchedEvent is one recorded dispatch: the event and the payload it
// carried.
//
// PHP records func_get_args(), which is [$event, $payload, $halt]; the halt
// flag is dropped because no assertion in the class reads it, and a recorded
// value nothing reads is a value that goes stale without anyone noticing.
type dispatchedEvent struct {
	name    string
	event   any
	payload []any
}

// EventFake answers Illuminate\Support\Testing\Fakes\EventFake: the dispatcher
// a test installs so that no listener runs, and every dispatch can be asserted
// on afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while the
// lock is held.
//
// One border of the PHP does not survive: an event that implements
// ShouldDispatchAfterCommit is held there until the database transaction
// commits, by asking the container for 'db.transactions'. The container is
// rejected (ADR 0001), so an event is recorded when it is dispatched, whatever
// it implements.
type EventFake struct {
	mu sync.Mutex
	// dispatcher answers EventFake::$dispatcher, public in PHP.
	dispatcher       Dispatcher
	eventsToFake     []any
	eventsToDispatch []any
	events           []dispatchedEvent
}

// NewEventFake answers EventFake::__construct.
//
// A nil dispatcher is the ordinary case: it is only reached for by an event
// that Except sent to the real thing, and by AssertListening.
func NewEventFake(dispatcher Dispatcher, eventsToFake ...any) *EventFake {
	return &EventFake{dispatcher: dispatcher, eventsToFake: eventsToFake}
}

func (f *EventFake) isFake() {}

// OriginalDispatcher answers EventFake::$dispatcher, the dispatcher the fake
// stands in for.
//
// PHP reads the public property; Dispatcher is taken by the interface here,
// and a method that says which dispatcher it is reads better than one that
// does not.
func (f *EventFake) OriginalDispatcher() Dispatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dispatcher
}

// Except answers EventFake::except: the events that should reach the real
// dispatcher instead of being recorded.
func (f *EventFake) Except(events ...any) *EventFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventsToDispatch = append(f.eventsToDispatch, events...)
	return f
}

// AssertListening answers EventFake::assertListening: the event has that
// listener attached to it.
//
// The listener is named the way Go names one -- a reflect.Type, a value, or the
// string the dispatcher filed it under -- and it is asked of the real
// dispatcher, because a fake records dispatches and never listeners.
func (f *EventFake) AssertListening(t TestingT, expectedEvent any, expectedListener any) {
	t.Helper()

	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()

	name := eventName(expectedEvent)
	if dispatcher == nil {
		t.Errorf(
			"AssertListening: the event [%s] could not be asked for its listeners, because the fake was built without a dispatcher to ask.",
			name,
		)
		return
	}

	listeners := dispatcher.GetListeners(name)
	attached := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		if instanceOf(listener, expectedListener) || tokenName(listener) == tokenName(expectedListener) {
			return
		}
		attached = append(attached, tokenName(listener))
	}

	t.Errorf(
		"AssertListening: the event [%s] does not have the [%s] listener attached to it. %s attached:%s",
		name, tokenName(expectedListener), countedAre(len(attached), "listener"), listOf(attached),
	)
}

// AssertDispatched answers EventFake::assertDispatched: it fails unless an
// event of the given type was dispatched and the truth test accepted it.
//
// The event is named the way Go names one, with reflect.TypeFor[OrderShipped]()
// where the PHP writes OrderShipped::class; a value of the type works too, and
// so does the plain string an event dispatched by name was filed under.
//
// The callback slot is whatever the PHP accepts there: nil for no truth test,
// an int to assert a count, a func(event any) bool, or a
// func(event any, payload []any) bool for an event dispatched by name, whose
// data travels in the payload rather than in the event.
func (f *EventFake) AssertDispatched(t TestingT, event any, callback any) {
	t.Helper()

	if times, ok := callback.(int); ok {
		f.AssertDispatchedTimes(t, event, times)
		return
	}

	test, ok := eventTest(t, "AssertDispatched", callback)
	if !ok {
		return
	}

	if len(f.dispatchedRecords(event, test)) > 0 {
		return
	}

	all := f.snapshot()
	t.Errorf(
		"AssertDispatched: the expected [%s] event was not dispatched. %s dispatched:%s",
		eventName(event), countedWere(len(all), "event"), listOf(describeEvents(all)),
	)
}

// AssertDispatchedTimes answers EventFake::assertDispatchedTimes.
func (f *EventFake) AssertDispatchedTimes(t TestingT, event any, times int) {
	t.Helper()

	found := f.dispatchedRecords(event, nil)
	if len(found) == times {
		return
	}

	all := f.snapshot()
	t.Errorf(
		"AssertDispatchedTimes: the expected [%s] event was dispatched %d %s instead of %d %s. %s dispatched:%s",
		eventName(event), len(found), plural("time", len(found)), times, plural("time", times),
		countedWere(len(all), "event"), listOf(describeEvents(all)),
	)
}

// AssertNotDispatched answers EventFake::assertNotDispatched. The callback slot
// takes the same forms as AssertDispatched, minus the count.
func (f *EventFake) AssertNotDispatched(t TestingT, event any, callback any) {
	t.Helper()

	test, ok := eventTest(t, "AssertNotDispatched", callback)
	if !ok {
		return
	}

	found := f.dispatchedRecords(event, test)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotDispatched: the unexpected [%s] event was dispatched %d %s:%s",
		eventName(event), len(found), plural("time", len(found)), listOf(describeEvents(found)),
	)
}

// AssertNothingDispatched answers EventFake::assertNothingDispatched.
func (f *EventFake) AssertNothingDispatched(t TestingT) {
	t.Helper()

	all := f.snapshot()
	if len(all) == 0 {
		return
	}

	counts := make(map[string]int, len(all))
	var order []string
	for _, record := range all {
		if counts[record.name] == 0 {
			order = append(order, record.name)
		}
		counts[record.name]++
	}
	lines := make([]string, 0, len(order))
	for _, name := range order {
		lines = append(lines, name+" dispatched "+countedAs(counts[name], "time"))
	}

	t.Errorf(
		"AssertNothingDispatched: %d unexpected %s dispatched:%s",
		len(all), plural("event", len(all)), listOf(lines),
	)
}

// Dispatched answers EventFake::dispatched: the events of the given type that
// the truth test accepted, in the order they were dispatched.
//
// PHP hands back the argument arrays func_get_args() made -- [$event, $payload,
// $halt] -- and every caller reads element zero or counts them. Here it is the
// events themselves; the payload is what the two-argument truth test is for.
func (f *EventFake) Dispatched(event any, callback any) []any {
	test, ok := eventTest(nil, "Dispatched", callback)
	if !ok {
		return nil
	}
	records := f.dispatchedRecords(event, test)
	events := make([]any, 0, len(records))
	for _, record := range records {
		events = append(events, record.event)
	}
	return events
}

// HasDispatched answers EventFake::hasDispatched.
func (f *EventFake) HasDispatched(event any) bool {
	return len(f.dispatchedRecords(event, nil)) > 0
}

// Listen answers EventFake::listen: forwarded to the real dispatcher, because
// a listener registered during a faked test is still a listener.
func (f *EventFake) Listen(events any, listener any) {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher != nil {
		dispatcher.Listen(events, listener)
	}
}

// GetListeners answers Dispatcher::getListeners, forwarded.
//
// EventFake declares no such method: the PHP reaches it through ForwardsCalls,
// which sends anything the fake does not define to the real dispatcher. Go has
// no __call, so the one method the contract needs and the fake does not record
// is written out.
func (f *EventFake) GetListeners(eventName string) []any {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher == nil {
		return nil
	}
	return dispatcher.GetListeners(eventName)
}

// HasListeners answers EventFake::hasListeners, forwarded.
func (f *EventFake) HasListeners(eventName string) bool {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher == nil {
		return false
	}
	return dispatcher.HasListeners(eventName)
}

// Push answers EventFake::push, and does nothing, as the PHP does: an event
// pushed for later is flushed by the real dispatcher, and there is none here.
func (f *EventFake) Push(event string, payload []any) {}

// Subscribe answers EventFake::subscribe, forwarded.
func (f *EventFake) Subscribe(subscriber any) {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher != nil {
		dispatcher.Subscribe(subscriber)
	}
}

// Flush answers EventFake::flush, and does nothing, as the PHP does.
func (f *EventFake) Flush(event string) {}

// Dispatch answers EventFake::dispatch: it records the event, or hands it to
// the real dispatcher when Except named it.
func (f *EventFake) Dispatch(event any, payload []any, halt bool) []any {
	name := eventName(event)

	if !f.shouldFakeEvent(name, payload) {
		f.mu.Lock()
		dispatcher := f.dispatcher
		f.mu.Unlock()
		if dispatcher == nil {
			return nil
		}
		return dispatcher.Dispatch(event, payload, halt)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, dispatchedEvent{
		name:    name,
		event:   event,
		payload: append([]any(nil), payload...),
	})
	return nil
}

// shouldFakeEvent answers EventFake::shouldFakeEvent.
//
// A fake built without a list of events to fake fakes everything, which is what
// an empty $eventsToFake means; Except wins over it, as shouldDispatchEvent
// does. A token may be a class, a name, or a
// func(eventName string, payload []any) bool, which is the Closure form.
func (f *EventFake) shouldFakeEvent(name string, payload []any) bool {
	f.mu.Lock()
	toFake := append([]any(nil), f.eventsToFake...)
	toDispatch := append([]any(nil), f.eventsToDispatch...)
	f.mu.Unlock()

	if matchesEventToken(toDispatch, name, payload) {
		return false
	}
	if len(toFake) == 0 {
		return true
	}
	return matchesEventToken(toFake, name, payload)
}

func matchesEventToken(tokens []any, name string, payload []any) bool {
	for _, token := range tokens {
		if test, ok := token.(func(eventName string, payload []any) bool); ok {
			if test != nil && test(name, payload) {
				return true
			}
			continue
		}
		if eventName(token) == name {
			return true
		}
	}
	return false
}

// Forget answers EventFake::forget, and does nothing, as the PHP does.
func (f *EventFake) Forget(event string) {}

// ForgetPushed answers EventFake::forgetPushed, and does nothing, as the PHP
// does.
func (f *EventFake) ForgetPushed() {}

// Until answers EventFake::until: a dispatch that stops at the first listener
// that answers, which for a fake is a dispatch that is recorded.
func (f *EventFake) Until(event any, payload []any) any {
	f.Dispatch(event, payload, true)
	return nil
}

// DispatchedEvents answers EventFake::dispatchedEvents: every event, keyed by
// the name it was filed under, which is how the PHP stores them.
func (f *EventFake) DispatchedEvents() map[string][]any {
	all := f.snapshot()
	events := make(map[string][]any, len(all))
	for _, record := range all {
		events[record.name] = append(events[record.name], record.event)
	}
	return events
}

// snapshot copies the ledger under the lock, so a truth test runs outside it.
func (f *EventFake) snapshot() []dispatchedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dispatchedEvent(nil), f.events...)
}

// dispatchedRecords answers the array lookup PHP does with $this->events[$event]:
// the records filed under that exact name, then the truth test.
func (f *EventFake) dispatchedRecords(event any, test func(dispatchedEvent) bool) []dispatchedEvent {
	name := eventName(event)
	var found []dispatchedEvent
	for _, record := range f.snapshot() {
		if record.name != name {
			continue
		}
		if !callFn(test, record) {
			continue
		}
		found = append(found, record)
	}
	return found
}

// eventName answers `is_object($event) ? get_class($event) : (string) $event`:
// the name an event is filed under.
func eventName(event any) string {
	switch e := event.(type) {
	case nil:
		return "<nil>"
	case string:
		return e
	default:
		return tokenName(event)
	}
}

// eventTest normalizes the truth test forms EventFake accepts. It reports the
// failure itself when handed a form it does not know, and answers false so the
// caller stops.
func eventTest(t TestingT, name string, callback any) (func(dispatchedEvent) bool, bool) {
	switch cb := callback.(type) {
	case nil:
		return nil, true
	case func(event any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record dispatchedEvent) bool { return cb(record.event) }, true
	case func(event any, payload []any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record dispatchedEvent) bool { return cb(record.event, record.payload) }, true
	default:
		if t != nil {
			t.Helper()
			t.Errorf("%s: the callback must be nil, a func(event any) bool, a func(event any, payload []any) bool or an int; got %T.", name, callback)
		}
		return nil, false
	}
}

// describeEvents renders the records a failure message ends with.
func describeEvents(records []dispatchedEvent) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		line := "[" + record.name + "]"
		if len(record.payload) > 0 {
			line += " with " + countedAs(len(record.payload), "payload argument")
		}
		lines = append(lines, line)
	}
	return lines
}
