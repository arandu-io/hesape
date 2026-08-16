package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

const tenant = "11111111-1111-4111-8111-111111111111"

func grant() auth.Grant { return auth.SystemGrant("invoice.send", tenant) }

// settled records what a job did about itself.
type settled struct {
	released time.Duration
	deleted  bool
	failed   error
}

func (s *settled) ReleaseJob(_ context.Context, _ *jobs.Job, delay time.Duration) error {
	s.released = delay
	return nil
}

func (s *settled) DeleteJob(context.Context, *jobs.Job) error {
	s.deleted = true
	return nil
}

func (s *settled) FailJob(_ context.Context, _ *jobs.Job, cause error) error {
	s.failed = cause
	return nil
}

// TestPrepareWritesTheRunTimeInUTC is a bug an audit found: a RunAt supplied by
// the caller kept whatever zone it arrived in, so a job scheduled from a UTC-3
// machine was stored three hours in the past on an engine that compares
// timestamps as text, and ran immediately.
func TestPrepareWritesTheRunTimeInUTC(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	j := jobs.Job{Name: "invoice.send", RunAt: time.Now().In(zone)}

	prepared := jobs.Prepare(grant(), j)
	if prepared.RunAt.Location() != time.UTC {
		t.Errorf("run_at is in %s", prepared.RunAt.Location())
	}
	if prepared.Queue != jobs.DefaultQueue {
		t.Errorf("queue = %q, want the default", prepared.Queue)
	}
	if prepared.TenantID != tenant {
		t.Errorf("tenant = %q, want the one on the Grant", prepared.TenantID)
	}
}

func TestPrepareDefaultsTheRunTimeToNow(t *testing.T) {
	before := time.Now().UTC()
	prepared := jobs.Prepare(grant(), jobs.Job{Name: "invoice.send"})
	if prepared.RunAt.Before(before) {
		t.Errorf("run_at = %s, want now or later", prepared.RunAt)
	}
}

// TestFailSettlesTheJob: parking a job marks it deleted too, and the worker
// reads DeletedOrReleased to find out whether it still has a decision to
// make.
func TestFailSettlesTheJob(t *testing.T) {
	driver := &settled{}
	j := jobs.Popped(driver, "test", jobs.Job{UUID: "j-1", Name: "invoice.send"})

	broken := errors.New("the payload is malformed")
	if err := j.Fail(context.Background(), broken); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if !errors.Is(driver.failed, broken) {
		t.Errorf("the driver was told %v", driver.failed)
	}
	if !j.HasFailed() || !j.IsDeleted() || !j.IsDeletedOrReleased() {
		t.Errorf("failed = %v, deleted = %v", j.HasFailed(), j.IsDeleted())
	}
	// The reason is on the job, so a driver that stores it does not have to be
	// handed it twice.
	if j.LastError != broken.Error() {
		t.Errorf("last error = %q", j.LastError)
	}
}

func TestReleaseIsNotADelete(t *testing.T) {
	driver := &settled{}
	j := jobs.Popped(driver, "test", jobs.Job{UUID: "j-1", Name: "invoice.send"})

	if err := j.Release(context.Background(), 10*time.Second); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if driver.released != 10*time.Second {
		t.Errorf("the driver was told %s", driver.released)
	}
	if !j.IsReleased() || j.IsDeleted() || j.HasFailed() {
		t.Errorf("released = %v, deleted = %v, failed = %v", j.IsReleased(), j.IsDeleted(), j.HasFailed())
	}
	if !j.IsDeletedOrReleased() {
		t.Error("a released job does not report itself settled, so the worker would settle it again")
	}
}

// TestADetachedJobSaysSo beats a nil dereference inside a handler.
func TestADetachedJobSaysSo(t *testing.T) {
	j, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, settle := range map[string]func() error{
		"Release": func() error { return j.Release(context.Background(), 0) },
		"Delete":  func() error { return j.Delete(context.Background()) },
		"Fail":    func() error { return j.Fail(context.Background(), errors.New("x")) },
	} {
		if err := settle(); !errors.Is(err, jobs.ErrDetached) {
			t.Errorf("%s = %v, want ErrDetached", name, err)
		}
	}
}

func TestTheConnectionTravelsWithTheJob(t *testing.T) {
	j := jobs.Popped(&settled{}, "redis", jobs.Job{UUID: "j-1", Name: "invoice.send"})
	if j.GetConnectionName() != "redis" {
		t.Errorf("connection = %q", j.GetConnectionName())
	}
}
