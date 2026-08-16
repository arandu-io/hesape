package fakes

import (
	"sync"
	"testing"
	"time"
)

func TestBatchFakeAddGrowsTheTotal(t *testing.T) {
	t.Parallel()

	batch := NewBatchFake("1", "receipts", 2, 2, 0, nil, nil, time.Now(), time.Time{}, time.Time{})
	batch.Add(sendReceipt{}, sendReceipt{})

	if batch.TotalJobs != 4 {
		t.Errorf("TotalJobs = %d, want 4", batch.TotalJobs)
	}
	if got := len(batch.Added()); got != 2 {
		t.Errorf("Added = %d, want 2", got)
	}
	// Adding nothing is not an error and changes nothing.
	batch.Add()
	if batch.TotalJobs != 4 {
		t.Errorf("TotalJobs after adding nothing = %d, want 4", batch.TotalJobs)
	}
	if batch.Fresh() != batch {
		t.Error("Fresh should answer the batch itself")
	}
}

func TestBatchFakeCancelAndDelete(t *testing.T) {
	t.Parallel()

	batch := NewBatchFake("1", "receipts", 0, 0, 0, nil, nil, time.Now(), time.Time{}, time.Time{})

	if batch.Cancelled() || batch.Deleted() || batch.Finished() {
		t.Error("a fresh batch is neither cancelled, deleted nor finished")
	}

	batch.Cancel()
	if !batch.Cancelled() {
		t.Error("Cancel should stamp the batch")
	}

	batch.Delete()
	if !batch.Deleted() {
		t.Error("Delete should mark the batch, and leave it readable")
	}

	// The three that do nothing on purpose.
	batch.RecordSuccessfulJob("1")
	batch.DecrementPendingJobs("1")
	batch.RecordFailedJob("1", nil)
	if counts := batch.IncrementFailedJobs("1"); counts != (UpdatedBatchJobCounts{}) {
		t.Errorf("IncrementFailedJobs = %+v, want empty counts", counts)
	}
	if !(UpdatedBatchJobCounts{}).AllJobsHaveRanExactlyOnce() {
		t.Error("empty counts mean every job ran exactly once")
	}
}

func TestBatchRepositoryFakeStoreAndFind(t *testing.T) {
	t.Parallel()

	repository := NewBatchRepositoryFake()
	bus := NewBusFake(nil)

	pending := NewPendingBatchFake(bus, []any{sendReceipt{}, sendReceipt{}})
	pending.Name = "receipts"
	stored := repository.Store(pending)

	if stored.Name != "receipts" || stored.TotalJobs != 2 || stored.PendingJobs != 2 || stored.FailedJobs != 0 {
		t.Errorf("the stored batch is %+v, want receipts with 2 jobs pending and none failed", stored)
	}
	if repository.Find(stored.ID) != stored {
		t.Error("Find should answer the batch that was stored")
	}
	if repository.Find("nothing here") != nil {
		t.Error("Find should answer nothing for an id that was never stored")
	}
	if got := len(repository.Get(10, nil)); got != 1 {
		t.Errorf("Get = %d, want 1", got)
	}
}

func TestBatchRepositoryFakeKeepsTheOrderItStoredIn(t *testing.T) {
	t.Parallel()

	repository := NewBatchRepositoryFake()
	bus := NewBusFake(nil)

	first := NewPendingBatchFake(bus, nil)
	first.Name = "first"
	second := NewPendingBatchFake(bus, nil)
	second.Name = "second"

	repository.Store(first)
	repository.Store(second)

	got := repository.Get(10, nil)
	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Errorf("Get gave %v, want the batches in the order they were stored", got)
	}
}

func TestBatchRepositoryFakeMarkAsFinishedAndCancel(t *testing.T) {
	t.Parallel()

	repository := NewBatchRepositoryFake()
	stored := repository.Store(NewPendingBatchFake(NewBusFake(nil), nil))

	repository.MarkAsFinished(stored.ID)
	if !stored.Finished() {
		t.Error("MarkAsFinished should stamp the batch")
	}

	repository.Cancel(stored.ID)
	if !stored.Cancelled() {
		t.Error("Cancel should stamp the batch")
	}

	// An id that was never stored is ignored rather than a panic.
	repository.MarkAsFinished("nothing here")
	repository.Cancel("nothing here")
	repository.Delete("nothing here")

	repository.Delete(stored.ID)
	if repository.Find(stored.ID) != nil {
		t.Error("Delete should forget the batch entirely")
	}
	if got := len(repository.Get(10, nil)); got != 0 {
		t.Errorf("Get after Delete = %d, want 0", got)
	}
}

func TestBatchRepositoryFakeTransactionJustCalls(t *testing.T) {
	t.Parallel()

	repository := NewBatchRepositoryFake()
	called := false
	got := repository.Transaction(func() any {
		called = true
		return "answer"
	})

	if !called || got != "answer" {
		t.Errorf("Transaction called=%v answered=%v, want it to call and answer", called, got)
	}
	// It does nothing on purpose.
	repository.RollBack()
	repository.IncrementTotalJobs("1", 5)
	if counts := repository.DecrementPendingJobs("1", "job"); counts != (UpdatedBatchJobCounts{}) {
		t.Errorf("DecrementPendingJobs = %+v, want empty counts", counts)
	}
	if counts := repository.IncrementFailedJobs("1", "job"); counts != (UpdatedBatchJobCounts{}) {
		t.Errorf("IncrementFailedJobs = %+v, want empty counts", counts)
	}
}

func TestBatchRepositoryFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	repository := NewBatchRepositoryFake()
	bus := NewBusFake(nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored := repository.Store(NewPendingBatchFake(bus, []any{sendReceipt{}}))
			repository.Find(stored.ID)
			repository.MarkAsFinished(stored.ID)
		}()
	}
	wg.Wait()

	if got := len(repository.Get(100, nil)); got != 50 {
		t.Errorf("Get = %d, want 50", got)
	}
}
