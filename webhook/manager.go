package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	httpclient "github.com/arandu-io/hesape/http/client"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

const (
	defaultTimeout       = 15 * time.Second
	terminalWriteTimeout = 3 * time.Second
	pruneInterval        = time.Hour
	minimumSecretBytes   = 32
	defaultUserAgent     = "Arandu-Webhook/1.0"
	signatureHeader      = "X-Arandu-Signature"
	timestampHeader      = "X-Arandu-Timestamp"
	deliveryHeader       = "X-Arandu-Delivery-ID"
)

var errRedirectLimit = errors.New("webhook: redirect limit exceeded")

type deliveryJob struct {
	DeliveryID string `json:"deliveryId"`
}

type deliveryQueue interface {
	Push(context.Context, auth.Grant, jobs.Job) error
}

// ManagerOptions configures authorization, routing, transport and retention.
// Empty Action, QueueName, DeliveryJobName and UserAgent values use the
// package defaults.
type ManagerOptions struct {
	Timeout   time.Duration
	Retention time.Duration
	// Action is the grant action persisted on delivery jobs.
	Action auth.Action
	// QueueName is the queue that carries delivery jobs.
	QueueName string
	// DeliveryJobName is the name registered with queue workers.
	DeliveryJobName string
	// UserAgent is sent with every webhook request.
	UserAgent string
	// HTTPClient supplies an application transport. The webhook client still
	// guards schemes, response size and every redirect hop, but a custom
	// transport owns DNS and dialing; nil uses Hesape's SSRF-guarded transport.
	HTTPClient *http.Client
}

// Manager owns durable dispatch and delivery execution.
type Manager struct {
	store     Store
	queue     deliveryQueue
	secrets   SecretProvider
	client    *http.Client
	timeout   time.Duration
	retention time.Duration
	action    auth.Action
	queueName string
	jobName   string
	userAgent string
	pruneMu   sync.Mutex
	lastPrune map[string]time.Time
}

// NewManager creates a manager whose store and DatabaseQueue share db, making
// each delivery snapshot and its job one transaction.
func NewManager(db *database.DB, secrets SecretProvider, options ManagerOptions) (*Manager, error) {
	if db == nil {
		return nil, errors.New("webhook: NewManager needs a database handle")
	}
	resolved, err := resolveManagerOptions(options)
	if err != nil {
		return nil, err
	}
	return newManagerResolved(newDatabaseStore(db, resolved.action), queue.NewDatabaseQueue(db), secrets, resolved)
}

// NewManagerWithStore creates a manager with a caller-owned delivery store and
// the DatabaseQueue owned by the engine. The store's Transaction method must
// use db when dispatch and queue persistence need to commit atomically. Store
// calls receive Grants for the resolved ManagerOptions.Action, whose default is
// ActionDispatch.
func NewManagerWithStore(db *database.DB, store Store, secrets SecretProvider, options ManagerOptions) (*Manager, error) {
	if db == nil {
		return nil, errors.New("webhook: NewManagerWithStore needs a database handle")
	}
	resolved, err := resolveManagerOptions(options)
	if err != nil {
		return nil, err
	}
	return newManagerResolved(store, queue.NewDatabaseQueue(db), secrets, resolved)
}

func newManager(store Store, target deliveryQueue, secrets SecretProvider, options ManagerOptions) (*Manager, error) {
	resolved, err := resolveManagerOptions(options)
	if err != nil {
		return nil, err
	}
	return newManagerResolved(store, target, secrets, resolved)
}

type managerOptions struct {
	timeout   time.Duration
	retention time.Duration
	action    auth.Action
	queueName string
	jobName   string
	userAgent string
	client    *http.Client
}

func resolveManagerOptions(options ManagerOptions) (managerOptions, error) {
	if options.Timeout < 0 || options.Retention < 0 {
		return managerOptions{}, errors.New("webhook: timeout and retention cannot be negative")
	}
	resolved := managerOptions{
		timeout: options.Timeout, retention: options.Retention, action: options.Action,
		queueName: options.QueueName, jobName: options.DeliveryJobName,
		userAgent: options.UserAgent, client: options.HTTPClient,
	}
	if resolved.timeout == 0 {
		resolved.timeout = defaultTimeout
	}
	if resolved.retention == 0 {
		resolved.retention = DefaultRetention
	}
	if resolved.action == "" {
		resolved.action = ActionDispatch
	}
	if resolved.queueName == "" {
		resolved.queueName = QueueName
	}
	if resolved.jobName == "" {
		resolved.jobName = DeliveryJobName
	}
	if resolved.userAgent == "" {
		resolved.userAgent = defaultUserAgent
	}
	for _, option := range []struct {
		label string
		value string
	}{
		{label: "action", value: string(resolved.action)},
		{label: "queue name", value: resolved.queueName},
		{label: "delivery job name", value: resolved.jobName},
	} {
		if !validRoutingName(option.value) {
			return managerOptions{}, fmt.Errorf("webhook: invalid %s %q", option.label, option.value)
		}
	}
	if !validUserAgent(resolved.userAgent) {
		return managerOptions{}, errors.New("webhook: user agent must be non-blank and contain no control characters")
	}
	return resolved, nil
}

