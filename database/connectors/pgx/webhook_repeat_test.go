package pgx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/webhook"
)

// Copies of one delivery arriving together on PostgreSQL: the unique index
// refuses every row but one, the refused insert aborts only its own
// transaction, and every copy is answered as delivered.
func TestConcurrentCopiesOfOneDeliveryAreStoredOnceOnPostgres(t *testing.T) {
	db, conn := webhookServer(t)
	ctx := context.Background()
	const secret = "shared-secret-shared-secret-0123"
	module, err := webhook.New(webhook.Config{Key: []byte(webhookKey), Receivers: []webhook.Receiver{{
		Name: "billing", Tenant: webhookTenant, Secret: secret, Validator: webhook.Arandu{},
		Process: webhook.ProcessorFunc(func(context.Context, auth.Grant, *webhook.Call) error { return nil }),
	}}}, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range module.Migrations() {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	router := routing.NewRouter()
	module.Routes(router.ForModule(module.Name()))

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := `{"invoice":"inv_1"}`
	const copies = 8
	codes := make([]int, copies)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < copies; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			req := httptest.NewRequest(http.MethodPost, "/webhooks/billing", strings.NewReader(body))
			req.Header.Set("X-Arandu-Timestamp", ts)
			req.Header.Set("X-Arandu-Delivery-ID", "dlv-race")
			req.Header.Set("X-Arandu-Signature", webhook.Sign([]byte(secret), ts, "dlv-race", []byte(body)))
			start.Wait()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	start.Done()
	done.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("copy %d answered %d, want 200", i+1, code)
		}
	}
	if n := countRows(t, db, "webhook_client_calls"); n != 1 {
		t.Fatalf("%d calls were stored for one delivery sent %d times at once, want 1", n, copies)
	}
	if n := countRows(t, db, "jobs"); n != 1 {
		t.Errorf("%d processings were queued, want 1", n)
	}
}
