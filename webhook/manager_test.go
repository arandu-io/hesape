package webhook

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
	httpclient "github.com/arandu-io/hesape/http/client"
	"github.com/arandu-io/hesape/queue/jobs"
)

const testSecret = "01234567890123456789012345678901"

type memoryStore struct {
	mu         sync.Mutex
	deliveries map[string]Delivery
	unique     map[string]string
	claims     map[string]Claim
	inTx       bool
	pruned     int64
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		deliveries: map[string]Delivery{},
		unique:     map[string]string{},
		claims:     map[string]Claim{},
	}
}

func (s *memoryStore) Transaction(ctx context.Context, fn func(context.Context) error) error {
	s.mu.Lock()
	beforeDeliveries := cloneDeliveries(s.deliveries)
	beforeUnique := cloneStrings(s.unique)
	s.inTx = true
	s.mu.Unlock()
	err := fn(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inTx = false
	if err != nil {
		s.deliveries = beforeDeliveries
		s.unique = beforeUnique
	}
	return err
}

func (s *memoryStore) Create(_ context.Context, g auth.Grant, delivery Delivery) (bool, error) {
	tenant, err := tenantFor(g)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenant + "\x00" + delivery.EventID + "\x00" + delivery.EndpointID
	if _, exists := s.unique[key]; exists {
		return false, nil
	}
	delivery.TenantID = tenant
	delivery.Headers = cloneHeaders(delivery.Headers)
	delivery.Body = append([]byte(nil), delivery.Body...)
	s.deliveries[delivery.ID] = delivery
	s.unique[key] = delivery.ID
	return true, nil
}

func (s *memoryStore) Find(_ context.Context, g auth.Grant, id string) (Delivery, error) {
	tenant, err := tenantFor(g)
	if err != nil {
		return Delivery{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery, exists := s.deliveries[id]
	if !exists || delivery.TenantID != tenant {
		return Delivery{}, ErrDeliveryNotFound
	}
	return delivery, nil
}

func (s *memoryStore) Claim(_ context.Context, g auth.Grant, id string, lease time.Duration) (Claim, bool, error) {
	tenant, err := tenantFor(g)
	if err != nil {
		return Claim{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery, exists := s.deliveries[id]
	if !exists || delivery.TenantID != tenant {
		return Claim{}, false, ErrDeliveryNotFound
	}
	if delivery.Status == StatusDelivered || delivery.Permanent || delivery.LeaseUntil.After(time.Now()) {
		return Claim{}, false, nil
	}
	prior := s.claims[id]
	claim := Claim{
		Delivery: delivery,
		Token:    id + "-claim-" + time.Now().Format(time.RFC3339Nano),
		Version:  prior.Version + 1,
		LeaseEnd: time.Now().Add(lease),
	}
	claim.Delivery.Attempts++
	claim.Delivery.Status = StatusProcessing
	claim.Delivery.LeaseUntil = claim.LeaseEnd
	s.deliveries[id] = claim.Delivery
	s.claims[id] = claim
	return claim, true, nil
}

func (s *memoryStore) Complete(_ context.Context, g auth.Grant, claim Claim, result Result) error {
	if _, err := tenantFor(g); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.claims[claim.Delivery.ID]
	if current.Token != claim.Token || current.Version != claim.Version {
		return ErrStaleClaim
	}
	delivery := s.deliveries[claim.Delivery.ID]
	delivery.Status = StatusDelivered
	delivery.ResponseStatus = result.StatusCode
	delivery.DeliveredAt = result.FinishedAt
	delivery.LeaseUntil = time.Time{}
	s.deliveries[delivery.ID] = delivery
	delete(s.claims, delivery.ID)
	return nil
}

func (s *memoryStore) Fail(_ context.Context, g auth.Grant, claim Claim, failure Failure) error {
	if _, err := tenantFor(g); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.claims[claim.Delivery.ID]
	if current.Token != claim.Token || current.Version != claim.Version {
		return ErrStaleClaim
	}
	delivery := s.deliveries[claim.Delivery.ID]
	delivery.Status = StatusFailed
	delivery.ResponseStatus = failure.StatusCode
	delivery.LastError = failure.Code
	delivery.Permanent = failure.Permanent
	delivery.LeaseUntil = time.Time{}
	s.deliveries[delivery.ID] = delivery
	delete(s.claims, delivery.ID)
	return nil
}

func (s *memoryStore) Prune(_ context.Context, g auth.Grant, cutoff time.Time) (int64, error) {
	tenant, err := tenantFor(g)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int64
	for id, delivery := range s.deliveries {
		if delivery.TenantID == tenant && delivery.CreatedAt.Before(cutoff) {
			delete(s.deliveries, id)
			removed++
		}
	}
	s.pruned += removed
	return removed, nil
}

type memoryQueue struct {
	store *memoryStore
	jobs  []jobs.Job
	err   error
	inTx  bool
}

func (q *memoryQueue) Push(_ context.Context, _ auth.Grant, job jobs.Job) error {
	q.store.mu.Lock()
	q.inTx = q.store.inTx
	q.store.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, job)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDispatchQueueHandlerDeliversSignedSnapshot(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	var received *http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received = request
		return response(request, http.StatusNoContent, ""), nil
	})}
	manager := mustManager(t, store, target, client)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	err := manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{"id":"inv-1"}`)},
		[]Endpoint{{ID: "accounting", URL: "https://hooks.example.test/invoices", SecretRef: "accounting-key"}},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(target.jobs) != 1 || !target.inTx {
		t.Fatalf("queued jobs = %d, in transaction = %v", len(target.jobs), target.inTx)
	}
	if err := manager.handleDelivery(context.Background(), g, &target.jobs[0]); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}
	if received == nil || received.Header.Get(deliveryHeader) == "" {
		t.Fatal("delivery request or delivery header is missing")
	}
	deliveryID := received.Header.Get(deliveryHeader)
	timestamp := received.Header.Get(timestampHeader)
	if !Verify(NewStaticSecret([]byte(testSecret)).set, timestamp, deliveryID,
		[]byte(`{"id":"inv-1"}`), received.Header.Get(signatureHeader)) {
		t.Fatal("request signature does not verify")
	}
	delivery, err := store.Find(context.Background(), g, deliveryID)
	if err != nil || delivery.Status != StatusDelivered || delivery.ResponseStatus != http.StatusNoContent {
		t.Fatalf("delivery = %#v, error = %v", delivery, err)
	}
}

func TestDispatchIsIdempotentPerTenantEventEndpoint(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	manager := mustManager(t, store, target, nil)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	event := Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)}
	endpoint := Endpoint{ID: "accounting", URL: "https://hooks.example.test"}
	if err := manager.Dispatch(context.Background(), g, event, []Endpoint{endpoint}); err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	if err := manager.Dispatch(context.Background(), g, event, []Endpoint{endpoint}); err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	if len(target.jobs) != 1 || len(store.deliveries) != 1 {
		t.Fatalf("jobs = %d, deliveries = %d", len(target.jobs), len(store.deliveries))
	}
}

func TestDispatchRollsBackSnapshotWhenQueueWriteFails(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store, err: errors.New("queue unavailable")}
	manager := mustManager(t, store, target, nil)
	err := manager.Dispatch(context.Background(), auth.SystemGrant(ActionDispatch, "tenant-a"),
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]Endpoint{{ID: "accounting", URL: "https://hooks.example.test"}},
	)
	if err == nil {
		t.Fatal("Dispatch() error = nil")
	}
	if len(store.deliveries) != 0 {
		t.Fatalf("deliveries after rollback = %d", len(store.deliveries))
	}
}

func TestHandlerReturnsRetryAfterWithoutTransportRetry(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		answer := response(request, http.StatusTooManyRequests, "")
		answer.Header.Set("Retry-After", "120")
		return answer, nil
	})}
	manager := mustManager(t, store, target, client)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	if err := manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]Endpoint{{ID: "accounting", URL: "https://hooks.example.test"}},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	err := manager.handleDelivery(context.Background(), g, &target.jobs[0])
	var failure Failure
	if !errors.As(err, &failure) || failure.RetryAfterDuration() != 120*time.Second {
		t.Fatalf("handleDelivery() error = %#v", err)
	}
	if attempts != 1 {
		t.Fatalf("transport attempts = %d", attempts)
	}
}

func TestHTTPFailureClassification(t *testing.T) {
	tests := []struct {
		status    int
		permanent bool
	}{
		{http.StatusRequestTimeout, false},
		{425, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadRequest, true},
		{http.StatusMovedPermanently, true},
		{http.StatusContinue, true},
	}
	for _, test := range tests {
		failure := classify(nil, test.status, "")
		if failure.Permanent != test.permanent {
			t.Errorf("classify(status %d).Permanent = %v", test.status, failure.Permanent)
		}
	}
}

func TestTransportAndGuardFailureClassification(t *testing.T) {
	if classify(errors.New("connection reset"), 0, "").Permanent {
		t.Fatal("transport failure is permanent")
	}
	if !classify(httpclient.ErrInternalAddress, 0, "").Permanent {
		t.Fatal("internal address failure is retryable")
	}
	if !classify(httpclient.ErrResponseTooLarge, 0, "").Permanent {
		t.Fatal("oversized response failure is retryable")
	}
}

func TestCancellationIsRetryable(t *testing.T) {
	failure := classify(context.Canceled, 0, "")
	if failure.Permanent || failure.Code != "request_canceled" {
		t.Fatalf("canceled request classification = %#v", failure)
	}
}

func TestTimeoutAndTLSFailuresAreRetryable(t *testing.T) {
	tests := []struct {
		cause error
		code  string
	}{
		{context.DeadlineExceeded, "request_timeout"},
		{tls.RecordHeaderError{Msg: "invalid TLS record", RecordHeader: [5]byte{0, 1, 2, 3, 4}}, "transport_failure"},
	}
	for _, test := range tests {
		failure := classify(test.cause, 0, "")
		if failure.Permanent || failure.Code != test.code {
			t.Errorf("classify(%T) = %#v", test.cause, failure)
		}
	}
}

func TestRetryAfterParsesSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("45", now); got != 45*time.Second {
		t.Fatalf("seconds Retry-After = %s", got)
	}
	if got := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Fatalf("date Retry-After = %s", got)
	}
}

func TestSignatureUsesWhatsAppWireInputAndRotatedSecret(t *testing.T) {
	body := []byte(`{"ok":true}`)
	got := Sign([]byte(testSecret), "1700000000", "delivery-1", body)
	want := "sha256=af34f08a70de04e50eaf9a7dda04a3dcf29aedd00f589f32b25bbc3bf912ba78"
	if got != want {
		t.Fatalf("Sign() = %q", got)
	}
	rotated := NewStaticSecret([]byte(strings.Repeat("n", 32)), []byte(testSecret))
	if !Verify(rotated.set, "1700000000", "delivery-1", body, got) {
		t.Fatal("Verify() rejected the previous rotation secret")
	}
	if Verify(rotated.set, "1700000000", "delivery-2", body, got) {
		t.Fatal("Verify() accepted a signature for another delivery")
	}
}

func TestVerifyRejectsAnEmptySecret(t *testing.T) {
	body := []byte(`{"ok":true}`)
	signature := Sign(nil, "1700000000", "delivery-1", body)
	if Verify(SecretSet{}, "1700000000", "delivery-1", body, signature) {
		t.Fatal("Verify() accepted a signature produced with an empty secret")
	}
}

func TestCustomHTTPClientRetainsSchemeAndResponseGuards(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		answer := response(request, http.StatusOK, "")
		answer.ContentLength = httpclient.DefaultMaxResponseBytes + 1
		return answer, nil
	})}
	store := newMemoryStore()
	manager := mustManager(t, store, &memoryQueue{store: store}, client)
	request, err := http.NewRequest(http.MethodPost, "ftp://example.test/hook", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := manager.client.Do(request); !errors.Is(err, httpclient.ErrUnsupportedScheme) {
		t.Fatalf("unsupported scheme error = %v", err)
	}
	request, err = http.NewRequest(http.MethodPost, "https://example.test/hook", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := manager.client.Do(request); !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Fatalf("oversized response error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("custom transport calls = %d", calls.Load())
	}
}

func TestRedirectLimitIsPermanentAndEveryHopIsGuarded(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		answer := response(request, http.StatusFound, "")
		answer.Header.Set("Location", "https://example.test/hop-"+time.Unix(call, 0).Format("150405"))
		return answer, nil
	})}
	store := newMemoryStore()
	manager := mustManager(t, store, &memoryQueue{store: store}, client)
	request, err := http.NewRequest(http.MethodPost, "https://example.test/start", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	_, err = manager.client.Do(request)
	failure := classify(err, 0, "")
	if !failure.Permanent || failure.Code != "too_many_redirects" {
		t.Fatalf("redirect limit classification = %#v from %v", failure, err)
	}
	if calls.Load() != 5 {
		t.Fatalf("guarded redirect hops = %d", calls.Load())
	}
}

func TestConcurrentHandlersEmitOneRequest(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		return response(request, http.StatusNoContent, ""), nil
	})}
	manager := mustManager(t, store, target, client)
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	if err := manager.Dispatch(context.Background(), g,
		Event{ID: "event-1", Name: "invoice.paid", Payload: []byte(`{}`)},
		[]Endpoint{{ID: "accounting", URL: "https://hooks.example.test"}},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- manager.handleDelivery(context.Background(), g, &target.jobs[0])
	}()
	<-entered
	secondErr := manager.handleDelivery(context.Background(), g, &target.jobs[0])
	var failure Failure
	if !errors.As(secondErr, &failure) || failure.Code != "delivery_claimed" {
		t.Fatalf("concurrent handler error = %#v", secondErr)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first handler error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP requests = %d", calls.Load())
	}
}

func TestStoreReadsAreTenantIsolated(t *testing.T) {
	store := newMemoryStore()
	owner := auth.SystemGrant(ActionDispatch, "tenant-a")
	if _, err := store.Create(context.Background(), owner,
		Delivery{ID: "delivery-1", EventID: "event-1", EndpointID: "endpoint-1"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	other := auth.SystemGrant(ActionDispatch, "tenant-b")
	if _, err := store.Find(context.Background(), other, "delivery-1"); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("cross-tenant Find() error = %v", err)
	}
}

func TestPruneAppliesRetentionWithinTenant(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	manager := mustManager(t, store, target, nil)
	now := time.Now().UTC()
	store.deliveries["old-a"] = Delivery{ID: "old-a", TenantID: "tenant-a", CreatedAt: now.Add(-DefaultRetention - time.Hour)}
	store.deliveries["old-b"] = Delivery{ID: "old-b", TenantID: "tenant-b", CreatedAt: now.Add(-DefaultRetention - time.Hour)}
	manager.prune(context.Background(), auth.SystemGrant(ActionDispatch, "tenant-a"))
	if _, exists := store.deliveries["old-a"]; exists {
		t.Fatal("expired tenant delivery was retained")
	}
	if _, exists := store.deliveries["old-b"]; !exists {
		t.Fatal("another tenant's delivery was pruned")
	}
}

func TestNormalizeURLAndPayloadBoundsArePermanentValidation(t *testing.T) {
	for _, value := range []string{
		"ftp://example.test/hook",
		"http://user@example.test/hook",
		"relative",
		"https://example.test/" + strings.Repeat("a", MaxURLLength),
	} {
		if _, err := NormalizeURL(value); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("NormalizeURL(%q) error = %v", value, err)
		}
	}
	store := newMemoryStore()
	manager := mustManager(t, store, &memoryQueue{store: store}, nil)
	err := manager.Dispatch(context.Background(), auth.SystemGrant(ActionDispatch, "tenant-a"),
		Event{ID: "event-1", Name: "large", Payload: make([]byte, MaxPayloadBytes+1)}, nil)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized Dispatch() error = %v", err)
	}
}

func TestFencingRejectsAStaleClaim(t *testing.T) {
	store := newMemoryStore()
	g := auth.SystemGrant(ActionDispatch, "tenant-a")
	created, err := store.Create(context.Background(), g, Delivery{ID: "delivery-1", EventID: "event-1", EndpointID: "endpoint-1"})
	if err != nil || !created {
		t.Fatalf("Create() = %v, %v", created, err)
	}
	first, claimed, err := store.Claim(context.Background(), g, "delivery-1", -time.Second)
	if err != nil || !claimed {
		t.Fatalf("first Claim() = %v, %v", claimed, err)
	}
	second, claimed, err := store.Claim(context.Background(), g, "delivery-1", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("second Claim() = %v, %v", claimed, err)
	}
	if err := store.Complete(context.Background(), g, first, Result{StatusCode: 204}); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("stale Complete() error = %v", err)
	}
	if err := store.Complete(context.Background(), g, second, Result{StatusCode: 204}); err != nil {
		t.Fatalf("current Complete() error = %v", err)
	}
}

func TestPublisherPreservesStoredEventIdentity(t *testing.T) {
	store := newMemoryStore()
	target := &memoryQueue{store: store}
	manager := mustManager(t, store, target, nil)
	resolver := EndpointResolverFunc(func(_ context.Context, g auth.Grant, stored events.Stored) ([]Endpoint, error) {
		if auth.Tenant(g) != stored.TenantID {
			t.Fatalf("resolver tenant = %q", auth.Tenant(g))
		}
		return []Endpoint{{ID: "endpoint-1", URL: "https://hooks.example.test"}}, nil
	})
	stored := events.Stored{ID: "outbox-1", TenantID: "tenant-a", Name: "invoice.paid", Payload: `{"id":"inv-1"}`}
	if err := NewPublisher(manager, resolver).Publish(context.Background(), stored); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	for _, delivery := range store.deliveries {
		if delivery.EventID != stored.ID {
			t.Fatalf("delivery event id = %q", delivery.EventID)
		}
		return
	}
	t.Fatal("publisher did not create a delivery")
}

func mustManager(t *testing.T, store *memoryStore, target *memoryQueue, client *http.Client) *Manager {
	t.Helper()
	manager, err := newManager(store, target, NewStaticSecret([]byte(testSecret)), ManagerOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("newManager() error = %v", err)
	}
	return manager
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}

func cloneDeliveries(source map[string]Delivery) map[string]Delivery {
	cloned := make(map[string]Delivery, len(source))
	for id, delivery := range source {
		cloned[id] = delivery
	}
	return cloned
}

func cloneStrings(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
