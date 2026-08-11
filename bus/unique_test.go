package bus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/cache"
)

func repo() *cache.Repository { return cache.New(cache.NewArrayStore()) }

func TestPushUniqueQueuesOnceWhileTheClaimStands(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c, queue := repo(), &recorder{}
	job := bus.UniqueJob{
		Key:     "report.monthly:2026-08",
		TTL:     time.Minute,
		Queue:   "reports",
		Name:    "report.monthly",
		Payload: row{N: 8},
	}

	pushed, err := bus.PushUnique(ctx, grant(), c, queue, job)
	if err != nil {
		t.Fatalf("PushUnique: %v", err)
	}
	if !pushed {
		t.Fatal("the first push was refused")
	}

	// The button pressed three more times.
	for i := range 3 {
		again, err := bus.PushUnique(ctx, grant(), c, queue, job)
		if err != nil {
			t.Fatalf("PushUnique %d: %v", i, err)
		}
		if again {
			t.Fatalf("press %d queued a second copy", i+2)
		}
	}
	if got := queue.count("report.monthly"); got != 1 {
		t.Fatalf("queued %d copies, want 1", got)
	}

	// The job finishes and gives the claim back.
	if err := bus.ReleaseUnique(ctx, grant(), c, job.Key); err != nil {
		t.Fatalf("ReleaseUnique: %v", err)
	}
	pushed, err = bus.PushUnique(ctx, grant(), c, queue, job)
	if err != nil {
		t.Fatalf("PushUnique after release: %v", err)
	}
	if !pushed {
		t.Fatal("the job stayed silenced after its claim was released")
	}

	// And what was queued is the arguments, not an envelope: a unique job is
	// not part of anything.
	var got row
	m, err := bus.Batched(queue.all()[0].Payload, &got)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if m.BatchID != "" || got.N != 8 {
		t.Errorf("queued %+v with membership %q, want n=8 and none", got, m.BatchID)
	}
	if queue.all()[0].Queue != "reports" {
		t.Errorf("landed on queue %q, want reports", queue.all()[0].Queue)
	}
}

func TestPushUniqueGivesTheClaimBackWhenTheQueueRefuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := repo()
	down := &recorder{fail: errors.New("the queue is down")}
	job := bus.UniqueJob{Key: "report.monthly", TTL: time.Minute, Name: "report.monthly"}

	if _, err := bus.PushUnique(ctx, grant(), c, down, job); err == nil {
		t.Fatal("a push against a queue that refused was reported as done")
	}

	// A queue that was briefly down must not silence the job for a whole TTL.
	up := &recorder{}
	pushed, err := bus.PushUnique(ctx, grant(), c, up, job)
	if err != nil {
		t.Fatalf("PushUnique: %v", err)
	}
	if !pushed {
		t.Fatal("the claim outlived the push that failed")
	}
}

func TestPushUniqueIsPerTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c, queue := repo(), &recorder{}
	job := bus.UniqueJob{Key: "report.monthly", TTL: time.Minute, Name: "report.monthly"}

	if _, err := bus.PushUnique(ctx, grant(), c, queue, job); err != nil {
		t.Fatalf("PushUnique: %v", err)
	}
	// One customer's claim must not silence another's job.
	pushed, err := bus.PushUnique(ctx, otherTenant(), c, queue, job)
	if err != nil {
		t.Fatalf("PushUnique for the other tenant: %v", err)
	}
	if !pushed {
		t.Fatal("one tenant's claim silenced another tenant's job")
	}
}

func TestPushUniqueRefusesWhatCannotBeUnique(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c, queue := repo(), &recorder{}

	_, err := bus.PushUnique(ctx, grant(), c, queue, bus.UniqueJob{TTL: time.Minute, Name: "x"})
	if !errors.Is(err, bus.ErrNoKey) {
		t.Errorf("err = %v, want ErrNoKey", err)
	}

	_, err = bus.PushUnique(ctx, grant(), c, queue, bus.UniqueJob{Key: "k", TTL: time.Minute})
	if !errors.Is(err, bus.ErrNoName) {
		t.Errorf("err = %v, want ErrNoName", err)
	}

	// No TTL is a job that would be silenced forever by a worker that died.
	_, err = bus.PushUnique(ctx, grant(), c, queue, bus.UniqueJob{Key: "k", Name: "x"})
	if !errors.Is(err, cache.ErrNoTTL) {
		t.Errorf("err = %v, want cache.ErrNoTTL", err)
	}

	if len(queue.all()) != 0 {
		t.Errorf("a refused unique job was queued anyway: %v", queue.names())
	}
}
