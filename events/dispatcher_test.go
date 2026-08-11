package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/events"
)

// collect returns a listener that appends the name of every event it is handed.
func collect(into *[]string) func(context.Context, events.Stored) error {
	return func(_ context.Context, e events.Stored) error {
		*into = append(*into, e.Name)
		return nil
	}
}

// TestListenDeliversOnlyTheEventItAnswers: a listener answers a name, and one
// that has to switch on the name is the place every event in the system passes
// through.
func TestListenDeliversOnlyTheEventItAnswers(t *testing.T) {
	var got []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", collect(&got))

	for _, name := range []string{"invoice.paid", "customer.created", "invoice.paid"} {
		if err := d.Publish(context.Background(), events.Stored{Name: name}); err != nil {
			t.Fatalf("Publish(%s): %v", name, err)
		}
	}

	if len(got) != 2 || got[0] != "invoice.paid" || got[1] != "invoice.paid" {
		t.Fatalf("received %v, want the two invoice.paid and nothing else", got)
	}
}

// TestAnEventNobodyAnsweredIsDelivered: it was delivered, to somebody who had
// nothing to do with it. Returning an error would park an event that is fine.
func TestAnEventNobodyAnsweredIsDelivered(t *testing.T) {
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func(context.Context, events.Stored) error { return nil })

	if err := d.Publish(context.Background(), events.Stored{Name: "customer.created"}); err != nil {
		t.Fatalf("an event with another name was reported undelivered: %v", err)
	}
}

// TestListenersFanOutInOrder: two listeners on one name both run, in the order
// they were registered.
func TestListenersFanOutInOrder(t *testing.T) {
	var order []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() { order = append(order, "first") })
	d.Listen("invoice.paid", func() { order = append(order, "second") })

	d.Dispatch("invoice.paid")

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v, want first then second", order)
	}
}

// TestPublishReportsTheFailureToTheRelay: swallowing the error would report a
// delivery that did not happen, and the event would be marked published and
// never retried. The listeners behind the failing one still run, because
// Dispatch does not stop for a response -- and the retry runs all of them
// again, which is why a listener is idempotent on Stored.ID.
func TestPublishReportsTheFailureToTheRelay(t *testing.T) {
	refused := errors.New("the broker refused the message")
	var after []string

	d := events.NewDispatcher()
	d.Listen("invoice.paid", func(context.Context, events.Stored) error { return refused })
	d.Listen("invoice.paid", collect(&after))

	err := d.Publish(context.Background(), events.Stored{Name: "invoice.paid"})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the listener's error to reach the relay", err)
	}
	if len(after) != 1 {
		t.Fatalf("after = %v, want the second listener to have run as well", after)
	}
}

// TestADispatcherWithNothingWiredIsNotAFailure: an application that stores
// events before it has anywhere to send them is the state this package calls
// useful.
func TestADispatcherWithNothingWiredIsNotAFailure(t *testing.T) {
	d := events.NewDispatcher()
	if err := d.Publish(context.Background(), events.Stored{Name: "invoice.paid"}); err != nil {
		t.Fatalf("Publish with no listeners: %v", err)
	}
}

// invoicePaid is an event dispatched as a value rather than as a name.
type invoicePaid struct{ Number string }

func TestAnEventValueNamesItself(t *testing.T) {
	var got string
	d := events.NewDispatcher()
	d.Listen(func(e invoicePaid) { got = e.Number })

	d.Dispatch(invoicePaid{Number: "2026-01"})

	if got != "2026-01" {
		t.Fatalf("got %q, want the closure to be registered against its parameter type", got)
	}
}

func TestHasListeners(t *testing.T) {
	d := events.NewDispatcher()
	if d.HasListeners("invoice.paid") {
		t.Fatal("an empty dispatcher reported a listener")
	}

	d.Listen("invoice.paid", func() {})
	if !d.HasListeners("invoice.paid") {
		t.Fatal("HasListeners did not see the listener that was just registered")
	}
	if d.HasListeners("invoice.voided") {
		t.Fatal("HasListeners answered for an event nobody registered")
	}
}

