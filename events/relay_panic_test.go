package events_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/events"
)

// TestAListenerThatPanicsFailsItsEventAndNotTheProcess: a panic in a listener
// is a failed delivery. The event is marked failed with the panic as the
// reason, the events behind it in the batch are still delivered, and the panic
// does not reach the goroutine the relay runs on -- which, unrecovered, ended
// the process serving requests and panicked again on the same event after
// every restart.
func TestAListenerThatPanicsFailsItsEventAndNotTheProcess(t *testing.T) {
	var delivered []string
	list := events.Listeners{
		events.On("invoice.paid", func(context.Context, struct{}, events.Stored) error {
			var receipts map[string]int
			receipts["inv-1"]++ // a nil map: the panic a listener ships with
			return nil
		}),
		events.On("order.shipped", func(_ context.Context, _ struct{}, s events.Stored) error {
			delivered = append(delivered, s.Name)
			return nil
		}),
	}
	relay, table := relayOver(t, events.RelayOptions{}, list)
	table.add("invoice.paid", 0)
	table.add("order.shipped", 0)

	if err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if len(delivered) != 1 {
		t.Fatalf("delivered %v, want the event behind the panic", delivered)
	}
	pending := table.stored()
	if len(pending) != 1 || pending[0].Name != "invoice.paid" || pending[0].Attempts != 1 {
		t.Fatalf("pending = %+v, want invoice.paid kept with one failed attempt", pending)
	}
}

// TestAListenerThatKeepsPanickingIsParked: the panic counts as an attempt like
// any other failure, so the event is parked when the attempts run out, and the
// row says it was a panic.
func TestAListenerThatKeepsPanickingIsParked(t *testing.T) {
	boom := events.PublisherFunc(func(context.Context, events.Stored) error {
		panic("the template is missing")
	})
	relay, table := relayOver(t, events.RelayOptions{MaxAttempts: 1}, boom)
	table.add("invoice.paid", 0)

	if err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	parked, err := relay.Parked(context.Background(), 10)
	if err != nil {
		t.Fatalf("Parked: %v", err)
	}
	if len(parked) != 1 {
		t.Fatalf("parked %d events, want 1", len(parked))
	}
	if reason := parked[0].LastError; !strings.Contains(reason, "panic: the template is missing") ||
		!strings.Contains(reason, "invoice.paid") {
		t.Errorf("the parked row does not say it panicked, or on which event: %q", reason)
	}
}

// TestAPassThatPanicsDoesNotStopTheRelay: a panic outside any listener -- the
// driver, here -- fails that pass, and the next tick delivers. The relay runs
// on a goroutine of the serving process, so a panic left to reach it ends the
// process.
func TestAPassThatPanicsDoesNotStopTheRelay(t *testing.T) {
	publisher := newRecorder()
	module, table := moduleWithRelay(t, events.RelayOptions{Interval: time.Millisecond}, publisher)
	table.panicOnTheNextRead()
	table.add("invoice.paid", 0)

	if err := module.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = module.Close(context.Background()) })

	select {
	case <-publisher.got:
	case <-time.After(5 * time.Second):
		t.Fatal("the relay never published after the pass that panicked")
	}
}
