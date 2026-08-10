package bus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// BatchesTable is where batches are stored. The name is Laravel's, because the
// vocabulary is (RULE 10).
const BatchesTable = "job_batches"

// SQL is the Store over the application's own database.
//
// It is the one an application runs. A batch counter is shared state between
// every worker on every replica, so the only place it can live is the place
// they all already talk to -- and putting it in the application's database
// means a batch dispatched inside database.Transaction is committed by the same
// transaction as the rows it is about.
type SQL struct {
	db  *database.DB
	now func() time.Time
}

// NewSQL returns the store.
func NewSQL(db *database.DB) *SQL {
	return &SQL{db: db, now: func() time.Time { return time.Now().UTC() }}
}

var _ Store = (*SQL)(nil)

// Migrations is the schema this store needs.
//
// It lives here rather than in the application, because a table the framework
// reads and writes is a table the framework has to be able to create -- the
// events outbox sets the same precedent.
func Migrations() []database.Migration {
	return []database.Migration{{
		ID: "2026_08_10_000001_create_job_batches_table",
		// INTEGER and TIMESTAMP are spelled the same way by all three engines,
		// and anything that takes part in a key is database.KeyText -- see
		// there for why TEXT is not portable in one.
		//
		// allow_failures is INTEGER and not BOOLEAN for the same reason: three
		// engines, three spellings, and one of them stores it as a string.
		Up: `CREATE TABLE ` + BatchesTable + ` (
			id             ` + database.KeyText + ` PRIMARY KEY,
			tenant_id      ` + database.KeyText + ` NOT NULL,
			name           TEXT NOT NULL,
			queue          TEXT NOT NULL,
			total_jobs     INTEGER NOT NULL,
			pending_jobs   INTEGER NOT NULL,
			failed_jobs    INTEGER NOT NULL,
			allow_failures INTEGER NOT NULL,
			callbacks      TEXT NOT NULL,
			created_at     TIMESTAMP NOT NULL,
			cancelled_at   TIMESTAMP NULL,
			finished_at    TIMESTAMP NULL
		);
		CREATE INDEX job_batches_tenant_created_idx ON ` + BatchesTable + ` (tenant_id, created_at)`,
		Down: `DROP TABLE ` + BatchesTable,
	}}
}

// callbacks is the three callbacks as one column.
//
// One JSON column rather than nine, because nothing queries a batch by the name
// of its Catch job and never will: they are read together, written once, and a
// column per field would be nine migrations the first time a callback grows an
// option.
type callbacks struct {
	Then    Step `json:"then"`
	Catch   Step `json:"catch"`
	Finally Step `json:"finally"`
}

// columns is the read shape, in the order scanBatch expects.
const columns = `id, tenant_id, name, queue, total_jobs, pending_jobs, failed_jobs,
	allow_failures, callbacks, created_at, cancelled_at, finished_at`

