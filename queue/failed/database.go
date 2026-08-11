package failed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// DatabaseFailedJobProvider keeps the failed jobs in a table.
//
// It answers Illuminate\Queue\Failed\DatabaseFailedJobProvider. It is what an
// application wires when it wants the dead letter list to survive the queue --
// a job that failed on a Redis queue that was then flushed is still a job
// somebody has to answer for.
//
// The table is its own, and that is the point of the type: a queue whose store
// is not a database has nowhere else to keep them.
type DatabaseFailedJobProvider struct {
	db    *database.DB
	table string
}

// NewDatabaseFailedJobProvider returns the provider over db.
//
// The table defaults to failed_jobs, which is Laravel's name.
func NewDatabaseFailedJobProvider(db *database.DB, table string) *DatabaseFailedJobProvider {
	if table == "" {
		table = "failed_jobs"
	}
	return &DatabaseFailedJobProvider{db: db, table: table}
}

var (
	_ FailedJobProvider          = (*DatabaseFailedJobProvider)(nil)
	_ CountableFailedJobProvider = (*DatabaseFailedJobProvider)(nil)
	_ PrunableFailedJobProvider  = (*DatabaseFailedJobProvider)(nil)
)

// DatabaseUUIDFailedJobProvider is [DatabaseFailedJobProvider] under the name
// Laravel gives the version keyed by the job's uuid.
//
// It answers Illuminate\Queue\Failed\DatabaseUuidFailedJobProvider, and it is an
// alias because there is nothing left to distinguish: Laravel has two providers
// because its ids are auto-increment integers and the uuid is a second column,
// so retrying by uuid needs a second index and a second class. Here the id is
// the uuid (see database.NewID), so the two providers would run the same query
// (RULE 9).
type DatabaseUUIDFailedJobProvider = DatabaseFailedJobProvider

// GetTable is the name of the table this provider reads.
//
// It answers getTable(), which in PHP returns a query builder over it. Here it
// returns the name: a builder handed out is a caller writing its own SQL against
// a table it does not own, and the tenant filter is not optional (RULE 17).
func (p *DatabaseFailedJobProvider) GetTable() string { return p.table }

// Migrations returns the failed jobs table.
//
// It answers Laravel's `queue:failed-table`. It is on the provider rather than
// on the queue module because it belongs to whoever wired this provider: an
// application that keeps its failures in the jobs table declares nothing here.
func (p *DatabaseFailedJobProvider) Migrations() []database.Migration {
	return []database.Migration{{
		ID: "2026_08_11_000020_create_failed_jobs_table",
		// Portable types only: TEXT, INTEGER and TIMESTAMP mean the same thing
		// on SQLite, Postgres and MySQL.
		Up: `
CREATE TABLE ` + p.table + ` (
    id          VARCHAR(255) PRIMARY KEY,
    uuid        VARCHAR(255) NOT NULL,
    tenant_id   VARCHAR(255) NOT NULL,
    connection  VARCHAR(255) NOT NULL,
    queue       VARCHAR(255) NOT NULL,
    name        TEXT NOT NULL,
    payload     TEXT NOT NULL,
    exception   TEXT NOT NULL,
    failed_at   TIMESTAMP NOT NULL
);

-- Every read filters by tenant and orders by failure time, and the monitor
-- narrows by queue. This index is those queries.
CREATE INDEX idx_failed_jobs_tenant ON ` + p.table + ` (tenant_id, queue, failed_at);
`,
		Down: `DROP TABLE ` + p.table + `;`,
	}}
}