func TestWildcardListenersAreToldWhichEventArrived(t *testing.T) {
	var seen []string
	d := events.NewDispatcher()
	d.Listen("invoice.*", func(name string, _ []any) any {
		seen = append(seen, name)
		return nil
	})

	if !d.HasWildcardListeners("invoice.paid") {
		t.Fatal("HasWildcardListeners did not match the pattern")
	}
	if d.HasWildcardListeners("customer.created") {
		t.Fatal("the pattern matched an event outside it")
	}

	d.Dispatch("invoice.paid")
	d.Dispatch("invoice.voided")
	d.Dispatch("customer.created")

	if len(seen) != 2 || seen[0] != "invoice.paid" || seen[1] != "invoice.voided" {
		t.Fatalf("seen = %v, want the two invoice events with their names", seen)
	}
}

func TestUntilStopsAtTheFirstAnswer(t *testing.T) {
	var ran []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() any { ran = append(ran, "first"); return nil })
	d.Listen("invoice.paid", func() any { ran = append(ran, "second"); return "answered" })
	d.Listen("invoice.paid", func() any { ran = append(ran, "third"); return "late" })

	if got := d.Until("invoice.paid"); got != "answered" {
		t.Fatalf("Until returned %v, want the first non-nil response", got)
	}
	if len(ran) != 2 {
		t.Fatalf("ran = %v, want the listener after the answer to be skipped", ran)
	}
}

// TestFalseStopsThePropagation is the PHP's `if ($response === false) break`,
// and it is not the same thing as halting: the responses already collected are
// still returned.
func TestFalseStopsThePropagation(t *testing.T) {
	var ran int
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() any { ran++; return "one" })
	d.Listen("invoice.paid", func() any { ran++; return false })
	d.Listen("invoice.paid", func() any { ran++; return "three" })

	responses := d.Dispatch("invoice.paid")

	if ran != 2 {
		t.Fatalf("ran = %d, want the listener after the false to be skipped", ran)
	}
	if len(responses) != 1 || responses[0] != "one" {
		t.Fatalf("responses = %v, want only what ran before the false", responses)
	}
}

func TestPushAndFlush(t *testing.T) {
	var got []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func(number string) { got = append(got, number) })

	d.Push("invoice.paid", "2026-01")
	if len(got) != 0 {
		t.Fatalf("Push fired the event straight away: %v", got)
	}

	d.Flush("invoice.paid")
	if len(got) != 1 || got[0] != "2026-01" {
		t.Fatalf("got = %v, want the pushed payload once Flush ran", got)
	}
}

func TestForgetPushed(t *testing.T) {
	var got []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func(number string) { got = append(got, number) })

	d.Push("invoice.paid", "2026-01")
	d.ForgetPushed()
	d.Flush("invoice.paid")

	if len(got) != 0 {
		t.Fatalf("got = %v, want the pushed event to have been forgotten", got)
	}
	if !d.HasListeners("invoice.paid") {
		t.Fatal("ForgetPushed removed a listener that was not pushed")
	}
}

func TestForget(t *testing.T) {
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() {})
	d.Listen("invoice.*", func(string, []any) any { return nil })

	d.Forget("invoice.paid")
	if len(d.GetListeners("invoice.paid")) != 1 {
		t.Fatal("Forget removed the wildcard listener as well as the direct one")
	}

	d.Forget("invoice.*")
	if len(d.GetListeners("invoice.paid")) != 0 {
		t.Fatal("Forget did not remove the pattern")
	}
}

// TestForgettingAPatternInvalidatesTheCache: the prepared wildcard listeners
// are cached by event name, and a stale cache would keep firing a listener that
// was forgotten.
func TestForgettingAPatternInvalidatesTheCache(t *testing.T) {
	var ran int
	d := events.NewDispatcher()
	d.Listen("invoice.*", func(string, []any) any { ran++; return nil })

	d.Dispatch("invoice.paid")
	d.Forget("invoice.*")
	d.Dispatch("invoice.paid")

	if ran != 1 {
		t.Fatalf("ran = %d, want the forgotten pattern not to fire again", ran)
	}
}

