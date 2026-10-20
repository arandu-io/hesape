package scheduling

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console/events"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/log"
)

// TaskOptions configures a [TaskScheduler].
type TaskOptions struct {
	// Locks makes a Singleton task run once per occurrence across replicas.
	// Nil means a single replica: with more than one, every replica runs every
	// task.
	//
	// The issuer's store is what the claim is shared through, so it has to be
	// one every replica reaches. A store local to the process makes every
	// replica win its own claim.
	Locks *cache.Locks

	// Tenants expands PerTenant tasks. Nil means those tasks do not run, which
	// is reported rather than silent.
	Tenants Tenants

	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time

	// Recorder receives each finished run, so the task shows on the debug
	// console with its queries and its timeline exactly like a request. Nil is
	// no instrumentation -- no Collector is built -- which is what production
	// looks like.
	Recorder *log.Recorder
}

// TaskStatus is one task as `schedule:list` prints it: what it is, when it
// runs next, and how its last run went.
type TaskStatus struct {
	ID        string
	Spec      string
	Scope     string
	Singleton bool
	Timeout   time.Duration
	Next      time.Time
	LastRun   time.Time
	LastError string
}

// defaultTaskTimeout bounds a task that declared no Timeout. A scheduled run
// with no bound is a run nothing ever stops.
const defaultTaskTimeout = 5 * time.Minute

// taskEntry is one task with the event that fires it and its bookkeeping.
type taskEntry struct {
	task foundation.Task
	cron CronExpression

	// lastRun and lastError are what Diagnose and List report. The scheduler's
	// four events carry no place to keep them.
	mu        sync.Mutex
	lastRun   time.Time
	lastError string
}

// TaskScheduler runs the tasks modules declare -- foundation.Task, collected
// from every Schedulable module -- on this package's Schedule and Runner.
//
// A task carries three things an Event has no field for: a Timeout, a Recorder
// and a durable claim on its occurrence. So the scheduler declares one
// CallbackEvent per task and hands the run back to itself. What is due in a
// minute, the expansion of a per-tenant task to one run per tenant and the
// Grant each run carries are the Runner's; the claim, the timeout, the
// Collector and the bookkeeping List and Diagnose read are here.
type TaskScheduler struct {
	entries  []*taskEntry
	schedule *Schedule
	opts     TaskOptions

	// stop cancels the loop, and done closes when it has stopped.
	stop context.CancelFunc
	done chan struct{}
}

// NewTaskScheduler parses the tasks and returns the scheduler.
//
// A task with no id, a repeated id, no Run or a spec that does not parse is an
// error here rather than a task that silently never runs -- the failure of
// every scheduler that validates lazily. A task with no Timeout gets five
// minutes.
func NewTaskScheduler(tasks []foundation.Task, opts TaskOptions) (*TaskScheduler, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	seen := map[string]bool{}
	entries := make([]*taskEntry, 0, len(tasks))
	for _, t := range tasks {
		if t.ID == "" {
			return nil, errors.New("scheduling: a task with no id cannot be claimed, listed or run by hand")
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("scheduling: two tasks share the id %q, and the claim cannot tell them apart", t.ID)
		}
		seen[t.ID] = true

		if t.Run == nil {
			return nil, fmt.Errorf("scheduling: %s has no Run", t.ID)
		}
		cron, err := ParseCronExpression(t.Spec)
		if err != nil {
			return nil, fmt.Errorf("scheduling: %s: %w", t.ID, err)
		}
		if t.Timeout <= 0 {
			t.Timeout = defaultTaskTimeout
		}
		entries = append(entries, &taskEntry{task: t, cron: cron})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].task.ID < entries[j].task.ID })

	s := &TaskScheduler{entries: entries, opts: opts}

	// Both mutexes are nil. Overlap and one-server are attributes of an Event
	// and not of a task, and nothing here sets either. Singleton is answered by
	// the occurrence claim instead, which is a third thing: one occurrence of
	// one minute happens once, where WithoutOverlapping says a run does not
	// start while the previous one is going and OnOneServer says one process
	// wins the window.
	s.schedule = NewSchedule(nil, nil, nil)
	s.schedule.Tenants = opts.Tenants

	for _, e := range entries {
		event := s.schedule.Call(s.work(e))
		s.declare(event, e, "")
	}
	return s, nil
}

// declare writes a task's identity onto the event that fires it.
func (s *TaskScheduler) declare(event *CallbackEvent, e *taskEntry, tenant string) {
	event.Name(e.task.ID)
	event.Cron(e.task.Spec)
	event.Action(e.task.Action)
	if e.task.Scope == foundation.PerTenant {
		event.PerTenant()
		if tenant != "" {
			event.Tenant(tenant)
		}
	}
}

// Schedule returns the schedule the tasks are declared on.
func (s *TaskScheduler) Schedule() *Schedule { return s.schedule }

