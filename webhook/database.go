package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/schema"
)

// CreateDeliveriesTable creates the durable webhook delivery store.
type CreateDeliveriesTable struct{ migrations.BaseMigration }

// GetName returns the migration identity.
func (CreateDeliveriesTable) GetName() string {
	return "2026_09_13_000010_create_webhook_deliveries_table"
}

// Up creates the deliveries table and its tenant-scoped indexes.
func (CreateDeliveriesTable) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, "webhook_deliveries", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("event_id")
		table.String("event_name")
		table.String("endpoint_id")
		table.String("url", MaxURLLength)
		table.Text("headers")
		table.String("secret_ref")
		table.Text("body")
		table.String("status")
		table.BigInteger("attempts").Default(0)
		table.Boolean("permanent").Default(false)
		table.Integer("response_status").Nullable()
		table.Text("last_error").Nullable()
		table.String("claim_token").Nullable()
		table.BigInteger("claim_version").Default(0)
		table.Timestamp("lease_until").Nullable()
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Timestamp("delivered_at").Nullable()
		table.Unique([]string{"tenant_id", "event_id", "endpoint_id"}, "webhook_delivery_event_endpoint_uidx")
		table.Index([]string{"tenant_id", "id"}, "webhook_delivery_tenant_id_idx")
		table.Index([]string{"tenant_id", "created_at"}, "webhook_delivery_retention_idx")
	})
}

// Down removes the deliveries table.
func (CreateDeliveriesTable) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, "webhook_deliveries")
}

// DatabaseStore persists delivery state through a Hesape database handle.
type DatabaseStore struct {
	db     *database.DB
	action auth.Action
}

// NewDatabaseStore returns a database-backed delivery store.
func NewDatabaseStore(db *database.DB) *DatabaseStore {
	return newDatabaseStore(db, ActionDispatch)
}

func newDatabaseStore(db *database.DB, action auth.Action) *DatabaseStore {
	return &DatabaseStore{db: db, action: action}
}

// Migrations returns the schema owned by the store.
func (*DatabaseStore) Migrations() []migrations.Migration {
	return []migrations.Migration{CreateDeliveriesTable{}}
}

// Transaction runs work atomically on the store's database.
func (s *DatabaseStore) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return database.Transaction(ctx, s.db, fn)
}

// Create inserts an immutable delivery, or reports an idempotent existing row.
func (s *DatabaseStore) Create(ctx context.Context, g auth.Grant, item Delivery) (bool, error) {
	if _, err := s.tenantFor(g); err != nil {
		return false, err
	}
	headers, err := json.Marshal(item.Headers)
	if err != nil {
		return false, fmt.Errorf("webhook: encoding delivery headers: %w", err)
	}
	created, err := query.NewBuilder(s.db, s.db.GetQueryGrammar(), s.db.GetPostProcessor()).
		From("webhook_deliveries").
		InsertOrIgnore(ctx, g, map[string]any{
			"id":            item.ID,
			"event_id":      item.EventID,
			"event_name":    item.EventName,
			"endpoint_id":   item.EndpointID,
			"url":           item.URL,
			"headers":       string(headers),
			"secret_ref":    item.SecretRef,
			"body":          string(item.Body),
			"status":        string(StatusPending),
			"attempts":      0,
			"permanent":     false,
			"claim_version": 0,
			"created_at":    item.CreatedAt,
			"updated_at":    item.UpdatedAt,
		})
	if err != nil {
		return false, fmt.Errorf("webhook: creating delivery: %w", err)
	}
	return created == 1, nil
}

