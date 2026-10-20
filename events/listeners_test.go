package events_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/events"
)

// paidInvoice is the consumer's own type for the payload, declared beside the
// listener the way an application declares it.
type paidInvoice struct {
	Invoice string `json:"invoice"`
	Cents   int    `json:"cents"`
}

func stored(name, payload string) events.Stored {
	return events.Stored{ID: "evt-" + name, TenantID: tenant, Name: name, Payload: payload}
}

// TestListenersRouteByNameAndDecode is the whole point of the list: the event
// reaches the listener registered for its name, decoded into the listener's own
// type, with the row it came from beside it -- and no other listener.
func TestListenersRouteByNameAndDecode(t *testing.T) {
	var paid []paidInvoice
	var rows []events.Stored
	var shipped int

	list := events.Listeners{
		events.On("invoice.paid", func(_ context.Context, e paidInvoice, s events.Stored) error {
			paid = append(paid, e)
			rows = append(rows, s)
			return nil
		}),
		events.On("order.shipped", func(context.Context, struct{}, events.Stored) error {
			shipped++
			return nil
		}),
	}

	err := list.Publish(context.Background(), stored("invoice.paid", `{"invoice":"inv-7","cents":1250}`))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(paid) != 1 || paid[0] != (paidInvoice{Invoice: "inv-7", Cents: 1250}) {
		t.Fatalf("decoded %+v, want the one payload", paid)
	}
	if rows[0].ID != "evt-invoice.paid" || rows[0].TenantID != tenant {
		t.Errorf("the listener got the row %+v, want the stored one", rows[0])
	}
	if shipped != 0 {
		t.Errorf("a listener for another name ran %d times", shipped)
	}
}

// TestEveryListenerSeesEveryEvent: an audit log is handed each event whatever
// its name, undecoded, in its place in the list.
func TestEveryListenerSeesEveryEvent(t *testing.T) {
	var order []string
	log := events.PublisherFunc(func(_ context.Context, e events.Stored) error {
		order = append(order, "log:"+e.Name)
		return nil
	})
	list := events.Listeners{
		events.Every(log),
		events.On("invoice.paid", func(context.Context, paidInvoice, events.Stored) error {
			order = append(order, "receipt")
			return nil
		}),
	}

	for _, name := range []string{"invoice.paid", "order.shipped"} {
		if err := list.Publish(context.Background(), stored(name, `{}`)); err != nil {
			t.Fatalf("Publish(%s): %v", name, err)
		}
	}
	want := []string{"log:invoice.paid", "receipt", "log:order.shipped"}
	if !slices.Equal(order, want) {
		t.Fatalf("ran %v, want %v", order, want)
	}
}

// TestTheFirstFailureStopsTheEvent: the error comes back as the listener
// returned it, so the relay keeps the event, and the listeners behind the
// failure do not run this time -- they run when the event is handed over again.
func TestTheFirstFailureStopsTheEvent(t *testing.T) {
	refused := errors.New("the mail server said no")
	var ran []string
	list := events.Listeners{
		events.On("invoice.paid", func(context.Context, paidInvoice, events.Stored) error {
			ran = append(ran, "first")
			return refused
		}),
		events.On("invoice.paid", func(context.Context, paidInvoice, events.Stored) error {
			ran = append(ran, "second")
			return nil
		}),
	}

	err := list.Publish(context.Background(), stored("invoice.paid", `{}`))
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the listener's error", err)
	}
	if !slices.Equal(ran, []string{"first"}) {
		t.Fatalf("ran %v, want only the listener that failed", ran)
	}
}

// TestAPayloadThatDoesNotDecodeIsNotHandedOn: the listener never sees a value
// half-read, and the failure is an error the relay counts, so a payload that
// will never decode is parked with the reason.
func TestAPayloadThatDoesNotDecodeIsNotHandedOn(t *testing.T) {
	called := false
	list := events.Listeners{
		events.On("invoice.paid", func(context.Context, paidInvoice, events.Stored) error {
			called = true
			return nil
		}),
	}

	err := list.Publish(context.Background(), stored("invoice.paid", `{"cents":"twelve"}`))
	if err == nil || !strings.Contains(err.Error(), "decoding invoice.paid") {
		t.Fatalf("err = %v, want the decode failure naming the event", err)
	}
	if called {
		t.Error("the listener was called with a payload that did not decode")
	}
}

// TestAnEventNobodyAnswersIsDelivered: an error here would park an event that
// simply has no consumer yet.
func TestAnEventNobodyAnswersIsDelivered(t *testing.T) {
	list := events.Listeners{
		events.On("invoice.paid", func(context.Context, paidInvoice, events.Stored) error {
			t.Error("a listener ran for an event it does not answer")
			return nil
		}),
	}
	if err := list.Publish(context.Background(), stored("customer.created", `{}`)); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if err := (events.Listeners)(nil).Publish(context.Background(), stored("customer.created", `{}`)); err != nil {
		t.Fatalf("an empty list: err = %v, want nil", err)
	}
}

// TestAListenerWrittenAsALiteralIsRefused: the zero Route has nothing to
// call, and skipping it would deliver the event past it.
func TestAListenerWrittenAsALiteralIsRefused(t *testing.T) {
	err := events.Listeners{{}}.Publish(context.Background(), stored("invoice.paid", `{}`))
	if err == nil || !strings.Contains(err.Error(), "not built by On or Every") {
		t.Fatalf("err = %v, want the refusal", err)
	}
}

// TestAMistakeInTheListStopsTheBoot: an empty name or a missing function is a
// mistake in bootstrap/app.go, and it panics there rather than leaving an event
// that reaches nobody.
func TestAMistakeInTheListStopsTheBoot(t *testing.T) {
	cases := map[string]func(){
		"no name":      func() { events.On("", func(context.Context, struct{}, events.Stored) error { return nil }) },
		"no function":  func() { events.On[struct{}]("invoice.paid", nil) },
		"no publisher": func() { events.Every(nil) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("building the listener did not panic")
				}
			}()
			build()
		})
	}
}

// TestTheRelayDeliversThroughTheList: the list is what NewRelay takes, and an
// event drained through it is decoded, handled and marked published.
func TestTheRelayDeliversThroughTheList(t *testing.T) {
	var got []string
	list := events.Listeners{
		events.On("invoice.paid", func(_ context.Context, _ struct{}, s events.Stored) error {
			got = append(got, s.ID)
			return nil
		}),
	}
	relay, table := relayOver(t, events.RelayOptions{}, list)
	table.add("invoice.paid", 0)
	table.add("order.shipped", 0)

	if err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the listener ran for %v, want the one invoice.paid", got)
	}
	if delivered := table.delivered(); len(delivered) != 2 {
		t.Fatalf("marked published %v, want both events: one answered, one with nobody to answer it", delivered)
	}
}
