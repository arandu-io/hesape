package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/attributes"
	"github.com/arandu-io/hesape/queue/jobs"
)

// TestDeferredQueueRunsAfterTheCallerHasMovedOn: the push returns, and the
// work happens afterwards.
func TestDeferredQueueRunsAfterTheCallerHasMovedOn(t *testing.T) {
	w := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
	ran := make(chan string, 1)
	w.HandleFunc("invoice.send", func(_ context.Context, _ auth.Grant, j *jobs.Job) error {
		ran <- j.Name
		return nil
	})

	var deferred []func()
	q := queue.NewDeferredQueue(w).DeferUsing(func(fn func()) { deferred = append(deferred, fn) })

	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}
	select {
	case name := <-ran:
		t.Fatalf("%s ran before the caller moved on", name)
	default:
	}

	for _, fn := range deferred {
		fn()
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("the deferred job never ran")
	}
}

// TestDeferredQueueRefusesAForgedJobAtThePush: a refusal that happened on
// another goroutine after the response is a refusal nobody sees (RULE 17).
func TestDeferredQueueRefusesAForgedJobAtThePush(t *testing.T) {
	w := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
	q := queue.NewDeferredQueue(w).DeferUsing(func(func()) {
		t.Error("a forged job was deferred instead of being refused")
	})

	forged := jobs.Job{UUID: "j-1", Name: "invoice.send", Action: "invoice.delete"}
	if err := q.Push(context.Background(), grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("err = %v, want ErrForged", err)
	}
}

// brokenQueue refuses everything, so a failover has somewhere to fail over
// from.
type brokenQueue struct{ queue.NullQueue }

var errBroken = errors.New("the broker refused")

func (brokenQueue) Push(context.Context, auth.Grant, jobs.Job) error { return errBroken }

func (brokenQueue) Later(context.Context, auth.Grant, time.Duration, jobs.Job) error {
	return errBroken
}

// acceptingQueue records what it was given.
type acceptingQueue struct {
	queue.NullQueue
	pushed []jobs.Job
}

func (q *acceptingQueue) Push(_ context.Context, _ auth.Grant, j jobs.Job) error {
	q.pushed = append(q.pushed, j)
	return nil
}

func TestFailoverQueueWritesToTheFirstConnectionThatAccepts(t *testing.T) {
	fallback := &acceptingQueue{}
	m := queue.NewQueueManager().
		Extend("redis", brokenQueue{}).
		Extend("database", fallback)

	dispatcher := &recordingDispatcher{}
	q := queue.NewFailoverQueue(m, "redis", "database").SetEvents(dispatcher)

	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(fallback.pushed) != 1 {
		t.Fatalf("the fallback took %d jobs", len(fallback.pushed))
	}
	if len(dispatcher.dispatched) == 0 {
		t.Error("nothing announced that redis was down")
	}
}

func TestFailoverQueueReportsWhenEveryConnectionIsDown(t *testing.T) {
	m := queue.NewQueueManager().
		Extend("redis", brokenQueue{}).
		Extend("valkey", brokenQueue{})
	q := queue.NewFailoverQueue(m, "redis", "valkey")

	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Push(context.Background(), grant(), j); !errors.Is(err, errBroken) {
		t.Errorf("err = %v, want the last failure", err)
	}
}

// TestFailoverQueueRefusesAForgedJobOnce: the check runs before any connection
// is tried, so a refusal cannot be mistaken for a broker being down.
func TestFailoverQueueRefusesAForgedJobOnce(t *testing.T) {
	target := &acceptingQueue{}
	m := queue.NewQueueManager().Extend("database", target)
	q := queue.NewFailoverQueue(m, "database")

	forged := jobs.Job{UUID: "j-1", Name: "invoice.send", TenantID: "somebody-else"}
	if err := q.Push(context.Background(), grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("err = %v, want ErrForged", err)
	}
	if len(target.pushed) != 0 {
		t.Error("a forged job reached a connection")
	}
}

