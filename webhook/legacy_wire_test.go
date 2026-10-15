package webhook

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/queue/jobs"
)

// These tests hold what a deployed Manager puts on the wire and in its table,
// now that its attempt is the engine's: an application still dispatching
// through it, and every receiver verifying its calls, must see no difference.

// TestManagerDispatchQueuesTheJobItAlwaysQueued holds the job a worker of any
// release reads: its name, queue, action, the {"deliveryId"} payload and the
// queue's retry plan, which is Manager's retry plan.
func TestManagerDispatchQueuesTheJobItAlwaysQueued(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	manager := mustManager(t, store, target, nil)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	if err := manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{"ok":true}`)},
		[]ManagerEndpoint{{ID: "accounting", URL: "https://hooks.example.test/invoices", Headers: map[string]string{"X-Custom": "1"}, SecretRef: "accounting-key"}},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(target.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(target.jobs))
	}
	job := target.jobs[0]
	if job.Name != "webhook.deliver" || job.Queue != "webhooks" || job.Action != "webhook.dispatch" {
		t.Fatalf("job route = name %q, queue %q, action %q", job.Name, job.Queue, job.Action)
	}
	var payload map[string]any
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}
	delivery := ""
	for _, stored := range store.deliveries {
		delivery = stored.ID
	}
	if len(payload) != 1 || payload["deliveryId"] != delivery || delivery == "" {
		t.Fatalf("payload = %s, want only deliveryId %q", job.Payload, delivery)
	}
	want := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	if job.Attributes.Tries != 5 || !reflect.DeepEqual(job.Attributes.Backoff, want) || job.Attributes.Timeout != 20*time.Second {
		t.Fatalf("retry plan = tries %d, backoff %v, timeout %s", job.Attributes.Tries, job.Attributes.Backoff, job.Attributes.Timeout)
	}
	stored := store.deliveries[delivery]
	if stored.Status != StatusPending || stored.EventID != "event-1" || stored.EndpointID != "accounting" ||
		stored.URL != "https://hooks.example.test/invoices" || string(stored.Body) != `{"ok":true}` ||
		stored.SecretRef != "accounting-key" || stored.Headers["X-Custom"] != "1" {
		t.Fatalf("stored delivery = %#v", stored)
	}
}

// TestManagerSendsTheWireItAlwaysSent holds the request itself, for a fixed
// secret, clock, delivery id and body: the header names, the values beside
// them, and the signature's bytes.
func TestManagerSendsTheWireItAlwaysSent(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	var received *http.Request
	var body []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received = request
		body, _ = io.ReadAll(request.Body)
		return response(request, http.StatusNoContent, ""), nil
	})}
	manager := mustManager(t, store, target, client)
	manager.now = func() time.Time { return time.Unix(1700000000, 0) }

	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	if _, err := store.Create(context.Background(), g, ManagerDelivery{
		ID: "delivery-1", EventID: "event-1", EventName: "invoice.paid", EndpointID: "accounting",
		URL: "https://hooks.example.test/invoices", Headers: map[string]string{"X-Custom": "1"},
		Body: []byte(`{"ok":true}`), Status: StatusPending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	store.deliveries["expired"] = ManagerDelivery{ID: "expired", TenantID: "tenant-a", CreatedAt: time.Now().Add(-DefaultRetention - time.Hour)}
	job, err := jobs.New(g, QueueName, DeliveryJobName, deliveryJob{DeliveryID: "delivery-1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(job.Payload) != `{"deliveryId":"delivery-1"}` {
		t.Fatalf("job payload = %s", job.Payload)
	}
	if err := manager.handleDelivery(context.Background(), g, &job); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if received == nil {
		t.Fatal("no request was sent")
	}

	if received.Method != http.MethodPost || received.URL.String() != "https://hooks.example.test/invoices" || string(body) != `{"ok":true}` {
		t.Fatalf("request = %s %s %q", received.Method, received.URL, body)
	}
	want := map[string]string{
		"X-Arandu-Signature":   "sha256=af34f08a70de04e50eaf9a7dda04a3dcf29aedd00f589f32b25bbc3bf912ba78",
		"X-Arandu-Timestamp":   "1700000000",
		"X-Arandu-Delivery-Id": "delivery-1",
		"Content-Type":         "application/json",
		"User-Agent":           "Arandu-Webhook/1.0",
		"X-Custom":             "1",
	}
	for name, value := range want {
		values, ok := received.Header[name]
		if !ok || len(values) != 1 || values[0] != value {
			t.Errorf("header %s = %v, want %q", name, received.Header[name], value)
		}
	}
	if len(received.Header) != len(want) {
		t.Errorf("headers = %v, want exactly %v", received.Header, want)
	}
	if received.Header.Get(deliveryHeader) != "delivery-1" || received.Header.Get(timestampHeader) != "1700000000" ||
		received.Header.Get(signatureHeader) != want["X-Arandu-Signature"] {
		t.Fatal("the header names Manager declares do not read the request it sent")
	}
	if _, kept := store.deliveries["expired"]; kept {
		t.Fatal("the attempt did not prune the tenant's deliveries past the retention")
	}
	stored, err := store.Find(context.Background(), g, "delivery-1")
	if err != nil || stored.Status != StatusDelivered || stored.ResponseStatus != http.StatusNoContent || !stored.DeliveredAt.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("settled delivery = %#v, %v", stored, err)
	}
}

// TestManagerGoesThroughTheEngineTransport holds that a Manager with no client
// of its own sends on the engine's guarded transport, which refuses the shared
// address space the client package lets through. The refusal happens as the
// address is dialed, so nothing leaves the machine.
func TestManagerGoesThroughTheEngineTransport(t *testing.T) {
	if !refusedAddress(netip.MustParseAddr("100.64.0.1")) {
		t.Fatal("the engine does not refuse 100.64.0.0/10")
	}
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	manager := mustManager(t, store, target, nil)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	if err := manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]ManagerEndpoint{{ID: "carrier", URL: "https://100.64.0.1/hook"}},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if err := manager.handleDelivery(context.Background(), g, &target.jobs[0]); err != nil {
		t.Fatalf("a permanent refusal is settled, not retried: %v", err)
	}
	for _, stored := range store.deliveries {
		if stored.Status != StatusFailed || !stored.Permanent || stored.LastError != FailureUnsafeDestination {
			t.Fatalf("delivery to 100.64.0.1 = %#v, want failed unsafe_destination", stored)
		}
		return
	}
	t.Fatal("no delivery was recorded")
}

// TestManagerKeepsItsTableAndTheModuleNeverDeclaresIt holds the migration of
// webhook_deliveries by name and by the columns it creates, and that the
// Module's own schema never creates that table.
func TestManagerKeepsItsTableAndTheModuleNeverDeclaresIt(t *testing.T) {
	if name := (CreateDeliveriesTable{}).GetName(); name != "2026_09_13_000010_create_webhook_deliveries_table" {
		t.Fatalf("migration name = %q", name)
	}
	statements := pretend(t, CreateDeliveriesTable{})
	create := ""
	for _, statement := range statements {
		if lower := strings.ToLower(statement); strings.HasPrefix(lower, `create table "webhook_deliveries"`) {
			create = lower
		}
	}
	if create == "" {
		t.Fatalf("no statement creates webhook_deliveries: %q", statements)
	}
	at := 0
	for _, column := range []string{
		"id", "tenant_id", "event_id", "event_name", "endpoint_id", "url", "headers", "secret_ref", "body",
		"status", "attempts", "permanent", "response_status", "last_error", "claim_token", "claim_version",
		"lease_until", "created_at", "updated_at", "delivered_at",
	} {
		next := strings.Index(create[at:], `"`+column+`" `)
		if next < 0 {
			t.Fatalf("column %s is missing, or out of order, in %q", column, create)
		}
		at += next + len(column)
	}
	joined := strings.ToLower(strings.Join(statements, "\n"))
	for _, index := range []string{"webhook_delivery_event_endpoint_uidx", "webhook_delivery_tenant_id_idx", "webhook_delivery_retention_idx"} {
		if !strings.Contains(joined, index) {
			t.Errorf("index %s is missing", index)
		}
	}

	module := &Module{}
	for _, migration := range module.Migrations() {
		if _, ok := migration.(CreateDeliveriesTable); ok {
			t.Fatal("the Module declares CreateDeliveriesTable")
		}
		for _, statement := range pretend(t, migration) {
			if strings.Contains(strings.ToLower(statement), `"webhook_deliveries"`) {
				t.Fatalf("%s touches webhook_deliveries: %q", migration.GetName(), statement)
			}
		}
	}
}

// unreachable is a database nothing connects to: pretending renders the
// statements and runs none, so the root module needs no driver for it.
type unreachable struct{}

func (unreachable) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("webhook test: no database behind a pretended migration")
}
func (unreachable) Driver() driver.Driver { return nil }

// pretend renders a migration's statements for SQLite without a database.
func pretend(t *testing.T, migration migrations.Migration) []string {
	t.Helper()
	conn := database.ForMigrations(database.NewConnection(sql.OpenDB(unreachable{}), "", "", map[string]any{"driver": "sqlite", "name": "pretend"}))
	pretender, ok := conn.(migrations.PretendingConnection)
	if !ok {
		t.Fatal("the migration connection cannot pretend")
	}
	statements, err := pretender.Pretend(context.Background(), func() error { return migration.Up(context.Background(), conn) })
	if err != nil {
		t.Fatalf("rendering %s: %v", migration.GetName(), err)
	}
	if len(statements) == 0 {
		t.Fatalf("%s renders nothing", migration.GetName())
	}
	return statements
}
