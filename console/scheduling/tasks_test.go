package scheduling_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/console/scheduling"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/log"
)

// threeAM is a minute "0 3 * * *" is due in.
var threeAM = time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)

// runs records every run of a task: the tenant and the action of its Grant.
type runs struct {
	mu   sync.Mutex
	seen []string
}

func (r *runs) task(id string, scope foundation.Scope, singleton bool) foundation.Task {
	return foundation.Task{
		ID: id, Spec: "0 3 * * *", Scope: scope, Singleton: singleton, Action: "invoice.close",
		Run: func(_ context.Context, g auth.Grant) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.seen = append(r.seen, string(g.Action())+"@"+g.Subject().Tenant)
			return nil
		},
	}
}

func (r *runs) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.seen)
}

func tenants(names ...string) scheduling.Tenants {
	return func(context.Context) ([]string, error) { return names, nil }
}

func newTaskScheduler(t *testing.T, tasks []foundation.Task, opts scheduling.TaskOptions) *scheduling.TaskScheduler {
	t.Helper()
	if opts.Now == nil {
		opts.Now = func() time.Time { return threeAM }
	}
	s, err := scheduling.NewTaskScheduler(tasks, opts)
	if err != nil {
		t.Fatalf("NewTaskScheduler: %v", err)
	}
	return s
}

// TestNewTaskSchedulerRefusesTasksThatCannotWork: a task with no id cannot be
// claimed or run by hand, two with one id share a claim, a task with no Run
// does nothing, and a spec that does not parse never fires -- each is refused
// at construction rather than found the night it should have run.
func TestNewTaskSchedulerRefusesTasksThatCannotWork(t *testing.T) {
	ok := func(context.Context, auth.Grant) error { return nil }
	cases := map[string][]foundation.Task{
		"no id":        {{Spec: "* * * * *", Run: ok}},
		"repeated id":  {{ID: "a", Spec: "* * * * *", Run: ok}, {ID: "a", Spec: "* * * * *", Run: ok}},
		"no run":       {{ID: "a", Spec: "* * * * *"}},
		"invalid spec": {{ID: "a", Spec: "every night", Run: ok}},
	}
	for name, tasks := range cases {
		if _, err := scheduling.NewTaskScheduler(tasks, scheduling.TaskOptions{}); err == nil {
			t.Errorf("%s: NewTaskScheduler accepted it", name)
		}
	}
}

// TestAPerTenantTaskRunsOncePerTenantUnderItsGrant: the tenant comes from the
// expansion and the action from the task, so each run reaches exactly one
// customer's rows and only what the task declared.
func TestAPerTenantTaskRunsOncePerTenantUnderItsGrant(t *testing.T) {
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("billing.close", foundation.PerTenant, false)},
		scheduling.TaskOptions{Tenants: tenants("acme", "globex")})

	s.Tick(context.Background(), threeAM)

	got := r.all()
	slices.Sort(got)
	if strings.Join(got, ",") != "invoice.close@acme,invoice.close@globex" {
		t.Fatalf("runs = %v, want one per tenant under invoice.close", got)
	}
}

// TestAGlobalTaskRunsWithNoTenant: global work gets the zero Grant -- no
// tenant and no action, because a system Grant refuses an empty tenant -- so it
// cannot pass a Check and cannot reach a customer's rows.
func TestAGlobalTaskRunsWithNoTenant(t *testing.T) {
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("cache.warm", foundation.Global, false)}, scheduling.TaskOptions{})

	s.Tick(context.Background(), threeAM)

	if got := r.all(); len(got) != 1 || got[0] != "@" {
		t.Fatalf("runs = %v, want one under the zero Grant", got)
	}
}

// TestAPerTenantTaskWithNoResolverDoesNotRun: with nothing to expand it, the
// task does not run for some tenant picked by accident.
func TestAPerTenantTaskWithNoResolverDoesNotRun(t *testing.T) {
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("billing.close", foundation.PerTenant, false)}, scheduling.TaskOptions{})

	s.Tick(context.Background(), threeAM)

	if got := r.all(); len(got) != 0 {
		t.Fatalf("runs = %v, want none", got)
	}
}

