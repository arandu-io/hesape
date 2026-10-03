package model

// Event is one of the model events a row fires.
//
// A callback is registered on the table, in TableSpec.Events, and runs for
// every row of it.
type Event string

// The events a row fires, in the order it fires them.
//
// There is no booting event: a Go value has no class initialisation to hook.
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

// fireModelEvent runs every callback registered for event, in registration
// order, stopping at the first one that returns an error. The callback is
// handed the entity, which is what a listener written against the
// application's own struct converts back.
func fireModelEvent(m *Model, event Event) error {
	if m.r.muted {
		return nil
	}
	for _, callback := range m.r.table.events[event] {
		if err := callback(m.r.self); err != nil {
			return err
		}
	}
	return nil
}
