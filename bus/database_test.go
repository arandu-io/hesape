package bus_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/database"
)

func TestDatabaseRepositoryRoundTripsEveryFieldOfABatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import invoices").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Then("import.done", row{N: 7}).
		Catch("import.failed", nil).
		Finally("import.over", nil).
		AllowFailures().
		OnQueue("imports").
		OnConnection("redis").
		WithOption("upload", "u-1").
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if got.ID != b.ID || got.Name != "import invoices" || got.TenantID != tenant {
		t.Errorf("identity = %+v", got)
	}
	if got.Options.Queue != "imports" || got.Options.Connection != "redis" {
		t.Errorf("routing = %+v, want imports on redis", got.Options)
	}
	if got.TotalJobs != 2 || got.PendingJobs != 2 || got.FailedJobs != 0 {
		t.Errorf("counters = %d/%d/%d, want 2/2/0", got.TotalJobs, got.PendingJobs, got.FailedJobs)
	}
	if !got.AllowsFailures() {
		t.Error("AllowFailures did not survive the round trip")
	}
	if got.Options.Extra["upload"] != "u-1" {
		t.Errorf("WithOption did not survive the round trip: %+v", got.Options.Extra)
	}
	if got.Options.Then.Name != "import.done" || string(got.Options.Then.Payload) != `{"n":7}` {
		t.Errorf("Then = %+v, want import.done carrying n=7", got.Options.Then)
	}
	if got.Options.Catch.Name != "import.failed" || got.Options.Finally.Name != "import.over" {
		t.Errorf("Catch = %+v, Finally = %+v", got.Options.Catch, got.Options.Finally)
	}
	if got.CreatedAt.IsZero() || got.Finished() || got.Cancelled() {
		t.Errorf("timestamps = %+v, want created and nothing else", got)
	}
}

func TestDatabaseRepositoryCountsTheSameWayMemoryDoes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		Then("import.done", nil).
		Catch("import.failed", nil).
		Finally("import.over", nil).
		AllowFailures().
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	jobs := queue.all()
	handle(t, store, queue, jobs[0], errors.New("bad row"))
	if got := queue.count("import.failed"); got != 1 {
		t.Errorf("Catch fired %d times, want 1", got)
	}
	if got := queue.count("import.over"); got != 0 {
		t.Error("Finally fired with a job still owed")
	}

	handle(t, store, queue, jobs[1], nil)
	if got := queue.count("import.over"); got != 1 {
		t.Errorf("Finally fired %d times, want 1", got)
	}
	if got := queue.count("import.done"); got != 0 {
		t.Error("Then fired on a batch that had a failure")
	}

	after, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// One succeeded and one failed, and only a success decrements: pending is
	// the failure that is still owed, and pending minus failed reaching zero is
	// what fired Finally.
	if after.PendingJobs != 1 || after.FailedJobs != 1 {
		t.Errorf("counters = pending %d, failed %d; want 1 and 1", after.PendingJobs, after.FailedJobs)
	}
}

// TestDatabaseRepositoryCounterIsCompareAndSet is the reason the counter update
// reads before it writes. Fifty workers report at once, and exactly one of them
// has to see the batch reach zero -- otherwise the report at the end of a ten
// thousand row import is emailed more than once.
func TestDatabaseRepositoryCounterIsCompareAndSet(t *testing.T) {
	t.Parallel()

	const n = 50
	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	p := bus.NewBatch("import").Then("import.done", nil).Finally("import.over", nil)
	for i := range n {
		p = p.Add("invoice.import", row{N: i})
	}
	b, err := p.Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		zeroed int
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			counts, err := store.DecrementPendingJobs(ctx, grant(), b.ID, fmt.Sprintf("job-%d", i))
			if err != nil {
				t.Errorf("DecrementPendingJobs: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if counts.PendingJobs == 0 {
				zeroed++
			}
		}(i)
	}
	wg.Wait()

	if zeroed != 1 {
		t.Errorf("%d of %d reports saw the batch reach zero, want 1", zeroed, n)
	}
	after, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if after.PendingJobs != 0 || after.FailedJobs != 0 {
		t.Errorf("counters = pending %d, failed %d; want 0 and 0", after.PendingJobs, after.FailedJobs)
	}
}

// TestDatabaseRepositoryDuplicateDeliveryDoesNotRefire covers the duplicate:
// the same job reported twice must not take the batch to zero twice, because
// zero is what fires Then. It is why the decrement is unconditional -- the
// second report takes the counter below zero rather than leaving it at it.
func TestDatabaseRepositoryDuplicateDeliveryDoesNotRefire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	first, err := store.DecrementPendingJobs(ctx, grant(), b.ID, "job-1")
	if err != nil {
		t.Fatalf("DecrementPendingJobs: %v", err)
	}
	if first.PendingJobs != 0 {
		t.Fatalf("the only job of the batch left %d pending, want 0", first.PendingJobs)
	}

	again, err := store.DecrementPendingJobs(ctx, grant(), b.ID, "job-1")
	if err != nil {
		t.Fatalf("DecrementPendingJobs on a duplicate delivery: %v", err)
	}
	if again.PendingJobs == 0 {
		t.Error("a duplicate delivery reached zero a second time, and Then would fire twice")
	}
}

