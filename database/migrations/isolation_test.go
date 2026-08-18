package migrations

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
)

// countingMigration counts its own Up and can be held inside it, which is what
// lets a test keep one run holding the lock while another asks for it.
type countingMigration struct {
	BaseMigration
	name    string
	ups     *atomic.Int64
	holding chan<- struct{}
	release <-chan struct{}
}

func (m countingMigration) GetName() string { return m.name }

func (m countingMigration) Up(context.Context, Connection) error {
	// Only the first Up waits. A second one is the failure under test, and a
	// second one that blocked would hang the test rather than report it.
	if m.ups.Add(1) != 1 {
		return nil
	}
	if m.holding != nil {
		close(m.holding)
	}
	if m.release != nil {
		<-m.release
	}
	return nil
}

// issuerOver answers the issuer IsolateWith takes, over a real lock store: the
// exclusion under test is the store's and not the test's.
func issuerOver(locks *cache.Locks) func(string) IsolationLock {
	return func(name string) IsolationLock { return locks.Lock(name, time.Minute) }
}

func TestIsolationLockNameCarriesTheConnection(t *testing.T) {
	if IsolationLockName("primary") == IsolationLockName("analytics") {
		t.Fatal("two connections got one lock name, so migrating one database would block the other")
	}
	if !strings.Contains(IsolationLockName("primary"), "primary") {
		t.Fatalf("IsolationLockName(%q) = %q, and the connection has to be in it", "primary", IsolationLockName("primary"))
	}
}

func TestRunIsolatedRefusesWithoutALockIssuer(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups atomic.Int64
	Register(countingMigration{name: "2026_01_01_000000_first", ups: &ups})

	migrator, _, _ := newTestMigrator()

	_, ran, err := migrator.RunIsolated(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("an isolated run with no lock issuer went ahead, which is the unprotected run it promised not to be")
	}
	if ran {
		t.Fatal("RunIsolated reported that it ran")
	}
	if ups.Load() != 0 {
		t.Fatalf("Up ran %d times, and nothing should have been applied", ups.Load())
	}
}

func TestRunIsolatedAppliesAndSaysItTookTheLock(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups atomic.Int64
	Register(countingMigration{name: "2026_01_01_000000_first", ups: &ups})

	migrator, repository, _ := newTestMigrator()
	migrator.IsolateWith(issuerOver(cache.NewLocks(cache.NewArrayStore())))

	applied, ran, err := migrator.RunIsolated(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("RunIsolated: %v", err)
	}
	if !ran {
		t.Fatal("RunIsolated did not take a lock nobody held")
	}
	if strings.Join(applied, ",") != "2026_01_01_000000_first" {
		t.Fatalf("RunIsolated applied %v, want the one registered migration", applied)
	}
	if ups.Load() != 1 {
		t.Fatalf("Up ran %d times, want 1", ups.Load())
	}
	if len(repository.records) != 1 {
		t.Fatalf("the repository recorded %d migrations, want 1", len(repository.records))
	}

	// The lock is given back, so the next run in the pipeline is not refused by
	// the one before it.
	if _, ran, err = migrator.RunIsolated(context.Background(), nil, Options{}); err != nil || !ran {
		t.Fatalf("the second RunIsolated answered (ran=%v, %v), so the first never released its lock", ran, err)
	}
}