// TestASingletonOccurrenceRunsOnceAcrossReplicas: two replicas over one shared
// store fire the same minute, and the claim lets exactly one of them run it --
// once per tenant, and again in the next window.
func TestASingletonOccurrenceRunsOnceAcrossReplicas(t *testing.T) {
	var r runs
	locks := cache.NewLocks(cache.NewArrayStore())
	opts := scheduling.TaskOptions{Locks: locks, Tenants: tenants("acme", "globex")}
	task := r.task("billing.close", foundation.PerTenant, true)

	first := newTaskScheduler(t, []foundation.Task{task}, opts)
	second := newTaskScheduler(t, []foundation.Task{task}, opts)

	first.Tick(context.Background(), threeAM)
	second.Tick(context.Background(), threeAM)
	if got := r.all(); len(got) != 2 {
		t.Fatalf("runs after two replicas fired one minute = %v, want one per tenant", got)
	}

	tomorrow := threeAM.Add(24 * time.Hour)
	second.Tick(context.Background(), tomorrow)
	if got := r.all(); len(got) != 4 {
		t.Fatalf("runs after the next window = %v, want the next occurrence to run", got)
	}
}

// TestANonSingletonRunsOnEveryReplica: Singleton false is the task saying it is
// harmless to run N times, and the claim is not taken for it.
func TestANonSingletonRunsOnEveryReplica(t *testing.T) {
	var r runs
	opts := scheduling.TaskOptions{Locks: cache.NewLocks(cache.NewArrayStore())}
	task := r.task("cache.warm", foundation.Global, false)

	newTaskScheduler(t, []foundation.Task{task}, opts).Tick(context.Background(), threeAM)
	newTaskScheduler(t, []foundation.Task{task}, opts).Tick(context.Background(), threeAM)

	if got := r.all(); len(got) != 2 {
		t.Fatalf("runs = %v, want one per replica", got)
	}
}

// TestNoLocksIsOneReplica: without an issuer there is nothing to claim through,
// so each scheduler runs its Singleton -- the single-replica behaviour, stated.
func TestNoLocksIsOneReplica(t *testing.T) {
	var r runs
	task := r.task("billing.close", foundation.Global, true)

	newTaskScheduler(t, []foundation.Task{task}, scheduling.TaskOptions{}).Tick(context.Background(), threeAM)
	newTaskScheduler(t, []foundation.Task{task}, scheduling.TaskOptions{}).Tick(context.Background(), threeAM)

	if got := r.all(); len(got) != 2 {
		t.Fatalf("runs = %v, want each scheduler to run it", got)
	}
}

// TestTheClaimOutlivesTheMinuteAndTheTimeout: the claim is never released, and
// its ttl is at least an hour and never shorter than the task's own timeout, so
// a late replica cannot repeat the occurrence and a run cannot outlast its
// claim.
func TestTheClaimOutlivesTheMinuteAndTheTimeout(t *testing.T) {
	store := cache.NewArrayStore()
	locks := cache.NewLocks(store)
	var r runs
	task := r.task("billing.close", foundation.Global, true)
	task.Timeout = 3 * time.Hour

	newTaskScheduler(t, []foundation.Task{task}, scheduling.TaskOptions{Locks: locks}).Tick(context.Background(), threeAM)

	name := scheduling.TaskOccurrenceName("billing.close", "", threeAM)
	held := locks.Lock(name, time.Minute).Acquire(context.Background())
	if !errors.Is(held, cache.ErrLocked) {
		t.Fatalf("after the run the claim %s answered %v, want it still held", name, held)
	}
}

// ttlRecorder is a lock store that remembers the ttl each claim asked for.
type ttlRecorder struct {
	*cache.ArrayStore
	mu   sync.Mutex
	ttls []time.Duration
}

func (r *ttlRecorder) AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	r.ttls = append(r.ttls, ttl)
	r.mu.Unlock()
	return r.ArrayStore.AcquireLock(ctx, key, token, ttl)
}