func validRoutingName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	separator := false
	for index := 0; index < len(name); index++ {
		character := name[index]
		alphanumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if alphanumeric {
			separator = false
			continue
		}
		if index == 0 || separator || character != '.' && character != '_' && character != '-' && character != ':' {
			return false
		}
		separator = true
	}
	return !separator
}

func validUserAgent(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < ' ' || value[index] == 0x7f {
			return false
		}
	}
	return true
}

func newManagerResolved(store Store, target deliveryQueue, secrets SecretProvider, options managerOptions) (*Manager, error) {
	if store == nil || target == nil {
		return nil, errors.New("webhook: manager needs a store and database queue")
	}
	if secrets == nil {
		return nil, ErrSecretRequired
	}
	client := httpclient.NewFactory(options.client).CreatePendingRequest().
		Timeout(options.timeout).MaxRedirects(5).CreateClient(nil)
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errRedirectLimit
		}
		return nil
	}
	return &Manager{store: store, queue: target, secrets: secrets, client: client,
		timeout: options.timeout, retention: options.retention, action: options.action,
		queueName: options.queueName, jobName: options.jobName, userAgent: options.userAgent,
		lastPrune: map[string]time.Time{}}, nil
}

// Migrations returns the delivery schema owned by the manager.
func (*Manager) Migrations() []migrations.Migration {
	return []migrations.Migration{CreateDeliveriesTable{}}
}

// RegisterJobHandlers registers this manager's configured delivery consumer.
func (m *Manager) RegisterJobHandlers(worker *queue.Worker) error {
	if worker == nil {
		return errors.New("webhook: RegisterJobHandlers needs a worker")
	}
	worker.HandleFunc(m.jobName, m.handleDelivery)
	return nil
}

