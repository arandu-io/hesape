package scheduling

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Callback is the work a scheduled closure does.
//
// It carries the Grant its event was declared with: a scheduled closure is a
// path to a repository, so it is handed the Grant directly rather than
// reaching for one.
type Callback func(ctx context.Context, g auth.Grant) error

// CallbackEvent is a scheduled closure rather than a command line.
//
// It is an Event with the process replaced by a function call, which is why
// it embeds one: the frequencies, the filters, the mutex and the callbacks
// are all the same.
//
// Because Go has no virtual dispatch and a Schedule holds *Event, running,
// executing and summarizing for display all live on Event and decide their
// behavior by whether there is a callback. What is left here is what a
// caller reaches on the way in: the two guards that demand a name before a
// lock can be named after it, and the refusal to background a closure.
type CallbackEvent struct {
	*Event
}

// NewCallbackEvent returns the event for a closure.
//
// The mutex name resolver is installed here rather than overridden as a
// method, for the reason the type says: a mutex holding an *Event would
// otherwise name the lock after a command line this event does not have.
func NewCallbackEvent(mutex EventMutex, callback Callback, timezone *time.Location) *CallbackEvent {
	event := NewEvent(mutex, "", timezone)
	event.callback = callback
	event.MutexNameResolver = func(e *Event) string {
		return "framework/schedule-" + sha1Hex(e.GetDescription())
	}

	return &CallbackEvent{Event: event}
}

// ShouldSkipDueToOverlapping reports whether another copy holds the mutex.
//
// An event with no description has no stable mutex name, so it never skips.
func (c *CallbackEvent) ShouldSkipDueToOverlapping(ctx context.Context) (bool, error) {
	if c.GetDescription() == "" {
		return false, nil
	}
	return c.Event.ShouldSkipDueToOverlapping(ctx)
}

// RunInBackground is refused.
//
// A command sent to the background is a program the scheduler waits for beside
// the schedule; a closure has no program, so there would be nothing to wait for
// and the call would mean running the closure itself next to everything that
// reads the event. It panics rather than returning an error because it is a
// mistake in the schedule, caught the first time the schedule is declared.
func (c *CallbackEvent) RunInBackground() *Event {
	panic("scheduling: scheduled closures can not be run in the background")
}

// WithoutOverlapping stops the closure from running while a copy of it is.
//
// The mutex is named after the description, so without one there is nothing
// to name the lock and two copies would each take a different key.
func (c *CallbackEvent) WithoutOverlapping(expiresAt ...int) *Event {
	if c.GetDescription() == "" {
		panic("scheduling: a scheduled event name is required to prevent overlapping; " +
			"call Name before WithoutOverlapping")
	}
	return c.Event.WithoutOverlapping(expiresAt...)
}

// OnOneServer allows the closure to run on exactly one replica per window.
//
// It requires a name for the same reason WithoutOverlapping does.
func (c *CallbackEvent) OnOneServer() *Event {
	if c.GetDescription() == "" {
		panic("scheduling: a scheduled event name is required to only run on one server; " +
			"call Name before OnOneServer")
	}
	return c.Event.OnOneServer()
}