// TestTheClaimTTLIsAtLeastAnHour: a task whose timeout is ten seconds still
// claims its occurrence for an hour, so a replica whose tick runs a minute late
// finds the claim and does not repeat the run; a task whose timeout is longer
// claims for that long.
func TestTheClaimTTLIsAtLeastAnHour(t *testing.T) {
	store := &ttlRecorder{ArrayStore: cache.NewArrayStore()}
	short := foundation.Task{ID: "short", Spec: "0 3 * * *", Singleton: true, Timeout: 10 * time.Second,
		Run: func(context.Context, auth.Grant) error { return nil }}
	long := foundation.Task{ID: "long", Spec: "0 3 * * *", Singleton: true, Timeout: 2 * time.Hour,
		Run: func(context.Context, auth.Grant) error { return nil }}

	newTaskScheduler(t, []foundation.Task{long, short}, scheduling.TaskOptions{Locks: cache.NewLocks(store)}).
		Tick(context.Background(), threeAM)

	store.mu.Lock()
	defer store.mu.Unlock()
	slices.Sort(store.ttls)
	if len(store.ttls) != 2 || store.ttls[0] != time.Hour || store.ttls[1] != 2*time.Hour {
		t.Fatalf("claims asked for %v, want an hour for the short task and two for the long one", store.ttls)
	}
}

// TestTheOccurrenceNameSeparatesTasksTenantsAndMinutes: the name is the
// identity of one occurrence, so it differs by task, by tenant and by minute,
// and not by the seconds inside one minute.
func TestTheOccurrenceNameSeparatesTasksTenantsAndMinutes(t *testing.T) {
	base := scheduling.TaskOccurrenceName("a", "acme", threeAM)
	if base != fmt.Sprintf("sched:acme:a:%d", threeAM.Unix()) {
		t.Fatalf("name = %q", base)
	}
	for _, other := range []string{
		scheduling.TaskOccurrenceName("b", "acme", threeAM),
		scheduling.TaskOccurrenceName("a", "globex", threeAM),
		scheduling.TaskOccurrenceName("a", "acme", threeAM.Add(time.Minute)),
	} {
		if other == base {
			t.Fatalf("%q names the same occurrence as %q", other, base)
		}
	}
	if scheduling.TaskOccurrenceName("a", "acme", threeAM.Add(42*time.Second)) != base {
		t.Fatal("two instants inside one minute name two occurrences")
	}
}

