package sqlite_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/webhook"
)

// A sender repeats a delivery under the same id when it did not see the
// answer, and treats a refusal as a delivery that failed. These are the
// receiving side's half: a repeat is answered as the first copy was, and
// processed once.

// arandu is a call to the static receiver "billing" signed in the Arandu
// scheme with the delivery id given.
func arandu(deliveryID, body string, at time.Time) *http.Request {
	ts := strconv.FormatInt(at.Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/billing", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arandu-Timestamp", ts)
	req.Header.Set("X-Arandu-Delivery-ID", deliveryID)
	req.Header.Set("X-Arandu-Signature", webhook.Sign([]byte(sharedSecret), ts, deliveryID, []byte(body)))
	return req
}

// deliveryIDOf reads the delivery id a stored call keeps, nil when it keeps
// none. It reads the column rather than the field, so this file builds
// against a release whose Call has no such field and fails only where it
// asserts on it.
func deliveryIDOf(t *testing.T, w *world, callID string) *string {
	t.Helper()
	var id sql.NullString
	if err := w.db.QueryRowContext(context.Background(), `SELECT delivery_id FROM webhook_client_calls WHERE id = ?`, callID).Scan(&id); err != nil {
		t.Fatalf("reading the delivery id of %s: %v", callID, err)
	}
	if !id.Valid {
		return nil
	}
	return &id.String
}

func billingReceiver() webhook.Receiver {
	return webhook.Receiver{Name: "billing", Tenant: tenant, Secret: sharedSecret, Validator: webhook.Arandu{}}
}

func TestARepeatedDeliveryIsAnsweredAndProcessedOnce(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) { c.Receivers = []webhook.Receiver{billingReceiver()} })
	now := time.Now()

	for i := 0; i < 3; i++ {
		rec := w.send(t, arandu("dlv-1", `{"invoice":"inv_1"}`, now))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"message":"ok"}` {
			t.Fatalf("copy %d answered %d %q, want the answer the first copy got", i+1, rec.Code, rec.Body.String())
		}
	}
	w.drain(t)

	calls := w.calls(t, tenant)
	if len(calls) != 1 {
		t.Fatalf("%d calls were stored for one delivery sent three times, want 1", len(calls))
	}
	if id := deliveryIDOf(t, w, calls[0].ID); id == nil || *id != "dlv-1" {
		t.Errorf("the call keeps the delivery id %v, want dlv-1", id)
	}
	if seen := w.handled.seen(); len(seen) != 1 {
		t.Errorf("the processor ran %d times for one delivery, want 1", len(seen))
	}
}

func TestTwoDeliveriesWithTheSameBodyAreBothProcessed(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) { c.Receivers = []webhook.Receiver{billingReceiver()} })
	now := time.Now()
	for _, id := range []string{"dlv-1", "dlv-2"} {
		if rec := w.send(t, arandu(id, `{"invoice":"inv_1"}`, now)); rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d", id, rec.Code)
		}
	}
	w.drain(t)
	if calls := w.calls(t, tenant); len(calls) != 2 {
		t.Errorf("%d calls were stored for two deliveries, want 2", len(calls))
	}
}

// A body-only HMAC signs no id: two equal calls cannot be told apart from one
// call sent twice, so both are processed, as the provider sends them. Both
// rows carry no delivery id, and the index does not make them collide.
func TestABodyOnlySignatureProcessesEveryCopy(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) {
		c.Receivers = []webhook.Receiver{{Name: "billing", Tenant: tenant, Secret: sharedSecret}}
	})
	body := `{"event":"evt_1"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/billing", strings.NewReader(body))
		req.Header.Set(webhook.DefaultSignatureHeader, webhook.SignBody([]byte(sharedSecret), []byte(body)))
		if rec := w.send(t, req); rec.Code != http.StatusOK {
			t.Fatalf("copy %d answered %d %q", i+1, rec.Code, rec.Body.String())
		}
	}
	calls := w.calls(t, tenant)
	if len(calls) != 2 {
		t.Fatalf("%d calls were stored, want both copies", len(calls))
	}
	for _, call := range calls {
		if id := deliveryIDOf(t, w, call.ID); id != nil {
			t.Errorf("a call signed with no id keeps the delivery id %q", *id)
		}
	}
}

