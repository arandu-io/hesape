package scheduler_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/scheduler"
)

func task(id string, run func(context.Context, auth.Grant) error) scheduler.Task {
	return scheduler.Task{
		ID: id, Spec: "* * * * *", Action: auth.Action(id),
		Timeout: time.Second, Run: run,
	}
}

// locks issues real locks over the in-memory store, which is what the kv
// adapter is a second implementation of. Nothing here is faked: the scheduler
// asks a cache.Locks for the window and honors the answer.
func locks() *cache.Locks { return cache.NewLocks(cache.NewArrayStore()) }

func TestATaskRunsUnderItsGrant(t *testing.T) {
	var got auth.Grant
	tk := task("billing.close", func(_ context.Context, g auth.Grant) error {
		got = g
		return nil
	})
	tk.Scope = scheduler.PerTenant

	s, err := scheduler.New([]scheduler.Task{tk}, scheduler.Options{
		Tenants: func(context.Context) ([]string, error) { return []string{"t-1"}, nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Tick(context.Background(), at("2026-08-03T13:00:00Z"))

	if got.Action() != "billing.close" || auth.Tenant(got) != "t-1" {
		t.Fatalf("the task ran under %q for %q", got.Action(), auth.Tenant(got))
	}
	// The Grant passes for its own action and nothing else, which is what makes
	// the scheduler no different from a request.
	if err := got.Check("billing.close"); err != nil {
		t.Errorf("the Grant fails its own action: %v", err)
	}
	if err := got.Check("billing.delete"); err == nil {
		t.Error("the Grant passed for an action it was not issued for")
	}
}

// TestAGlobalTaskCannotReachTenantData is RULE 14 meeting the scheduler, and
// the answer is the strict one: SystemGrant refuses an empty tenant, so a
// Global task holds the zero Grant and cannot pass any Check.
//
// That is a constraint, not an oversight. Global work is cleaning temporary
// files, warming a cache, checking a certificate -- none of which reads a
// customer's rows. A task that needs to read them is per tenant, and saying so
// is the whole point.
func TestAGlobalTaskCannotReachTenantData(t *testing.T) {
	var got auth.Grant
	tk := task("cache.warm", func(_ context.Context, g auth.Grant) error {
		got = g
		return nil
	})
	// Global is the default scope, stated here because it is the subject.
	tk.Scope = scheduler.Global

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{})
	s.Tick(context.Background(), at("2026-08-03T13:00:00Z"))

	if auth.Tenant(got) != "" {
		t.Fatalf("a global task got a tenant: %q", auth.Tenant(got))
	}
	if err := got.Check("cache.warm"); err == nil {
		t.Fatal("a global task holds a Grant that passes Check, and could read a tenant's rows")
	}
}

// TestAPerTenantTaskExpands: one lock and one Grant per tenant, which is what
// keeps a task from reading across customers.
func TestAPerTenantTaskExpands(t *testing.T) {
	var mu sync.Mutex
	var tenants []string

	tk := task("billing.close", func(_ context.Context, g auth.Grant) error {
		mu.Lock()
		defer mu.Unlock()
		tenants = append(tenants, auth.Tenant(g))
		return nil
	})
	tk.Scope = scheduler.PerTenant

	s, err := scheduler.New([]scheduler.Task{tk}, scheduler.Options{
		Tenants: func(context.Context) ([]string, error) { return []string{"t-1", "t-2"}, nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Tick(context.Background(), at("2026-08-03T13:00:00Z"))

	if len(tenants) != 2 || tenants[0] != "t-1" || tenants[1] != "t-2" {
		t.Fatalf("ran for %v, want one run per tenant", tenants)
	}
}

// TestAPerTenantTaskWithNoResolverDoesNotRunSilently: a task that never fires
// is the kind of thing found months later, and only if somebody goes looking.
func TestAPerTenantTaskWithNoResolverDoesNotRunSilently(t *testing.T) {
	ran := false
	tk := task("billing.close", func(context.Context, auth.Grant) error {
		ran = true
		return nil
	})
	tk.Scope = scheduler.PerTenant

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{})
	s.Tick(context.Background(), at("2026-08-03T13:00:00Z"))

	if ran {
		t.Fatal("a per-tenant task ran with no tenant")
	}
}

// TestOnlyOneReplicaRunsASingleton is what the lock is for: with N replicas, a
// task scheduled every minute runs N times unless exactly one wins.
func TestOnlyOneReplicaRunsASingleton(t *testing.T) {
	var mu sync.Mutex
	runs := 0

	// One issuer over one store, like one Redis shared by two pods.
	shared := locks()

	build := func() *scheduler.Scheduler {
		tk := task("billing.close", func(context.Context, auth.Grant) error {
			mu.Lock()
			defer mu.Unlock()
			runs++
			return nil
		})
		tk.Singleton = true
		// The timeout is also the ttl of the window mark. A minute, so a slow
		// machine cannot let the mark expire between the two ticks below.
		tk.Timeout = time.Minute
		s, err := scheduler.New([]scheduler.Task{tk}, scheduler.Options{Locks: shared})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return s
	}

	first, second := build(), build()

	window := at("2026-08-03T13:00:00Z")
	first.Tick(context.Background(), window)
	second.Tick(context.Background(), window)

	if runs != 1 {
		t.Fatalf("the task ran %d times in one window, want 1", runs)
	}
}

// TestANonSingletonRunsEverywhere: opting out has to actually opt out, or the
// flag is decoration.
func TestANonSingletonRunsEverywhere(t *testing.T) {
	var mu sync.Mutex
	runs := 0

	shared := locks()
	build := func() *scheduler.Scheduler {
		tk := task("cache.warm", func(context.Context, auth.Grant) error {
			mu.Lock()
			defer mu.Unlock()
			runs++
			return nil
		})
		s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{Locks: shared})
		return s
	}

	window := at("2026-08-03T13:00:00Z")
	build().Tick(context.Background(), window)
	build().Tick(context.Background(), window)

	if runs != 2 {
		t.Fatalf("a non-singleton ran %d times on two replicas, want 2", runs)
	}
}

// TestTheLockIsPerWindow: two replicas a second apart still contend for the
// same lock, and the next minute is a new one.
func TestTheLockIsPerWindow(t *testing.T) {
	var mu sync.Mutex
	runs := 0

	tk := task("billing.close", func(context.Context, auth.Grant) error {
		mu.Lock()
		defer mu.Unlock()
		runs++
		return nil
	})
	tk.Singleton = true
	tk.Timeout = time.Minute

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{Locks: locks()})

	s.Tick(context.Background(), at("2026-08-03T13:00:10Z"))
	s.Tick(context.Background(), at("2026-08-03T13:00:50Z")) // same minute
	s.Tick(context.Background(), at("2026-08-03T13:01:00Z")) // the next one

	if runs != 2 {
		t.Fatalf("ran %d times across two windows, want 2", runs)
	}
}

// TestALockedWindowIsNotAnError: another holder is the lock working, and
// reporting it as a failure would page somebody every minute.
func TestALockedWindowIsNotAnError(t *testing.T) {
	runs := 0
	tk := task("billing.close", func(context.Context, auth.Grant) error {
		runs++
		return nil
	})
	tk.Singleton = true
	tk.Timeout = time.Minute

	now := at("2026-08-03T13:00:00Z")
	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{
		Locks: locks(),
		Now:   func() time.Time { return now },
	})

	if err := s.RunNow(context.Background(), "billing.close", ""); err != nil {
		t.Fatalf("the first run: %v", err)
	}
	if err := s.RunNow(context.Background(), "billing.close", ""); err != nil {
		t.Fatalf("a window somebody else holds = %v, want no error", err)
	}
	if runs != 1 {
		t.Fatalf("the task ran %d times in one window, want 1", runs)
	}
}

func TestNewRefusesTasksThatCannotWork(t *testing.T) {
	valid := task("ok", func(context.Context, auth.Grant) error { return nil })

	for _, c := range []struct {
		name  string
		tasks []scheduler.Task
	}{
		{"no id", []scheduler.Task{{Spec: "* * * * *", Run: valid.Run}}},
		{"no run", []scheduler.Task{{ID: "x", Spec: "* * * * *"}}},
		{"bad spec", []scheduler.Task{{ID: "x", Spec: "nope", Run: valid.Run}}},
		{"duplicate id", []scheduler.Task{valid, valid}},
	} {
		if _, err := scheduler.New(c.tasks, scheduler.Options{}); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

// TestRunNowUsesTheSamePath: a manual run that took a different route would be
// a back door, and the two would drift.
func TestRunNowUsesTheSamePath(t *testing.T) {
	var got auth.Grant
	tk := task("billing.close", func(_ context.Context, g auth.Grant) error {
		got = g
		return nil
	})
	tk.Spec = "0 3 * * *" // not now
	tk.Scope = scheduler.PerTenant

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{})

	if err := s.RunNow(context.Background(), "billing.close", "t-9"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if auth.Tenant(got) != "t-9" {
		t.Errorf("ran for %q", auth.Tenant(got))
	}

	// A per-tenant task with no tenant is a mistake worth naming.
	if err := s.RunNow(context.Background(), "billing.close", ""); err == nil {
		t.Error("a per-tenant task ran with no tenant")
	}
	if err := s.RunNow(context.Background(), "nope", ""); err == nil {
		t.Error("an unknown id ran")
	}
}

func TestListReportsTheNextRun(t *testing.T) {
	tk := task("billing.close", func(context.Context, auth.Grant) error { return nil })
	tk.Spec = "0 3 * * *"
	tk.Singleton = true

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{
		Now: func() time.Time { return at("2026-08-03T13:00:00Z") },
	})

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("listed %d tasks", len(list))
	}
	if list[0].ID != "billing.close" || list[0].Spec != "0 3 * * *" || !list[0].Singleton {
		t.Fatalf("listed %+v", list[0])
	}
	if want := at("2026-08-04T03:00:00Z"); !list[0].Next.Equal(want) {
		t.Errorf("next = %s, want %s", list[0].Next, want)
	}
}

// TestAFailedTaskIsDiagnosed: a task that fails silently every night is the
// worst kind, because the first sign is the thing it was supposed to do never
// having happened.
func TestAFailedTaskIsDiagnosed(t *testing.T) {
	tk := task("billing.close", func(context.Context, auth.Grant) error {
		return errors.New("the ledger is locked")
	})

	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{})
	if got := s.Diagnose(context.Background()); len(got) != 0 {
		t.Fatalf("a scheduler that has not run yet diagnosed %v", got)
	}

	s.Tick(context.Background(), at("2026-08-03T13:00:00Z"))

	diagnosis := s.Diagnose(context.Background())
	if len(diagnosis) == 0 {
		t.Fatal("a failed task produced no diagnosis")
	}
	if !strings.Contains(diagnosis[0], "the ledger is locked") {
		t.Errorf("the diagnosis does not say why: %q", diagnosis[0])
	}
}

