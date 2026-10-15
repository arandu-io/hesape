package pgx_test

// The webhook Module's tables on a real PostgreSQL server: its migrations
// apply, roll back and apply again beside the webhook_deliveries table the
// deprecated Manager keeps, and a second worker claiming a delivery whose
// attempt is on the wire is refused, so one attempt goes out.
//
// Both skip without ARANDU_TEST_POSTGRES_DSN, like the conformance suite. Each
// test runs in a schema of its own, made for it and dropped after it, so the
// fixed table names the Module and the queue use never meet another run's.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/webhook"
	jackpgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// webhookKey seals the endpoints' secrets at rest.
const webhookKey = "0123456789abcdef0123456789abcdef"

// webhookTenant is the tenant every row of these tests belongs to.
const webhookTenant = "acme"

// nativeTables are the tables the Module's migrations own.
var nativeTables = []string{
	"webhook_server_endpoints", "webhook_client_calls",
	"webhook_server_deliveries", "webhook_server_delivery_attempts",
}

// webhookServer opens a schema of its own on the server, with the queue's and
// the outbox's tables in it, and drops it when the test ends.
func webhookServer(t *testing.T) (*database.DB, migrations.Connection) {
	t.Helper()
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_POSTGRES_DSN to run this against a real server")
	}
	ctx := context.Background()
	admin, err := sql.Open(driverName(t), dsn())
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	schema := "arandu_webhook_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("creating the schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("dropping the schema %s: %v", schema, err)
		}
		_ = admin.Close()
	})

	cfg, err := jackpgx.ParseConfig(dsn())
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	raw := stdlib.OpenDB(*cfg)
	raw.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = raw.Close() })

	db := database.Wrap(raw, database.DialectPostgres)
	conn := database.ForMigrations(database.NewConnection(raw, cfg.Database, "", map[string]any{
		"driver": string(database.DialectPostgres), "name": "pgsql",
	}))
	for _, m := range append(queue.NewDatabaseQueue(db).Migrations(), events.NewModule().Migrations()...) {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	return db, conn
}

