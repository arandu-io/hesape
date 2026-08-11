package middleware_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/arandu-io/hesape/queue/middleware"
)

const (
	tenant = "11111111-1111-4111-8111-111111111111"
	other  = "22222222-2222-4222-8222-222222222222"
)

func grantFor(t string) auth.Grant { return auth.SystemGrant("invoice.send", t) }

// recorder is the queue a job in these tests came off: it records how the job
// was settled and does nothing else.
type recorder struct {
	mu       sync.Mutex
	released []time.Duration
	deleted  int
	failed   []error
}

var _ jobs.Driver = (*recorder)(nil)

func (r *recorder) ReleaseJob(_ context.Context, _ *jobs.Job, delay time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, delay)
	return nil
}

func (r *recorder) DeleteJob(context.Context, *jobs.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted++
	return nil
}

func (r *recorder) FailJob(_ context.Context, _ *jobs.Job, cause error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, cause)
	return nil
}

// job builds a popped job belonging to a tenant.
func job(t *testing.T, tenantID, name string) (*jobs.Job, *recorder) {
	t.Helper()
	j, err := jobs.New(grantFor(tenantID), "", name, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	return jobs.Popped(r, "test", j), r
}

func ran(flag *bool) func(context.Context) error {
	return func(context.Context) error {
		*flag = true
		return nil
	}
}

func TestWithoutOverlappingRunsOneAtATime(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	first, _ := job(t, tenant, "invoice.send")
	second, secondQueue := job(t, tenant, "invoice.send")

	m := middleware.NewWithoutOverlapping(locks, "acct-1").ReleaseAfter(10 * time.Second)

	// The first job holds the lock while the second one asks for it.
	inside := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.Handle(context.Background(), first, func(context.Context) error {
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside

	var secondRan bool
	if err := m.Handle(context.Background(), second, ran(&secondRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the first job: %v", err)
	}

	if secondRan {
		t.Error("two jobs with the same key ran at once")
	}
	if len(secondQueue.released) != 1 || secondQueue.released[0] != 10*time.Second {
		t.Errorf("the blocked job was released as %v, want once after ten seconds", secondQueue.released)
	}
}

// TestWithoutOverlappingIsScopedByTenant is RULE 14 at a lock. Without the
// tenant in the key, one customer's slow import blocks every other customer's.
func TestWithoutOverlappingIsScopedByTenant(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	mine, _ := job(t, tenant, "invoice.send")
	theirs, theirQueue := job(t, other, "invoice.send")

	m := middleware.NewWithoutOverlapping(locks, "acct-1")
	if m.GetLockKey(mine) == m.GetLockKey(theirs) {
		t.Fatalf("two tenants share the lock %q", m.GetLockKey(mine))
	}

	inside := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.Handle(context.Background(), mine, func(context.Context) error {
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside

	var theirsRan bool
	if err := m.Handle(context.Background(), theirs, ran(&theirsRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	close(release)
	<-done

	if !theirsRan {
		t.Error("one tenant's job blocked another tenant's")
	}
	if len(theirQueue.released) != 0 {
		t.Error("the other tenant's job was released")
	}
}

func TestWithoutOverlappingCanDropInsteadOfReleasing(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	first, _ := job(t, tenant, "invoice.send")
	second, secondQueue := job(t, tenant, "invoice.send")

	m := middleware.NewWithoutOverlapping(locks, "acct-1").DontRelease()

	inside := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = m.Handle(context.Background(), first, func(context.Context) error {
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside

	var secondRan bool
	if err := m.Handle(context.Background(), second, ran(&secondRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	close(release)

	if secondRan || len(secondQueue.released) != 0 {
		t.Errorf("ran = %v, released = %v, want the job dropped", secondRan, secondQueue.released)
	}
}

func TestRateLimitedReleasesTheJobOverBudget(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	m := middleware.NewRateLimited(limiter, cache.PerMinute(1))

	first, _ := job(t, tenant, "invoice.send")
	var firstRan bool
	if err := m.Handle(context.Background(), first, ran(&firstRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !firstRan {
		t.Fatal("the first job did not run")
	}

	second, secondQueue := job(t, tenant, "invoice.send")
	var secondRan bool
	if err := m.Handle(context.Background(), second, ran(&secondRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if secondRan {
		t.Error("the job over budget ran anyway")
	}
	if len(secondQueue.released) != 1 || secondQueue.released[0] <= 0 {
		t.Errorf("the job over budget was released as %v, want a wait", secondQueue.released)
	}
}

// TestRateLimitedIsScopedByTenant is RULE 14 at a counter: one customer must
// not be able to spend another customer's budget.
func TestRateLimitedIsScopedByTenant(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	m := middleware.NewRateLimited(limiter, cache.PerMinute(1))

	mine, _ := job(t, tenant, "invoice.send")
	var mineRan bool
	_ = m.Handle(context.Background(), mine, ran(&mineRan))

	theirs, theirQueue := job(t, other, "invoice.send")
	var theirsRan bool
	if err := m.Handle(context.Background(), theirs, ran(&theirsRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !theirsRan {
		t.Error("one tenant spent another tenant's budget")
	}
	if len(theirQueue.released) != 0 {
		t.Error("the other tenant's job was released")
	}
}

// TestThrottlesExceptionsStopsHammeringAFailingDependency: after the allowed
// number of failures, the next job is released without running at all.
func TestThrottlesExceptionsStopsHammeringAFailingDependency(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	m := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(1)).RetryAfter(time.Minute)
	broken := errors.New("the broker refused")

	first, firstQueue := job(t, tenant, "invoice.send")
	if err := m.Handle(context.Background(), first, func(context.Context) error { return broken }); err != nil {
		t.Fatalf("a failure it caught was returned to the worker: %v", err)
	}
	if len(firstQueue.released) != 1 || firstQueue.released[0] != time.Minute {
		t.Errorf("the failed job was released as %v, want once after a minute", firstQueue.released)
	}

	second, secondQueue := job(t, tenant, "invoice.send")
	var secondRan bool
	if err := m.Handle(context.Background(), second, ran(&secondRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if secondRan {
		t.Error("the job ran against a dependency that had already failed too often")
	}
	if len(secondQueue.released) != 1 {
		t.Errorf("the throttled job was released as %v", secondQueue.released)
	}
}

// TestThrottlesExceptionsPassesOnWhatItDoesNotClaim: a failure the predicate
// says no to goes back to the worker, which parks it on the usual schedule.
func TestThrottlesExceptionsPassesOnWhatItDoesNotClaim(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	m := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(5)).
		When(func(error) bool { return false })

	j, q := job(t, tenant, "invoice.send")
	broken := errors.New("the payload is malformed")
	if err := m.Handle(context.Background(), j, func(context.Context) error { return broken }); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the failure back", err)
	}
	if len(q.released) != 0 {
		t.Error("a failure it does not claim was released anyway")
	}
}

func TestSkip(t *testing.T) {
	j, _ := job(t, tenant, "invoice.send")

	var whenRan bool
	if err := (middleware.Skip{}).When(true).Handle(context.Background(), j, ran(&whenRan)); err != nil {
		t.Fatal(err)
	}
	if whenRan {
		t.Error("Skip{}.When(true) ran the job")
	}

	var unlessRan bool
	if err := (middleware.Skip{}).Unless(true).Handle(context.Background(), j, ran(&unlessRan)); err != nil {
		t.Fatal(err)
	}
	if !unlessRan {
		t.Error("Skip{}.Unless(true) skipped the job")
	}
}

func TestFailOnExceptionParksAndStillReportsTheFailure(t *testing.T) {
	malformed := errors.New("the payload is malformed")
	m := middleware.NewFailOnException(func(err error) bool { return errors.Is(err, malformed) })

	j, q := job(t, tenant, "invoice.send")
	if err := m.Handle(context.Background(), j, func(context.Context) error { return malformed }); !errors.Is(err, malformed) {
		t.Fatalf("err = %v, want the failure back", err)
	}
	if len(q.failed) != 1 {
		t.Fatalf("the job was not parked: %v", q.failed)
	}
	if !j.HasFailed() {
		t.Error("the job does not report itself parked, so the worker would park it again")
	}

	// A failure the predicate says no to is left to the worker.
	other, otherQueue := job(t, tenant, "invoice.send")
	timeout := errors.New("the broker timed out")
	if err := m.Handle(context.Background(), other, func(context.Context) error { return timeout }); !errors.Is(err, timeout) {
		t.Fatalf("err = %v", err)
	}
	if len(otherQueue.failed) != 0 {
		t.Error("a failure it does not claim was parked")
	}
}

func TestSkipIfBatchCancelled(t *testing.T) {
	ctx := context.Background()
	g := grantFor(tenant)
	store := bus.NewMemory()

	pushed := &pushRecorder{}
	batch, err := bus.NewBatch("import").
		Add("invoice.send", nil).
		Add("invoice.send", nil).
		Dispatch(ctx, g, store, pushed)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	m := middleware.NewSkipIfBatchCancelled(store)

	// While the batch is live, the job runs.
	live, _ := jobs.New(g, "", "invoice.send", nil)
	live.Payload = pushed.payloads[0]
	liveJob := jobs.Popped(&recorder{}, "test", live)
	var liveRan bool
	if err := m.Handle(ctx, liveJob, ran(&liveRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !liveRan {
		t.Fatal("a job in a live batch was skipped")
	}

	// Cancelling cannot recall what is already queued, so the rest are
	// delivered, they ask, and they skip.
	if err := store.Cancel(ctx, g, batch.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	cancelled, _ := jobs.New(g, "", "invoice.send", nil)
	cancelled.Payload = pushed.payloads[1]
	cancelledJob := jobs.Popped(&recorder{}, "test", cancelled)
	var cancelledRan bool
	if err := m.Handle(ctx, cancelledJob, ran(&cancelledRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if cancelledRan {
		t.Error("a job whose batch was cancelled ran anyway")
	}
}

// TestAJobInNoBatchIsNotSkipped: the middleware has to be safe on a worker that
// also runs jobs pushed on their own.
func TestAJobInNoBatchIsNotSkipped(t *testing.T) {
	m := middleware.NewSkipIfBatchCancelled(bus.NewMemory())
	j, _ := job(t, tenant, "invoice.send")

	var jobRan bool
	if err := m.Handle(context.Background(), j, ran(&jobRan)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !jobRan {
		t.Error("a job in no batch was skipped")
	}
}

// pushRecorder is the bus.Queue a dispatched batch is pushed onto: it keeps the
// envelopes so a test can hand one to a job.
type pushRecorder struct {
	payloads [][]byte
}

func (p *pushRecorder) Push(_ context.Context, _ auth.Grant, _, _ string, payload []byte) error {
	p.payloads = append(p.payloads, payload)
	return nil
}

// TestThrottlesExceptionsDropsWhatCannotSucceed: a failure that means the work
// is pointless is not evidence that the dependency is down, so it is neither
// counted nor released.
func TestThrottlesExceptionsDropsWhatCannotSucceed(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	gone := errors.New("the invoice was deleted")

	dropping := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(5)).
		DeleteWhen(func(err error) bool { return errors.Is(err, gone) })
	j, q := job(t, tenant, "invoice.send")
	if err := dropping.Handle(context.Background(), j, func(context.Context) error { return gone }); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if q.deleted != 1 || len(q.released) != 0 {
		t.Errorf("deleted = %d, released = %v, want the job dropped", q.deleted, q.released)
	}

	failing := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(5)).
		FailWhen(func(err error) bool { return errors.Is(err, gone) })
	second, secondQueue := job(t, tenant, "invoice.send")
	if err := failing.Handle(context.Background(), second, func(context.Context) error { return gone }); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(secondQueue.failed) != 1 || len(secondQueue.released) != 0 {
		t.Errorf("failed = %v, released = %v, want the job parked", secondQueue.failed, secondQueue.released)
	}
}

// TestThrottlesExceptionsCountsPerKey: By names what is actually failing, so
// one account's broken integration does not throttle every other account's.
func TestThrottlesExceptionsCountsPerKey(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	broken := errors.New("the broker refused")
	m := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(1)).By("acct-1")

	first, _ := job(t, tenant, "invoice.send")
	_ = m.Handle(context.Background(), first, func(context.Context) error { return broken })

	// A different key has its own budget, so the job runs.
	other := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(1)).By("acct-2")
	second, secondQueue := job(t, tenant, "invoice.send")
	var ranSecond bool
	if err := other.Handle(context.Background(), second, ran(&ranSecond)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !ranSecond || len(secondQueue.released) != 0 {
		t.Errorf("one key's failures throttled another: ran = %v, released = %v", ranSecond, secondQueue.released)
	}
}

// TestThrottlesExceptionsReportIsAskedBeforeAnythingIsReported: a dependency
// that is down produces one failure per job, and reporting all of them is how
// the report becomes noise.
func TestThrottlesExceptionsReportIsAskedBeforeAnythingIsReported(t *testing.T) {
	limiter := cache.NewRateLimiter(cache.NewArrayStore())
	timeout := errors.New("the broker timed out")

	m := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(5)).
		Report(func(err error) bool { return !errors.Is(err, timeout) })

	if m.ShouldReport(timeout) {
		t.Error("a failure the predicate said no to is reportable")
	}
	if !m.ShouldReport(errors.New("something else")) {
		t.Error("a failure the predicate said yes to is not reportable")
	}
	// No predicate reports everything, which is the PHP's default.
	if !middleware.NewThrottlesExceptions(limiter, cache.PerMinute(5)).ShouldReport(timeout) {
		t.Error("a middleware with no predicate refused to report")
	}
}
