package concerns

import "sync"

// Event names a moment in a model's life that a listener can be registered
// for: retrieved, saving, created, deleting and the rest, declared as constants
// below.
//
// It is a string rather than an integer because the name is the identity: it is
// what HasEvents keys its listener registry by, what a custom event registered
// by an application is spelled as, and what shows up in a log line. Anything
// declared as an Event is fireable, including a name this package does not
// define.
//
// Answers the model events Illuminate\Database\Eloquent\Concerns\HasEvents
// declares in $observables.
type Event string

// The observable events, spelled as the PHP spells them.
const (
	Retrieved    Event = "retrieved"
	Creating     Event = "creating"
	Created      Event = "created"
	Updating     Event = "updating"
	Updated      Event = "updated"
	Saving       Event = "saving"
	Saved        Event = "saved"
	Restoring    Event = "restoring"
	Restored     Event = "restored"
	Replicating  Event = "replicating"
	Deleting     Event = "deleting"
	Deleted      Event = "deleted"
	Trashed      Event = "trashed"
	ForceDeleted Event = "forceDeleted"
)

// Listener answers a model event listener. Returning false halts the operation,
// which is the PHP's `if ($this->fireModelEvent('saving') === false)`.
type Listener func(model any) bool

// HasEvents answers Illuminate\Database\Eloquent\Concerns\HasEvents.
//
// The PHP dispatches through the container's event dispatcher and resolves
// listeners by class name. There is no container (ADR 0001) and no class name,
// so listeners are functions registered on the model's own registry -- which is
// what an observer is, once the resolution is taken out.
//
// A listener that returns false stops the operation. That is the whole reason
// the return value is a bool rather than nothing: creating and deleting are
// vetoable, and a listener that could not veto would have to raise an error to
// stop a save, which turns a business rule into a failure.
type HasEvents struct {
	listeners   map[Event][]Listener
	observables []Event
	dispatches  map[Event]string
	mutex       sync.RWMutex
}

// muted answers HasEvents::withoutEvents, as a depth so that nesting works.
var muted = struct {
	sync.RWMutex
	depth int
}{}

// Listen registers a listener for an event.
//
// The PHP has one static registrar per event -- Model::creating(...),
// Model::saved(...) -- because it registers against a class name. Here the
// event is the argument, and the named helpers below are kept for the
// vocabulary.
func (h *HasEvents) Listen(event Event, listener Listener) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.listeners == nil {
		h.listeners = map[Event][]Listener{}
	}
	h.listeners[event] = append(h.listeners[event], listener)
}

// Retrieved answers HasEvents::retrieved.
func (h *HasEvents) Retrieved(listener Listener) { h.Listen(Retrieved, listener) }

// Creating answers HasEvents::creating.
func (h *HasEvents) Creating(listener Listener) { h.Listen(Creating, listener) }

// Created answers HasEvents::created.
func (h *HasEvents) Created(listener Listener) { h.Listen(Created, listener) }

// Updating answers HasEvents::updating.
func (h *HasEvents) Updating(listener Listener) { h.Listen(Updating, listener) }

// Updated answers HasEvents::updated.
func (h *HasEvents) Updated(listener Listener) { h.Listen(Updated, listener) }

// Saving answers HasEvents::saving.
func (h *HasEvents) Saving(listener Listener) { h.Listen(Saving, listener) }

// Saved answers HasEvents::saved.
func (h *HasEvents) Saved(listener Listener) { h.Listen(Saved, listener) }

// Deleting answers HasEvents::deleting.
func (h *HasEvents) Deleting(listener Listener) { h.Listen(Deleting, listener) }

// Deleted answers HasEvents::deleted.
func (h *HasEvents) Deleted(listener Listener) { h.Listen(Deleted, listener) }

// Replicating answers HasEvents::replicating.
func (h *HasEvents) Replicating(listener Listener) { h.Listen(Replicating, listener) }

// FireModelEvent answers HasEvents::fireModelEvent.
//
// halt stops at the first listener that answers false and reports it, which is
// what the write path checks before it touches the database.
func (h *HasEvents) FireModelEvent(event Event, model any, halt bool) bool {
	if AreEventsMuted() {
		return true
	}

	h.mutex.RLock()
	listeners := append([]Listener(nil), h.listeners[event]...)
	h.mutex.RUnlock()

	ok := true
	for _, listener := range listeners {
		if !listener(model) {
			if halt {
				return false
			}
			ok = false
		}
	}
	return ok
}

// GetObservableEvents answers HasEvents::getObservableEvents.
func GetObservableEvents() []Event {
	return []Event{
		Retrieved, Creating, Created, Updating, Updated, Saving, Saved,
		Restoring, Restored, Replicating, Deleting, Deleted, Trashed, ForceDeleted,
	}
}

// FlushEventListeners answers HasEvents::flushEventListeners.
func (h *HasEvents) FlushEventListeners() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.listeners = nil
}

// WithoutEvents answers HasEvents::withoutEvents: the callback runs with the
// listeners muted, and they come back even if it fails.
func WithoutEvents(callback func() error) error {
	muted.Lock()
	muted.depth++
	muted.Unlock()

	defer func() {
		muted.Lock()
		muted.depth--
		muted.Unlock()
	}()

	return callback()
}

// AreEventsMuted reports whether the process is inside WithoutEvents.
func AreEventsMuted() bool {
	muted.RLock()
	defer muted.RUnlock()
	return muted.depth > 0
}
