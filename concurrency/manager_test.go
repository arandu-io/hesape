package concurrency_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/concurrency"
)

// config is the two methods concurrency.Config asks for, over a flat map keyed
// by the dotted key. It is enough for the three keys the manager reads, and it
// keeps the test from importing the configuration package.
type config struct {
	mu    sync.Mutex
	items map[string]any
}

func newConfig(items map[string]any) *config {
	if items == nil {
		items = map[string]any{}
	}
	return &config{items: items}
}

func (c *config) Get(key string, def ...any) any {
	c.mu.Lock()
	defer c.mu.Unlock()

	if v, ok := c.items[key]; ok {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

func (c *config) Set(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = val
}

// countingDriver is what Extend registers: it records that it was reached and
// forwards to the package-level functions.
type countingDriver[T any] struct {
	runs   atomic.Int64
	defers atomic.Int64
}

func (d *countingDriver[T]) Run(ctx context.Context, tasks ...concurrency.Task[T]) ([]T, error) {
	d.runs.Add(1)
	return concurrency.Run(ctx, tasks...)
}

func (d *countingDriver[T]) Defer(ctx context.Context, tasks ...concurrency.Task[T]) func() error {
	d.defers.Add(1)
	return concurrency.Defer(ctx, tasks...)
}

func TestDeferRunsNothingUntilTheCallbackIsInvoked(t *testing.T) {
	t.Parallel()

	var ran atomic.Bool
	callback := concurrency.Defer(context.Background(), func(ctx context.Context) (int, error) {
		ran.Store(true)
		return 1, nil
	})

	// Nothing has started: Illuminate hands back a DeferredCallback and the
	// kernel invokes it after the response.
	time.Sleep(10 * time.Millisecond)
	if ran.Load() {
		t.Fatal("Defer started the task before the callback was invoked")
	}

	if err := callback(); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !ran.Load() {
		t.Fatal("the callback did not run the task")
	}
}

func TestDeferRunsAfterTheContextIsDone(t *testing.T) {
	t.Parallel()

	// The request the work came from is over by the time deferred work runs. A
	// callback that inherited the request context would never run at all.
	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Bool
	callback := concurrency.Defer(ctx, func(ctx context.Context) (int, error) {
		ran.Store(true)
		return 1, nil
	})
	cancel()

	if err := callback(); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !ran.Load() {
		t.Fatal("the deferred task did not run after its context was cancelled")
	}
}

func TestDeferRunsTheTasksAtTheSameTime(t *testing.T) {
	t.Parallel()

	const n = 3
	started := make(chan struct{}, n)
	release := make(chan struct{})

	tasks := make([]concurrency.Task[int], n)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) (int, error) {
			started <- struct{}{}
			<-release
			return 0, nil
		}
	}

	callback := concurrency.Defer(context.Background(), tasks...)

	done := make(chan error, 1)
	go func() { done <- callback() }()

	for range n {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Error("not every deferred task had started")
			close(release)
			return
		}
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("callback: %v", err)
	}
}

func TestDeferReturnsTheFirstFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	callback := concurrency.Defer(context.Background(), func(ctx context.Context) (int, error) {
		return 0, sentinel
	})

	if err := callback(); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %v", err, sentinel)
	}
}

func TestDeferTurnsAPanicIntoAnError(t *testing.T) {
	t.Parallel()

	callback := concurrency.Defer(context.Background(), func(ctx context.Context) (int, error) {
		panic("deferred work exploded")
	})

	err := callback()
	var panicErr *concurrency.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("got %v (%T), want a *PanicError", err, err)
	}
	if !strings.Contains(panicErr.Error(), "deferred work exploded") {
		t.Fatalf("the message lost the panic value: %s", panicErr)
	}
}

