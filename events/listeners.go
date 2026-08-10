package events

import "context"

// On returns a Publisher that only hands over the event called name.
//
// A listener answers a name, not a Go type: the outbox carries names and JSON,
// so the producer and the consumer never have to be compiled together, let
// alone deployed together. The name comparison lives here rather than as the
// first three lines of every listener, which is where it used to live.
//
// An event with another name is not an error and is not a failure to deliver:
// it was delivered, to somebody who had nothing to do with it.
func On(name string, h Publisher) Publisher {
	return PublisherFunc(func(ctx context.Context, e Stored) error {
		if e.Name != name {
			return nil
		}
		return h.Publish(ctx, e)
	})
}

// Listeners returns a Publisher that hands every event to each of list, in
// order.
//
// This is the fan-out, and it is the only one: the relay publishes to one
// Publisher, so an application with six listeners composes them here instead of
// running six relays.
//
// It stops at the first error, and the event is then retried whole -- which
// means the listeners that already ran will run again. That is at-least-once
// arriving where it always arrives, and the answer is the same one the outbox
// gives everywhere else: a listener is idempotent on Stored.ID. Swallowing the
// error to "let the others finish" would report a delivery that did not happen,
// and losing an event silently is the one outcome this package exists to
// prevent.
func Listeners(list ...Publisher) Publisher {
	return PublisherFunc(func(ctx context.Context, e Stored) error {
		for _, p := range list {
			if err := p.Publish(ctx, e); err != nil {
				return err
			}
		}
		return nil
	})
}
