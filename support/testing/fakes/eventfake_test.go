package fakes

import (
	"reflect"
	"sync"
	"testing"
)

type orderShipped struct{ Order string }

type orderPaid struct{ Order string }

// spyDispatcher records what the fake forwarded, which is the only way to see
// the Except path and the listeners AssertListening reads.
type spyDispatcher struct {
	mu          sync.Mutex
	dispatched  []any
	listeners   map[string][]any
	subscribers []any
	listened    []any
}

func newSpyDispatcher() *spyDispatcher {
	return &spyDispatcher{listeners: map[string][]any{}}
}

func (d *spyDispatcher) Listen(events any, listener any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listened = append(d.listened, listener)
	if name, ok := events.(string); ok {
		d.listeners[name] = append(d.listeners[name], listener)
	}
}

func (d *spyDispatcher) HasListeners(eventName string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.listeners[eventName]) > 0
}

func (d *spyDispatcher) Subscribe(subscriber any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribers = append(d.subscribers, subscriber)
}

func (d *spyDispatcher) Dispatch(event any, payload []any, halt bool) []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched = append(d.dispatched, event)
	return []any{"handled"}
}

func (d *spyDispatcher) Until(event any, payload []any) any {
	return d.Dispatch(event, payload, true)
}

func (d *spyDispatcher) Push(event string, payload []any) {}

func (d *spyDispatcher) Flush(event string) {}

func (d *spyDispatcher) Forget(event string) {}

func (d *spyDispatcher) ForgetPushed() {}

func (d *spyDispatcher) GetListeners(eventName string) []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]any(nil), d.listeners[eventName]...)
}

type sendShipmentNotice struct{}

type writeAuditLog struct{}

func TestEventFakeAssertDispatched(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	events.Dispatch(orderShipped{Order: "1"}, nil, false)

	r := &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderShipped](), nil)
	assertPasses(t, r)

	r = &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderPaid](), nil)
	assertFails(t, r, "orderPaid", "not dispatched", "1 event was dispatched", "fakes.orderShipped")
}

func TestEventFakeAssertDispatchedWithATruthTest(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	events.Dispatch(orderShipped{Order: "1"}, nil, false)

	r := &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderShipped](), func(event any) bool {
		return event.(orderShipped).Order == "1"
	})
	assertPasses(t, r)

	r = &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderShipped](), func(event any) bool {
		return event.(orderShipped).Order == "2"
	})
	assertFails(t, r, "not dispatched")
}

func TestEventFakeStringEventsCarryTheirPayload(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	events.Dispatch("order.shipped", []any{"1"}, false)

	r := &recorder{}
	events.AssertDispatched(r, "order.shipped", nil)
	assertPasses(t, r)

	r = &recorder{}
	events.AssertDispatched(r, "order.shipped", func(event any, payload []any) bool {
		return len(payload) == 1 && payload[0] == "1"
	})
	assertPasses(t, r)

	r = &recorder{}
	events.AssertDispatched(r, "order.paid", nil)
	assertFails(t, r, "order.paid", "order.shipped", "1 payload argument")
}

func TestEventFakeAssertDispatchedTimes(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	events.Dispatch(orderShipped{}, nil, false)
	events.Dispatch(orderShipped{}, nil, false)

	r := &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderShipped](), 2)
	assertPasses(t, r)

	r = &recorder{}
	events.AssertDispatchedTimes(r, reflect.TypeFor[orderShipped](), 1)
	assertFails(t, r, "dispatched 2 times instead of 1 time")
}

func TestEventFakeAssertNotDispatchedAndNothingDispatched(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)

	r := &recorder{}
	events.AssertNothingDispatched(r)
	assertPasses(t, r)

	events.Dispatch(orderShipped{}, nil, false)
	events.Dispatch(orderShipped{}, nil, false)
	events.Dispatch(orderPaid{}, nil, false)

	r = &recorder{}
	events.AssertNotDispatched(r, reflect.TypeFor[orderShipped](), nil)
	assertFails(t, r, "unexpected [fakes.orderShipped] event was dispatched 2 times")

	// The count per class is what tells a noisy test from a wrong one.
	r = &recorder{}
	events.AssertNothingDispatched(r)
	assertFails(t, r, "3 unexpected events dispatched", "fakes.orderShipped dispatched 2 times", "fakes.orderPaid dispatched 1 time")
}

func TestEventFakeDispatchedAndHasDispatched(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	events.Dispatch(orderShipped{Order: "1"}, nil, false)

	if !events.HasDispatched(reflect.TypeFor[orderShipped]()) {
		t.Error("HasDispatched should answer for an event that was dispatched")
	}
	if events.HasDispatched(reflect.TypeFor[orderPaid]()) {
		t.Error("HasDispatched should not answer for an event that was not")
	}

	dispatched := events.Dispatched(reflect.TypeFor[orderShipped](), nil)
	if len(dispatched) != 1 || dispatched[0].(orderShipped).Order != "1" {
		t.Errorf("Dispatched = %v, want the one event", dispatched)
	}
	// An event nobody dispatched yields nothing rather than a nil map read.
	if got := events.Dispatched(reflect.TypeFor[orderPaid](), nil); len(got) != 0 {
		t.Errorf("Dispatched for an event never dispatched = %v, want nothing", got)
	}

	byName := events.DispatchedEvents()
	if got := len(byName["fakes.orderShipped"]); got != 1 {
		t.Errorf("DispatchedEvents[fakes.orderShipped] = %d, want 1", got)
	}
}

