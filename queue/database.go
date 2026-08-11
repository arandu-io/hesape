package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/queue/jobs"
)

// DatabaseQueue is the queue backed by the application's own database.
//
// It answers Illuminate\Queue\DatabaseQueue, and it is the default driver for
// the reason Laravel's is: it needs nothing installed. The jobs table sits in
// the database the application already has.
//
// What it offers that no other driver can is the outbox guarantee. A job pushed
// inside database.Transaction is committed by the same transaction as the row it
// is about, so it exists if and only if the write did -- the mechanism the
// events package uses for events, applied to work. That is the mechanism and
// not the name: this is DatabaseQueue because that is what Laravel calls the
// queue that lives in the application's database (ADR 0044), and naming it
// after the guarantee would hide it from everyone looking for the driver.
type DatabaseQueue struct {
	db *database.DB
}

// NewDatabaseQueue returns the queue over db.
func NewDatabaseQueue(db *database.DB) *DatabaseQueue { return &DatabaseQueue{db: db} }

var (
	_ Queue       = (*DatabaseQueue)(nil)
	_ jobs.Driver = (*DatabaseQueue)(nil)
)

// databaseConnection is what a popped job reports as its connection.
const databaseConnection = "database"

// Migrations returns the jobs table.
//
// It answers Laravel's `queue:table`, which generates the migration for this
// driver and only this one. [Module] collects it, so an application wired to
// another driver declares no schema for a table it will never read.
func (q *DatabaseQueue) Migrations() []database.Migration {
	return []database.Migration{{
		ID: "2026_07_31_000010_create_jobs_table",
		// Portable types only: TEXT, INTEGER and TIMESTAMP mean the same thing
		// on SQLite, Postgres and MySQL.
		Up: `
CREATE TABLE jobs (
    id             VARCHAR(255) PRIMARY KEY,
    -- queue is indexed, so VARCHAR rather than TEXT: see database.KeyText.
    queue          VARCHAR(255) NOT NULL,
    name           TEXT NOT NULL,
    tenant_id      VARCHAR(255) NOT NULL,
    payload        TEXT NOT NULL,
    authorized_by  TEXT NOT NULL,
    action         TEXT NOT NULL,
    run_at         TIMESTAMP NOT NULL,
    reserved_until TIMESTAMP,
    attempts       INTEGER NOT NULL DEFAULT 0,
    failed_at      TIMESTAMP,
    last_error     TEXT
);

-- The pop query filters on queue, failed_at and run_at and orders by run_at.
-- This index is that query.
CREATE INDEX idx_jobs_ready ON jobs (queue, failed_at, run_at);

-- The dead letter queue is read by the diagnosis, newest failure first.
CREATE INDEX idx_jobs_parked ON jobs (failed_at);
`,
		Down: `DROP TABLE jobs;`,
	}}
}