// Create persists a new batch.
func (s *SQL) Create(ctx context.Context, g auth.Grant, b Batch) error {
	tenant, err := tenantOf(g)
	if err != nil {
		return err
	}
	b.TenantID = tenant
	if b.CreatedAt.IsZero() {
		b.CreatedAt = s.now()
	}

	raw, err := json.Marshal(callbacks{Then: b.Then, Catch: b.Catch, Finally: b.Finally})
	if err != nil {
		return fmt.Errorf("bus: serializing the callbacks of batch %s: %w", b.ID, err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO `+BatchesTable+` (`+columns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, tenant, b.Name, b.Queue, b.Total, b.Pending, b.Failed,
		boolToInt(b.AllowFailures), string(raw), b.CreatedAt.UTC(),
		nullTime(b.CancelledAt), nullTime(b.FinishedAt))
	if err != nil {
		return fmt.Errorf("bus: creating batch %s: %w", b.ID, err)
	}
	return nil
}

// Find returns a batch.
func (s *SQL) Find(ctx context.Context, g auth.Grant, id string) (Batch, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return Batch{}, err
	}
	return s.find(ctx, tenant, id)
}

func (s *SQL) find(ctx context.Context, tenant, id string) (Batch, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+columns+` FROM `+BatchesTable+` WHERE id = ? AND tenant_id = ?`, id, tenant)

	b, err := scanBatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Batch{}, notFound(id)
	}
	if err != nil {
		return Batch{}, fmt.Errorf("bus: reading batch %s: %w", id, err)
	}
	return b, nil
}

// Cancel marks the batch cancelled.
func (s *SQL) Cancel(ctx context.Context, g auth.Grant, id string) error {
	tenant, err := tenantOf(g)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, `UPDATE `+BatchesTable+`
		SET cancelled_at = ?
		WHERE id = ? AND tenant_id = ? AND cancelled_at IS NULL AND finished_at IS NULL`,
		s.now(), id, tenant)
	if err != nil {
		return fmt.Errorf("bus: cancelling batch %s: %w", id, err)
	}

	// Nothing updated means one of three things, and only one of them is an
	// error: the batch does not exist. Already cancelled and already finished
	// both mean the caller got what they asked for. The read settles which,
	// and it only runs on the path that changed nothing.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		if _, err := s.find(ctx, tenant, id); err != nil {
			return err
		}
	}
	return nil
}

// recordAttempts is how many times Record may lose its compare-and-set without
// anything having moved before it gives up.
//
// It does not bound the loop as a whole. A lost race means another worker wrote
// its own report in between, which is the system making progress, and counting
// that as a failure caps a batch at recordAttempts concurrent workers: fifty
// reporting at once exhausted a budget of ten every time. What the budget
// catches is the spin -- the UPDATE matching nothing while the row keeps
// reading back the same counters, which no amount of retrying resolves.
const recordAttempts = 10

// Record registers the outcome of one job.
//
// Read, decide, then write conditioned on the counters not having moved. The
// alternative -- UPDATE ... SET pending_jobs = pending_jobs - 1 followed by a
// read -- cannot say which caller was the one that reached zero, and firing the
// Then callback of a ten thousand job import twice is worse than a retry loop.
//
// It deliberately does not open a transaction. Inside one, every retry would
// re-read the same snapshot and the loop would spin to its limit; the
// conditional UPDATE is the whole guarantee and it is a single statement.
func (s *SQL) Record(ctx context.Context, g auth.Grant, id string, ok bool) (Recorded, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return Recorded{}, err
	}

	// stalled counts the consecutive attempts that lost the race without the
	// row having moved. Any attempt that reads different counters from the last
	// one resets it: somebody else recorded their job, and this one is queued
	// behind real work rather than spinning.
	stalled := 0
	lastPending, lastFailed := -1, -1
	for {
		if err := ctx.Err(); err != nil {
			return Recorded{}, err
		}

		before, err := s.find(ctx, tenant, id)
		if err != nil {
			return Recorded{}, err
		}
		if before.Pending != lastPending || before.Failed != lastFailed {
			lastPending, lastFailed, stalled = before.Pending, before.Failed, 0
		}
		after, r := advance(before, ok, s.now())

		// A report that changes nothing writes nothing. It happens on a
		// duplicate delivery of a job whose batch already finished with nothing
		// pending, and it has to be short-circuited rather than sent: MySQL
		// counts rows changed, not rows matched, so an UPDATE that sets the
		// values already there affects zero rows and the loop below would read
		// that as contention and spin to its limit.
		if after.Pending == before.Pending && after.Failed == before.Failed &&
			after.FinishedAt.Equal(before.FinishedAt) {
			return r, nil
		}

		res, err := s.db.ExecContext(ctx, `UPDATE `+BatchesTable+`
			SET pending_jobs = ?, failed_jobs = ?, finished_at = ?
			WHERE id = ? AND tenant_id = ? AND pending_jobs = ? AND failed_jobs = ?`,
			after.Pending, after.Failed, nullTime(after.FinishedAt),
			id, tenant, before.Pending, before.Failed)
		if err != nil {
			return Recorded{}, fmt.Errorf("bus: recording a job of batch %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return Recorded{}, fmt.Errorf("bus: recording a job of batch %s: %w", id, err)
		}
		if n > 0 {
			return r, nil
		}

		stalled++
		if stalled >= recordAttempts {
			return Recorded{}, fmt.Errorf("bus: batch %s did not move under %d attempts to record a job against it", id, recordAttempts)
		}
	}
}

// Prune deletes finished batches created before the cut.
func (s *SQL) Prune(ctx context.Context, g auth.Grant, before time.Time) (int, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return 0, err
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM `+BatchesTable+`
		WHERE tenant_id = ? AND finished_at IS NOT NULL AND created_at < ?`,
		tenant, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("bus: pruning batches: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("bus: pruning batches: %w", err)
	}
	return int(n), nil
}

// scanner is what both *sql.Row and *sql.Rows are.
type scanner interface{ Scan(dest ...any) error }

// scanBatch reads one row in the order of columns.
func scanBatch(row scanner) (Batch, error) {
	var (
		b         Batch
		allow     int
		raw       string
		cancelled sql.NullTime
		finished  sql.NullTime
	)
	err := row.Scan(&b.ID, &b.TenantID, &b.Name, &b.Queue, &b.Total, &b.Pending,
		&b.Failed, &allow, &raw, &b.CreatedAt, &cancelled, &finished)
	if err != nil {
		return Batch{}, err
	}

	var cb callbacks
	if err := json.Unmarshal([]byte(raw), &cb); err != nil {
		return Batch{}, fmt.Errorf("reading the callbacks of batch %s: %w", b.ID, err)
	}

	b.AllowFailures = allow != 0
	b.Then, b.Catch, b.Finally = cb.Then, cb.Catch, cb.Finally
	b.CreatedAt = b.CreatedAt.UTC()
	if cancelled.Valid {
		b.CancelledAt = cancelled.Time.UTC()
	}
	if finished.Valid {
		b.FinishedAt = finished.Time.UTC()
	}
	return b, nil
}

// nullTime writes a zero time as NULL.
//
// A zero time.Time written as a timestamp is the year 1, which reads as "this
// batch finished two thousand years ago" on every dashboard and sorts before
// everything.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// boolToInt writes a bool as the integer all three engines agree on.
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