// Log records a job that gave up. It answers log().
func (p *DatabaseFailedJobProvider) Log(ctx context.Context, g auth.Grant, job FailedJob) (string, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return "", err
	}

	id := job.UUID
	if id == "" {
		if id, err = database.NewID(); err != nil {
			return "", err
		}
	}
	failedAt := job.FailedAt
	if failedAt.IsZero() {
		failedAt = time.Now()
	}

	_, err = p.db.ExecContext(ctx, `
		INSERT INTO `+p.table+` (
			id, uuid, tenant_id, connection, queue, name, payload, exception, failed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, id, tenant, job.Connection, job.Queue, job.Name,
		string(job.Payload), job.Exception, failedAt.UTC())
	if err != nil {
		return "", fmt.Errorf("queue/failed: recording %s: %w", job.Name, err)
	}
	return id, nil
}

// IDs is the identifiers of this tenant's failed jobs, newest first. It answers
// ids().
func (p *DatabaseFailedJobProvider) IDs(ctx context.Context, g auth.Grant, queue string) ([]string, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return nil, err
	}

	query := `SELECT id FROM ` + p.table + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if queue != "" {
		query += ` AND queue = ?`
		args = append(args, queue)
	}
	query += ` ORDER BY failed_at DESC`

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("queue/failed: listing the failed jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("queue/failed: listing the failed jobs: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// All is this tenant's failed jobs, newest first. It answers all().
func (p *DatabaseFailedJobProvider) All(ctx context.Context, g auth.Grant) ([]FailedJob, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return nil, err
	}
	return p.query(ctx, `WHERE tenant_id = ? ORDER BY failed_at DESC`, tenant)
}

// Find is one of this tenant's failed jobs. It answers find().
func (p *DatabaseFailedJobProvider) Find(ctx context.Context, g auth.Grant, id string) (FailedJob, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return FailedJob{}, err
	}
	// The tenant is in the WHERE and not checked afterwards: a query that finds
	// the row and then refuses it has already read another customer's payload
	// into this process (RULE 17).
	found, err := p.query(ctx, `WHERE tenant_id = ? AND id = ?`, tenant, id)
	if err != nil {
		return FailedJob{}, err
	}
	if len(found) == 0 {
		return FailedJob{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return found[0], nil
}

// Forget removes one of this tenant's failed jobs. It answers forget().
func (p *DatabaseFailedJobProvider) Forget(ctx context.Context, g auth.Grant, id string) (bool, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return false, err
	}
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM `+p.table+` WHERE tenant_id = ? AND id = ?`, tenant, id)
	if err != nil {
		return false, fmt.Errorf("queue/failed: forgetting %s: %w", id, err)
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("queue/failed: forgetting %s: %w", id, err)
	}
	return removed > 0, nil
}

// Flush removes this tenant's failed jobs older than age, or all of them when
// age is zero. It answers flush().
func (p *DatabaseFailedJobProvider) Flush(ctx context.Context, g auth.Grant, age time.Duration) error {
	tenant, err := tenantOf(g)
	if err != nil {
		return err
	}

	query := `DELETE FROM ` + p.table + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if age > 0 {
		query += ` AND failed_at <= ?`
		args = append(args, time.Now().UTC().Add(-age))
	}
	if _, err := p.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("queue/failed: flushing the failed jobs: %w", err)
	}
	return nil
}

// Prune removes this tenant's failed jobs that failed before an instant, and
// returns how many went. It answers prune().
func (p *DatabaseFailedJobProvider) Prune(ctx context.Context, g auth.Grant, before time.Time) (int, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return 0, err
	}
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM `+p.table+` WHERE tenant_id = ? AND failed_at < ?`, tenant, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("queue/failed: pruning the failed jobs: %w", err)
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("queue/failed: pruning the failed jobs: %w", err)
	}
	return int(removed), nil
}

// Count is how many of this tenant's jobs have failed. It answers count().
func (p *DatabaseFailedJobProvider) Count(ctx context.Context, g auth.Grant, connectionName, queue string) (int, error) {
	tenant, err := tenantOf(g)
	if err != nil {
		return 0, err
	}

	query := `SELECT count(*) FROM ` + p.table + ` WHERE tenant_id = ?`
	args := []any{tenant}
	if connectionName != "" {
		query += ` AND connection = ?`
		args = append(args, connectionName)
	}
	if queue != "" {
		query += ` AND queue = ?`
		args = append(args, queue)
	}

	var count int
	if err := p.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("queue/failed: counting the failed jobs: %w", err)
	}
	return count, nil
}

// query runs the standard projection with a caller-supplied tail.
func (p *DatabaseFailedJobProvider) query(ctx context.Context, tail string, args ...any) ([]FailedJob, error) {
	// The newline is load-bearing: without it a tail starting with WHERE
	// concatenates into "failed_jobsWHERE", which SQLite reads as a table alias
	// and then fails on the next word.
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, uuid, tenant_id, connection, queue, name, payload, exception, failed_at
		FROM `+p.table+`
		`+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("queue/failed: reading the failed jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FailedJob
	for rows.Next() {
		var job FailedJob
		var payload string
		if err := rows.Scan(&job.ID, &job.UUID, &job.TenantID, &job.Connection, &job.Queue,
			&job.Name, &payload, &job.Exception, &job.FailedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return out, nil
			}
			return nil, fmt.Errorf("queue/failed: reading the failed jobs: %w", err)
		}
		job.Payload = []byte(payload)
		out = append(out, job)
	}
	return out, rows.Err()
}