func TestBackgroundQueueSaysSoWhenItHasNoWayToStartAProcess(t *testing.T) {
	q := queue.NewBackgroundQueue(nil)
	j, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Push(context.Background(), grant(), j); !errors.Is(err, queue.ErrNoBackgroundRunner) {
		t.Errorf("err = %v, want ErrNoBackgroundRunner", err)
	}
}

// TestCreatePayloadCarriesTheJobsOwnSettings: the settings used to reach
// nowhere, so a job that asked for five tries got the worker's number.
func TestCreatePayloadCarriesTheJobsOwnSettings(t *testing.T) {
	j, err := queue.CreatePayload(grant(), "database", "", "invoice.send",
		invoiceArgs{ID: "i-1"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePayload: %v", err)
	}

	if j.MaxTries() != 5 {
		t.Errorf("maxTries = %d, want the job's own 5", j.MaxTries())
	}
	if j.Timeout() != 30*time.Second {
		t.Errorf("timeout = %s", j.Timeout())
	}
	if j.Backoff() != 10*time.Second {
		t.Errorf("the first backoff is %s, want the first entry", j.Backoff())
	}
	if j.DisplayName != "send an invoice" {
		t.Errorf("displayName = %q", j.DisplayName)
	}
	if !j.RunAt.After(time.Now().Add(30 * time.Minute)) {
		t.Errorf("run_at = %s, want an hour out", j.RunAt)
	}
}

// invoiceArgs is a job's arguments that declare their own settings, which is
// what attributes.Attributes replaces the PHP attributes with.
type invoiceArgs struct {
	ID string
}

func (invoiceArgs) QueueAttributes() attributes.Attributes {
	return attributes.Attributes{
		Tries:   5,
		Timeout: 30 * time.Second,
		Backoff: []time.Duration{10 * time.Second, time.Minute},
	}
}

func (invoiceArgs) DisplayName() string { return "send an invoice" }

// TestCreatePayloadUsingAddsToEveryJob is the seam a tracing package uses, and
// the nil case is how a test takes its hook back.
func TestCreatePayloadUsingAddsToEveryJob(t *testing.T) {
	t.Cleanup(func() { queue.CreatePayloadUsing(nil) })

	queue.CreatePayloadUsing(func(connection, queueName string, j *jobs.Job) {
		j.DisplayName = connection + ":" + queueName + ":" + j.Name
	})
	j, err := queue.CreatePayload(grant(), "database", "reports", "report.monthly", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if j.DisplayName != "database:reports:report.monthly" {
		t.Errorf("displayName = %q, the hook did not run", j.DisplayName)
	}

	queue.CreatePayloadUsing(nil)
	again, err := queue.CreatePayload(grant(), "database", "reports", "report.monthly", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if again.DisplayName != "" {
		t.Errorf("displayName = %q after the hooks were cleared", again.DisplayName)
	}
}

func TestInteractsWithQueueRecordsWhatTheHandlerDecided(t *testing.T) {
	var handler queue.InteractsWithQueue
	handler.WithFakeQueueInteractions()

	if got := handler.Attempts(); got != 1 {
		t.Errorf("attempts = %d on a job being handled for the first time", got)
	}
	if err := handler.Release(context.Background(), time.Minute); err != nil {
		t.Fatalf("Release: %v", err)
	}
	handler.AssertReleased(t, time.Minute).AssertNotFailed(t).AssertNotDeleted(t)

	var second queue.InteractsWithQueue
	second.WithFakeQueueInteractions()
	if err := second.Fail(context.Background(), nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	second.AssertFailed(t).AssertFailedWith(t, queue.ErrManuallyFailed)
}

// TestInteractsWithQueueOutsideAWorkerIsSafe: a handler called directly, with
// no job, must not panic -- which is what the PHP's `if ($this->job)` guards
// buy.
func TestInteractsWithQueueOutsideAWorkerIsSafe(t *testing.T) {
	var handler queue.InteractsWithQueue
	if got := handler.Attempts(); got != 1 {
		t.Errorf("attempts = %d with no job", got)
	}
	if err := handler.Release(context.Background(), time.Minute); err != nil {
		t.Errorf("Release with no job returned %v", err)
	}
	if err := handler.Delete(context.Background()); err != nil {
		t.Errorf("Delete with no job returned %v", err)
	}
}
