package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/redis/go-redis/v9"
)

// connection is what a popped job reports as its connection.
const connection = "redis"

// RedisQueue is the queue over RESP.
type RedisQueue struct {
	client *redis.Client
	prefix string
}

// Options is what it takes to connect.
type Options struct {
	// Address is host:port.
	Address string
	// Password and Database come from configuration, never from a literal.
	Password string
	Database int
	// Prefix namespaces the keys, so two applications can share one server.
	Prefix string
}

// New returns the queue over its own client.
func New(opts Options) *RedisQueue {
	if opts.Address == "" {
		opts.Address = "127.0.0.1:6379"
	}
	return &RedisQueue{
		client: redis.NewClient(&redis.Options{
			Addr:        opts.Address,
			Password:    opts.Password,
			DB:          opts.Database,
			DialTimeout: 5 * time.Second,
			ReadTimeout: 3 * time.Second,
		}),
		prefix: opts.Prefix,
	}
}

// Wrap returns a queue over a client the application already opened.
func Wrap(client *redis.Client, prefix string) *RedisQueue {
	return &RedisQueue{client: client, prefix: prefix}
}

var (
	_ queue.Queue = (*RedisQueue)(nil)
	_ jobs.Driver = (*RedisQueue)(nil)
)

// Close releases the pool.
func (q *RedisQueue) Close() error { return q.client.Close() }

// Ping verifies the connection, for the health check.
func (q *RedisQueue) Ping(ctx context.Context) error {
	if err := q.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("queue/redis: %w", err)
	}
	return nil
}

// The key layout. Two keys per queue, one hash for every job body, and one
// list of what gave up:
//
//	<prefix>:q:<queue>:scheduled   sorted set, score = when it becomes eligible
//	<prefix>:q:<queue>:leased      sorted set, score = when the lease expires
//	<prefix>:q:jobs                hash, id -> the serialized job
//	<prefix>:q:parked              sorted set, score = when it gave up
//
// Scheduled and leased are the same shape on purpose: popping is moving an id
// from one to the other, and an expired lease is just an id whose score passed.
func (q *RedisQueue) key(parts ...string) string {
	key := "q"
	for _, p := range parts {
		key += ":" + p
	}
	if q.prefix == "" {
		return key
	}
	return q.prefix + ":" + key
}

// Push adds a job.
func (q *RedisQueue) Push(ctx context.Context, g auth.Grant, j jobs.Job) error {
	// The job has to match the Grant pushing it. See jobs.Authorized.
	if err := jobs.Authorized(g, j); err != nil {
		return err
	}
	j = jobs.Prepare(g, j)

	body, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("queue/redis: serializing %s: %w", j.Name, err)
	}

	pipe := q.client.TxPipeline()
	pipe.HSet(ctx, q.key("jobs"), j.UUID, body)
	pipe.ZAdd(ctx, q.key(j.Queue, "scheduled"), redis.Z{
		Score:  float64(j.RunAt.UnixMilli()),
		Member: j.UUID,
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("queue/redis: pushing %s: %w", j.Name, err)
	}
	return nil
}

// PushOn adds a job to a named queue.
func (q *RedisQueue) PushOn(ctx context.Context, g auth.Grant, name string, j jobs.Job) error {
	j.Queue = name
	return q.Push(ctx, g, j)
}

// Later adds a job that becomes eligible after delay.
func (q *RedisQueue) Later(ctx context.Context, g auth.Grant, delay time.Duration, j jobs.Job) error {
	j.RunAt = time.Now().UTC().Add(delay)
	return q.Push(ctx, g, j)
}

// Bulk adds many jobs.
func (q *RedisQueue) Bulk(ctx context.Context, g auth.Grant, js []jobs.Job) error {
	for _, j := range js {
		if err := q.Push(ctx, g, j); err != nil {
			return err
		}
	}
	return nil
}