func TestTheStandardWebhooksIDIsKeptAsTheDeliveryID(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_700_000_000, 0)
	w := install(t, func(c *webhook.Config, _ *handled) {
		c.Receivers = []webhook.Receiver{{
			Name: "standard", Tenant: tenant, Secret: stdSecret,
			Validator: webhook.StandardWebhooks{Now: func() time.Time { return at }},
		}}
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/standard", strings.NewReader(stdBody))
		req.Header.Set(webhook.StandardHeaderID, stdMessageID)
		req.Header.Set(webhook.StandardHeaderTimestamp, strconv.FormatInt(at.Unix(), 10))
		req.Header.Set(webhook.StandardHeaderSignature, stdSignature)
		if rec := w.send(t, req); rec.Code != http.StatusOK {
			t.Fatalf("copy %d answered %d %q", i+1, rec.Code, rec.Body.String())
		}
	}
	calls := w.calls(t, tenant)
	if len(calls) != 1 || deliveryIDOf(t, w, calls[0].ID) == nil || *deliveryIDOf(t, w, calls[0].ID) != stdMessageID {
		t.Fatalf("the calls are %d, want one keeping %s", len(calls), stdMessageID)
	}
}

// The id is one sender's: the same id at another receiver, or for another
// tenant, is another delivery.
func TestTheDeliveryIDIsScopedToTheReceiverAndTheTenant(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) {
		other := billingReceiver()
		other.Name = "shipping"
		elsewhere := billingReceiver()
		elsewhere.Name, elsewhere.Tenant = "billing-globex", "globex"
		c.Receivers = []webhook.Receiver{billingReceiver(), other, elsewhere}
	})
	now := time.Now()
	for _, path := range []string{"/webhooks/billing", "/webhooks/shipping", "/webhooks/billing-globex"} {
		req := arandu("dlv-1", `{"n":1}`, now)
		req.URL.Path = path
		if rec := w.send(t, req); rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d", path, rec.Code)
		}
	}
	if n := len(w.calls(t, tenant)); n != 2 {
		t.Errorf("acme kept %d calls for one id at two receivers, want 2", n)
	}
	if n := len(w.calls(t, "globex")); n != 1 {
		t.Errorf("globex kept %d calls, want 1", n)
	}
}

// Two copies arriving together are decided by the unique index, not by a read
// before the write: every copy is answered as delivered, and one is stored.
func TestConcurrentCopiesOfOneDeliveryAreStoredOnce(t *testing.T) {
	t.Parallel()
	w := install(t, func(c *webhook.Config, _ *handled) { c.Receivers = []webhook.Receiver{billingReceiver()} })
	now := time.Now()

	const copies = 8
	codes := make([]int, copies)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < copies; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			req := arandu("dlv-race", `{"invoice":"inv_9"}`, now)
			start.Wait()
			rec := httptest.NewRecorder()
			w.router.ServeHTTP(rec, req)
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
	if n := len(w.calls(t, tenant)); n != 1 {
		t.Fatalf("%d calls were stored for one delivery sent %d times at once, want 1", n, copies)
	}
	w.drain(t)
	if seen := w.handled.seen(); len(seen) != 1 {
		t.Errorf("the processor ran %d times, want 1", len(seen))
	}
}

// syncCounter is a synchronous processor that fails until told otherwise and
// counts what it ran.
type syncCounter struct {
	mu   sync.Mutex
	runs int
	fail bool
}

func (p *syncCounter) ProcessNow(context.Context, auth.Grant, *webhook.Call) (webhook.ProcessResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runs++
	if p.fail {
		return webhook.ProcessResult{}, errors.New("the ledger is down")
	}
	return webhook.ProcessResult{Status: http.StatusCreated, Body: []byte(`{"booked":true}`)}, nil
}

func (p *syncCounter) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runs
}

func (p *syncCounter) recover() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = false
}

