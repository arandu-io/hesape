package scheduling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/routing"
)

// TaskModule runs a [TaskScheduler] in the application process, as a
// foundation.Module.
//
// It is registered like any other module and is given the tasks of the ones
// registered before it, which is why it goes last. A module never starts its
// own goroutine; it declares work, and this is what runs it. Boot parses the
// tasks and only Start, which only the serving process calls, begins the loop:
// a worker or a `schedule:run` that booted the application must not also run
// every task.
type TaskModule struct {
	tasks     []foundation.Task
	opts      TaskOptions
	scheduler *TaskScheduler
}

// NewTaskModule returns the module for the tasks the registered modules
// declared.
func NewTaskModule(tasks []foundation.Task, opts TaskOptions) *TaskModule {
	return &TaskModule{tasks: tasks, opts: opts}
}

var (
	_ foundation.Module     = (*TaskModule)(nil)
	_ foundation.Bootable   = (*TaskModule)(nil)
	_ foundation.Background = (*TaskModule)(nil)
	_ foundation.Closable   = (*TaskModule)(nil)
	_ foundation.Diagnostic = (*TaskModule)(nil)
)

// Name is the module identifier.
func (*TaskModule) Name() string { return "scheduler" }

// Routes registers nothing. A scheduled task is not reachable over HTTP, and
// making it reachable would be a way to trigger billing by URL.
func (*TaskModule) Routes(*routing.Router) {}

// Boot parses the tasks. It does not start the loop.
//
// A spec that does not parse fails the boot of every command, not only the
// one that serves, so `routes` and `schedule:list` catch it too.
func (m *TaskModule) Boot(context.Context) error {
	if len(m.tasks) == 0 {
		return nil
	}
	s, err := NewTaskScheduler(m.tasks, m.opts)
	if err != nil {
		return err
	}
	m.scheduler = s
	return nil
}

// Start begins the loop.
func (m *TaskModule) Start(ctx context.Context) error {
	if m.scheduler == nil {
		return nil
	}
	m.scheduler.Start(ctx)
	return nil
}

// Close stops the loop and waits for the run in flight.
func (m *TaskModule) Close(ctx context.Context) error {
	if m.scheduler == nil {
		return nil
	}
	return m.scheduler.Stop(ctx)
}

// Diagnose reports overdue and failed tasks on the error page.
func (m *TaskModule) Diagnose(ctx context.Context) []string {
	if m.scheduler == nil {
		return nil
	}
	return m.scheduler.Diagnose(ctx)
}

// Scheduler returns the scheduler, or nil before Boot and for an application
// that declared no task.
func (m *TaskModule) Scheduler() *TaskScheduler { return m.scheduler }

// TaskCommands answers `schedule:list` and `schedule:run <id> [--tenant=]` over
// the module's scheduler, read when each command runs -- after the kernel
// booted the application, which is when the scheduler exists.
//
// It is a function rather than a Commands method so the module is not
// foundation.Commandable: the kernel composes these two as its own, once, and a
// module answering them as well would declare the same names twice.
//
// schedule:run here runs one task by id, on the path the loop uses -- not every
// task that is due, which is what Commands.ScheduleRunCommand answers for a
// Schedule built by hand.
func TaskCommands(m *TaskModule) []console.Command {
	return []console.Command{
		{
			Name:        "schedule:list",
			Description: "list the scheduled tasks, with the next run of each",
			Run: func(_ context.Context, o *console.IO) error {
				return listTasks(m.Scheduler(), o)
			},
		},
		{
			Name:        "schedule:run",
			Description: "run one scheduled task now, on the same path the scheduler uses",
			Run: func(ctx context.Context, o *console.IO) error {
				return runTask(ctx, m.Scheduler(), o)
			},
		},
	}
}

// listTasks prints the tasks with their next and last run, then every task
// whose last run failed -- which is usually why somebody ran the command.
func listTasks(s *TaskScheduler, o *console.IO) error {
	if err := o.Flags().Parse(o.Args()); err != nil {
		return err
	}
	var tasks []TaskStatus
	if s != nil {
		tasks = s.List()
	}
	if len(tasks) == 0 {
		o.Line("no scheduled tasks.")
		o.Line("A module declares them with Schedule() []foundation.Task.")
		return nil
	}

	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		next := "never"
		if !t.Next.IsZero() {
			next = t.Next.Format(time.RFC3339)
		}
		last := "-"
		if !t.LastRun.IsZero() {
			last = t.LastRun.Format(time.RFC3339)
		}
		rows = append(rows, []string{t.ID, t.Spec, t.Scope, fmt.Sprint(t.Singleton), next, last})
	}
	o.Table([]string{"id", "schedule", "scope", "singleton", "next run", "last run"}, rows)

	for _, t := range tasks {
		if t.LastError != "" {
			o.Line("%s failed on its last run: %s", t.ID, t.LastError)
		}
	}
	return nil
}

// runTask runs the task its first argument names, for --tenant.
func runTask(ctx context.Context, s *TaskScheduler, o *console.IO) error {
	args := o.Args()
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	flags := o.Flags()
	tenant := flags.String("tenant", "", "which tenant, for a task whose scope is per tenant")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("usage: schedule:run <id> [--tenant=<id>]\n`schedule:list` shows the registered tasks")
	}
	if s == nil {
		return fmt.Errorf("this application has no scheduled tasks")
	}
	return s.RunNow(ctx, id, *tenant)
}
