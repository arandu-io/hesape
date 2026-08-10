package bus_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/database"
)

func TestSQLRoundTripsEveryFieldOfABatch(t *testing.T) {
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
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if got.ID != b.ID || got.Name != "import invoices" || got.TenantID != tenant || got.Queue != "imports" {
		t.Errorf("identity = %+v", got)
	}
	if got.Total != 2 || got.Pending != 2 || got.Failed != 0 {
		t.Errorf("counters = %d/%d/%d, want 2/2/0", got.Total, got.Pending, got.Failed)
	}
	if !got.AllowFailures {
		t.Error("AllowFailures did not survive the round trip")
	}
	if got.Then.Name != "import.done" || string(got.Then.Payload) != `{"n":7}` {
		t.Errorf("Then = %+v, want import.done carrying n=7", got.Then)
	}
	if got.Catch.Name != "import.failed" || got.Finally.Name != "import.over" {
		t.Errorf("Catch = %+v, Finally = %+v", got.Catch, got.Finally)
	}
	if got.CreatedAt.IsZero() || got.Finished() || got.Cancelled() {
		t.Errorf("timestamps = %+v, want created and nothing else", got)
	}
}

func TestSQLCountsTheSameWayMemoryDoes(t *testing.T) {
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
		t.Error("the batch finished on a failure it was told to allow")
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
	if after.Pending != 0 || after.Failed != 1 || !after.Finished() {
		t.Errorf("batch = %+v, want nothing pending, one failure, finished", after)
	}
}

// TestSQLRecordIsCompareAndSet is the reason Record reads before it writes.
// Fifty workers report at once, and exactly one of them has to be told it
// finished the batch -- otherwise the report at the end of a ten thousand row
// import is emailed more than once.
func TestSQLRecordIsCompareAndSet(t *testing.T) {
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
		wg       sync.WaitGroup
		mu       sync.Mutex
		finished int
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := store.Record(ctx, grant(), b.ID, true)
			if err != nil {
				t.Errorf("Record: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if r.Finished {
				finished++
			}
		}()
	}
	wg.Wait()

	if finished != 1 {
		t.Errorf("%d of %d reports were told they finished the batch, want 1", finished, n)
	}
	after, err := store.Find(ctx, grant(), b.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if after.Pending != 0 || after.Failed != 0 {
		t.Errorf("counters = pending %d, failed %d; want 0 and 0", after.Pending, after.Failed)
	}
}

// TestSQLRecordOnAFinishedBatchWritesNothing covers the duplicate delivery:
// MySQL counts rows changed and not rows matched, so an update that sets the
// values already there affects nothing -- and the retry loop must not read
// that as contention.
func TestSQLRecordOnAFinishedBatchWritesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, queue := newFakeStore(), &recorder{}

	b, err := bus.NewBatch("import").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	first, err := store.Record(ctx, grant(), b.ID, true)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !first.Finished {
		t.Fatal("the only job of the batch did not finish it")
	}

	again, err := store.Record(ctx, grant(), b.ID, true)
	if err != nil {
		t.Fatalf("Record on a duplicate delivery: %v", err)
	}
	if again.Finished || again.FirstFailure {
		t.Errorf("a duplicate delivery reported %+v, want nothing", again)
	}
}

func TestSQLCancelIsIdempotentAndSaysWhenThereIsNoBatch(t *testing.T) {
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
	if !got.Cancelled() {
		t.Error("the batch was not cancelled")
	}
}

func TestSQLPruneTakesFinishedBatchesOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newFakeStore()

	done, err := bus.NewBatch("done").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := store.Record(ctx, grant(), done.ID, true); err != nil {
		t.Fatalf("Record: %v", err)
	}
	running, err := bus.NewBatch("running").Add("invoice.import", row{N: 1}).
		Dispatch(ctx, grant(), store, &recorder{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	n, err := store.Prune(ctx, grant(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d batches, want 1", n)
	}
	if _, err := store.Find(ctx, grant(), running.ID); err != nil {
		t.Errorf("the unfinished batch was pruned: %v", err)
	}
	if _, err := store.Find(ctx, grant(), done.ID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("the finished batch survived the prune: %v", err)
	}
}

func TestSQLScopesEveryStatementByTenant(t *testing.T) {
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
	if _, err := store.Record(ctx, other, b.ID, true); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("Record across tenants: %v, want database.ErrNotFound", err)
	}
	if n, err := store.Prune(ctx, other, time.Now().UTC().Add(time.Hour)); err != nil || n != 0 {
		t.Errorf("Prune across tenants took %d rows (%v), want 0", n, err)
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