// Pop takes jobs whose time has come, and leases them.
//
// It also sweeps expired leases back into the scheduled set first, which is
// what makes a worker that died recoverable: its jobs simply become eligible
// again when the lease passes.
func (q *RedisQueue) Pop(ctx context.Context, name string, n int, lease time.Duration) ([]*jobs.Job, error) {
	if name == "" {
		name = jobs.DefaultQueue
	}
	if n <= 0 {
		n = 1
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	now := time.Now().UTC()

	if err := q.recoverExpiredLeases(ctx, name, now); err != nil {
		return nil, err
	}

	scheduled := q.key(name, "scheduled")
	ids, err := q.client.ZRangeByScore(ctx, scheduled, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(now.UnixMilli(), 10),
		Count: int64(n),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("queue/redis: reading %s: %w", name, err)
	}

	out := make([]*jobs.Job, 0, len(ids))
	for _, id := range ids {
		j, claimed, err := q.claim(ctx, name, id, now, lease)
		if err != nil {
			return nil, err
		}
		if claimed {
			out = append(out, jobs.Popped(q, connection, j))
		}
	}
	return out, nil
}

var errStateChanged = errors.New("queue/redis: the job moved to another state")

// transaction retries an optimistic transaction until it commits, the caller
// cancels, or the callback reports a real error. WATCH is deliberately used
// instead of Lua so the connector remains portable across RESP servers.
func (q *RedisQueue) transaction(ctx context.Context, keys []string, run func(*redis.Tx) error) error {
	for {
		err := q.client.Watch(ctx, run, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// claim moves one eligible id from scheduled to leased and increments its
// attempt in the same transaction. No observer can see the id in neither set.
func (q *RedisQueue) claim(ctx context.Context, name, id string, now time.Time, lease time.Duration) (jobs.Job, bool, error) {
	scheduled := q.key(name, "scheduled")
	leased := q.key(name, "leased")
	var claimed jobs.Job

	err := q.transaction(ctx, []string{scheduled}, func(tx *redis.Tx) error {
		score, err := tx.ZScore(ctx, scheduled, id).Result()
		if errors.Is(err, redis.Nil) || score > float64(now.UnixMilli()) {
			return errStateChanged
		}
		if err != nil {
			return err
		}

		raw, err := tx.HGet(ctx, q.key("jobs"), id).Bytes()
		if err != nil {
			return err
		}
		var j jobs.Job
		if err := json.Unmarshal(raw, &j); err != nil {
			return fmt.Errorf("queue/redis: decoding %s: %w", id, err)
		}
		j.Attempts++
		body, err := json.Marshal(j)
		if err != nil {
			return fmt.Errorf("queue/redis: serializing %s: %w", id, err)
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, q.key("jobs"), id, body)
			pipe.ZRem(ctx, scheduled, id)
			pipe.ZAdd(ctx, leased, redis.Z{
				Score: float64(now.Add(lease).UnixMilli()), Member: id,
			})
			return nil
		})
		if err == nil {
			claimed = j
		}
		return err
	})
	if errors.Is(err, errStateChanged) {
		return jobs.Job{}, false, nil
	}
	if err != nil {
		return jobs.Job{}, false, fmt.Errorf("queue/redis: claiming %s: %w", id, err)
	}
	return claimed, true, nil
}

// recoverExpiredLeases moves jobs whose lease passed back into the queue.
func (q *RedisQueue) recoverExpiredLeases(ctx context.Context, name string, now time.Time) error {
	leased := q.key(name, "leased")
	expired, err := q.client.ZRangeByScore(ctx, leased, &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatInt(now.UnixMilli(), 10),
	}).Result()
	if err != nil {
		return fmt.Errorf("queue/redis: reading the leases of %s: %w", name, err)
	}

	for _, id := range expired {
		if err := q.recoverLease(ctx, name, id, now); err != nil {
			return fmt.Errorf("queue/redis: requeueing %s: %w", id, err)
		}
	}
	return nil
}

func (q *RedisQueue) recoverLease(ctx context.Context, name, id string, now time.Time) error {
	leased := q.key(name, "leased")
	scheduled := q.key(name, "scheduled")
	err := q.transaction(ctx, []string{leased}, func(tx *redis.Tx) error {
		score, err := tx.ZScore(ctx, leased, id).Result()
		if errors.Is(err, redis.Nil) || score > float64(now.UnixMilli()) {
			return errStateChanged
		}
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.ZRem(ctx, leased, id)
			pipe.ZAdd(ctx, scheduled, redis.Z{Score: float64(now.UnixMilli()), Member: id})
			return nil
		})
		return err
	})
	if errors.Is(err, errStateChanged) {
		return nil
	}
	return err
}