// Start runs the loop until Stop, ticking at the top of each minute.
//
// Aligned to the minute rather than every sixty seconds from boot, because a
// task specified as "0 3 * * *" has to fire at 3:00 and not at 3:00 plus however
// long after a deploy the process happened to start.
func (s *TaskScheduler) Start(ctx context.Context) {
	loop, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.stop = cancel
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		log.For(loop).With("component", "scheduler").Info("scheduler started", "tasks", len(s.entries))
		for {
			now := s.opts.Now()
			next := now.Truncate(time.Minute).Add(time.Minute)

			select {
			case <-loop.Done():
				return
			case <-time.After(next.Sub(now)):
			}

			s.Tick(loop, s.opts.Now())
		}
	}()
}

// Stop cancels the loop and waits for the run in flight.
//
// Waiting matters: a task killed halfway is a task whose work is half done
// under a claim that says the occurrence happened.
func (s *TaskScheduler) Stop(ctx context.Context) error {
	if s.stop == nil {
		return nil
	}
	s.stop()

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Tick fires everything due in the minute of at.
//
// It is the code path the loop drives, exported so a manual run and a test
// drive the same one: what is due, and what a per-tenant task expands to, is
// decided by the Runner.
func (s *TaskScheduler) Tick(ctx context.Context, at time.Time) {
	runner := NewRunner(s.schedule)
	runner.Now = s.opts.Now
	runner.Listen = s.report(ctx)
	runner.Run(withTaskWindow(ctx, at), at)
}

// RunNow runs one task by id, outside its schedule, for the tenant given when
// the task runs per tenant.
//
// Same claim, same Grant, same instrumentation -- which is what makes the
// manual run auditable rather than a back door. A Singleton whose occurrence
// is already claimed therefore does not run and reports no error, so a second
// manual run inside the same UTC minute does nothing; a manual path that
// skipped the claim is how one occurrence gets executed twice.
func (s *TaskScheduler) RunNow(ctx context.Context, id, tenant string) error {
	for _, e := range s.entries {
		if e.task.ID != id {
			continue
		}
		if e.task.Scope == foundation.PerTenant && tenant == "" {
			return fmt.Errorf("%s runs per tenant: say which one with --tenant", id)
		}

		// The scheduled event is shared by every tick. A manual run gets its
		// own, because the tenant and the result are this operation's state,
		// and on the shared event concurrent runs would overwrite each other.
		event := NewCallbackEvent(nil, s.work(e), nil)
		s.declare(event, e, tenant)
		return event.Run(withTaskWindow(ctx, s.opts.Now()))
	}
	return fmt.Errorf("no task with id %s. `schedule:list` shows the registered ones", id)
}

// List returns the tasks, by id, with their next run and how the last went.
func (s *TaskScheduler) List() []TaskStatus {
	now := s.opts.Now()
	out := make([]TaskStatus, 0, len(s.entries))
	for _, e := range s.entries {
		e.mu.Lock()
		lastRun, lastError := e.lastRun, e.lastError
		e.mu.Unlock()

		scope := "global"
		if e.task.Scope == foundation.PerTenant {
			scope = "per tenant"
		}
		out = append(out, TaskStatus{
			ID:        e.task.ID,
			Spec:      e.cron.String(),
			Scope:     scope,
			Singleton: e.task.Singleton,
			Timeout:   e.task.Timeout,
			Next:      e.cron.GetNextRunDate(now),
			LastRun:   lastRun,
			LastError: lastError,
		})
	}
	return out
}

// Diagnose reports the tasks that failed on their last run and the ones that
// are two windows late, in sentences a person can act on. A task that stopped
// firing looks exactly like a task with nothing to do, and the gap between the
// last run and the schedule is what tells them apart.
func (s *TaskScheduler) Diagnose(context.Context) []string {
	now := s.opts.Now()
	var out []string
	for _, e := range s.entries {
		e.mu.Lock()
		lastRun, lastError := e.lastRun, e.lastError
		e.mu.Unlock()

		if lastError != "" {
			out = append(out, fmt.Sprintf("The scheduled task %s failed on its last run: %s", e.task.ID, lastError))
		}
		if lastRun.IsZero() {
			// Nothing to say: the process may have started a minute ago.
			continue
		}
		expected := e.cron.GetNextRunDate(lastRun)
		if expected.IsZero() {
			continue
		}
		if second := e.cron.GetNextRunDate(expected); !second.IsZero() && now.After(second) {
			out = append(out, fmt.Sprintf(
				"The scheduled task %s last ran %s and was due at %s. Is the scheduler running?",
				e.task.ID, lastRun.Format(time.RFC3339), expected.Format(time.RFC3339)))
		}
	}
	return out
}

// report logs what the runner refuses to run: a per-tenant task with no
// resolver, a resolver that failed, and the outer line of a run that failed.
func (s *TaskScheduler) report(ctx context.Context) Listener {
	return func(event any) {
		failed, ok := event.(events.ScheduledTaskFailed)
		if !ok {
			return
		}
		logger := log.For(ctx).With("component", "scheduler", "task", failed.Task.GetSummaryForDisplay())
		if task, ok := failed.Task.(*Event); ok {
			if tenant := task.Grant().Subject().Tenant; tenant != "" {
				logger = logger.With("tenant", tenant)
			}
		}
		logger.Error("task failed", "error", failed.Exception)
	}
}

// work is what a task's event calls, with the Grant the Runner built from the
// task's action and the tenant it was expanded for.
func (s *TaskScheduler) work(e *taskEntry) Callback {
	return func(ctx context.Context, g auth.Grant) error {
		return s.runOne(ctx, e, g, s.window(ctx))
	}
}

// runOne executes a task after claiming its occurrence, when it is a Singleton
// and there is somewhere to claim it.
func (s *TaskScheduler) runOne(ctx context.Context, e *taskEntry, g auth.Grant, at time.Time) error {
	if !e.task.Singleton || s.opts.Locks == nil {
		return s.execute(ctx, e, g, at)
	}

	// The claim has to outlive the minute it names, or a replica whose clock or
	// tick runs late finds the key free and repeats the occurrence -- the
	// failure a lock released at the end of the work leaves open. An hour is
	// far past any tick that is still a tick, and the floor rises to the
	// timeout so a run never outlasts its own claim. The claim is never
	// released: its expiry is the boundary of the occurrence.
	ttl := max(time.Hour, e.task.Timeout)
	err := s.opts.Locks.Lock(TaskOccurrenceName(e.task.ID, g.Subject().Tenant, at), ttl).Acquire(ctx)
	switch {
	case err == nil:
		return s.execute(ctx, e, g, at)
	case errors.Is(err, cache.ErrLocked):
		return nil
	default:
		return err
	}
}

// TaskOccurrenceName is the claim one task takes for one tenant in one UTC
// minute: sched:<tenant>:<task-id>:<minute-epoch>.
//
// The id is the task's identity. Its action, its spec and whatever its Run
// captured are deliberately not part of the name, so two variants of one task
// declare two ids. The tenant is part of it, because one occurrence of a
// per-tenant task is one run per tenant.
func TaskOccurrenceName(taskID, tenant string, at time.Time) string {
	return fmt.Sprintf("sched:%s:%s:%d", tenant, taskID, at.UTC().Truncate(time.Minute).Unix())
}

// execute is the run itself: the timeout, the Collector, the log and the
// bookkeeping.
func (s *TaskScheduler) execute(ctx context.Context, e *taskEntry, g auth.Grant, at time.Time) error {
	runCtx, cancel := context.WithTimeout(ctx, e.task.Timeout)
	defer cancel()

	id := fmt.Sprintf("%s@%d", e.task.ID, at.Unix())
	var collector *log.Collector
	if s.opts.Recorder != nil {
		collector = log.NewCollector(id)
		runCtx = log.WithCollector(runCtx, collector)
	}

	logger := log.For(runCtx).With("component", "scheduler", "task", e.task.ID, "tenant", g.Subject().Tenant)
	runCtx = log.Into(runCtx, logger)

	start := time.Now()
	err := e.task.Run(runCtx, g)
	duration := time.Since(start)

	if collector != nil {
		// Method and Path name the task rather than a route, so the console
		// reads "task billing.nightly" beside "GET /invoices".
		s.opts.Recorder.Record(log.Recorded{
			RequestID: id,
			Method:    "task",
			Path:      e.task.ID,
			Duration:  duration,
			At:        start,
			Collector: collector,
		})
	}

	e.mu.Lock()
	e.lastRun = at
	e.lastError = ""
	if err != nil {
		e.lastError = err.Error()
	}
	e.mu.Unlock()

	if err != nil {
		logger.Error("task failed", "duration_ms", duration.Milliseconds(), "queries", collector.QueryCount(), "error", err)
		return err
	}
	logger.Info("task done",
		"duration_ms", duration.Milliseconds(),
		"queries", collector.QueryCount(),
		"sql_ms", collector.QueryTime().Milliseconds())
	return nil
}

// taskWindowKey is how the minute being fired reaches the run. It travels in
// the context because the call between them is the Runner's, which hands the
// callback nothing but a context and a Grant -- and the claim is named after
// the window.
type taskWindowKey struct{}

// withTaskWindow marks the context with the minute being fired.
func withTaskWindow(ctx context.Context, at time.Time) context.Context {
	return context.WithValue(ctx, taskWindowKey{}, at)
}

// window reads the minute being fired, and falls back to the clock for a run
// that reached the callback some other way.
func (s *TaskScheduler) window(ctx context.Context) time.Time {
	if at, ok := ctx.Value(taskWindowKey{}).(time.Time); ok {
		return at
	}
	return s.opts.Now()
}
