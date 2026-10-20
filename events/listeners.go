package events

import (
	"context"
	"fmt"
)

// Route is one entry in a [Listeners] list: the event name it answers and what
// it does with a stored event of that name. It is built by [On] or [Every], and
// its fields are unexported so that an entry cannot exist without the decode On
// puts in front of the handler.
//
// It is not called Listener because that name is the dispatcher's prepared
// closure, which takes a name and a spread payload rather than a stored row.
type Route struct {
	name   string
	handle func(ctx context.Context, e Stored) error
}

// On returns the listener that answers the event named name: it decodes the
// stored payload into an E and hands fn the decoded value together with the row
// it came from.
//
//	events.On("note.published", func(ctx context.Context, e NotePublished, s events.Stored) error {
//		return notifier.Send(ctx, auth.SystemGrant(notifications.ActionSend, s.TenantID), ...)
//	})
//
// E is the consumer's own type, declared beside the listener rather than
// shared with the code that stored the event: the payload is JSON so that a
// consumer does not have to compile against the producer. The Stored row comes
// along because the payload is not all a listener needs -- the id is what a
// listener deduplicates on, delivery being at-least-once, and the tenant is
// what it rebuilds a Grant from.
//
// A payload that does not decode into E is an error from the listener, so the
// relay keeps the event, counts the attempt and eventually parks it with the
// decode failure as the reason; it is never handed on half-read.
//
// The name is matched exactly. An empty name or a nil fn panics: both are
// mistakes in the list written in bootstrap/app.go, and a panic there stops the
// boot instead of leaving an event that silently reaches nobody.
func On[E any](name string, fn func(ctx context.Context, event E, stored Stored) error) Route {
	if name == "" {
		panic("events: On needs the name of the event it answers")
	}
	if fn == nil {
		panic(fmt.Sprintf("events: On(%q) needs a function to call", name))
	}
	return Route{name: name, handle: func(ctx context.Context, e Stored) error {
		var event E
		if err := e.Decode(&event); err != nil {
			return err
		}
		return fn(ctx, event, e)
	}}
}

// Every returns the listener that is handed every event, whatever its name,
// undecoded: an audit log, a metrics counter, a forwarder to a broker. It takes
// a Publisher, so anything that already receives stored events -- a
// PublisherFunc, a client for a broker -- is a listener as it is.
//
// A nil Publisher panics, for the reason [On] gives.
func Every(p Publisher) Route {
	if p == nil {
		panic("events: Every needs a Publisher to hand the events to")
	}
	return Route{handle: p.Publish}
}

// Listeners is the list of listeners the relay hands each committed event to,
// and it is a [Publisher], so it is what NewRelay is given:
//
//	relay := events.NewRelay(outbox, events.Listeners{
//		events.Every(auditLog),
//		events.On("note.published", notifyAuthor.Handle),
//		events.On("invoice.paid", receipts.Send),
//	}, events.RelayOptions{Locks: locks})
//
// It is the fan-out, the name filter and the decode, which every application
// otherwise writes around the one Publisher the relay takes. The list is the
// registry: there is nothing to subscribe and nothing that registers itself,
// so a listener that is not written in it does not run, and that is visible in
// the wiring.
//
// An event is handed to the listeners whose name it carries, and to every
// [Every] listener, in the order they are listed. An event no listener answers
// is delivered: there was nothing to do with it, and an error would park it.
//
// The first listener that fails stops the event there, and the error is
// returned as it is. The relay then hands the whole event over again later, to
// every listener in the list -- delivery is at-least-once, so each of them is
// written to be safe to run twice, keyed on Stored.ID.
type Listeners []Route

var _ Publisher = Listeners(nil)

// Publish hands e to the listeners that answer its name, in order, and returns
// the first error one of them returns.
func (l Listeners) Publish(ctx context.Context, e Stored) error {
	for _, listener := range l {
		if listener.name != "" && listener.name != e.Name {
			continue
		}
		if listener.handle == nil {
			// The zero Route: written as a literal rather than built by On or
			// Every. It is an error and not a skip, because skipping it
			// would deliver the event past a listener somebody meant to run.
			return fmt.Errorf("events: a listener in the list for %s was not built by On or Every", e.Name)
		}
		if err := listener.handle(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