// DeleteJob removes a finished job.
func (q *RedisQueue) DeleteJob(ctx context.Context, j *jobs.Job) error {
	if err := q.settleLeased(ctx, j, func(tx *redis.Tx) error {
		_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.ZRem(ctx, q.key(j.Queue, "leased"), j.UUID)
			pipe.HDel(ctx, q.key("jobs"), j.UUID)
			return nil
		})
		return err
	}); err != nil {
		return fmt.Errorf("queue/redis: deleting %s: %w", j.UUID, err)
	}
	return nil
}

// ReleaseJob puts the job back on its queue, eligible again after delay.
func (q *RedisQueue) ReleaseJob(ctx context.Context, j *jobs.Job, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}
	runAt := time.Now().UTC().Add(delay)
	j.RunAt = runAt

	body, err := json.Marshal(*j)
	if err != nil {
		return fmt.Errorf("queue/redis: serializing %s: %w", j.UUID, err)
	}

	if err := q.settleLeased(ctx, j, func(tx *redis.Tx) error {
		_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, q.key("jobs"), j.UUID, body)
			pipe.ZRem(ctx, q.key(j.Queue, "leased"), j.UUID)
			pipe.ZAdd(ctx, q.key(j.Queue, "scheduled"), redis.Z{
				Score: float64(runAt.UnixMilli()), Member: j.UUID,
			})
			return nil
		})
		return err
	}); err != nil {
		return fmt.Errorf("queue/redis: releasing %s: %w", j.UUID, err)
	}
	return nil
}

// FailJob parks the job.
func (q *RedisQueue) FailJob(ctx context.Context, j *jobs.Job, cause error) error {
	if cause != nil {
		j.LastError = cause.Error()
	}
	body, err := json.Marshal(*j)
	if err != nil {
		return fmt.Errorf("queue/redis: serializing %s: %w", j.UUID, err)
	}

	failedAt := time.Now().UTC()
	if err := q.settleLeased(ctx, j, func(tx *redis.Tx) error {
		_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, q.key("jobs"), j.UUID, body)
			pipe.ZRem(ctx, q.key(j.Queue, "leased"), j.UUID)
			pipe.ZAdd(ctx, q.key("parked"), redis.Z{
				Score: float64(failedAt.UnixMilli()), Member: j.UUID,
			})
			return nil
		})
		return err
	}); err != nil {
		return fmt.Errorf("queue/redis: parking %s: %w", j.UUID, err)
	}
	return nil
}

// settleLeased applies one outcome only to the delivery represented by j. The
// attempt count is the delivery generation: if a lease expired and another
// worker already claimed the job, the old pointer cannot delete or release the
// new worker's delivery.
func (q *RedisQueue) settleLeased(ctx context.Context, j *jobs.Job, settle func(*redis.Tx) error) error {
	leased := q.key(j.Queue, "leased")
	return q.transaction(ctx, []string{leased}, func(tx *redis.Tx) error {
		if _, err := tx.ZScore(ctx, leased, j.UUID).Result(); err != nil {
			if errors.Is(err, redis.Nil) {
				return errors.New("the job is not leased for this delivery")
			}
			return err
		}

		stored, err := q.loadFrom(ctx, tx, j.UUID)
		if err != nil {
			return err
		}
		if stored.Attempts != j.Attempts {
			return fmt.Errorf("the lease belongs to attempt %d, not stale attempt %d", stored.Attempts, j.Attempts)
		}
		return settle(tx)
	})
}

