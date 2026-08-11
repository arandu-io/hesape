package scheduling

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/console/events"
)

// Listener is told about every scheduled event that starts, finishes, fails or
// is skipped.
//
// It answers what ScheduleRunCommand dispatches through the event dispatcher.
// It is a function rather than a dispatcher interface because the scheduler
// fires four events and nothing else, and a listener that wants them all
// switches on the type.
type Listener func(event any)

// Runner runs the events of a schedule that are due.
//
// It answers the body of ScheduleRunCommand::handle, lifted out of the command:
// schedule:run and schedule:work both do exactly this, and a second copy in the
// worker is the copy that drifts.
type Runner struct {
	schedule *Schedule

	// Listen receives ScheduledTaskStarting, ScheduledTaskFinished,
	// ScheduledTaskFailed and ScheduledTaskSkipped. Nil means nobody is
	// listening.
	Listen Listener

	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time
}

// NewRunner returns the runner for a schedule.
func NewRunner(schedule *Schedule) *Runner {
	return &Runner{schedule: schedule, Now: time.Now}
}

// Run fires everything due in the minute of at, and reports how many ran.
//
// It answers ScheduleRunCommand::handle: the filters first, then the one-server
// claim, then the run. An event that fails does not stop the ones after it --
// a schedule where one broken task silences the rest is a schedule that hides
// its own failures.
func (r *Runner) Run(ctx context.Context, at time.Time) int {
	r.schedule.ForgetMutexCache()

	ran := 0
	for _, event := range r.schedule.DueEvents(at) {
		if !event.FiltersPass(ctx) {
			r.dispatch(events.ScheduledTaskSkipped{Task: event})
			continue
		}

		if event.RunsOnOneServer() {
			mine, err := r.schedule.ServerShouldRun(ctx, event, at)
			if err != nil {
				r.dispatch(events.ScheduledTaskFailed{Task: event, Exception: err})
				continue
			}
			if !mine {
				r.dispatch(events.ScheduledTaskSkipped{Task: event})
				continue
			}
		}

		ran += r.runEvent(ctx, event)
	}
	return ran
}

// runEvent runs one event, expanded over its tenants when it has any.
//
// The expansion is not in the PHP. RULE 14 says the tenant comes from the Grant,
// so a per-tenant event is run once per tenant with a Grant each rather than
// once with a tenant read off a flag.
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