// TestAnOverdueTaskIsDiagnosed: a scheduler that stopped looks exactly like one
// with nothing to do.
func TestAnOverdueTaskIsDiagnosed(t *testing.T) {
	tk := task("billing.close", func(context.Context, auth.Grant) error { return nil })

	now := at("2026-08-03T13:00:00Z")
	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{
		Now: func() time.Time { return now },
	})

	// It ran an hour ago and the schedule is every minute.
	s.Tick(context.Background(), at("2026-08-03T12:00:00Z"))

	diagnosis := s.Diagnose(context.Background())
	if len(diagnosis) == 0 {
		t.Fatal("a task an hour overdue produced no diagnosis")
	}
	if !strings.Contains(diagnosis[0], "Is the scheduler running?") {
		t.Errorf("the diagnosis does not say what to check: %q", diagnosis[0])
	}
}

func TestStartAndStop(t *testing.T) {
	tk := task("noop", func(context.Context, auth.Grant) error { return nil })
	s, _ := scheduler.New([]scheduler.Task{tk}, scheduler.Options{})

	s.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stopping twice, and stopping one that never started, are both no-ops:
	// shutdown paths get called from more places than anyone expects.
	if err := s.Stop(ctx); err != nil {
		t.Errorf("the second Stop: %v", err)
	}
	fresh, _ := scheduler.New(nil, scheduler.Options{})
	if err := fresh.Stop(ctx); err != nil {
		t.Errorf("stopping a scheduler that never started: %v", err)
	}
}

