package fakes

import (
	"sync"
)

// Dispatcher is what an [EventFake] needs from the dispatcher it stands in
// for.
//
// The fake forwards to it whatever it does not intercept, and asks it for the
// listeners [EventFake.AssertListening] reads.
type Dispatcher interface {
	// Listen attaches a listener to one or more events.
	Listen(events any, listener any)
	// HasListeners reports whether the event has any listener attached.
	HasListeners(eventName string) bool
	// Subscribe registers a value that attaches its own listeners.
	Subscribe(subscriber any)
	// Dispatch sends the event to its listeners and returns what they
	// returned; halt stops at the first listener that answers.
	Dispatch(event any, payload []any, halt bool) []any
	// Until dispatches the event and stops at the first listener that
	// answers, returning that answer.
	Until(event any, payload []any) any
	// Push queues an event to be dispatched later, when it is flushed.
	Push(event string, payload []any)
	// Flush dispatches the events pushed under that name.
	Flush(event string)
	// Forget drops the listeners attached to the event.
	Forget(event string)
	// ForgetPushed drops every event queued for later.
	ForgetPushed()
	// GetListeners returns the listeners attached to the event.
	GetListeners(eventName string) []any
}

// dispatchedEvent is one recorded dispatch: the event and the payload it
// carried.
type dispatchedEvent struct {
	name    string
	event   any
	payload []any
}

// EventFake is the dispatcher a test installs so that no listener runs, and
// every dispatch can be asserted on afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while the
// lock is held.
//
// An event is recorded when it is dispatched, whatever it implements.
type EventFake struct {
	mu sync.Mutex
	// dispatcher is the real dispatcher the fake stands in for.
	dispatcher       Dispatcher
	eventsToFake     []any
	eventsToDispatch []any
	events           []dispatchedEvent
}

// NewEventFake builds a dispatcher that records the named events and forwards
// the rest.
//
// A nil dispatcher is the ordinary case: it is only reached for by an event
// that Except sent to the real thing, and by AssertListening.
func NewEventFake(dispatcher Dispatcher, eventsToFake ...any) *EventFake {
	return &EventFake{dispatcher: dispatcher, eventsToFake: eventsToFake}
}

func (f *EventFake) isFake() {}

// OriginalDispatcher returns the dispatcher the fake stands in for, which may
// be nil.
func (f *EventFake) OriginalDispatcher() Dispatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dispatcher
}

// Except names the events that should reach the real dispatcher instead of
// being recorded, and returns the fake.
func (f *EventFake) Except(events ...any) *EventFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventsToDispatch = append(f.eventsToDispatch, events...)
	return f
}

// AssertListening fails the test unless the event has that listener attached
// to it.
//
// The listener is named as a reflect.Type, a value, or the string the
// dispatcher filed it under. The question goes to the real dispatcher, because
// a fake records dispatches and never listeners, so a fake built without one
// fails the assertion and says so.
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

// AssertDispatched fails the test unless an event of the given type was
// dispatched and the truth test accepted it.
//
// The callback slot accepts nil (any event of the type), an int (an exact
// count, handled by [EventFake.AssertDispatchedTimes]), a
// func(event any) bool, or a func(event any, payload []any) bool. Any other
// form fails the test naming the ones that are accepted.
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

// AssertDispatchedTimes fails the test unless an event of the given type was
// dispatched exactly that many times.
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

// AssertNotDispatched fails the test when an event of the given type was
// dispatched and the truth test accepted it. The callback slot takes the same
// forms as [EventFake.AssertDispatched], minus the count.
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

// AssertNothingDispatched fails the test unless no event was dispatched at
// all.
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

// Dispatched returns the events of the given type that the truth test
// accepted, in the order they were dispatched. It returns the events
// themselves; to reach a payload, use the two-argument truth test.
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

// HasDispatched reports whether an event of the given type was dispatched at
// all.
func (f *EventFake) HasDispatched(event any) bool {
	return len(f.dispatchedRecords(event, nil)) > 0
}

// Listen forwards to the real dispatcher, because a listener registered during
// a faked test is still a listener. A fake with no dispatcher drops the call.
func (f *EventFake) Listen(events any, listener any) {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher != nil {
		dispatcher.Listen(events, listener)
	}
}

// GetListeners forwards to the real dispatcher, and returns nil when there is
// none.
func (f *EventFake) GetListeners(eventName string) []any {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher == nil {
		return nil
	}
	return dispatcher.GetListeners(eventName)
}

// HasListeners forwards to the real dispatcher, and returns false when there
// is none.
func (f *EventFake) HasListeners(eventName string) bool {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher == nil {
		return false
	}
	return dispatcher.HasListeners(eventName)
}

// Push does nothing: an event pushed for later is flushed by the real
// dispatcher, and a fake never flushes.
func (f *EventFake) Push(event string, payload []any) {}

// Subscribe forwards to the real dispatcher. A fake with no dispatcher drops
// the call.
func (f *EventFake) Subscribe(subscriber any) {
	f.mu.Lock()
	dispatcher := f.dispatcher
	f.mu.Unlock()
	if dispatcher != nil {
		dispatcher.Subscribe(subscriber)
	}
}

// Flush does nothing: a fake records dispatches and never queues them.
func (f *EventFake) Flush(event string) {}

// Dispatch records the event and returns nil, or hands it to the real
// dispatcher when [EventFake.Except] named it.
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

// shouldFakeEvent reports whether the event is recorded rather than forwarded.
//
// A fake built without a list of events records everything. A token is a type,
// a name, or a func(eventName string, payload []any) bool.
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

// Forget does nothing: a fake holds no listeners to drop.
func (f *EventFake) Forget(event string) {}

// ForgetPushed does nothing: a fake queues no event for later.
func (f *EventFake) ForgetPushed() {}

// Until records the dispatch and returns nil: stopping at the first listener
// that answers means nothing when no listener runs.
func (f *EventFake) Until(event any, payload []any) any {
	f.Dispatch(event, payload, true)
	return nil
}

// DispatchedEvents returns every event recorded, keyed by the name it was
// filed under.
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

// dispatchedRecords returns the records filed under that exact name and
// accepted by the truth test.
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

// eventName returns the name an event is filed under: a string event is its
// own name, and anything else is named by its type.
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
