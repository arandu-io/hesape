package bus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
)

// UniqueJob is a job that must not be queued twice while one is still in
// flight.
//
// It is Laravel's ShouldBeUnique, and the case it exists for is the one every
// application meets: a button that recalculates a report, pressed four times
// because the first press showed no feedback. Four jobs, four identical
// reports, four times the work -- and none of it wrong enough to notice.
type UniqueJob struct {
	// Key is what "the same job" means. It is per tenant already, because the
	// cache namespaces every key by the tenant on the Grant, so it should name
	// the work and not the customer: "report.monthly:2026-08".
	Key string
	// TTL is how long the claim lasts if nothing releases it. It is required,
	// and it is the deadlock protection: a worker that dies holding a claim
	// blocks that job until the claim expires and there is no other way out.
	// Size it above the longest run of the job.
	TTL time.Duration
	// Queue, Name and Payload are the job itself.
	Queue   string
	Name    string
	Payload any
}

// ErrNoKey is returned when a unique job has no key to be unique by.
var ErrNoKey = errors.New("bus: a unique job needs a key, and an empty one names every job at once")

// PushUnique pushes the job unless one with the same key is already in flight,
// and reports whether it pushed.
//
// False is not an error. "Somebody already queued this" is the answer the
// caller asked for, and a handler that treats it as a failure turns the fourth
// button press into a red banner.
//
// The claim is taken before the push and given back by ReleaseUnique, which the
// handler calls when the work is done. It is an atomic add in the cache rather
// than a cache.Lock, and that is not a second kind of lock: a cache.Lock is held
// by a handle in one process's memory, and the process that releases this claim
// is not the one that took it. What crosses that gap is a key that is either
// there or not.
//
// If the claim is taken and the push then fails, the claim is given back, so a
// queue that was briefly down does not silence the job for a whole TTL.
func PushUnique(ctx context.Context, g auth.Grant, c *cache.Repository, q Queue, j UniqueJob) (bool, error) {
	if j.Key == "" {
		return false, ErrNoKey
	}
	if j.Name == "" {
		return false, ErrNoName
	}
	if j.TTL <= 0 {
		return false, fmt.Errorf("%w: the unique job %q has none, and a worker that dies holding the claim would silence it forever", cache.ErrNoTTL, j.Key)
	}
	if c == nil || q == nil {
		return false, fmt.Errorf("bus: pushing the unique job %q needs a cache and a queue", j.Key)
	}

	payload, err := encode(j.Name, j.Payload)
	if err != nil {
		return false, err
	}

	taken, err := c.Add(ctx, g, uniqueKey(j.Key), j.Name, j.TTL)
	if err != nil {
		return false, err
	}
	if !taken {
		return false, nil
	}

	if err := q.Push(ctx, g, j.Queue, j.Name, payload); err != nil {
		// The release runs on a context that is not cancelled with the
		// caller's: a request abandoned at exactly this moment would otherwise
		// leave the claim standing with no job behind it.
		_ = ReleaseUnique(context.WithoutCancel(ctx), g, c, j.Key)
		return false, fmt.Errorf("bus: pushing the unique job %s: %w", j.Name, err)
	}
	return true, nil
}

// ReleaseUnique gives the claim back, so the job can be queued again.
//
// The handler calls it when the work is done, on the failing path as much as on
// the succeeding one -- a job that failed and left its claim standing is a job
// nobody can retry until the TTL runs out. Releasing a claim that has already
// expired is not an error: the caller wanted it gone and it is.
func ReleaseUnique(ctx context.Context, g auth.Grant, c *cache.Repository, key string) error {
	if key == "" {
		return ErrNoKey
	}
	if c == nil {
		return errors.New("bus: releasing a unique job needs a cache")
	}
	return c.Forget(ctx, g, uniqueKey(key))
}

// uniqueKey namespaces the claim, so it cannot collide with an application's
// own cache entry of the same name.
func uniqueKey(key string) string { return "bus:unique:" + key }
