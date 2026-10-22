package webhook

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// The schema the Module owns: the endpoints, the received calls, the
// deliveries and their attempts, in the order the names sort in. Each name is
// the migration's identity in the migrations table, so none of them is ever
// renamed. CreateDeliveriesTable is not among them: it is Manager's table.
var (
	_ migrations.ReversibleMigration = createEndpoints{}
	_ migrations.ReversibleMigration = createCalls{}
	_ migrations.ReversibleMigration = extendEndpoints{}
	_ migrations.ReversibleMigration = createDeliveries{}
	_ migrations.ReversibleMigration = createAttempts{}
	_ migrations.ReversibleMigration = addCallReference{}
	_ migrations.ReversibleMigration = addCallRequest{}
	_ migrations.ReversibleMigration = addCallDeliveryID{}
)

// createEndpoints is the sending side's endpoints.
type createEndpoints struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createEndpoints) GetName() string { return "20261014_0001_create_webhook_server_endpoints" }

// Up creates the table.
func (createEndpoints) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, endpointsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("name", 120)
		table.String("url", MaxURLLength)
		table.Text("events")
		table.String("signer", 16)
		table.String("signature_header", 64).Default("")
		table.Text("secret")
		table.Text("headers")
		table.UnsignedSmallInteger("active").Default(1)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Index([]string{"tenant_id", "name", "id"}, "webhook_server_endpoints_name_idx")
		table.Index([]string{"tenant_id", "active"}, "webhook_server_endpoints_active_idx")
	})
}

// Down drops the table.
func (createEndpoints) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, endpointsTable)
}

// createCalls is the receiving side's calls.
type createCalls struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createCalls) GetName() string { return "20261014_0002_create_webhook_client_calls" }

// Up creates the table.
func (createCalls) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, callsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("name", 64)
		table.String("method", 8)
		table.String("url", MaxURLLength)
		table.Text("headers")
		table.Text("payload")
		table.String("status", 16)
		table.Integer("attempts").Default(0)
		table.Text("exception")
		table.Timestamp("processed_at")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Index([]string{"tenant_id", "created_at", "id"}, "webhook_client_calls_created_idx")
		table.Index([]string{"tenant_id", "name", "status"}, "webhook_client_calls_status_idx")
	})
}

// Down drops the table.
func (createCalls) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, callsTable)
}

// extendEndpoints gives the endpoints their dialect, their previous secrets,
// their own policy and what the engine records when it turns one off. Every
// column has a default, so a row a binary of the previous release writes during
// a rollout reads back; the dialect starts as the signer that release wrote,
// and the signer column stays.
type extendEndpoints struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (extendEndpoints) GetName() string {
	return "20261014_0003_add_dialect_and_policy_to_webhook_server_endpoints"
}

// Up adds the columns and fills them for the rows already there.
func (extendEndpoints) Up(ctx context.Context, conn migrations.Connection) error {
	err := conn.Schema().Table(ctx, endpointsTable, func(table *schema.Blueprint) {
		table.String("dialect", 32).Default(DialectArandu)
		table.UnsignedSmallInteger("include_arandu_headers").Default(1)
		table.String("previous_secrets", maxSealedLen).Default("")
		table.String("policy", maxSealedLen).Default("")
		table.Integer("consecutive_failures").Default(0)
		table.Timestamp("disabled_at").Nullable()
		table.String("disabled_reason", 64).Default("")
	})
	if err != nil {
		return err
	}
	_, err = conn.Statement(ctx, "UPDATE "+endpointsTable+" SET dialect = signer", nil)
	return err
}

// Down drops the columns.
func (extendEndpoints) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, endpointsTable, func(table *schema.Blueprint) {
		table.DropColumn("dialect", "include_arandu_headers", "previous_secrets", "policy",
			"consecutive_failures", "disabled_at", "disabled_reason")
	})
}

// createDeliveries is the deliveries this module records and attempts.
type createDeliveries struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createDeliveries) GetName() string { return "20261014_0004_create_webhook_server_deliveries" }