func TestGetRawListeners(t *testing.T) {
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() {})

	raw := d.GetRawListeners()
	if len(raw["invoice.paid"]) != 1 {
		t.Fatalf("raw = %v, want the one registered listener", raw)
	}

	delete(raw, "invoice.paid")
	if !d.HasListeners("invoice.paid") {
		t.Fatal("the returned map is the dispatcher's own, so a caller can empty it")
	}
}

// notifier is a class listener: an object whose Handle answers the event.
type notifier struct{ seen []string }

func (n *notifier) Handle(e invoicePaid) { n.seen = append(n.seen, e.Number) }

func (n *notifier) WhenPaid(e invoicePaid) { n.seen = append(n.seen, "when:"+e.Number) }

func TestAClassListenerAnswersThroughHandle(t *testing.T) {
	n := &notifier{}
	d := events.NewDispatcher()
	d.Listen("invoice.paid", n)

	d.Dispatch("invoice.paid", invoicePaid{Number: "2026-01"})

	if len(n.seen) != 1 || n.seen[0] != "2026-01" {
		t.Fatalf("seen = %v, want Handle to have been called", n.seen)
	}
}

func TestAClassListenerCanNameItsMethod(t *testing.T) {
	n := &notifier{}
	d := events.NewDispatcher()
	d.Listen("invoice.paid", events.ClassListener{Class: n, Method: "WhenPaid"})

	d.Dispatch("invoice.paid", invoicePaid{Number: "2026-01"})

	if len(n.seen) != 1 || n.seen[0] != "when:2026-01" {
		t.Fatalf("seen = %v, want WhenPaid to have been called", n.seen)
	}
}

// subscriber registers several listeners at once, which is what a subscriber is
// for.
type subscriber struct{ seen []string }

func (s *subscriber) WhenPaid()    { s.seen = append(s.seen, "paid") }
func (s *subscriber) WhenVoided()  { s.seen = append(s.seen, "voided") }
func (s *subscriber) WhenCreated() { s.seen = append(s.seen, "created") }

func (s *subscriber) Subscribe(*events.Dispatcher) map[string][]any {
	return map[string][]any{
		"invoice.paid":     {"WhenPaid"},
		"invoice.voided":   {"WhenVoided"},
		"customer.created": {func() { s.seen = append(s.seen, "created") }},
	}
}

func TestSubscribe(t *testing.T) {
	s := &subscriber{}
	d := events.NewDispatcher()
	d.Subscribe(s)

	d.Dispatch("invoice.paid")
	d.Dispatch("invoice.voided")
	d.Dispatch("customer.created")

	if len(s.seen) != 3 {
		t.Fatalf("seen = %v, want all three registrations to have fired", s.seen)
	}
}

func TestDeferHoldsEventsUntilTheCallbackReturns(t *testing.T) {
	var order []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() { order = append(order, "listener") })

	result := d.Defer(func() any {
		d.Dispatch("invoice.paid")
		order = append(order, "callback")
		return "done"
	})

	if result != "done" {
		t.Fatalf("Defer returned %v, want the callback's result", result)
	}
	if len(order) != 2 || order[0] != "callback" || order[1] != "listener" {
		t.Fatalf("order = %v, want the listener to run after the callback", order)
	}
}

func TestDeferOnlyHoldsTheEventsItNames(t *testing.T) {
	var order []string
	d := events.NewDispatcher()
	d.Listen("invoice.paid", func() { order = append(order, "paid") })
	d.Listen("customer.created", func() { order = append(order, "created") })

	d.Defer(func() any {
		d.Dispatch("customer.created")
		d.Dispatch("invoice.paid")
		return nil
	}, "invoice.paid")

	if len(order) != 2 || order[0] != "created" || order[1] != "paid" {
		t.Fatalf("order = %v, want only invoice.paid held back", order)
	}
}

// queuedListener wants the queue rather than the caller's goroutine.
type queuedListener struct{ ran bool }

func (q *queuedListener) ShouldQueue(any) bool { return true }
func (q *queuedListener) Handle()              { q.ran = true }