// Dispatch atomically records and queues one immutable delivery per endpoint.
func (m *Manager) Dispatch(ctx context.Context, g auth.Grant, event Event, endpoints []Endpoint) error {
	if _, err := tenantForAction(g, m.action); err != nil {
		return err
	}
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Name) == "" {
		return errors.New("webhook: event id and name are required")
	}
	if len(event.Payload) > MaxPayloadBytes {
		return ErrPayloadTooLarge
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	seen := map[string]bool{}
	prepared := make([]Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.ID) == "" {
			return errors.New("webhook: endpoint id is required")
		}
		if seen[endpoint.ID] {
			continue
		}
		seen[endpoint.ID] = true
		normalized, err := NormalizeURL(endpoint.URL)
		if err != nil {
			return err
		}
		endpoint.URL = normalized
		prepared = append(prepared, endpoint)
	}
	return m.store.Transaction(ctx, func(txCtx context.Context) error {
		for _, endpoint := range prepared {
			id, err := database.NewID()
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			item := Delivery{ID: id, EventID: event.ID, EventName: event.Name, EndpointID: endpoint.ID,
				URL: endpoint.URL, Headers: cloneHeaders(endpoint.Headers), SecretRef: endpoint.SecretRef,
				Body: append([]byte(nil), event.Payload...), Status: StatusPending, CreatedAt: now, UpdatedAt: now}
			created, err := m.store.Create(txCtx, g, item)
			if err != nil {
				return err
			}
			if !created {
				continue
			}
			job, err := jobs.New(g, m.queueName, m.jobName, deliveryJob{DeliveryID: id})
			if err != nil {
				return err
			}
			job.Attributes.Tries = 5
			job.Attributes.Backoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
			job.Attributes.Timeout = m.timeout + 5*time.Second
			if err := m.queue.Push(txCtx, g, job); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Manager) handleDelivery(ctx context.Context, g auth.Grant, job *jobs.Job) error {
	if _, err := tenantForAction(g, m.action); err != nil {
		return err
	}
	if job == nil {
		return errors.New("webhook: delivery job is nil")
	}
	var input deliveryJob
	if err := job.Decode(&input); err != nil {
		return err
	}
	if input.DeliveryID == "" {
		return errors.New("webhook: delivery job has no delivery id")
	}
	m.prune(ctx, g)
	lease := m.timeout + 10*time.Second
	claim, claimed, err := m.store.Claim(ctx, g, input.DeliveryID, lease)
	if errors.Is(err, ErrDeliveryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !claimed {
		return Failure{Code: "delivery_claimed", RetryAfter: lease}
	}
	item := claim.Delivery
	secrets, err := m.secrets.Secrets(ctx, g, item.SecretRef)
	if err != nil {
		return m.settleFailure(ctx, g, claim, classify(err, 0, ""))
	}
	if len(secrets.Current) == 0 {
		return m.settleFailure(ctx, g, claim, Failure{Code: ErrSecretRequired.Error(), Permanent: true})
	}
	if len(secrets.Current) < minimumSecretBytes {
		return m.settleFailure(ctx, g, claim, Failure{Code: ErrSecretTooShort.Error(), Permanent: true})
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URL, bytes.NewReader(item.Body))
	if err != nil {
		return m.settleFailure(ctx, g, claim, classify(err, 0, ""))
	}
	for name, value := range item.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", m.userAgent)
	request.Header.Set(deliveryHeader, item.ID)
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	request.Header.Set(timestampHeader, timestamp)
	request.Header.Set(signatureHeader, Sign(secrets.Current, timestamp, item.ID, item.Body))
	response, err := m.client.Do(request)
	if err != nil {
		return m.settleFailure(ctx, g, claim, classify(err, 0, ""))
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return m.settleFailure(ctx, g, claim, classify(err, response.StatusCode, ""))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return m.settleFailure(ctx, g, claim, classify(nil, response.StatusCode, response.Header.Get("Retry-After")))
	}
	return m.store.Complete(ctx, g, claim, Result{StatusCode: response.StatusCode, FinishedAt: time.Now().UTC()})
}

func (m *Manager) settleFailure(ctx context.Context, g auth.Grant, claim Claim, failure Failure) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
	defer cancel()
	if err := m.store.Fail(writeCtx, g, claim, failure); err != nil {
		return errors.Join(failure, err)
	}
	if failure.Permanent {
		return nil
	}
	return failure
}

func classify(cause error, status int, retryHeader string) Failure {
	if errors.Is(cause, httpclient.ErrInternalAddress) {
		return Failure{Code: "unsafe_destination", Permanent: true}
	}
	if errors.Is(cause, httpclient.ErrResponseTooLarge) {
		return Failure{Code: "response_too_large", Permanent: true}
	}
	if errors.Is(cause, httpclient.ErrUnsupportedScheme) {
		return Failure{Code: "unsupported_scheme", Permanent: true}
	}
	if errors.Is(cause, errRedirectLimit) {
		return Failure{Code: "too_many_redirects", Permanent: true}
	}
	if cause != nil {
		code := "transport_failure"
		if errors.Is(cause, context.DeadlineExceeded) {
			code = "request_timeout"
		}
		if errors.Is(cause, context.Canceled) {
			code = "request_canceled"
		}
		return Failure{Code: code}
	}
	failure := Failure{Code: fmt.Sprintf("http_status_%d", status), StatusCode: status}
	retryable := status == http.StatusRequestTimeout || status == 425 || status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError && status <= 599
	failure.Permanent = !retryable
	if !failure.Permanent {
		failure.RetryAfter = parseRetryAfter(retryHeader, time.Now().UTC())
	}
	return failure
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func (m *Manager) prune(ctx context.Context, g auth.Grant) {
	if m.retention <= 0 {
		return
	}
	tenant := auth.Tenant(g)
	now := time.Now().UTC()
	m.pruneMu.Lock()
	last := m.lastPrune[tenant]
	if !last.IsZero() && now.Sub(last) < pruneInterval {
		m.pruneMu.Unlock()
		return
	}
	m.lastPrune[tenant] = now
	m.pruneMu.Unlock()
	if _, err := m.store.Prune(ctx, g, now.Add(-m.retention)); err != nil {
		m.pruneMu.Lock()
		delete(m.lastPrune, tenant)
		m.pruneMu.Unlock()
	}
}

func cloneHeaders(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
