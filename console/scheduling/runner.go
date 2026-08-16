package scheduling

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/console/events"
)

// Listener is told about every scheduled event that starts, finishes, fails or
// is skipped.
//
// It is a function rather than a dispatcher interface because the scheduler
// fires four events and nothing else, and a listener that wants them all
// switches on the type.
type Listener func(event any)

// Runner runs the events of a schedule that are due.
//
// schedule:run and schedule:work both do exactly this, and a second copy in
// the worker is the copy that drifts.
type Runner struct {
	schedule *Schedule

	// Listen receives ScheduledTaskStarting, ScheduledTaskFinished,
	// ScheduledTaskFailed and ScheduledTaskSkipped. Nil means nobody is
	// listening.
	Listen Listener

	// Now is the clock, for tests. Nil means time.Now. It is also the clock the
	// due events read, so a sub-minute repeat is decided against the same time
	// the loop around it is.
	Now func() time.Time

	// Sleep is how the repeat loop waits between passes, for tests. Nil means
	// time.Sleep.
	Sleep func(d time.Duration)
}

// repeatInterval is how long the repeat loop waits between passes.
//
// The finest repeat the frequencies allow is one second, so a tenth of a
// second is ten chances to notice it and no busy loop.
const repeatInterval = 100 * time.Millisecond

// NewRunner returns the runner for a schedule.
func NewRunner(schedule *Schedule) *Runner {
	return &Runner{schedule: schedule, Now: time.Now}
}

// Run fires everything due in the minute of at, and reports how many ran.
//
// The order is the filters first, then the one-server claim, then the run.
// An event that fails does not stop the ones after it -- a schedule where
// one broken task silences the rest is a schedule that hides its own
// failures.
func (r *Runner) Run(ctx context.Context, at time.Time) int {
	r.schedule.ForgetMutexCache()

	due := r.schedule.DueEvents(at)
	var repeatable []*Event
	for _, event := range due {
		// The events and the runner read one clock, which is what lets a test
		// walk a whole minute of repeats without waiting for one.
		event.useClock(r.Now)
		if event.IsRepeatable() {
			repeatable = append(repeatable, event)
		}
	}

	ran := 0
	for _, event := range due {
		if !event.FiltersPass(ctx) {
			r.dispatch(events.ScheduledTaskSkipped{Task: event})
			continue
		}

		if !r.claimed(ctx, event, at) {
			continue
		}

		ran += r.runEvent(ctx, event)
	}

	return ran + r.repeatEvents(ctx, repeatable, at)
}

// repeatEvents runs the sub-minute events again until the minute is over.
//
// It is what makes EverySecond and its companions mean anything: they set
// RepeatSeconds and leave the expression on every minute, so without this
// loop the field would never be read and EveryFifteenSeconds would run once
// a minute instead of four times.
//
// The loop stops when the context is cancelled, which is what a stopped
// process is here; and it does not read the interrupt mark, because
// Interrupter reports it by consuming it and the worker's own tick is what
// has to see it -- a repeat loop that consumed it would swallow the stop the
// operator asked for.
func (r *Runner) repeatEvents(ctx context.Context, repeatable []*Event, at time.Time) int {
	if len(repeatable) == 0 {
		return 0
	}

	ran := 0
	// endOfMinute is the last instant of the minute the run began in.
	endOfMinute := at.Truncate(time.Minute).Add(time.Minute)
	enteredMaintenanceMode := false

	for r.now().Before(endOfMinute) {
		for _, event := range repeatable {
			if ctx.Err() != nil {
				return ran
			}
			if !event.ShouldRepeatNow() {
				continue
			}

			// An application that went down during the minute stops the events
			// that did not ask to run while it is down, and stays down for the
			// rest of the loop: the state is asked once and remembered.
			enteredMaintenanceMode = enteredMaintenanceMode || r.schedule.downForMaintenance()
			if enteredMaintenanceMode && !event.RunsInMaintenanceMode() {
				continue
			}

			if !event.FiltersPass(ctx) {
				r.dispatch(events.ScheduledTaskSkipped{Task: event})
				continue
			}

			if !r.claimed(ctx, event, at) {
				continue
			}

			ran += r.runEvent(ctx, event)
		}

		if ctx.Err() != nil {
			return ran
		}
		r.sleep(repeatInterval)
	}
	return ran
}

// claimed reports whether this replica may run the event, and reports why not
// when it may not.
//
// Both the first pass and the repeat loop go through it.
func (r *Runner) claimed(ctx context.Context, event *Event, at time.Time) bool {
	if !event.RunsOnOneServer() {
		return true
	}

	mine, err := r.schedule.ServerShouldRun(ctx, event, at)
	if err != nil {
		r.dispatch(events.ScheduledTaskFailed{Task: event, Exception: err})
		return false
	}
	if !mine {
		r.dispatch(events.ScheduledTaskSkipped{Task: event})
		return false
	}
	return true
}

// now is the clock, defaulted.
func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// sleep is the wait between repeat passes, defaulted.
func (r *Runner) sleep(d time.Duration) {
	if r.Sleep != nil {
		r.Sleep(d)
		return
	}
	time.Sleep(d)
}

// runEvent runs one event, expanded over its tenants when it has any.
//
// The tenant comes from the Grant, so a per-tenant event is run once per
// tenant with a Grant each rather than once with a tenant read off a flag.
func (r *Runner) runEvent(ctx context.Context, event *Event) int {
	if !event.RunsPerTenant() {
		r.runOne(ctx, event)
		return 1
	}

	if r.schedule.Tenants == nil {
		// Reported rather than skipped in silence: a per-tenant event with no
		// resolver never runs, and that is the kind of thing found months later.
		r.dispatch(events.ScheduledTaskFailed{
			Task:      event,
			Exception: errNoTenantResolver,
		})
		return 0
	}

	tenants, err := r.schedule.Tenants(ctx)
	if err != nil {
		r.dispatch(events.ScheduledTaskFailed{Task: event, Exception: err})
		return 0
	}

	for _, tenant := range tenants {
		event.Tenant(tenant)
		r.runOne(ctx, event)
	}
	return len(tenants)
}

// runOne runs one event and reports what happened.
func (r *Runner) runOne(ctx context.Context, event *Event) {
	r.dispatch(events.ScheduledTaskStarting{Task: event})

	started := time.Now()
	err := event.Run(ctx)
	elapsed := time.Since(started)

	if err != nil {
		r.dispatch(events.ScheduledTaskFailed{Task: event, Exception: err})
		return
	}
	r.dispatch(events.ScheduledTaskFinished{Task: event, Runtime: elapsed})
}

// dispatch hands one event to the listener, if there is one.
func (r *Runner) dispatch(event any) {
	if r.Listen != nil {
		r.Listen(event)
	}
}

// errNoTenantResolver is what a per-tenant event with nothing to expand it
// reports.
var errNoTenantResolver = schedulingError(
	"this event runs per tenant and no tenant resolver was wired; it will never run")

// schedulingError is an error of this package, spelt once.
type schedulingError string

// Error implements error.
func (e schedulingError) Error() string { return "scheduling: " + string(e) }
