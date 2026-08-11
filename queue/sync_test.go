package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

// registry wires the sync connection the way an application does: a worker over
// a NullQueue is the handler registry with nothing to drain, which is exactly
// what the sync connection is.
func registry() *queue.Worker {
	return queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
}

func TestSyncRunsTheJobAtPush(t *testing.T) {
	w := registry()
	var ranWith string
	var gotGrant auth.Grant
	w.HandleFunc("invoice.send", func(_ context.Context, g auth.Grant, j *jobs.Job) error {
		ranWith, gotGrant = j.UUID, g
		return nil
	})

	q := queue.NewSyncQueue(w)
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if ranWith != j.UUID {
		t.Errorf("the job did not run at Push: %q", ranWith)
	}
	// The handler reaches repositories the same way as under a worker: through a
	// Grant rebuilt from the job.
	if gotGrant.Subject().Tenant != tenant {
		t.Errorf("the handler got a Grant for %q", gotGrant.Subject().Tenant)
	}
	// Nothing is stored, so nothing is waiting.
	if size, err := q.Size(context.Background(), ""); err != nil || size != 0 {
		t.Errorf("size = %d (%v)", size, err)
	}
}

// TestSyncFailsThePush: nothing is stored, so nothing is retried. That is the
// trade the sync connection makes, and it is why it is not a production driver.
func TestSyncFailsThePush(t *testing.T) {
	w := registry()
	broken := errors.New("the invoice has no address")
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error { return broken })

	q := queue.NewSyncQueue(w)
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Push(context.Background(), grant(), j); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the handler's failure", err)
	}
}

func TestSyncRefusesAForgedJob(t *testing.T) {
	q := queue.NewSyncQueue(registry())
	forged := jobs.Job{UUID: "j-1", Name: "invoice.send", Action: "invoice.delete"}
	if err := q.Push(context.Background(), grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("err = %v, want ErrForged", err)
	}
}

// TestSyncDropsTheDelay: a test and a laptop are what this driver is for, and
// neither wants the process to stop for an hour because the job was scheduled
// for one.
func TestSyncDropsTheDelay(t *testing.T) {
	w := registry()
	var ran bool
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		ran = true
		return nil
	})

	q := queue.NewSyncQueue(w)
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Later(context.Background(), grant(), time.Hour, j); err != nil {
		t.Fatalf("Later: %v", err)
	}
	if !ran {
		t.Error("a job scheduled for an hour out did not run now")
	}
}

func TestTheNullQueueKeepsNothing(t *testing.T) {
	q := queue.NullQueue{}
	ctx := context.Background()
	j, _ := jobs.New(grant(), "", "invoice.send", nil)

	if err := q.Push(ctx, grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}
	popped, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(popped) != 0 {
		t.Fatalf("Pop returned %d jobs (%v)", len(popped), err)
	}
	if oldest, err := q.CreationTimeOfOldestPendingJob(ctx, ""); err != nil || !oldest.IsZero() {
		t.Errorf("oldest = %s (%v)", oldest, err)
	}
}

func TestTheManagerHandsOutConnectionsByName(t *testing.T) {
	m := queue.NewManager()
	first := queue.NullQueue{}
	m.Extend("database", first)
	m.Extend("redis", queue.NewSyncQueue(registry()))

	// The first one registered becomes the default, so an application with one
	// queue never has to say which it means.
	if m.DefaultConnection() != "database" {
		t.Errorf("the default connection is %q", m.DefaultConnection())
	}
	got, err := m.Connection("")
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if got != queue.Queue(first) {
		t.Error("the empty name did not resolve to the default")
	}

	if _, err := m.Connection("beanstalkd"); !errors.Is(err, queue.ErrNoConnection) {
		t.Errorf("err = %v, want ErrNoConnection", err)
	}

	m.SetDefaultConnection("redis")
	if m.DefaultConnection() != "redis" {
		t.Errorf("the default connection is %q after being set", m.DefaultConnection())
	}
	if len(m.Connections()) != 2 {
		t.Errorf("the manager lists %v", m.Connections())
	}
}