// TestRunNowTakesTheSamePath: a manual run is the task under its Grant, for
// the tenant named, and it takes the same claim -- so it cannot repeat an
// occurrence the loop already ran.
func TestRunNowTakesTheSamePath(t *testing.T) {
	var r runs
	locks := cache.NewLocks(cache.NewArrayStore())
	s := newTaskScheduler(t, []foundation.Task{r.task("billing.close", foundation.PerTenant, true)},
		scheduling.TaskOptions{Locks: locks, Tenants: tenants("acme")})

	if err := s.RunNow(context.Background(), "billing.close", ""); err == nil {
		t.Fatal("a per-tenant task ran by hand with no tenant")
	}
	if err := s.RunNow(context.Background(), "billing.close", "globex"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if got := r.all(); len(got) != 1 || got[0] != "invoice.close@globex" {
		t.Fatalf("runs = %v, want one for globex", got)
	}

	if err := s.RunNow(context.Background(), "billing.close", "globex"); err != nil {
		t.Fatalf("second RunNow: %v", err)
	}
	if got := r.all(); len(got) != 1 {
		t.Fatalf("runs = %v: a second manual run in the same minute repeated the occurrence", got)
	}

	if err := s.RunNow(context.Background(), "nothing.here", ""); err == nil {
		t.Fatal("RunNow of an id nobody declared succeeded")
	}
}

// TestRunNowReportsWhatTheTaskFailedWith, and List and Diagnose remember it:
// a task that failed is the reason somebody opens either.
func TestRunNowReportsWhatTheTaskFailedWith(t *testing.T) {
	boom := errors.New("the ledger is locked")
	s := newTaskScheduler(t, []foundation.Task{{
		ID: "ledger.close", Spec: "0 3 * * *",
		Run: func(context.Context, auth.Grant) error { return boom },
	}}, scheduling.TaskOptions{})

	if err := s.RunNow(context.Background(), "ledger.close", ""); !errors.Is(err, boom) {
		t.Fatalf("RunNow = %v, want what the task failed with", err)
	}

	listed := s.List()
	if len(listed) != 1 || listed[0].LastError != boom.Error() || listed[0].LastRun.IsZero() {
		t.Fatalf("List() = %+v, want the last run and its error", listed)
	}
	diagnosis := strings.Join(s.Diagnose(context.Background()), "\n")
	if !strings.Contains(diagnosis, "ledger.close failed on its last run: the ledger is locked") {
		t.Fatalf("Diagnose() = %q, want the failure named", diagnosis)
	}
}

// TestATaskRunsUnderItsTimeout: a scheduled run with no bound is a run nothing
// ever stops, so the task's context carries its Timeout -- five minutes when
// it declared none.
func TestATaskRunsUnderItsTimeout(t *testing.T) {
	var deadlines []time.Duration
	record := func(ctx context.Context, _ auth.Grant) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("the task ran with no deadline")
			return nil
		}
		deadlines = append(deadlines, time.Until(deadline))
		return nil
	}
	s := newTaskScheduler(t, []foundation.Task{
		{ID: "a", Spec: "0 3 * * *", Timeout: 10 * time.Second, Run: record},
		{ID: "b", Spec: "0 3 * * *", Run: record},
	}, scheduling.TaskOptions{})

	s.Tick(context.Background(), threeAM)

	if len(deadlines) != 2 || deadlines[0] > 10*time.Second || deadlines[1] <= time.Minute || deadlines[1] > 5*time.Minute {
		t.Fatalf("deadlines = %v, want at most 10s for a and five minutes for b", deadlines)
	}
	if listed := s.List(); listed[1].Timeout != 5*time.Minute {
		t.Fatalf("List() timeout for b = %s, want the five minute default", listed[1].Timeout)
	}
}

// TestAFinishedRunReachesTheRecorder: a task is investigated on the same page
// as a request, named so the console reads "task <id>".
func TestAFinishedRunReachesTheRecorder(t *testing.T) {
	recorder := log.NewRecorder(4)
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("cache.warm", foundation.Global, false)},
		scheduling.TaskOptions{Recorder: recorder})

	s.Tick(context.Background(), threeAM)

	recent := recorder.Recent(4)
	if len(recent) != 1 || recent[0].Method != "task" || recent[0].Path != "cache.warm" || recent[0].Collector == nil {
		t.Fatalf("recorded %+v, want one task entry with its collector", recent)
	}
}

// TestListReportsTheNextRun and the shape schedule:list prints.
func TestListReportsTheNextRun(t *testing.T) {
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("billing.close", foundation.PerTenant, true)},
		scheduling.TaskOptions{Now: func() time.Time { return threeAM.Add(time.Minute) }})

	listed := s.List()
	want := threeAM.Add(24 * time.Hour)
	if len(listed) != 1 || !listed[0].Next.Equal(want) || listed[0].Scope != "per tenant" || !listed[0].Singleton || listed[0].Spec != "0 3 * * *" {
		t.Fatalf("List() = %+v, want billing.close per tenant, singleton, next at %s", listed, want)
	}
}

// TestAnOverdueTaskIsDiagnosed: two windows without a run is a scheduler that
// stopped, and it says so.
func TestAnOverdueTaskIsDiagnosed(t *testing.T) {
	now := threeAM
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("cache.warm", foundation.Global, false)},
		scheduling.TaskOptions{Now: func() time.Time { return now }})

	s.Tick(context.Background(), threeAM)
	if got := s.Diagnose(context.Background()); len(got) != 0 {
		t.Fatalf("Diagnose() right after a run = %v, want nothing", got)
	}

	now = threeAM.Add(49 * time.Hour)
	if got := strings.Join(s.Diagnose(context.Background()), "\n"); !strings.Contains(got, "Is the scheduler running?") {
		t.Fatalf("Diagnose() two windows late = %q", got)
	}
}