// Push adds a job.
//
// Inside database.Transaction it joins it, which is the property this driver
// exists for: the job is committed by the same transaction as the row it
// describes, so it cannot refer to a write that rolled back.
func (q *DatabaseQueue) Push(ctx context.Context, g auth.Grant, j jobs.Job) error {
	// The job has to match the Grant pushing it. See jobs.Authorized: without
	// this, the queue is the one way past the authorization the collection
	// exists to enforce.
	if err := jobs.Authorized(g, j); err != nil {
		return err
	}
	j = jobs.Prepare(g, j)

	_, err := q.db.ExecContext(ctx, `
		INSERT INTO jobs (
			id, queue, name, tenant_id, payload, authorized_by, action,
			run_at, attempts
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.UUID, j.Queue, j.Name, j.TenantID, string(j.Payload), j.AuthorizedBy, j.Action,
		j.RunAt, j.Attempts)
	if err != nil {
		return fmt.Errorf("queue: pushing %s: %w", j.Name, err)
	}
	return nil
}

// PushOn adds a job to a named queue.
func (q *DatabaseQueue) PushOn(ctx context.Context, g auth.Grant, queue string, j jobs.Job) error {
	j.Queue = queue
	return q.Push(ctx, g, j)
}

// Later adds a job that becomes eligible after delay.
func (q *DatabaseQueue) Later(ctx context.Context, g auth.Grant, delay time.Duration, j jobs.Job) error {
	j.RunAt = time.Now().UTC().Add(delay)
	return q.Push(ctx, g, j)
}

// Bulk adds many jobs.
//
// One statement each rather than a multi-row insert, because inside
// database.Transaction the difference is a round trip and outside it a
// multi-row insert would be the only place in the package where a partial
// failure leaves half a batch behind with no way to say which half.
func (q *DatabaseQueue) Bulk(ctx context.Context, g auth.Grant, js []jobs.Job) error {
	for _, j := range js {
		if err := q.Push(ctx, g, j); err != nil {
			return err
		}
	}
	return nil
}

// Pop takes jobs off the queue and hides them for the lease.
//
// Two statements rather than one, and the reason is portability: the tight form
// is UPDATE ... RETURNING with a FOR UPDATE SKIP LOCKED subquery, which
// Postgres has and SQLite does not. Selecting the candidates and then claiming
// each one by its id -- with reserved_until in the WHERE -- is correct on every
// engine, because the claim itself is the compare-and-set.
//
// The cost is that two workers can pick the same candidate and one of them
// loses the claim. It gets nothing back, which is exactly right.
func (q *DatabaseQueue) Pop(ctx context.Context, queue string, n int, lease time.Duration) ([]*jobs.Job, error) {
	if queue == "" {
		queue = jobs.DefaultQueue
	}
	if n <= 0 {
		n = 1
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}

	now := time.Now().UTC()
	candidates, err := q.query(ctx, `
		WHERE queue = ? AND failed_at IS NULL AND run_at <= ?
		  AND (reserved_until IS NULL OR reserved_until < ?)
		ORDER BY run_at
		LIMIT ?`, queue, now, now, n)
	if err != nil {
		return nil, err
	}

	until := now.Add(lease)
	out := make([]*jobs.Job, 0, len(candidates))
	for _, j := range candidates {
		res, err := q.db.ExecContext(ctx, `
			UPDATE jobs SET reserved_until = ?, attempts = attempts + 1
			WHERE id = ? AND (reserved_until IS NULL OR reserved_until < ?)`,
			until, j.UUID, now)
		if err != nil {
			return nil, fmt.Errorf("queue: reserving %s: %w", j.UUID, err)
		}
		claimed, err := res.RowsAffected()
		if err != nil || claimed == 0 {
			// Another worker got it between the select and the update. Not an
			// error: that is the compare-and-set doing its job.
			continue
		}
		// Attempts was incremented by the claim, so the worker sees the number
		// this delivery is.
		j.Attempts++
		out = append(out, jobs.Popped(q, databaseConnection, j))
	}
	return out, nil
}

// DeleteJob removes a finished job.
//
// Deleted rather than marked done. A jobs table that keeps every job ever run
// is a table that needs its own cleanup job, and the history that matters --
// what ran, how long it took, what it queried -- is on the console.
func (q *DatabaseQueue) DeleteJob(ctx context.Context, j *jobs.Job) error {
	if _, err := q.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, j.UUID); err != nil {
		return fmt.Errorf("queue: deleting %s: %w", j.UUID, err)
	}
	return nil
}

// ReleaseJob puts the job back on its queue, eligible again after delay.
func (q *DatabaseQueue) ReleaseJob(ctx context.Context, j *jobs.Job, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}
	_, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET run_at = ?, last_error = ?, reserved_until = NULL WHERE id = ?`,
		time.Now().UTC().Add(delay), j.LastError, j.UUID)
	if err != nil {
		return fmt.Errorf("queue: releasing %s: %w", j.UUID, err)
	}
	return nil
}

// FailJob parks the job.
func (q *DatabaseQueue) FailJob(ctx context.Context, j *jobs.Job, cause error) error {
	message := j.LastError
	if cause != nil {
		message = cause.Error()
	}
	_, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET failed_at = ?, last_error = ?, reserved_until = NULL WHERE id = ?`,
		time.Now().UTC(), message, j.UUID)
	if err != nil {
		return fmt.Errorf("queue: parking %s: %w", j.UUID, err)
	}
	return nil
}

// Failed lists the jobs that gave up, most recent failure first.
func (q *DatabaseQueue) Failed(ctx context.Context, limit int) ([]jobs.Job, error) {
	if limit <= 0 {
		limit = 100
	}
	return q.query(ctx, `WHERE failed_at IS NOT NULL ORDER BY failed_at DESC LIMIT ?`, limit)
}

// Retry puts a failed job back in line with its attempts reset.
func (q *DatabaseQueue) Retry(ctx context.Context, uuid string) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE jobs SET failed_at = NULL, attempts = 0, last_error = NULL,
		                reserved_until = NULL, run_at = ?
		WHERE id = ?`, time.Now().UTC(), uuid)
	if err != nil {
		return fmt.Errorf("queue: retrying %s: %w", uuid, err)
	}
	return nil
}

