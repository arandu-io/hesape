package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

const (
	managerTimeout       = 15 * time.Second
	terminalWriteTimeout = 3 * time.Second
	pruneInterval        = time.Hour
)

// The headers a delivery of Manager carries: the engine's Arandu dialect,
// under its default prefix.
const (
	signatureHeader = aranduPrefix + "-Signature"
	timestampHeader = aranduPrefix + "-Timestamp"
	deliveryHeader  = aranduPrefix + "-Delivery-ID"
)

// managerBackoff is the wait before each retry of a delivery of Manager,
// which the queue makes: 5 attempts in all.
var managerBackoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

type deliveryJob struct {
	DeliveryID string `json:"deliveryId"`
}

type deliveryQueue interface {
	Push(context.Context, auth.Grant, jobs.Job) error
}

// ManagerOptions configures authorization, routing, transport and retention.
// Empty Action, QueueName, DeliveryJobName and UserAgent values use the
// package defaults.
//
// Deprecated: configure the Module with Config, and its deliveries with Policy.
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
	// transport owns DNS and dialing; nil uses the guarded transport, which
	// refuses loopback, private, link-local and shared (100.64.0.0/10)
	// addresses on every hop.
	HTTPClient *http.Client
}

// Manager owns durable dispatch and delivery execution.
//
// Deprecated: use New, and send through Module.Send, Module.Outgoing or
// Module.Publisher. Manager remains for the applications whose deliveries are
// still in the webhook_deliveries table.
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
	// engine makes the attempts, under policy, in the Arandu dialect.
	engine *engine
	policy Policy
	now    func() time.Time
}

// NewManager creates a manager whose store and DatabaseQueue share db, making
// each delivery snapshot and its job one transaction.
//
// Deprecated: use New.
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
//
// Deprecated: use New.
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
		resolved.timeout = managerTimeout
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
		resolved.userAgent = DefaultUserAgent
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
	m := &Manager{store: store, queue: target, secrets: secrets,
		timeout: options.timeout, retention: options.retention, action: options.action,
		queueName: options.queueName, jobName: options.jobName, userAgent: options.userAgent,
		lastPrune: map[string]time.Time{}, now: time.Now}
	// The policy says what Manager always did: five attempts, the redirects
	// and the answers it retries, Retry-After honored. The queue makes the
	// retries, so Tries and Backoff are what Dispatch gives the job.
	m.policy = Policy{
		Tries: 5, Backoff: managerBackoff, Retry: RetryTransient, HonorRetryAfter: true, MaxRedirects: 5,
		MaxRequestBytes: MaxPayloadBytes, MaxResponseBytes: MaxResponseBytes, Timeout: options.timeout,
		UserAgent: options.userAgent,
	}
	engine, err := newEngine(options.client, aranduPrefix, func() time.Time { return m.now() })
	if err != nil {
		return nil, err
	}
	m.engine, m.client = engine, engine.clientFor(m.policy)
	return m, nil
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
func (m *Manager) Dispatch(ctx context.Context, g auth.Grant, event Event, endpoints []ManagerEndpoint) error {
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
	prepared := make([]ManagerEndpoint, 0, len(endpoints))
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
			item := ManagerDelivery{ID: id, EventID: event.ID, EventName: event.Name, EndpointID: endpoint.ID,
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

// handleDelivery is the job: the engine's attempt of a delivery the Store
// keeps.
func (m *Manager) handleDelivery(ctx context.Context, g auth.Grant, job *jobs.Job) error {
	return m.engine.run(ctx, g, storeLedger{m: m}, job)
}

func (m *Manager) clock() time.Time { return m.now().UTC() }

// storeLedger is the deliveries of Manager: a Store, and the queue that
// retries them. A failed attempt that another may fix is answered to the
// queue as the Failure it was, and the queue makes the next attempt after the
// job's backoff, or after the Retry-After the receiver asked for.
type storeLedger struct{ m *Manager }

// claim takes the delivery the job names with a lease long enough for one
// attempt, after pruning the tenant's old deliveries at most once an hour.
// A delivery somebody else holds is answered to the queue as
// delivery_claimed, to be tried again when the lease ends.
func (l storeLedger) claim(ctx context.Context, g auth.Grant, job *jobs.Job) (*take, error) {
	m := l.m
	if _, err := tenantForAction(g, m.action); err != nil {
		return nil, err
	}
	if job == nil {
		return nil, errors.New("webhook: delivery job is nil")
	}
	var input deliveryJob
	if err := job.Decode(&input); err != nil {
		return nil, err
	}
	if input.DeliveryID == "" {
		return nil, errors.New("webhook: delivery job has no delivery id")
	}
	m.prune(ctx, g)
	lease := m.timeout + 10*time.Second
	claim, claimed, err := m.store.Claim(ctx, g, input.DeliveryID, lease)
	if errors.Is(err, ErrDeliveryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, Failure{Code: "delivery_claimed", RetryAfter: lease}
	}

	t := &take{policy: m.policy, client: m.client, own: claim}
	item := claim.Delivery
	secrets, err := m.secrets.Secrets(ctx, g, item.SecretRef)
	switch {
	case err != nil:
		refused := classify(err, 0, "")
		t.refused = &refused
	case len(secrets.Current) == 0:
		t.refused = &verdict{Code: ErrSecretRequired.Error(), Permanent: true}
	case len(secrets.Current) < MinSecretBytes:
		t.refused = &verdict{Code: ErrSecretTooShort.Error(), Permanent: true}
	default:
		t.shot = shot{
			id: item.ID, url: item.URL, dialect: DialectArandu, body: item.Body, headers: item.Headers,
			keys: signing{secrets: []string{string(secrets.Current)}},
		}
	}
	return t, nil
}

// settle completes the claim, or fails it with what the attempt came to.
func (l storeLedger) settle(ctx context.Context, g auth.Grant, t *take, r result) error {
	claim := t.own.(Claim)
	if r.delivered() {
		return l.m.store.Complete(ctx, g, claim, Result{StatusCode: r.StatusCode, FinishedAt: l.m.clock()})
	}
	return l.m.settleFailure(ctx, g, claim, Failure{
		Code: r.Code, StatusCode: r.StatusCode, Permanent: r.Permanent, RetryAfter: r.RetryAfter,
	})
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
