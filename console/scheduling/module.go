package scheduling

import (
	"context"
	"fmt"
	"time"
)

// Module runs the schedule in the application process.
//
// It is what `schedule:work` is, wired as a module rather than typed as a
// command: this binary has a resident process, so nothing external has to
// call `schedule:run` on a timer. That is one artifact instead of two, and
// nothing to forget when a machine is replaced.
//
// It registers no routes. A scheduled event is not reachable over HTTP, and
// making it reachable would be a way to trigger billing by URL.
//
// It is the last piece of the hesape/scheduler package, which this one replaced:
// Task became Event, Schedule became CronExpression, and the loop is here.
type Module struct {
	schedule *Schedule
	runner   *Runner

	// stop cancels the loop, and done closes when it has stopped.
	stop context.CancelFunc
	done chan struct{}
}

// NewModule returns the module for a schedule.
func NewModule(schedule *Schedule) *Module {
	return &Module{schedule: schedule, runner: NewRunner(schedule)}
}

// Name is the module identifier.
//
// It belongs to the Arandu module contract.
func (*Module) Name() string { return "scheduling" }

// Runner returns the runner, so a listener can be installed before Start.
func (m *Module) Runner() *Runner { return m.runner }

// Boot checks that every declared expression parses.
//
// Waiting for Event.IsDue to parse it would mean a schedule nobody can read
// is found only at the first tick.
//
// The application does not start with an event that would silently never run,
// and it fails for every command and not only the one that serves -- so
// `schedule:list` catches a bad expression too.
func (m *Module) Boot(context.Context) error {
	for _, event := range m.schedule.Events() {
		if _, err := ParseCronExpression(event.GetExpression()); err != nil {
			return fmt.Errorf("scheduling: %s: %w", event.GetSummaryForDisplay(), err)
		}
	}
	return nil
}

// Start begins the loop, and only the process that serves calls it.
//
// It ticks at the top of each minute rather than every sixty seconds from boot,
// because an event specified as "0 3 * * *" has to fire at 3:00 and not at 3:00
// plus however long after a deploy the process happened to start.
func (m *Module) Start(ctx context.Context) error {
	if len(m.schedule.Events()) == 0 {
		return nil
	}

	loop, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.stop = cancel
	m.done = make(chan struct{})

	go func() {
		defer close(m.done)
		for {
			now := time.Now()
			next := now.Truncate(time.Minute).Add(time.Minute)

			select {
			case <-loop.Done():
				return
			case <-time.After(next.Sub(now)):
			}

			m.runner.Run(loop, time.Now())
		}
	}()
	return nil
}

// Close stops the loop and waits for the run in flight.
//
// Waiting matters: an event killed halfway is an event whose lock is still held
// and whose work is half done, and the next window will not know either.
func (m *Module) Close(ctx context.Context) error {
	if m.stop == nil {
		return nil
	}
	m.stop()

	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