// TestAModuleDeclaresItsTasks: same shape as a module's migrations, so a module
// declares its scheduled work the way it declares everything else.
//
// The kernel that collects them is not in this collection yet; the half that
// is, and the half that third-party modules implement, is the interface.
func TestAModuleDeclaresItsTasks(t *testing.T) {
	var m scheduler.Schedulable = &schedulingModule{}

	tasks := m.Schedule()
	if len(tasks) != 1 || tasks[0].ID != "billing.close" {
		t.Fatalf("declared %+v", tasks)
	}
}

// TestBootFailsOnAnUnparseableSpec: the application does not start with a task
// that would silently never run. That is "fail fast at boot" applied to
// schedules.
func TestBootFailsOnAnUnparseableSpec(t *testing.T) {
	m := scheduler.NewModule([]scheduler.Task{{
		ID: "x", Spec: "not a cron", Run: func(context.Context, auth.Grant) error { return nil },
	}}, scheduler.Options{})

	if err := m.Boot(context.Background()); err == nil {
		t.Fatal("the module booted with an unparseable spec")
	}
}

// TestAModuleWithNoTasksIsInert: registering the scheduler in an application
// that schedules nothing must not start a loop or fail.
func TestAModuleWithNoTasksIsInert(t *testing.T) {
	m := scheduler.NewModule(nil, scheduler.Options{})

	if err := m.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := m.Diagnose(context.Background()); len(got) != 0 {
		t.Errorf("diagnosed %v", got)
	}
	if m.Scheduler() != nil {
		t.Error("a module with no tasks handed out a scheduler")
	}
}

// TestTheModuleRunsWhatItBooted: Boot parses, Start runs the loop, Close waits.
func TestTheModuleRunsWhatItBooted(t *testing.T) {
	m := scheduler.NewModule([]scheduler.Task{
		task("billing.close", func(context.Context, auth.Grant) error { return nil }),
	}, scheduler.Options{})

	if err := m.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if m.Scheduler() == nil {
		t.Fatal("Boot produced no scheduler")
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m.Name() != "scheduler" {
		t.Errorf("the module is named %q", m.Name())
	}
}

// schedulingModule is a module that declares a task and nothing else.
type schedulingModule struct{}

func (*schedulingModule) Name() string { return "billing" }
func (*schedulingModule) Schedule() []scheduler.Task {
	return []scheduler.Task{{
		ID: "billing.close", Spec: "0 3 * * *", Action: "billing.close",
		Run: func(context.Context, auth.Grant) error { return nil },
	}}
}