func TestDeferWithoutTasks(t *testing.T) {
	t.Parallel()

	if err := concurrency.Defer[int](context.Background())(); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestDeferCallbackRunsAgainWhenInvokedAgain(t *testing.T) {
	t.Parallel()

	var runs atomic.Int64
	callback := concurrency.Defer(context.Background(), func(ctx context.Context) (int, error) {
		runs.Add(1)
		return 0, nil
	})

	for range 2 {
		if err := callback(); err != nil {
			t.Fatalf("callback: %v", err)
		}
	}
	if got := runs.Load(); got != 2 {
		t.Fatalf("the task ran %d times, want 2", got)
	}
}

func TestManagerRunForwardsToTheDefaultDriver(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	got, err := m.Run(context.Background(),
		func(ctx context.Context) (int, error) { return 1, nil },
		func(ctx context.Context) (int, error) { return 2, nil },
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v, want [1 2]", got)
	}
}

func TestManagerDeferForwardsToTheDefaultDriver(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	var ran atomic.Bool
	callback, err := m.Defer(context.Background(), func(ctx context.Context) (int, error) {
		ran.Store(true)
		return 0, nil
	})
	if err != nil {
		t.Fatalf("Defer: %v", err)
	}
	if ran.Load() {
		t.Fatal("Defer started the task")
	}
	if err := callback(); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !ran.Load() {
		t.Fatal("the callback did not run the task")
	}
}

func TestZeroValueManagerIsUsable(t *testing.T) {
	t.Parallel()

	var m concurrency.Manager[int]
	if got := m.GetDefaultInstance(); got != concurrency.DriverProcess {
		t.Fatalf("got %q, want %q", got, concurrency.DriverProcess)
	}
	got, err := m.Run(context.Background(), func(ctx context.Context) (int, error) { return 7, nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("got %v, want [7]", got)
	}
}

func TestTheThreeDriverNamesReachTheSameDriver(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)

	process, err := m.Driver(concurrency.DriverProcess)
	if err != nil {
		t.Fatalf("Driver(process): %v", err)
	}
	for _, name := range []string{concurrency.DriverFork, concurrency.DriverSync} {
		d, err := m.Driver(name)
		if err != nil {
			t.Fatalf("Driver(%s): %v", name, err)
		}
		if d != process {
			t.Fatalf("Driver(%s) is not the driver Driver(process) answered with", name)
		}
	}
}

func TestTheThreeFactoriesAnswerWithTheSameDriver(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)

	process := m.CreateProcessDriver()
	if process == nil {
		t.Fatal("CreateProcessDriver answered with nothing")
	}
	if fork := m.CreateForkDriver(); fork != process {
		t.Fatal("CreateForkDriver is not the driver CreateProcessDriver answered with")
	}
	if sync := m.CreateSyncDriver(); sync != process {
		t.Fatal("CreateSyncDriver is not the driver CreateProcessDriver answered with")
	}

	// resolve() reaches the factory whose name the driver key spells, so what
	// Driver answers with has to be what the factory answers with.
	resolved, err := m.Driver(concurrency.DriverFork)
	if err != nil {
		t.Fatalf("Driver(fork): %v", err)
	}
	if resolved != process {
		t.Fatal("Driver(fork) did not answer with what CreateForkDriver builds")
	}
}

func TestAFactoryDriverRunsTasksInArgumentOrder(t *testing.T) {
	t.Parallel()

	// The last task finishes first. PHP's sync driver holds the order by
	// running the tasks in a row; here they run at the same time and the order
	// is held by where the result is written.
	done := make(chan struct{})
	got, err := concurrency.NewManager[string](nil).CreateSyncDriver().Run(context.Background(),
		func(ctx context.Context) (string, error) { <-done; return "first", nil },
		func(ctx context.Context) (string, error) { <-done; return "second", nil },
		func(ctx context.Context) (string, error) { close(done); return "third", nil },
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
		t.Fatalf("got %v, want [first second third]", got)
	}
}

func TestManagerRunReturnsResultsInArgumentOrder(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	got, err := concurrency.NewManager[int](nil).Run(context.Background(),
		func(ctx context.Context) (int, error) { <-done; return 1, nil },
		func(ctx context.Context) (int, error) { <-done; return 2, nil },
		func(ctx context.Context) (int, error) { close(done); return 3, nil },
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("got %v, want [1 2 3]", got)
	}
}

func TestManagerRunTurnsAPanicIntoAnErrorInsteadOfEndingTheProcess(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)

	_, err := m.Run(context.Background(),
		func(ctx context.Context) (int, error) { return 1, nil },
		func(ctx context.Context) (int, error) { panic("held wrong") },
	)

	var panicErr *concurrency.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("got %v (%T), want a *concurrency.PanicError", err, err)
	}
	if panicErr.Value != "held wrong" {
		t.Fatalf("value is %v, want the recovered value", panicErr.Value)
	}

	// Reaching this line at all is half the assertion: an unguarded panic in a
	// goroutine ends the test binary rather than failing the test. The other
	// half is that the manager is still usable afterwards.
	got, err := m.Run(context.Background(), func(ctx context.Context) (int, error) { return 7, nil })
	if err != nil {
		t.Fatalf("Run after a panic: %v", err)
	}
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("got %v, want [7]", got)
	}
}

