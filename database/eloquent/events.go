package eloquent

import "slices"

// Event is one of the model events HasEvents fires.
//
// The PHP dispatches them through the container's event dispatcher and lets an
// observer class subscribe by method name. There is no container and no facade
// (ADR 0001, ADR 0002), so a callback is registered on the model that will fire
// it, and NewInstance carries the registrations onto every instance -- which is
// what registering on the class does there.
type Event string

// The events Model fires, in the order Illuminate fires them.
//
// booting, booted and the broadcast events are not here: booting is the static
// initialisation of a PHP class, which a Go value does not have, and
// broadcasting is service-provider surface.
const (
	Retrieved     Event = "retrieved"
	Creating      Event = "creating"
	Created       Event = "created"
	Updating      Event = "updating"
	Updated       Event = "updated"
	Saving        Event = "saving"
	Saved         Event = "saved"
	Deleting      Event = "deleting"
	Deleted       Event = "deleted"
	Trashed       Event = "trashed"
	Restoring     Event = "restoring"
	Restored      Event = "restored"
	ForceDeleting Event = "forceDeleting"
	ForceDeleted  Event = "forceDeleted"
	Replicating   Event = "replicating"
)

// RegisterModelEvent answers Model::registerModelEvent.
//
// A callback that returns an error stops the operation, which is what returning
// false does in PHP -- and the error says why, where the false said nothing. A
// saving callback that fails means nothing was written.
func (m *Model[T]) RegisterModelEvent(event Event, callback func(*Model[T]) error) *Model[T] {
	if m.events == nil {
		m.events = map[Event][]func(*Model[T]) error{}
	}
	m.events[event] = append(m.events[event], callback)
	return m
}

// WithoutEvents answers Model::withoutEvents.
//
// The PHP swaps the global dispatcher out and back. There is no global
// dispatcher here, so this mutes the model it is called on -- and the instances
// it makes while muted, because they are made from it.
func (m *Model[T]) WithoutEvents(callback func() error) error {
	previous := m.muted
	m.muted = true
	defer func() { m.muted = previous }()
	return callback()
}

// fireModelEvent answers Model::fireModelEvent. The PHP's $halt is always true
// for the events that can cancel and false for the ones that only announce; here
// every callback can stop the operation, because it can only do so by failing.
func (m *Model[T]) fireModelEvent(event Event) error {
	if m.muted {
		return nil
	}
	for _, callback := range m.events[event] {
		if err := callback(m); err != nil {
			return err
		}
	}
	return nil
}

func cloneEvents[T any](in map[Event][]func(*Model[T]) error) map[Event][]func(*Model[T]) error {
	if in == nil {
		return nil
	}
	out := make(map[Event][]func(*Model[T]) error, len(in))
	for event, callbacks := range in {
		out[event] = slices.Clone(callbacks)
	}
	return out
}
