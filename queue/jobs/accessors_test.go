package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/queue/attributes"
	"github.com/arandu-io/hesape/queue/jobs"
)

// TestTheJobAnswersItsOwnSettings is what the payload envelope buys: a job that
// asked for five tries has to be able to say so on the other side, or the
// setting reached nowhere.
func TestTheJobAnswersItsOwnSettings(t *testing.T) {
	deadline := time.Now().Add(time.Hour).UTC()
	j := &jobs.Job{
		Name:     "invoice.send",
		Attempts: 2,
		Attributes: attributes.Attributes{
			Tries:         5,
			MaxExceptions: 2,
			Timeout:       30 * time.Second,
			FailOnTimeout: true,
			RetryUntil:    deadline,
			Backoff:       []time.Duration{time.Second, time.Minute},
		},
	}

	if j.MaxTries() != 5 || j.MaxExceptions() != 2 {
		t.Errorf("maxTries = %d, maxExceptions = %d", j.MaxTries(), j.MaxExceptions())
	}
	if j.Timeout() != 30*time.Second || !j.ShouldFailOnTimeout() {
		t.Errorf("timeout = %s, failOnTimeout = %v", j.Timeout(), j.ShouldFailOnTimeout())
	}
	if !j.RetryUntil().Equal(deadline) {
		t.Errorf("retryUntil = %s", j.RetryUntil())
	}
	// The backoff is the entry for this attempt, which is the second one on the
	// second delivery.
	if j.Backoff() != time.Minute {
		t.Errorf("the second attempt waits %s", j.Backoff())
	}
	// And the last entry repeats once the list runs out, which is what
	// Laravel's `?? last($backoff)` does.
	j.Attempts = 9
	if j.Backoff() != time.Minute {
		t.Errorf("the ninth attempt waits %s, want the last entry", j.Backoff())
	}
}

// TestAJobWithNoSettingsLetsTheWorkerDecide: the zero value has to mean "no
// opinion", or every job silently overrides the worker with a zero.
func TestAJobWithNoSettingsLetsTheWorkerDecide(t *testing.T) {
	j := &jobs.Job{Name: "invoice.send", Attempts: 1}
	if j.MaxTries() != 0 || j.Timeout() != 0 || j.Backoff() != 0 {
		t.Errorf("a job with no settings answers %d, %s, %s", j.MaxTries(), j.Timeout(), j.Backoff())
	}
	if !j.RetryUntil().IsZero() {
		t.Errorf("retryUntil = %s on a job with no deadline", j.RetryUntil())
	}
}

func TestTheJobAnswersItsNames(t *testing.T) {
	j := &jobs.Job{Name: "invoice.send", UUID: "j-1", Queue: "mail"}

	if j.GetName() != "invoice.send" || j.GetJobID() != "j-1" || j.GetQueue() != "mail" {
		t.Errorf("names = %q, %q, %q", j.GetName(), j.GetJobID(), j.GetQueue())
	}
	if j.ResolveName() != "invoice.send" {
		t.Errorf("resolveName = %q with no display name", j.ResolveName())
	}

	j.DisplayName = "send an invoice"
	if j.ResolveName() != "send an invoice" {
		t.Errorf("resolveName = %q", j.ResolveName())
	}
	// The class behind a wrapper is still the routing name: the two only differ
	// in PHP because the wrapper is a class and the routing key is the class.
	if j.ResolveQueuedJobClass() != "invoice.send" {
		t.Errorf("resolveQueuedJobClass = %q", j.ResolveQueuedJobClass())
	}
}

// TestJobNameParseUnderstandsThePhpForm: a payload written by a bridge from a
// PHP application still carries "Class@method", and routing it to the class
// beats failing to route it.
func TestJobNameParseUnderstandsThePhpForm(t *testing.T) {
	class, method := jobs.JobName{}.Parse("App\\Jobs\\SendInvoice@handle")
	if class != "App\\Jobs\\SendInvoice" || method != "handle" {
		t.Errorf("Parse returned %q, %q", class, method)
	}

	class, method = jobs.JobName{}.Parse("invoice.send")
	if class != "invoice.send" || method != "fire" {
		t.Errorf("Parse of a plain name returned %q, %q", class, method)
	}
}

func TestFakeJobRecordsWhatWasDoneToIt(t *testing.T) {
	ctx := context.Background()
	f := jobs.NewFakeJob("invoice.send", "t-1")

	if f.Attempts != 1 {
		t.Errorf("attempts = %d on a fake job being handled", f.Attempts)
	}
	if err := f.Release(ctx, time.Minute); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !f.IsReleased() || f.ReleaseDelay != time.Minute {
		t.Errorf("released = %v after %s", f.IsReleased(), f.ReleaseDelay)
	}

	broken := context.Canceled
	second := jobs.NewFakeJob("invoice.send", "t-1")
	if err := second.Fail(ctx, broken); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if !second.HasFailed() || second.FailedWith != broken {
		t.Errorf("failed = %v with %v", second.HasFailed(), second.FailedWith)
	}
}

func TestDatabaseJobRecordCountsAndStamps(t *testing.T) {
	record := &jobs.DatabaseJobRecord{Job: &jobs.Job{Name: "invoice.send"}}

	if got := record.Increment(); got != 1 {
		t.Errorf("the first delivery counted as %d", got)
	}
	if got := record.Increment(); got != 2 {
		t.Errorf("the second delivery counted as %d", got)
	}

	deadline := record.Touch(time.Minute)
	if deadline.IsZero() || !deadline.After(time.Now()) {
		t.Errorf("the reservation was stamped at %s", deadline)
	}
	// A non-positive lease leaves the deadline alone: unreserving a running job
	// is the one outcome nobody wants.
	if again := record.Touch(0); !again.Equal(deadline) {
		t.Errorf("Touch(0) moved the deadline to %s", again)
	}
}

// TestMarkAsFailedDoesNotSettleTheJob: it is the half of Fail that only records
// the decision, for a handler that already wrote its own failure somewhere.
func TestMarkAsFailedDoesNotSettleTheJob(t *testing.T) {
	j := &jobs.Job{Name: "invoice.send"}
	j.MarkAsFailed()

	if !j.HasFailed() {
		t.Error("MarkAsFailed did not mark the job")
	}
	if j.IsDeleted() || j.IsReleased() {
		t.Error("MarkAsFailed settled the job")
	}
}