// A synchronous receiver answers a repeat of a processed delivery 200 without
// processing it, and processes again a delivery whose processing failed: the
// sender saw that failure and is trying again, and answering it 200 would
// lose the delivery.
func TestASynchronousReceiverProcessesARepeatOnlyWhenTheFirstCopyFailed(t *testing.T) {
	t.Parallel()
	processor := &syncCounter{fail: true}
	w := install(t, func(c *webhook.Config, _ *handled) {
		r := billingReceiver()
		r.ProcessSync = processor
		c.Receivers = []webhook.Receiver{r}
	})
	now := time.Now()

	if rec := w.send(t, arandu("dlv-1", `{"n":1}`, now)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed processing answered %d, want 500", rec.Code)
	}
	processor.recover()
	rec := w.send(t, arandu("dlv-1", `{"n":1}`, now))
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"booked":true}` {
		t.Fatalf("the retry of a failed delivery answered %d %q, want it processed", rec.Code, rec.Body.String())
	}
	rec = w.send(t, arandu("dlv-1", `{"n":1}`, now))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("a repeat of a processed delivery answered %d %q, want 200 and no body", rec.Code, rec.Body.String())
	}
	if n := processor.count(); n != 2 {
		t.Errorf("the processor ran %d times, want 2: the failure and its retry", n)
	}
	calls := w.calls(t, tenant)
	if len(calls) != 1 || calls[0].Status != webhook.CallProcessed {
		t.Fatalf("the calls are %+v, want one, processed", calls)
	}
}

// A repeat of a failed synchronous delivery is processed by one copy: the
// claim on the failed call is a conditional update, and the copies that lose
// it are told to come back rather than told it was done.
func TestConcurrentRetriesOfAFailedSynchronousDeliveryProcessItOnce(t *testing.T) {
	t.Parallel()
	processor := &syncCounter{fail: true}
	w := install(t, func(c *webhook.Config, _ *handled) {
		r := billingReceiver()
		r.ProcessSync = processor
		c.Receivers = []webhook.Receiver{r}
	})
	now := time.Now()
	if rec := w.send(t, arandu("dlv-1", `{"n":1}`, now)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed processing answered %d", rec.Code)
	}
	processor.recover()

	const copies = 6
	codes := make([]int, copies)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < copies; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			req := arandu("dlv-1", `{"n":1}`, now)
			start.Wait()
			rec := httptest.NewRecorder()
			w.router.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	start.Done()
	done.Wait()

	if n := processor.count(); n != 2 {
		t.Errorf("the processor ran %d times, want 2: the failure and one retry", n)
	}
	for i, code := range codes {
		switch code {
		case http.StatusCreated, http.StatusOK, http.StatusServiceUnavailable:
		default:
			t.Errorf("copy %d answered %d, want the processed answer, 200, or 503 to come back", i+1, code)
		}
	}
}

// standardSignature signs a Standard Webhooks call the way the specification
// says, for a body and id this file chooses.
func standardSignature(secret, id, ts, body string) string {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// An id longer than the column is kept as its digest, so it is still told
// apart from another and still fits.
func TestAnIDLongerThanTheColumnIsKeptAsItsDigest(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_700_000_000, 0)
	w := install(t, func(c *webhook.Config, _ *handled) {
		c.Receivers = []webhook.Receiver{{
			Name: "standard", Tenant: tenant, Secret: stdSecret,
			Validator: webhook.StandardWebhooks{Now: func() time.Time { return at }},
		}}
	})
	ts := strconv.FormatInt(at.Unix(), 10)
	for _, id := range []string{strings.Repeat("a", 300), strings.Repeat("a", 300), strings.Repeat("a", 299) + "b"} {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/standard", strings.NewReader(stdBody))
		req.Header.Set(webhook.StandardHeaderID, id)
		req.Header.Set(webhook.StandardHeaderTimestamp, ts)
		req.Header.Set(webhook.StandardHeaderSignature, standardSignature(stdSecret, id, ts, stdBody))
		if rec := w.send(t, req); rec.Code != http.StatusOK {
			t.Fatalf("a long id answered %d %q", rec.Code, rec.Body.String())
		}
	}
	calls := w.calls(t, tenant)
	if len(calls) != 2 {
		t.Fatalf("%d calls were stored for two long ids, one sent twice, want 2", len(calls))
	}
	for _, call := range calls {
		if id := deliveryIDOf(t, w, call.ID); id == nil || !strings.HasPrefix(*id, "sha256:") || len(*id) > 128 {
			t.Errorf("a long id is kept as %v, want its digest", id)
		}
	}
}