// TestRunIsolatedMigratesNothingWhileAnotherProcessHoldsTheLock is the case the
// whole option exists for: the second replica of a rolling deploy asks while
// the first is still applying. It has to come back having done nothing, and it
// has to come back successful, or the deployment fails.
func TestRunIsolatedMigratesNothingWhileAnotherProcessHoldsTheLock(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups atomic.Int64
	holding := make(chan struct{})
	release := make(chan struct{})
	// Released whatever happens, so a failed assertion reports itself instead
	// of leaving the run that holds the lock waiting forever.
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	Register(countingMigration{
		name:    "2026_01_01_000000_first",
		ups:     &ups,
		holding: holding,
		release: release,
	})

	// One store, two migrators: two processes against one database, which is
	// what N replicas are.
	locks := cache.NewLocks(cache.NewArrayStore())
	repository := &fakeRepository{exists: true}
	first := migratorOver(repository, locks)
	second := migratorOver(repository, locks)

	var firstApplied []string
	var firstRan bool
	var firstErr error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstApplied, firstRan, firstErr = first.RunIsolated(context.Background(), nil, Options{})
	}()

	// The first run is now inside Up, holding the lock.
	<-holding

	applied, ran, err := second.RunIsolated(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("the run that lost the lock answered an error, and every multi-replica deployment would fail on it: %v", err)
	}
	if ran {
		t.Fatal("both runs believed they took the lock")
	}
	if len(applied) != 0 {
		t.Fatalf("the run that lost the lock applied %v", applied)
	}

	releaseOnce()
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("the run that took the lock: %v", firstErr)
	}
	if !firstRan {
		t.Fatal("the run that took the lock reported that it did not")
	}
	if strings.Join(firstApplied, ",") != "2026_01_01_000000_first" {
		t.Fatalf("the run that took the lock applied %v", firstApplied)
	}
	if ups.Load() != 1 {
		t.Fatalf("Up ran %d times, and two of them is the duplicate-key failure this prevents", ups.Load())
	}
	if len(repository.records) != 1 {
		t.Fatalf("the repository recorded %d migrations, want 1", len(repository.records))
	}
}

// TestConcurrentIsolatedRunsApplyOnce starts the runs together rather than one
// after the other, so the exclusion is the store's and not the test's ordering.
func TestConcurrentIsolatedRunsApplyOnce(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups atomic.Int64
	Register(countingMigration{name: "2026_01_01_000000_first", ups: &ups})

	locks := cache.NewLocks(cache.NewArrayStore())
	repository := &fakeRepository{exists: true}

	const replicas = 8
	start := make(chan struct{})
	errs := make([]error, replicas)
	tookTheLock := make([]bool, replicas)

	var wg sync.WaitGroup
	for i := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			migrator := migratorOver(repository, locks)
			<-start
			_, ran, err := migrator.RunIsolated(context.Background(), nil, Options{})
			tookTheLock[i], errs[i] = ran, err
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d answered an error: %v", i, err)
		}
	}
	if ups.Load() != 1 {
		t.Fatalf("Up ran %d times across %d replicas, want 1", ups.Load(), replicas)
	}
	if len(repository.records) != 1 {
		t.Fatalf("the repository recorded %d migrations, want 1", len(repository.records))
	}

	// A replica that arrives after the lock was given back takes it and finds
	// nothing pending, so more than one may report that it ran. None may fail.
	took := 0
	for _, ran := range tookTheLock {
		if ran {
			took++
		}
	}
	if took == 0 {
		t.Fatal("no replica took the lock, so nothing would ever be migrated")
	}
}

func TestRunIsolatedCarriesTheMigrationError(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	boom := errors.New("column already exists")
	var ups, downs []string
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs, upErr: boom})

	migrator, _, _ := newTestMigrator()
	migrator.IsolateWith(issuerOver(cache.NewLocks(cache.NewArrayStore())))

	_, ran, err := migrator.RunIsolated(context.Background(), nil, Options{})
	if !errors.Is(err, boom) {
		t.Fatalf("RunIsolated answered %v, want the migration's own error", err)
	}
	if !ran {
		t.Fatal("the run took the lock and failed inside it, and it has to say it took the lock")
	}
}

// migratorOver builds a migrator over a shared repository and lock store, which
// is what makes two of them two processes against one database.
func migratorOver(repository *fakeRepository, locks *cache.Locks) *Migrator {
	resolver := &fakeResolver{connection: &fakeConnection{}}
	return NewMigrator(repository, resolver, nil).IsolateWith(issuerOver(locks))
}