// Size is how many jobs the queue holds, waiting or in flight.
func (q *DatabaseQueue) Size(ctx context.Context, queue string) (int, error) {
	if queue == "" {
		queue = jobs.DefaultQueue
	}
	var count int
	err := q.db.QueryRowContext(ctx,
		`SELECT count(*) FROM jobs WHERE queue = ? AND failed_at IS NULL`, queue).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("queue: sizing %s: %w", queue, err)
	}
	return count, nil
}

// PendingSize is how many jobs are waiting.
func (q *DatabaseQueue) PendingSize(ctx context.Context, queue string) (int, error) {
	if queue == "" {
		queue = jobs.DefaultQueue
	}
	// A reserved job is running, not waiting. Pop has always known that -- its
	// WHERE says so -- and these two did not, so a worker doing exactly what it
	// should looked like a backlog: a two-minute handler with a one-minute
	// threshold made /_arandu/health answer 503 while the job ran, and a load
	// balancer took the instance out of rotation for it. Found by audit.
	//
	// An expired lease is waiting again, which is why the comparison is against
	// now rather than a plain IS NULL.
	var count int
	err := q.db.QueryRowContext(ctx, `
		SELECT count(*) FROM jobs
		WHERE queue = ? AND failed_at IS NULL
		  AND (reserved_until IS NULL OR reserved_until < ?)`,
		queue, time.Now().UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("queue: counting %s: %w", queue, err)
	}
	return count, nil
}

// CreationTimeOfOldestPendingJob is when the oldest waiting job became
// eligible, or the zero time when nothing is waiting.
func (q *DatabaseQueue) CreationTimeOfOldestPendingJob(ctx context.Context, queue string) (time.Time, error) {
	if queue == "" {
		queue = jobs.DefaultQueue
	}

	// ORDER BY ... LIMIT 1 rather than min(run_at): an aggregate loses the
	// declared type of the column, and SQLite then hands back a string that
	// will not scan into a time.Time.
	// The same definition of "waiting" as PendingSize and Pop: a reserved job
	// is running.
	var oldest time.Time
	err := q.db.QueryRowContext(ctx, `
		SELECT run_at FROM jobs
		WHERE queue = ? AND failed_at IS NULL
		  AND (reserved_until IS NULL OR reserved_until < ?)
		ORDER BY run_at
		LIMIT 1`, queue, time.Now().UTC()).Scan(&oldest)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("queue: measuring the age of %s: %w", queue, err)
	}
	return oldest, nil
}

// Clear removes every job waiting or in flight on a queue, and returns how many
// went.
//
// Parked jobs are not cleared: a job that gave up is no longer on a queue, it
// is in the dead letter list, and [DatabaseQueue.Failed] and
// [DatabaseQueue.Retry] are how it is dealt with. The RESP driver draws the
// line in the same place.
func (q *DatabaseQueue) Clear(ctx context.Context, queue string) (int, error) {
	if queue == "" {
		queue = jobs.DefaultQueue
	}
	res, err := q.db.ExecContext(ctx, `DELETE FROM jobs WHERE queue = ? AND failed_at IS NULL`, queue)
	if err != nil {
		return 0, fmt.Errorf("queue: clearing %s: %w", queue, err)
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("queue: clearing %s: %w", queue, err)
	}
	return int(removed), nil
}

// query runs the standard projection with a caller-supplied tail.
func (q *DatabaseQueue) query(ctx context.Context, tail string, args ...any) ([]jobs.Job, error) {
	// The newline is load-bearing: without it a tail starting with WHERE
	// concatenates into "FROM jobsWHERE", which SQLite reads as a table alias
	// and then fails on the next word -- an error that names a keyword three
	// tokens away from the actual problem.
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, queue, name, tenant_id, payload, authorized_by, action,
		       run_at, attempts, last_error
		FROM jobs
		`+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("queue: reading the queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []jobs.Job
	for rows.Next() {
		var j jobs.Job
		var payload string
		var lastError sql.NullString
		if err := rows.Scan(&j.UUID, &j.Queue, &j.Name, &j.TenantID, &payload,
			&j.AuthorizedBy, &j.Action, &j.RunAt, &j.Attempts, &lastError); err != nil {
			return nil, fmt.Errorf("queue: reading the queue: %w", err)
		}
		j.Payload = []byte(payload)
		j.LastError = lastError.String
		out = append(out, j)
	}
	return out, rows.Err()
}