func TestDriverWithoutANameIsTheDefaultInstance(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{"concurrency.default": "sync"}))

	byName, err := m.Driver("sync")
	if err != nil {
		t.Fatalf("Driver(sync): %v", err)
	}
	def, err := m.Driver()
	if err != nil {
		t.Fatalf("Driver(): %v", err)
	}
	if def != byName {
		t.Fatal("Driver() did not answer with the default instance")
	}
	// An empty name is no name, which is what `$pipeline ?: 'default'` and
	// `$name ?: $this->getDefaultInstance()` do with the falsy string.
	empty, err := m.Driver("")
	if err != nil {
		t.Fatalf("Driver(\"\"): %v", err)
	}
	if empty != byName {
		t.Fatal("an empty name did not answer with the default instance")
	}
}

func TestDefaultInstanceFallsBackThroughBothKeys(t *testing.T) {
	t.Parallel()

	cfg := newConfig(nil)
	m := concurrency.NewManager[int](cfg)
	if got := m.GetDefaultInstance(); got != concurrency.DriverProcess {
		t.Fatalf("with no keys: got %q, want %q", got, concurrency.DriverProcess)
	}

	// concurrency.driver is the older spelling, and it is read second.
	cfg.Set("concurrency.driver", "fork")
	if got := m.GetDefaultInstance(); got != "fork" {
		t.Fatalf("with concurrency.driver: got %q, want fork", got)
	}

	cfg.Set("concurrency.default", "sync")
	if got := m.GetDefaultInstance(); got != "sync" {
		t.Fatalf("with both keys: got %q, want sync", got)
	}
}

func TestSetDefaultInstanceWritesBothKeys(t *testing.T) {
	t.Parallel()

	cfg := newConfig(nil)
	m := concurrency.NewManager[int](cfg)
	m.SetDefaultInstance("fork")

	if got := cfg.Get("concurrency.default"); got != "fork" {
		t.Fatalf("concurrency.default: got %v, want fork", got)
	}
	if got := cfg.Get("concurrency.driver"); got != "fork" {
		t.Fatalf("concurrency.driver: got %v, want fork", got)
	}
	if got := m.GetDefaultInstance(); got != "fork" {
		t.Fatalf("GetDefaultInstance: got %q, want fork", got)
	}
}

func TestSetDefaultInstanceWithoutAConfiguration(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	m.SetDefaultInstance("sync")

	if got := m.GetDefaultInstance(); got != "sync" {
		t.Fatalf("got %q, want sync", got)
	}
}

func TestGetInstanceConfigFallsBackToTheName(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	cfg := m.GetInstanceConfig("fork")
	if got := cfg["driver"]; got != "fork" {
		t.Fatalf("got %v, want fork", got)
	}
}

func TestGetInstanceConfigReadsTheConfiguredSettings(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{
		"concurrency.driver.reports": map[string]any{"driver": "fork", "tasks": 4},
	}))

	cfg := m.GetInstanceConfig("reports")
	if got := cfg["driver"]; got != "fork" {
		t.Fatalf("driver: got %v, want fork", got)
	}
	if got := cfg["tasks"]; got != 4 {
		t.Fatalf("tasks: got %v, want 4", got)
	}
}

func TestAnInstanceIsResolvedByItsDriverKeyAndNotByItsName(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{
		"concurrency.driver.reports": map[string]any{"driver": "fork"},
	}))

	byInstance, err := m.Driver("reports")
	if err != nil {
		t.Fatalf("Driver(reports): %v", err)
	}
	byDriver, err := m.Driver("fork")
	if err != nil {
		t.Fatalf("Driver(fork): %v", err)
	}
	if byInstance != byDriver {
		t.Fatal("the instance was not resolved by its driver key")
	}
}

func TestAnInstanceWithoutADriverKeyFails(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{
		"concurrency.driver.reports": map[string]any{"tasks": 4},
	}))

	_, err := m.Driver("reports")
	if !errors.Is(err, concurrency.ErrDriverNotSpecified) {
		t.Fatalf("got %v, want ErrDriverNotSpecified", err)
	}
	if !strings.Contains(err.Error(), "reports") {
		t.Fatalf("the message lost the instance name: %v", err)
	}
}

func TestAnUnknownDriverFails(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	_, err := m.Driver("swoole")
	if !errors.Is(err, concurrency.ErrDriverNotSupported) {
		t.Fatalf("got %v, want ErrDriverNotSupported", err)
	}
	if !strings.Contains(err.Error(), "swoole") {
		t.Fatalf("the message lost the driver name: %v", err)
	}
}

func TestRunReportsTheDriverThatCouldNotBeResolved(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{"concurrency.default": "swoole"}))

	if _, err := m.Run(context.Background(), func(ctx context.Context) (int, error) { return 1, nil }); !errors.Is(err, concurrency.ErrDriverNotSupported) {
		t.Fatalf("Run: got %v, want ErrDriverNotSupported", err)
	}
	// The failure is reported where the driver is asked for, not when the
	// deferred callback runs after the response.
	if _, err := m.Defer(context.Background(), func(ctx context.Context) (int, error) { return 1, nil }); !errors.Is(err, concurrency.ErrDriverNotSupported) {
		t.Fatalf("Defer: got %v, want ErrDriverNotSupported", err)
	}
}