// Failed lists the jobs that gave up, most recent failure first.
func (q *RedisQueue) Failed(ctx context.Context, limit int) ([]jobs.Job, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := q.client.ZRevRange(ctx, q.key("parked"), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("queue/redis: reading the parked jobs: %w", err)
	}

	out := make([]jobs.Job, 0, len(ids))
	for _, id := range ids {
		j, err := q.load(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

// Retry puts a parked job back in line with its attempts reset.
func (q *RedisQueue) Retry(ctx context.Context, uuid string) error {
	parked := q.key("parked")
	err := q.transaction(ctx, []string{parked}, func(tx *redis.Tx) error {
		if _, err := tx.ZScore(ctx, parked, uuid).Result(); err != nil {
			if errors.Is(err, redis.Nil) {
				return fmt.Errorf("%w: %s", queue.ErrNotParked, uuid)
			}
			return err
		}

		j, err := q.loadFrom(ctx, tx, uuid)
		if err != nil {
			return err
		}
		j.Attempts = 0
		j.Exceptions = 0
		j.LastError = ""
		j.RunAt = time.Now().UTC()

		body, err := json.Marshal(j)
		if err != nil {
			return fmt.Errorf("queue/redis: serializing %s: %w", uuid, err)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, q.key("jobs"), uuid, body)
			pipe.ZRem(ctx, parked, uuid)
			pipe.ZAdd(ctx, q.key(j.Queue, "scheduled"), redis.Z{
				Score: float64(j.RunAt.UnixMilli()), Member: uuid,
			})
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("queue/redis: retrying %s: %w", uuid, err)
	}
	return nil
}

// Size is how many jobs the queue holds, waiting or in flight.
func (q *RedisQueue) Size(ctx context.Context, name string) (int, error) {
	if name == "" {
		name = jobs.DefaultQueue
	}
	waiting, err := q.client.ZCard(ctx, q.key(name, "scheduled")).Result()
	if err != nil {
		return 0, fmt.Errorf("queue/redis: sizing %s: %w", name, err)
	}
	running, err := q.client.ZCard(ctx, q.key(name, "leased")).Result()
	if err != nil {
		return 0, fmt.Errorf("queue/redis: sizing %s: %w", name, err)
	}
	return int(waiting + running), nil
}

// PendingSize is how many jobs are waiting. A leased job is running, not
// waiting.
func (q *RedisQueue) PendingSize(ctx context.Context, name string) (int, error) {
	if name == "" {
		name = jobs.DefaultQueue
	}
	count, err := q.client.ZCard(ctx, q.key(name, "scheduled")).Result()
	if err != nil {
		return 0, fmt.Errorf("queue/redis: counting %s: %w", name, err)
	}
	return int(count), nil
}

// CreationTimeOfOldestPendingJob is when the oldest waiting job became
// eligible, or the zero time when nothing is waiting.
func (q *RedisQueue) CreationTimeOfOldestPendingJob(ctx context.Context, name string) (time.Time, error) {
	if name == "" {
		name = jobs.DefaultQueue
	}

	first, err := q.client.ZRangeWithScores(ctx, q.key(name, "scheduled"), 0, 0).Result()
	if err != nil {
		return time.Time{}, fmt.Errorf("queue/redis: measuring the age of %s: %w", name, err)
	}
	if len(first) == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(int64(first[0].Score)).UTC(), nil
}

// Clear removes every job waiting or in flight on a queue, and returns how many
// went.
//
// Parked jobs are not cleared: a job that gave up is no longer on a queue, it
// is in the dead letter list, and Failed and Retry are how it is dealt with.
// The DatabaseQueue draws the line in the same place.
func (q *RedisQueue) Clear(ctx context.Context, name string) (int, error) {
	if name == "" {
		name = jobs.DefaultQueue
	}

	var removed int
	for _, set := range []string{"scheduled", "leased"} {
		key := q.key(name, set)
		ids, err := q.client.ZRange(ctx, key, 0, -1).Result()
		if err != nil {
			return removed, fmt.Errorf("queue/redis: clearing %s: %w", name, err)
		}
		if len(ids) == 0 {
			continue
		}
		pipe := q.client.TxPipeline()
		pipe.HDel(ctx, q.key("jobs"), ids...)
		pipe.Del(ctx, key)
		if _, err := pipe.Exec(ctx); err != nil {
			return removed, fmt.Errorf("queue/redis: clearing %s: %w", name, err)
		}
		removed += len(ids)
	}
	return removed, nil
}

func (q *RedisQueue) load(ctx context.Context, id string) (jobs.Job, error) {
	return q.loadFrom(ctx, q.client, id)
}

type hashGetter interface {
	HGet(ctx context.Context, key, field string) *redis.StringCmd
}

func (q *RedisQueue) loadFrom(ctx context.Context, source hashGetter, id string) (jobs.Job, error) {
	raw, err := source.HGet(ctx, q.key("jobs"), id).Bytes()
	if err != nil {
		return jobs.Job{}, fmt.Errorf("queue/redis: reading %s: %w", id, err)
	}
	var j jobs.Job
	if err := json.Unmarshal(raw, &j); err != nil {
		return jobs.Job{}, fmt.Errorf("queue/redis: decoding %s: %w", id, err)
	}
	return j, nil
}