// TestTheModuleBootsTheSchedulerAndFailsOnABadSpec: Boot parses, so every
// command that boots catches a spec that would never fire.
func TestTheModuleBootsTheSchedulerAndFailsOnABadSpec(t *testing.T) {
	bad := scheduling.NewTaskModule([]foundation.Task{{ID: "a", Spec: "nightly", Run: func(context.Context, auth.Grant) error { return nil }}}, scheduling.TaskOptions{})
	if err := bad.Boot(context.Background()); err == nil {
		t.Fatal("Boot accepted a spec that does not parse")
	}

	empty := scheduling.NewTaskModule(nil, scheduling.TaskOptions{})
	if err := empty.Boot(context.Background()); err != nil || empty.Scheduler() != nil {
		t.Fatalf("a module with no tasks booted to %v, %v; want inert", empty.Scheduler(), err)
	}
	if err := empty.Start(context.Background()); err != nil {
		t.Fatalf("Start of an inert module: %v", err)
	}
	if err := empty.Close(context.Background()); err != nil {
		t.Fatalf("Close of an inert module: %v", err)
	}
}

// TestTheModuleIsNotCommandable: the kernel composes schedule:list and
// schedule:run itself, from TaskCommands, and a module answering them as well
// would declare the same names twice.
func TestTheModuleIsNotCommandable(t *testing.T) {
	var m foundation.Module = scheduling.NewTaskModule(nil, scheduling.TaskOptions{})
	if _, ok := m.(foundation.Commandable); ok {
		t.Fatal("TaskModule is foundation.Commandable, so its commands would be collected beside the kernel's")
	}
}

func runConsole(t *testing.T, c console.Command, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := c.Run(context.Background(), console.NewIO(c.Name, args, &out, &errOut, strings.NewReader("")))
	return out.String() + errOut.String(), err
}

// TestTaskCommandsListAndRunTheBootedScheduler: the two commands read the
// scheduler when they run, after the boot that built it, and schedule:run
// takes the task's id first and --tenant after.
func TestTaskCommandsListAndRunTheBootedScheduler(t *testing.T) {
	var r runs
	m := scheduling.NewTaskModule([]foundation.Task{r.task("billing.close", foundation.PerTenant, false)},
		scheduling.TaskOptions{Tenants: tenants("acme")})
	commands := scheduling.TaskCommands(m)

	byName := map[string]console.Command{}
	for _, c := range commands {
		byName[c.Name] = c
	}
	list, run := byName["schedule:list"], byName["schedule:run"]
	if list.Run == nil || run.Run == nil || len(commands) != 2 {
		t.Fatalf("TaskCommands answered %d commands, want schedule:list and schedule:run", len(commands))
	}

	if printed, err := runConsole(t, list); err != nil || !strings.Contains(printed, "no scheduled tasks") {
		t.Fatalf("schedule:list before boot printed %q (%v)", printed, err)
	}

	if err := m.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	printed, err := runConsole(t, list)
	if err != nil || !strings.Contains(printed, "billing.close") || !strings.Contains(printed, "per tenant") {
		t.Fatalf("schedule:list printed %q (%v)", printed, err)
	}

	if _, err := runConsole(t, run); err == nil {
		t.Fatal("schedule:run with no id succeeded")
	}
	if _, err := runConsole(t, run, "billing.close", "--tenant=acme"); err != nil {
		t.Fatalf("schedule:run billing.close --tenant=acme: %v", err)
	}
	if got := r.all(); len(got) != 1 || got[0] != "invoice.close@acme" {
		t.Fatalf("runs = %v, want one for acme", got)
	}
}

// TestStopEndsTheLoop: Stop cancels the loop and waits for it, and a Stop that
// did not cancel would hold the shutdown until its context gave up.
func TestStopEndsTheLoop(t *testing.T) {
	var r runs
	s := newTaskScheduler(t, []foundation.Task{r.task("cache.warm", foundation.Global, false)},
		scheduling.TaskOptions{Now: time.Now})

	s.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := scheduling.NewTaskModule(nil, scheduling.TaskOptions{}).Close(ctx); err != nil {
		t.Fatalf("Close of a module that never started: %v", err)
	}
}