// Up creates the table.
func (createDeliveries) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, deliveriesTable, func(table *schema.Blueprint) {
		table.String("id", maxDeliveryIDLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("event_id", maxDeliveryIDLen)
		table.String("event_name", 255)
		table.String("endpoint_id", maxIdentifierLen)
		table.String("url", MaxURLLength)
		table.String("dialect", 32)
		table.MediumText("body")
		table.String("body_sha256", 64)
		table.Text("headers")
		table.String("secret_ref", 128)
		table.String("signature_header", 64).Default("")
		table.UnsignedSmallInteger("include_arandu_headers").Default(1)
		table.String("status", 16)
		table.Integer("attempts").Default(0)
		table.Integer("max_attempts")
		table.Text("backoff")
		table.UnsignedSmallInteger("permanent").Default(0)
		table.Timestamp("next_attempt_at").Nullable()
		table.Integer("response_status").Nullable()
		table.Text("last_error").Nullable()
		table.String("claim_token", maxIdentifierLen).Nullable()
		table.Timestamp("lease_until").Nullable()
		table.Timestamp("created_at")
		table.Timestamp("updated_at")
		table.Timestamp("delivered_at").Nullable()
		table.Unique([]string{"tenant_id", "event_id", "endpoint_id"}, "webhook_server_deliveries_event_endpoint_uidx")
		table.Index([]string{"tenant_id", "endpoint_id", "created_at", "id"}, "webhook_server_deliveries_endpoint_idx")
		table.Index([]string{"tenant_id", "status", "next_attempt_at"}, "webhook_server_deliveries_due_idx")
		table.Index([]string{"tenant_id", "created_at"}, "webhook_server_deliveries_retention_idx")
	})
}

// Down drops the table.
func (createDeliveries) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, deliveriesTable)
}

// createAttempts is the history of every attempt of a delivery.
type createAttempts struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (createAttempts) GetName() string {
	return "20261014_0005_create_webhook_server_delivery_attempts"
}

// Up creates the table.
func (createAttempts) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, attemptsTable, func(table *schema.Blueprint) {
		table.String("id", maxIdentifierLen).Primary()
		table.String("tenant_id", maxIdentifierLen)
		table.String("delivery_id", maxDeliveryIDLen)
		table.String("endpoint_id", maxIdentifierLen)
		table.Integer("attempt")
		table.String("triggered_by", 16)
		table.UnsignedSmallInteger("succeeded").Default(0)
		table.Integer("response_status").Nullable()
		table.String("error", 64).Default("")
		table.Text("response")
		table.BigInteger("duration_ms").Default(0)
		table.Timestamp("signed_at")
		table.Timestamp("created_at")
		table.Unique([]string{"tenant_id", "delivery_id", "attempt"}, "webhook_server_delivery_attempts_uidx")
		table.Index([]string{"tenant_id", "created_at"}, "webhook_server_delivery_attempts_retention_idx")
	})
}

// Down drops the table.
func (createAttempts) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().DropIfExists(ctx, attemptsTable)
}

// addCallReference gives a received call the record its receiver resolved it
// to. The column has a default, so the calls already stored, and a row a
// binary of the previous release writes during a rollout, read back.
type addCallReference struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (addCallReference) GetName() string {
	return "20261015_0006_add_reference_to_webhook_client_calls"
}

// Up adds the column.
func (addCallReference) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.String("reference", 255).Default("")
	})
}

// Down drops the column.
func (addCallReference) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.DropColumn("reference")
	})
}

// addCallRequest gives a received call who connected and the digest of every
// header it arrived with. Both columns have a default.
type addCallRequest struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (addCallRequest) GetName() string {
	return "20261015_0007_add_remote_addr_and_headers_digest_to_webhook_client_calls"
}

// Up adds the columns.
func (addCallRequest) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.String("remote_addr", 255).Default("")
		table.String("headers_sha256", 64).Default("")
	})
}

// Down drops the columns.
func (addCallRequest) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.DropColumn("remote_addr", "headers_sha256")
	})
}

// widenTimePrecision raises the instants of the module's four tables to
// microseconds where an earlier default created them in whole seconds.
var widenTimePrecision = migrations.WidenTimePrecision{
	Name:   "20261019_0008_widen_webhook_time_precision",
	Tables: []string{endpointsTable, callsTable, deliveriesTable, attemptsTable},
}

// addCallDeliveryID gives a received call the delivery id that tells a repeat
// from another call, unique for one receiver of one tenant. The column is
// nullable and an index holds NULL once per row on every engine, so the calls
// already stored, and a row a binary of the previous release writes during a
// rollout, neither collide nor need a value.
type addCallDeliveryID struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order.
func (addCallDeliveryID) GetName() string {
	return "20261022_0009_add_delivery_id_to_webhook_client_calls"
}

// Up adds the column and the index that decides which of two copies of one
// delivery is stored.
func (addCallDeliveryID) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.String("delivery_id", maxDeliveryIDLen).Nullable()
		table.Unique([]string{"tenant_id", "name", "delivery_id"}, callsDeliveryIndex)
	})
}

// Down drops the index before the column it names.
func (addCallDeliveryID) Down(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Table(ctx, callsTable, func(table *schema.Blueprint) {
		table.DropUnique(callsDeliveryIndex)
		table.DropColumn("delivery_id")
	})
}

// callsDeliveryIndex is the unique index of a call's delivery id.
const callsDeliveryIndex = "webhook_client_calls_delivery_uidx"