// tableExists reads the schema the connection's search_path names.
func tableExists(t *testing.T, db *database.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?`, table).Scan(&n); err != nil {
		t.Fatalf("looking for %s: %v", table, err)
	}
	return n == 1
}

func countRows(t *testing.T, db *database.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// webhookManager is a subject of the tenant given every action of the
// package.
func webhookManager() auth.Subject {
	return auth.Subject{ID: "manager", Tenant: webhookTenant, Actions: webhook.Actions(), Verified: true}
}

func newWebhookModule(t *testing.T, db *database.DB, server webhook.Server) *webhook.Module {
	t.Helper()
	module, err := webhook.New(webhook.Config{Key: []byte(webhookKey), Server: server}, db, nil)
	if err != nil {
		t.Fatalf("webhook.New() error = %v", err)
	}
	return module
}

func TestTheNativeMigrationsApplyRollBackAndApplyAgainBesideManagersTable(t *testing.T) {
	db, conn := webhookServer(t)
	ctx := context.Background()

	// The deprecated Manager's table, holding a row of its own.
	if err := (webhook.CreateDeliveriesTable{}).Up(ctx, conn); err != nil {
		t.Fatalf("creating webhook_deliveries: %v", err)
	}
	if _, err := webhook.NewDatabaseStore(db).Create(ctx, auth.SystemGrant(webhook.ActionDispatch, webhookTenant), webhook.ManagerDelivery{
		ID: "delivery-1", EventID: "event-1", EndpointID: "endpoint-1", URL: "https://hooks.example.test",
		Headers: map[string]string{}, Body: []byte(`{}`),
	}); err != nil {
		t.Fatalf("storing a Manager delivery: %v", err)
	}

	module := newWebhookModule(t, db, webhook.Server{})
	declared := module.Migrations()
	for round := 0; round < 2; round++ {
		for _, m := range declared {
			if err := m.Up(ctx, conn); err != nil {
				t.Fatalf("round %d: applying %s: %v", round, m.GetName(), err)
			}
		}
		for _, table := range nativeTables {
			if !tableExists(t, db, table) {
				t.Fatalf("round %d: %s was not created", round, table)
			}
		}
		// The schema takes a row: the types the migrations chose are ones
		// this server stores.
		if _, _, err := module.EndpointService().Create(ctx, webhookManager(), webhook.EndpointRequest{
			Name: "Kept", URL: "https://example.com/hooks", Events: []string{"*"}, Active: true,
		}); err != nil {
			t.Fatalf("round %d: creating an endpoint: %v", round, err)
		}

		for i := len(declared) - 1; i >= 0; i-- {
			reversible, ok := declared[i].(migrations.ReversibleMigration)
			if !ok {
				t.Fatalf("%s cannot be rolled back", declared[i].GetName())
			}
			if err := reversible.Down(ctx, conn); err != nil {
				t.Fatalf("round %d: rolling back %s: %v", round, declared[i].GetName(), err)
			}
		}
		for _, table := range nativeTables {
			if tableExists(t, db, table) {
				t.Fatalf("round %d: %s survived the rollback", round, table)
			}
		}
		if !tableExists(t, db, "webhook_deliveries") {
			t.Fatalf("round %d: the Module's migrations dropped webhook_deliveries", round)
		}
		if n := countRows(t, db, "webhook_deliveries"); n != 1 {
			t.Fatalf("round %d: webhook_deliveries holds %d rows after the Module's migrations, want its 1", round, n)
		}
	}
}

func TestTwoWorkersClaimingOneDeliverySendOneAttempt(t *testing.T) {
	db, conn := webhookServer(t)
	ctx := context.Background()

	// The receiver holds the first request until the test lets it go, so the
	// second claim is made while the first attempt is on the wire.
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	var mu sync.Mutex
	requests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		arrived <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(release) }) }

	// A clock in the past: the jobs it queues are due on the queue's clock,
	// and whole seconds leave nothing to a column's precision.
	epoch := time.Unix(1_700_000_000, 0).UTC()
	module := newWebhookModule(t, db, webhook.Server{HTTPClient: target.Client(), Now: func() time.Time { return epoch }})
	for _, m := range module.Migrations() {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	jobs := queue.NewDatabaseQueue(db)
	first := queue.NewWorker(jobs, queue.WorkerOptions{Name: "first", Queue: webhook.DefaultQueue, Concurrency: 1, Sleep: time.Millisecond})
	second := queue.NewWorker(jobs, queue.WorkerOptions{Name: "second", Queue: webhook.DefaultQueue, Concurrency: 1, Sleep: time.Millisecond})
	for _, worker := range []*queue.Worker{first, second} {
		if err := module.RegisterJobHandlers(worker); err != nil {
			t.Fatal(err)
		}
	}

	endpoint, _, err := module.EndpointService().Create(ctx, webhookManager(), webhook.EndpointRequest{
		Name: "Billing", URL: target.URL + "/hooks", Events: []string{"*"}, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := auth.SystemGrant(webhook.DeliveryDispatch, webhookTenant)
	delivery, err := module.Send(ctx, dispatch, endpoint.ID, webhook.Message{Event: "order.paid", Body: []byte(`{"order":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	// A redelivery asked for before the first attempt ran: two jobs for one
	// attempt.
	if _, err := module.EndpointService().Redeliver(ctx, webhookManager(), delivery.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := jobs.Size(ctx, webhook.DefaultQueue); err != nil || n != 2 {
		t.Fatalf("%d jobs wait (%v), want the scheduled one and the redelivery", n, err)
	}

	// The first worker takes one job, claims the delivery and is held on the
	// wire. Started together, two workers can pick the same job from the queue
	// and one of them gets nothing, which is the queue's compare-and-set and
	// not the claim this test is about; so the second starts only once the
	// first holds its job, and the queue can hand it nothing but the other.
	var firstErr error
	firstDone := make(chan struct{})
	go func() { firstErr = first.RunNextJob(ctx); close(firstDone) }()
	// Whatever happens below, the held attempt is let go and finishes before
	// the database closes under it.
	t.Cleanup(func() {
		letGo()
		select {
		case <-firstDone:
		case <-time.After(30 * time.Second):
		}
	})
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("no attempt reached the receiver")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- second.RunNextJob(ctx) }()
	select {
	case <-secondDone:
	case <-time.After(30 * time.Second):
		t.Fatal("the second claim waited on the first attempt instead of being refused")
	}
	// The second worker took the other job, found the attempt in progress and
	// put the job back for later; the first still holds its own.
	if held, err := jobs.ReservedSize(ctx, webhook.DefaultQueue); err != nil || held != 1 {
		t.Fatalf("%d jobs are held (%v), want the attempt on the wire", held, err)
	}
	if later, err := jobs.DelayedSize(ctx, webhook.DefaultQueue); err != nil || later != 1 {
		t.Fatalf("%d jobs wait for later (%v), want the one refused the claim", later, err)
	}
	if row, err := webhook.Deliveries(db).WhereKey(delivery.ID).First(ctx, dispatch); err != nil ||
		row == nil || row.Status != webhook.DeliveryProcessing || row.Attempts != 1 {
		t.Fatalf("while the attempt is out the delivery is %+v, %v", row, err)
	}

	letGo()
	select {
	case <-firstDone:
		if firstErr != nil {
			t.Fatalf("the attempt that went out: %v", firstErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the attempt that went out never finished")
	}

	// The job refused the claim comes back after the attempt settled, and
	// finds nothing left to send.
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = ? WHERE queue = ?`, time.Now().UTC().Add(-time.Second), webhook.DefaultQueue); err != nil {
		t.Fatal(err)
	}
	if err := second.RunNextJob(ctx); err != nil {
		t.Fatalf("the job refused the claim, run again: %v", err)
	}
	if n, err := jobs.Size(ctx, webhook.DefaultQueue); err != nil || n != 0 {
		t.Fatalf("%d jobs are left (%v), want none", n, err)
	}

	mu.Lock()
	sent := requests
	mu.Unlock()
	if sent != 1 {
		t.Fatalf("two claims of one attempt sent %d requests", sent)
	}
	row, err := webhook.Deliveries(db).WhereKey(delivery.ID).First(ctx, dispatch)
	if err != nil || row == nil || row.Status != webhook.DeliveryDelivered || row.Attempts != 1 {
		t.Fatalf("after the attempt the delivery is %+v, %v", row, err)
	}
	attempts, err := module.EndpointService().Attempts(ctx, webhookManager(), delivery.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("the attempt history is %v, %v", attempts, err)
	}
}