// Find returns one tenant-scoped delivery.
func (s *DatabaseStore) Find(ctx context.Context, g auth.Grant, id string) (Delivery, error) {
	tenant, err := s.tenantFor(g)
	if err != nil {
		return Delivery{}, err
	}
	var item Delivery
	var headers, body, status string
	var response sql.NullInt64
	var lastError sql.NullString
	var lease, delivered sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT id, event_id, event_name, endpoint_id, url,
		headers, secret_ref, body, status, attempts, permanent, response_status,
		last_error, lease_until, created_at, updated_at, delivered_at
		FROM webhook_deliveries WHERE tenant_id = ? AND id = ?`, tenant, id).Scan(
		&item.ID, &item.EventID, &item.EventName, &item.EndpointID, &item.URL,
		&headers, &item.SecretRef, &body, &status, &item.Attempts, &item.Permanent,
		&response, &lastError, &lease, &item.CreatedAt, &item.UpdatedAt, &delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrDeliveryNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("webhook: finding delivery: %w", err)
	}
	if err := json.Unmarshal([]byte(headers), &item.Headers); err != nil {
		return Delivery{}, fmt.Errorf("webhook: decoding delivery headers: %w", err)
	}
	item.TenantID, item.Body, item.Status = tenant, []byte(body), Status(status)
	if response.Valid {
		item.ResponseStatus = int(response.Int64)
	}
	if lastError.Valid {
		item.LastError = lastError.String
	}
	if lease.Valid {
		item.LeaseUntil = lease.Time.UTC()
	}
	if delivered.Valid {
		item.DeliveredAt = delivered.Time.UTC()
	}
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return item, nil
}

// Claim takes a fenced lease when no current worker owns the delivery.
func (s *DatabaseStore) Claim(ctx context.Context, g auth.Grant, id string, lease time.Duration) (Claim, bool, error) {
	tenant, err := s.tenantFor(g)
	if err != nil {
		return Claim{}, false, err
	}
	token, err := database.NewID()
	if err != nil {
		return Claim{}, false, err
	}
	now := time.Now().UTC()
	until := now.Add(lease)
	result, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET status = ?, claim_token = ?,
		claim_version = claim_version + 1, lease_until = ?, attempts = attempts + 1, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND status <> ? AND permanent = ?
		AND (lease_until IS NULL OR lease_until <= ?)`, string(StatusProcessing), token, until,
		now, tenant, id, string(StatusDelivered), false, now)
	if err != nil {
		return Claim{}, false, fmt.Errorf("webhook: claiming delivery: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Claim{}, false, err
	}
	if rows == 0 {
		return Claim{}, false, nil
	}
	item, err := s.Find(ctx, g, id)
	if err != nil {
		return Claim{}, false, err
	}
	var version int64
	err = s.db.QueryRowContext(ctx, `SELECT claim_version FROM webhook_deliveries
		WHERE tenant_id = ? AND id = ? AND claim_token = ?`, tenant, id, token).Scan(&version)
	if err != nil {
		return Claim{}, false, fmt.Errorf("webhook: reading claim fence: %w", err)
	}
	return Claim{Delivery: item, Token: token, Version: version, LeaseEnd: until}, true, nil
}

// Complete records a successful result only for the current claim.
func (s *DatabaseStore) Complete(ctx context.Context, g auth.Grant, claim Claim, result Result) error {
	tenant, err := s.tenantFor(g)
	if err != nil {
		return err
	}
	finished := result.FinishedAt.UTC()
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET status = ?, response_status = ?,
		last_error = NULL, claim_token = NULL, lease_until = NULL, updated_at = ?, delivered_at = ?
		WHERE tenant_id = ? AND id = ? AND claim_token = ? AND claim_version = ?`,
		string(StatusDelivered), result.StatusCode, finished, finished, tenant, claim.Delivery.ID, claim.Token, claim.Version)
	if err != nil {
		return fmt.Errorf("webhook: completing delivery: %w", err)
	}
	return requireClaim(res)
}

// Fail records a classified failure only for the current claim.
func (s *DatabaseStore) Fail(ctx context.Context, g auth.Grant, claim Claim, failure Failure) error {
	tenant, err := s.tenantFor(g)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET status = ?, response_status = ?,
		last_error = ?, permanent = ?, claim_token = NULL, lease_until = NULL, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND claim_token = ? AND claim_version = ?`,
		string(StatusFailed), nullableStatus(failure.StatusCode), failure.Code, failure.Permanent,
		time.Now().UTC(), tenant, claim.Delivery.ID, claim.Token, claim.Version)
	if err != nil {
		return fmt.Errorf("webhook: failing delivery: %w", err)
	}
	return requireClaim(res)
}

// Prune removes delivery records older than cutoff for one tenant.
func (s *DatabaseStore) Prune(ctx context.Context, g auth.Grant, cutoff time.Time) (int64, error) {
	tenant, err := s.tenantFor(g)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE tenant_id = ? AND created_at < ?`, tenant, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("webhook: pruning deliveries: %w", err)
	}
	return res.RowsAffected()
}

func tenantFor(g auth.Grant) (string, error) {
	return tenantForAction(g, ActionDispatch)
}

func (s *DatabaseStore) tenantFor(g auth.Grant) (string, error) {
	action := s.action
	if action == "" {
		action = ActionDispatch
	}
	return tenantForAction(g, action)
}

func tenantForAction(g auth.Grant, action auth.Action) (string, error) {
	if err := g.Check(action); err != nil {
		return "", err
	}
	tenant := auth.Tenant(g)
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: invalid tenant", auth.ErrForbidden)
	}
	return tenant, nil
}

func requireClaim(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrStaleClaim
	}
	return nil
}

func nullableStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}

var _ migrations.ReversibleMigration = CreateDeliveriesTable{}
