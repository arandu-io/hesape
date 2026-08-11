package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/log"
)

// Publisher is where events go once they are committed.
//
// The framework does not pick one. NATS, a webhook, an in-process handler and a
// queue are all the same shape from here, and the choice belongs to the
// application -- what the framework guarantees is that whatever you plug in
// receives every event that was stored, at least once.
type Publisher interface {
	Publish(ctx context.Context, e Stored) error
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(ctx context.Context, e Stored) error

// Publish calls f.
func (f PublisherFunc) Publish(ctx context.Context, e Stored) error { return f(ctx, e) }

// RelayOptions configures the relay.
type RelayOptions struct {
	// Interval is how often the outbox is polled. Default 1s.
	//
	// Polling rather than LISTEN/NOTIFY, and that is a deliberate trade:
	// LISTEN/NOTIFY is lower latency and is Postgres-specific, which would put a
	// driver dependency in the core and give SQLite and MySQL a second code
	// path. One second of latency on a background publish is not the problem
	// this framework exists to solve.
	Interval time.Duration
	// Batch is how many events one pass publishes. Default 100.
	Batch int
	// MaxAttempts is how many failures an event gets before it is parked.
	// Default 10.
	MaxAttempts int
	// LockTTL bounds how long one pass may hold the lock. Default 30s.
	LockTTL time.Duration
	// Locks keeps N replicas from publishing the same event N times. Nil means
	// a single replica, and with more than one it means each of them publishes
	// every event.
	//
	// It is cache.Locks and not an interface of this package's own. There used
	// to be a Locker interface declared here, a second one in the kernel and a
	// third implementation in the kv adapter; one lock in the collection is
	// what replaced all three, and it is the same field the scheduler takes.
	//
	// What a missing lock costs is duplicate delivery, which every consumer
	// here has to tolerate anyway, because delivery is at-least-once by design.
	Locks *cache.Locks
}

func (o RelayOptions) withDefaults() RelayOptions {
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	if o.Batch <= 0 {
		o.Batch = 100
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 10
	}
	if o.LockTTL <= 0 {
		o.LockTTL = 30 * time.Second
	}
	return o
}

// Relay publishes what the outbox stored.
//
// Delivery is at-least-once, and that is not a limitation to fix -- it is the
// price of never losing an event. The consumer deduplicates on Stored.ID, which
// is why the id is stable and why it travels with the event.
type Relay struct {
	outbox    *Outbox
	publisher Publisher
	opts      RelayOptions
}

// NewRelay returns the relay.
func NewRelay(o *Outbox, p Publisher, opts RelayOptions) *Relay {
	return &Relay{outbox: o, publisher: p, opts: opts.withDefaults()}
}

// Run polls until the context is cancelled.
//
// It is started by the module at boot and stopped at shutdown, in the same
// process as the application -- like the scheduler, and for the same reason: a
// second deployable to run background work is a second thing to monitor, page
// on, and forget to restart.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.opts.Interval)
	defer ticker.Stop()

	logger := log.For(ctx).With("component", "outbox-relay")

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if err := r.pass(ctx); err != nil && !errors.Is(err, context.Canceled) {
			// A failed pass is not fatal: the next tick tries again, and the
			// events are still in the table. Stopping the relay because the
			// database blinked would turn a hiccup into a backlog.
			logger.Warn("outbox pass failed", "error", err)
		}
	}
}

// pass publishes one batch, under the lock when there is one.
//
// It is Cache::lock('outbox-relay', $ttl)->get($callback): the lock runs the
// batch when it took it, and answers false when another replica holds it --
// which is the lock working, not a failure, and logging it every second would
// bury everything else. This used to read the refusal out of the error message
// with strings.Contains, because the lock came from an adapter the core could
// not import.
func (r *Relay) pass(ctx context.Context) error {
	if r.opts.Locks == nil {
		return r.publishBatch(ctx)
	}

	_, err := r.opts.Locks.Lock("outbox-relay", r.opts.LockTTL).Get(ctx, r.publishBatch)
	return err
}

// publishBatch publishes the oldest unpublished events.
func (r *Relay) publishBatch(ctx context.Context) error {
	pending, err := r.outbox.PendingAll(ctx, r.opts.Batch)
	if err != nil {
		return err
	}

	logger := log.For(ctx).With("component", "outbox-relay")

	for _, e := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := r.publisher.Publish(ctx, e); err != nil {
			attempts := e.Attempts + 1
			if attempts >= r.opts.MaxAttempts {
				// Parked rather than retried forever. An event that has failed
				// ten times is not going to succeed on the eleventh, and a relay
				// stuck on it stops delivering everything behind it.
				if parkErr := r.outbox.Park(ctx, e.ID, err); parkErr != nil {
					return parkErr
				}
				logger.Error("event parked after repeated failures",
					"event", e.Name, "id", e.ID, "attempts", attempts, "error", err)
				continue
			}
			if markErr := r.outbox.MarkFailed(ctx, e.ID, err); markErr != nil {
				return markErr
			}
			logger.Warn("publishing failed", "event", e.Name, "id", e.ID, "attempt", attempts, "error", err)
			continue
		}

		if err := r.outbox.MarkPublished(ctx, e.ID); err != nil {
			// The event was delivered and the mark failed, so the next pass
			// delivers it again. That is at-least-once behaving exactly as
			// documented, and it is why the consumer deduplicates on the id.
			return fmt.Errorf("published %s and could not mark it: %w", e.ID, err)
		}
	}
	return nil
}

// Drain publishes everything pending, once, and returns.
//
// This is what a test uses. There is no synchronous mode -- the test runs the
// same code path as production, with the relay executed inline instead of on a
// ticker. "Sync only in tests" is a second way to do one thing, and the second
// way always leaks into production.
func (r *Relay) Drain(ctx context.Context) error {
	return r.publishBatch(ctx)
}

// Parked returns the events that gave up, for the diagnosis and for whoever is
// deciding whether to retry them.
func (r *Relay) Parked(ctx context.Context, limit int) ([]Stored, error) {
	return r.outbox.Parked(ctx, limit)
}

// Lag is how long the oldest unpublished event has been waiting.
//
// This is the number that matters: a relay that stopped looks exactly like a
// relay with nothing to do, and only the age of the oldest pending event tells
// them apart. It feeds the health check and the hint on the error page.
func (r *Relay) Lag(ctx context.Context) (time.Duration, error) {
	return r.outbox.Lag(ctx)
}