func TestEventFakeExceptForwardsToTheRealDispatcher(t *testing.T) {
	t.Parallel()

	spy := newSpyDispatcher()
	events := NewEventFake(spy).Except(reflect.TypeFor[orderPaid]())

	events.Dispatch(orderShipped{}, nil, false)
	answer := events.Dispatch(orderPaid{}, nil, false)

	if len(spy.dispatched) != 1 {
		t.Fatalf("the real dispatcher got %d events, want 1", len(spy.dispatched))
	}
	if _, ok := spy.dispatched[0].(orderPaid); !ok {
		t.Errorf("the real dispatcher got %T, want fakes.orderPaid", spy.dispatched[0])
	}
	// What the real dispatcher answered comes back to the caller.
	if len(answer) != 1 || answer[0] != "handled" {
		t.Errorf("Dispatch answered %v, want what the real dispatcher answered", answer)
	}
	// The faked event answers nothing, because no listener ran.
	if got := events.Dispatch(orderShipped{}, nil, false); got != nil {
		t.Errorf("a faked dispatch answered %v, want nothing", got)
	}
}

func TestEventFakeOnlyFakesTheEventsItWasNamed(t *testing.T) {
	t.Parallel()

	spy := newSpyDispatcher()
	events := NewEventFake(spy, reflect.TypeFor[orderShipped]())

	events.Dispatch(orderShipped{}, nil, false)
	events.Dispatch(orderPaid{}, nil, false)

	if !events.HasDispatched(reflect.TypeFor[orderShipped]()) {
		t.Error("the named event should have been recorded")
	}
	if events.HasDispatched(reflect.TypeFor[orderPaid]()) {
		t.Error("the event that was not named should have gone to the real dispatcher")
	}
}

func TestEventFakeAssertListening(t *testing.T) {
	t.Parallel()

	spy := newSpyDispatcher()
	spy.Listen("fakes.orderShipped", sendShipmentNotice{})
	events := NewEventFake(spy)

	r := &recorder{}
	events.AssertListening(r, reflect.TypeFor[orderShipped](), reflect.TypeFor[sendShipmentNotice]())
	assertPasses(t, r)

	r = &recorder{}
	events.AssertListening(r, reflect.TypeFor[orderShipped](), reflect.TypeFor[writeAuditLog]())
	assertFails(t, r, "does not have the [fakes.writeAuditLog] listener", "1 listener is attached", "fakes.sendShipmentNotice")

	// An event with no listener at all still gets a message that says so.
	r = &recorder{}
	events.AssertListening(r, reflect.TypeFor[orderPaid](), reflect.TypeFor[writeAuditLog]())
	assertFails(t, r, "0 listeners are attached")

	// A fake with no dispatcher cannot answer, and says why rather than
	// failing with an empty list that looks like an answer.
	r = &recorder{}
	NewEventFake(nil).AssertListening(r, reflect.TypeFor[orderShipped](), reflect.TypeFor[writeAuditLog]())
	assertFails(t, r, "built without a dispatcher")
}

func TestEventFakeForwardsWhatItDoesNotRecord(t *testing.T) {
	t.Parallel()

	spy := newSpyDispatcher()
	events := NewEventFake(spy)

	events.Listen("fakes.orderShipped", sendShipmentNotice{})
	if !events.HasListeners("fakes.orderShipped") {
		t.Error("Listen should reach the real dispatcher")
	}

	events.Subscribe("a subscriber")
	if len(spy.subscribers) != 1 {
		t.Error("Subscribe should reach the real dispatcher")
	}

	// These four do nothing on purpose, as the PHP does; the test is that they
	// do not reach the dispatcher and do not panic.
	events.Push("fakes.orderShipped", nil)
	events.Flush("fakes.orderShipped")
	events.Forget("fakes.orderShipped")
	events.ForgetPushed()
	if len(spy.dispatched) != 0 {
		t.Errorf("the real dispatcher saw %d events, want none", len(spy.dispatched))
	}
}

func TestEventFakeUntilIsADispatch(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	if got := events.Until(orderShipped{}, nil); got != nil {
		t.Errorf("Until answered %v, want nothing", got)
	}

	r := &recorder{}
	events.AssertDispatched(r, reflect.TypeFor[orderShipped](), nil)
	assertPasses(t, r)
}

func TestEventFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	events := NewEventFake(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events.Dispatch(orderShipped{}, nil, false)
			events.HasDispatched(reflect.TypeFor[orderShipped]())
		}()
	}
	wg.Wait()

	r := &recorder{}
	events.AssertDispatchedTimes(r, reflect.TypeFor[orderShipped](), 50)
	assertPasses(t, r)
}