// TestDatabaseRepositoryRetriedFailureStopsCounting is why the failed jobs are
// a list and not a number: a job that failed, was retried and then succeeded is
// not a failure, and the batch has to be able to say so.
func TestDatabaseRepositoryRetriedFailureStopsCounting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import").
		Add("invoice.import", row{N: 1}).
		Add("invoice.import", row{N: 2}).
		AllowFailures().
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if _, err := store.IncrementFailedJobs(ctx, grant(), b.ID, "job-1"); err != nil {
		t.Fatalf("IncrementFailedJobs: %v", err)
	}
	// The same failure reported twice is still one failure.
	counts, err := store.IncrementFailedJobs(ctx, grant(), b.ID, "job-1")
	if err != nil {
		t.Fatalf("IncrementFailedJobs again: %v", err)
	}
	if counts.FailedJobs != 1 {
		t.Errorf("the same job failing twice counted %d failures, want 1", counts.FailedJobs)
	}

	counts, err = store.DecrementPendingJobs(ctx, grant(), b.ID, "job-1")
	if err != nil {
		t.Fatalf("DecrementPendingJobs: %v", err)
	}
	if counts.FailedJobs != 0 {
		t.Errorf("a retried job still counts as %d failures, want 0", counts.FailedJobs)
	}
}

func TestDatabaseRepositoryCancelIsIdempotentAndSaysWhenThereIsNoBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if err := store.Cancel(ctx, grant(), b.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := store.Cancel(ctx, grant(), b.ID); err != nil {
		t.Fatalf("Cancel again: %v", err)
	}
	if err := store.Cancel(ctx, grant(), "nope"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("err = %v, want database.ErrNotFound", err)
	}

	got, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// Cancelling stamps both columns, as Illuminate's does: nothing the jobs
	// still queued could report would change the outcome.
	if !got.Cancelled() || !got.Finished() {
		t.Errorf("batch = %+v, want cancelled and finished", got)
	}
}

func TestDatabaseRepositoryPrunesByWhatTheBatchDid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newFakeStore()
	cut := time.Now().UTC().Add(time.Hour)

	done, err := bus.NewBatch("done").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if err := store.MarkAsFinished(ctx, grant(), done.ID); err != nil {
		t.Fatalf("MarkAsFinished: %v", err)
	}
	running, err := bus.NewBatch("running").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	stopped, err := bus.NewBatch("stopped").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if err := store.Cancel(ctx, grant(), stopped.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if n, err := store.PruneCancelled(ctx, grant(), cut); err != nil || n != 1 {
		t.Fatalf("PruneCancelled took %d rows (%v), want 1", n, err)
	}
	if n, err := store.Prune(ctx, grant(), cut); err != nil || n != 1 {
		t.Fatalf("Prune took %d rows (%v), want 1", n, err)
	}
	if _, err := store.Find(ctx, grant(), running.ID); err != nil {
		t.Errorf("the unfinished batch was pruned: %v", err)
	}
	if n, err := store.PruneUnfinished(ctx, grant(), cut); err != nil || n != 1 {
		t.Fatalf("PruneUnfinished took %d rows (%v), want 1", n, err)
	}
	if _, err := store.Find(ctx, grant(), running.ID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("the unfinished batch survived PruneUnfinished: %v", err)
	}
}

func TestDatabaseRepositoryScopesEveryStatementByTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newFakeStore()

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	other := otherTenant()
	if _, err := store.Find(ctx, other, b.ID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("Find across tenants: %v, want database.ErrNotFound", err)
	}
	if err := store.Cancel(ctx, other, b.ID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("Cancel across tenants: %v, want database.ErrNotFound", err)
	}
	if _, err := store.DecrementPendingJobs(ctx, other, b.ID, "job-1"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("DecrementPendingJobs across tenants: %v, want database.ErrNotFound", err)
	}
	if err := store.IncrementTotalJobs(ctx, other, b.ID, 1); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("IncrementTotalJobs across tenants: %v, want database.ErrNotFound", err)
	}
	if n, err := store.Prune(ctx, other, time.Now().UTC().Add(time.Hour)); err != nil || n != 0 {
		t.Errorf("Prune across tenants took %d rows (%v), want 0", n, err)
	}
	if list, err := store.Get(ctx, other, 10, ""); err != nil || len(list) != 0 {
		t.Errorf("Get across tenants returned %d batches (%v), want 0", len(list), err)
	}
}

func TestDatabaseRepositoryGetListsNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newFakeStore()

	var ids []string
	for i := range 3 {
		b, err := bus.NewBatch(fmt.Sprintf("import-%d", i)).Add("invoice.import", row{N: i}).
			Dispatch(ctx, grant(), store, &recorder{})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		ids = append(ids, b.ID)
	}

	list, err := store.Get(ctx, grant(), 2, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("Get returned %d batches, want 2", len(list))
	}
	if list[0].ID != ids[2] || list[1].ID != ids[1] {
		t.Errorf("Get returned %s then %s, want the two newest", list[0].ID, list[1].ID)
	}

	page, err := store.Get(ctx, grant(), 10, list[1].ID)
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}
	if len(page) != 1 || page[0].ID != ids[0] {
		t.Errorf("the page before %s is %d batches, want the oldest one", list[1].ID, len(page))
	}
}

func TestMigrationsDeclareTheTable(t *testing.T) {
	t.Parallel()

	ms := bus.Migrations()
	if len(ms) != 1 {
		t.Fatalf("%d migrations, want 1", len(ms))
	}
	m := ms[0]
	if !strings.HasPrefix(m.ID, "2026_") {
		t.Errorf("id = %q, and a migration id carries its own order", m.ID)
	}
	if !strings.Contains(m.Up, "CREATE TABLE "+bus.BatchesTable) {
		t.Errorf("the migration does not create %s", bus.BatchesTable)
	}
	// TEXT in a key is not portable: MySQL refuses it without a prefix length.
	if strings.Contains(m.Up, "id             TEXT") {
		t.Error("the key column is TEXT, which MySQL refuses in a key")
	}
	if !strings.Contains(m.Down, "DROP TABLE "+bus.BatchesTable) {
		t.Errorf("the migration cannot be rolled back: %q", m.Down)
	}
}
