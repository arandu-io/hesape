package middleware

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/queue/jobs"
)

// ThrottlesExceptions stops hammering a dependency that is already failing.
//
// It answers Illuminate\Queue\Middleware\ThrottlesExceptions. It counts
// failures rather than attempts: once a job has failed more than the limit
// allows inside the window, the ones that follow are released without being
// run at all, so a third-party API that is down gets a pause instead of the
// whole queue retrying against it.
//
//	m := middleware.NewThrottlesExceptions(limiter, cache.PerMinute(10)).
//		RetryAfter(5 * time.Minute)
//
// A failure it catches is not returned to the worker: the job is released and
// tries again, which is the point -- the worker's own attempt counter is for
// jobs that are wrong, and this is for dependencies that are down.
type ThrottlesExceptions struct {
	limiter    *cache.RateLimiter
	limit      cache.Limit
	retryAfter time.Duration
	byJob      bool
	when       func(error) bool
}

var _ Middleware = (*ThrottlesExceptions)(nil)

// NewThrottlesExceptions returns the middleware.
func NewThrottlesExceptions(limiter *cache.RateLimiter, limit cache.Limit) *ThrottlesExceptions {
	return &ThrottlesExceptions{limiter: limiter, limit: limit, retryAfter: time.Minute}
}

// RetryAfter is how long a job waits after a failure this middleware caught.
func (m *ThrottlesExceptions) RetryAfter(d time.Duration) *ThrottlesExceptions {
	m.retryAfter = d
	return m
}

// ByJob counts each job separately instead of counting the whole name
// together.
//
// It answers byJob(). Use it when the dependency is per-record rather than
// shared: one broken invoice should not throttle the other nine hundred.
func (m *ThrottlesExceptions) ByJob() *ThrottlesExceptions {
	m.byJob = true
	return m
}

// When narrows which failures are counted. A failure it says no to is returned
// to the worker untouched, which parks the job on the usual schedule.
func (m *ThrottlesExceptions) When(f func(error) bool) *ThrottlesExceptions {
	m.when = f
	return m
}

// Handle refuses while the failures are piling up, and counts the ones it sees.
func (m *ThrottlesExceptions) Handle(ctx context.Context, j *jobs.Job, next func(context.Context) error) error {
	name := m.limit.Key
	if m.byJob {
		name = j.UUID
	}
	limit := m.limit.By(key(j, name))

	// Reading the counter without spending it: Attempt counts and answers, and
	// Release gives the count straight back. It answers Laravel's
	// tooManyAttempts(), which cache.RateLimiter has no method for -- and
	// adding one there for a caller in this package would be a second way to
	// ask a counter a question (RULE 9).
	//
	// The window this counter measures is failures, not deliveries, which is
	// why nothing is spent here and Hit is called below only when something
	// actually failed.
	res, err := m.limiter.Attempt(ctx, limit)
	if err != nil {
		return err
	}
	if err := m.limiter.Release(ctx, limit); err != nil {
		return err
	}
	if !res.OK {
		return j.Release(ctx, res.RetryAfter+retryPadding)
	}

	runErr := next(ctx)
	if runErr == nil {
		return nil
	}
	if m.when != nil && !m.when(runErr) {
		return runErr
	}
	if _, err := m.limiter.Hit(ctx, limit); err != nil {
		return err
	}
	return j.Release(ctx, m.retryAfter)
}