// fakeQueue records what was pushed, which is all the dispatcher's contract
// with a queue amounts to.
type fakeQueue struct{ jobs []*events.CallQueuedListener }

func (f *fakeQueue) Connection(string) events.Connection { return f }

func (f *fakeQueue) PushOn(_ string, job any) error {
	f.jobs = append(f.jobs, job.(*events.CallQueuedListener))
	return nil
}

func (f *fakeQueue) LaterOn(_ string, _ time.Duration, job any) error {
	f.jobs = append(f.jobs, job.(*events.CallQueuedListener))
	return nil
}

func TestAQueuedListenerIsPushedRatherThanCalled(t *testing.T) {
	queue := &fakeQueue{}
	listener := &queuedListener{}

	d := events.NewDispatcher().SetQueueResolver(func() events.Queue { return queue })
	d.Listen("invoice.paid", listener)

	d.Dispatch("invoice.paid", invoicePaid{Number: "2026-01"})

	if listener.ran {
		t.Fatal("the listener ran inline instead of going to the queue")
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("%d jobs pushed, want one", len(queue.jobs))
	}
	if got := queue.jobs[0].Method; got != "Handle" {
		t.Fatalf("job method = %q, want Handle", got)
	}

	// The worker is what runs it, and running it is calling Handle.
	queue.jobs[0].Handle()
	if !listener.ran {
		t.Fatal("the job did not reach the listener")
	}
}

func TestQueueableClosureCarriesItsQueueOntoTheJob(t *testing.T) {
	queue := &fakeQueue{}
	d := events.NewDispatcher().SetQueueResolver(func() events.Queue { return queue })

	d.Listen("invoice.paid", events.Queueable(func(number string) {}).
		OnConnection("redis").
		OnQueue("billing").
		Delay(30*time.Second))

	d.Dispatch("invoice.paid", "2026-01")

	if len(queue.jobs) != 1 {
		t.Fatalf("%d jobs pushed, want one", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.Connection != "redis" || job.Queue != "billing" || job.Delay != 30*time.Second {
		t.Fatalf("job = %+v, want the closure's connection, queue and delay", job)
	}
}

func TestInvokeQueuedClosureRunsTheClosure(t *testing.T) {
	var got string
	job := events.NewCallQueuedListener(events.InvokeQueuedClosure{}, "Handle", []any{
		func(number string) { got = number },
		[]any{"2026-01"},
		[]any(nil),
	})

	job.Handle()

	if got != "2026-01" {
		t.Fatalf("got %q, want the closure to have run with its arguments", got)
	}
}

func TestInvokeQueuedClosureCallsTheCatchCallbacks(t *testing.T) {
	failure := errors.New("the queue gave up")
	var caught error

	events.InvokeQueuedClosure{}.Failed(nil, []any{"2026-01"}, []any{
		func(_ string, err error) { caught = err },
	}, failure)

	if !errors.Is(caught, failure) {
		t.Fatalf("caught = %v, want the failure to reach the catch callback", caught)
	}
}

func TestNullDispatcherRegistersAndDoesNotFire(t *testing.T) {
	var ran int
	real := events.NewDispatcher()
	null := events.NewNullDispatcher(real)

	null.Listen("invoice.paid", func() { ran++ })

	if !null.HasListeners("invoice.paid") {
		t.Fatal("the listener was not registered on the underlying dispatcher")
	}
	if null.Dispatch("invoice.paid") != nil || ran != 0 {
		t.Fatal("the null dispatcher fired the event")
	}
	if null.Until("invoice.paid") != nil {
		t.Fatal("Until answered on a null dispatcher")
	}

	// The registry is the real one, so the same listener fires through it.
	real.Dispatch("invoice.paid")
	if ran != 1 {
		t.Fatalf("ran = %d, want the underlying dispatcher to fire it", ran)
	}
}

func TestDisplayNameIsTheListenersType(t *testing.T) {
	job := events.NewCallQueuedListener(&notifier{}, "Handle", nil)
	if got := job.DisplayName(); got != "github.com/arandu-io/hesape/events_test.notifier" {
		t.Fatalf("DisplayName = %q", got)
	}
}
