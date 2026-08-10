package scheduler

import (
	"context"
)

// Module runs the scheduler in the application process.
//
// It is registered like any other module and collects the tasks from the ones
// registered before it -- which is why it goes last in the Register call. A
// module never starts its own goroutine; it declares work, and this is what
// runs it.
//
// It registers no routes. A scheduled task is not reachable over HTTP, and
// making it reachable would be a way to trigger billing by URL.
type Module struct {
	tasks     []Task
	opts      Options
	scheduler *Scheduler
}

// NewModule returns the module for a set of tasks.
//
// The tasks come from the modules registered before it, collected by the kernel
// from everything that implements Schedulable:
//
//	k := foundation.New().Register(billing.New(...), reports.New(...))
//	k.Register(scheduler.NewModule(k.Tasks(), scheduler.Options{
//	    Locks:   cache.NewLocks(store),
//	    Tenants: tenants.Active,
//	}))
func NewModule(tasks []Task, opts Options) *Module {
	return &Module{tasks: tasks, opts: opts}
}

// Name is the module identifier.
func (*Module) Name() string { return "scheduler" }

// Boot parses the tasks. It does not start the loop.
//
// An unparseable spec fails the boot. The application does not start with a
// task that would silently never run, which is what "config validated at boot,
// fail fast" means applied to schedules -- and it fails for every command, not
// just the one that serves, so `aru routes` catches a bad spec too.
func (m *Module) Boot(_ context.Context) error {
	if len(m.tasks) == 0 {
		return nil
	}

	s, err := New(m.tasks, m.opts)
	if err != nil {
		return err
	}
	m.scheduler = s
	return nil
}

// Start begins the loop, and only the process that serves calls it.
//
// It used to happen in Boot, so every `aru work` replica ran a scheduler and
// `aru schedule:run` -- the command for running one task by hand -- started the
// loop that runs all of them. The lock made it harmless; it did not make it
// right.
func (m *Module) Start(ctx context.Context) error {
	if m.scheduler == nil {
		return nil
	}
	m.scheduler.Start(ctx)
	return nil
}

// Close stops the loop and waits for the run in flight.
func (m *Module) Close(ctx context.Context) error {
	if m.scheduler == nil {
		return nil
	}
	return m.scheduler.Stop(ctx)
}

// Diagnose reports overdue and failed tasks on the error page.
func (m *Module) Diagnose(ctx context.Context) []string {
	if m.scheduler == nil {
		return nil
	}
	return m.scheduler.Diagnose(ctx)
}

// Scheduler returns the running scheduler, for `aru schedule:list` and
// `aru schedule:run` to reach through the application binary.
//
// Nil before Boot.
func (m *Module) Scheduler() *Scheduler { return m.scheduler }
