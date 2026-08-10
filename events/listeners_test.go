package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/events"
)

// collect returns a Publisher that appends every event it is handed.
func collect(into *[]string) events.Publisher {
	return events.PublisherFunc(func(_ context.Context, e events.Stored) error {
		*into = append(*into, e.Name)
		return nil
	})
}

// TestOnDeliversOnlyTheEventItAnswers: a listener answers a name, and one that
// has to switch on the name is the place every event in the system passes
// through.
func TestOnDeliversOnlyTheEventItAnswers(t *testing.T) {
	var got []string
	publisher := events.On("invoice.paid", collect(&got))

	for _, name := range []string{"invoice.paid", "customer.created", "invoice.paid"} {
		if err := publisher.Publish(context.Background(), events.Stored{Name: name}); err != nil {
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
	var got []string
	publisher := events.On("invoice.paid", collect(&got))

	if err := publisher.Publish(context.Background(), events.Stored{Name: "customer.created"}); err != nil {
		t.Fatalf("an event with another name was reported undelivered: %v", err)
	}
}

// TestListenersFanOutInOrder: an application with six listeners composes them
// here rather than running six relays.
func TestListenersFanOutInOrder(t *testing.T) {
	var first, second []string
	publisher := events.Listeners(collect(&first), collect(&second))

	if err := publisher.Publish(context.Background(), events.Stored{Name: "invoice.paid"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first got %v and second got %v, want the event in both", first, second)
	}
}

// TestListenersStopAtTheFirstFailure: swallowing the error to let the others
// finish would report a delivery that did not happen, and the event would be
// marked published and never retried.
func TestListenersStopAtTheFirstFailure(t *testing.T) {
	refused := errors.New("the broker refused the message")
	var after []string

	publisher := events.Listeners(
		events.PublisherFunc(func(context.Context, events.Stored) error { return refused }),
		collect(&after),
	)

	err := publisher.Publish(context.Background(), events.Stored{Name: "invoice.paid"})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the listener's error to reach the relay", err)
	}
	if len(after) != 0 {
		t.Fatalf("the listener behind the failing one ran anyway: %v", after)
	}
}

// TestListenersComposesWithOn is the shape an application wires: one fan-out,
// each branch answering one name.
func TestListenersComposesWithOn(t *testing.T) {
	var paid, created []string
	publisher := events.Listeners(
		events.On("invoice.paid", collect(&paid)),
		events.On("customer.created", collect(&created)),
	)

	for _, name := range []string{"invoice.paid", "customer.created", "invoice.refunded"} {
		if err := publisher.Publish(context.Background(), events.Stored{Name: name}); err != nil {
			t.Fatalf("Publish(%s): %v", name, err)
		}
	}

	if len(paid) != 1 || len(created) != 1 {
		t.Fatalf("paid = %v, created = %v", paid, created)
	}
}

// TestListenersWithNothingWiredIsNotAFailure: an application that stores events
// before it has anywhere to send them is the state this package calls useful.
func TestListenersWithNothingWiredIsNotAFailure(t *testing.T) {
	if err := events.Listeners().Publish(context.Background(), events.Stored{Name: "invoice.paid"}); err != nil {
		t.Fatalf("Publish with no listeners: %v", err)
	}
}