func TestDriverIsBuiltOnceAndReused(t *testing.T) {
	t.Parallel()

	var built atomic.Int64
	m := concurrency.NewManager[int](nil)
	m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] {
		built.Add(1)
		return &countingDriver[int]{}
	})
	m.SetDefaultInstance("counting")

	first, err := m.Driver()
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	second, err := m.Driver("counting")
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if first != second {
		t.Fatal("the instance was built twice")
	}
	if got := built.Load(); got != 1 {
		t.Fatalf("the creator ran %d times, want 1", got)
	}
}

func TestExtendedDriverReceivesRunAndDefer(t *testing.T) {
	t.Parallel()

	driver := &countingDriver[int]{}
	m := concurrency.NewManager[int](newConfig(map[string]any{"concurrency.default": "counting"}))
	if m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] { return driver }) != m {
		t.Fatal("Extend did not answer with the manager")
	}

	if _, err := m.Run(context.Background(), func(ctx context.Context) (int, error) { return 1, nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	callback, err := m.Defer(context.Background(), func(ctx context.Context) (int, error) { return 1, nil })
	if err != nil {
		t.Fatalf("Defer: %v", err)
	}
	if err := callback(); err != nil {
		t.Fatalf("callback: %v", err)
	}

	if got := driver.runs.Load(); got != 1 {
		t.Fatalf("Run reached the extended driver %d times, want 1", got)
	}
	if got := driver.defers.Load(); got != 1 {
		t.Fatalf("Defer reached the extended driver %d times, want 1", got)
	}
}

func TestExtendReceivesTheInstanceConfiguration(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](newConfig(map[string]any{
		"concurrency.driver.reports": map[string]any{"driver": "counting", "tasks": 4},
	}))

	var seen map[string]any
	m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] {
		seen = cfg
		return &countingDriver[int]{}
	})

	if _, err := m.Driver("reports"); err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if seen["tasks"] != 4 {
		t.Fatalf("the creator was handed %v, want the instance configuration", seen)
	}
}

func TestExtendReplacesABuiltInName(t *testing.T) {
	t.Parallel()

	driver := &countingDriver[int]{}
	m := concurrency.NewManager[int](nil)
	m.Extend(concurrency.DriverProcess, func(cfg map[string]any) concurrency.Driver[int] { return driver })

	got, err := m.Driver()
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if got != concurrency.Driver[int](driver) {
		t.Fatal("the built-in driver was not replaced")
	}
}

func TestExtendKeepsTheSecondCallback(t *testing.T) {
	t.Parallel()

	second := &countingDriver[int]{}
	m := concurrency.NewManager[int](nil)
	m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] { return &countingDriver[int]{} })
	m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] { return second })
	m.SetDefaultInstance("counting")

	got, err := m.Driver()
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if got != concurrency.Driver[int](second) {
		t.Fatal("the first callback won")
	}
}

func TestExtendDoesNotReplaceAnInstanceAlreadyBuilt(t *testing.T) {
	t.Parallel()

	// MultipleInstanceManager caches instances, so a creator registered after
	// one was resolved does not reach it. Documented rather than fixed: the
	// same call answering two different drivers is worse.
	m := concurrency.NewManager[int](nil)
	first, err := m.Driver(concurrency.DriverProcess)
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}

	m.Extend(concurrency.DriverProcess, func(cfg map[string]any) concurrency.Driver[int] {
		return &countingDriver[int]{}
	})

	again, err := m.Driver(concurrency.DriverProcess)
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if again != first {
		t.Fatal("the cached instance was replaced")
	}
}

func TestACreatorThatAnswersNothingFails(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)
	m.Extend("counting", func(cfg map[string]any) concurrency.Driver[int] { return nil })
	m.SetDefaultInstance("counting")

	if _, err := m.Driver(); !errors.Is(err, concurrency.ErrDriverNotSupported) {
		t.Fatalf("got %v, want ErrDriverNotSupported", err)
	}
}

func TestManagerResolvesFromSeveralGoroutines(t *testing.T) {
	t.Parallel()

	m := concurrency.NewManager[int](nil)

	const n = 8
	drivers := make([]concurrency.Driver[int], n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := m.Driver()
			if err != nil {
				t.Errorf("Driver: %v", err)
				return
			}
			drivers[i] = d
		}()
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if drivers[i] != drivers[0] {
			t.Fatal("the manager handed out more than one instance for one name")
		}
	}
}
