package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
)

const (
	// ActionDispatch authorizes durable webhook dispatch and delivery access.
	//
	// Deprecated: use DeliveryDispatch, which has the same value, so a Grant
	// issued for either is one Grant.
	ActionDispatch auth.Action = "webhook.dispatch"
	// QueueName is the default queue carrying webhook delivery jobs.
	//
	// Deprecated: use DefaultQueue, which has the same value.
	QueueName = "webhooks"
	// DeliveryJobName is the stable worker handler name.
	DeliveryJobName = "webhook.deliver"
	// MaxURLLength bounds a persisted endpoint URL.
	MaxURLLength = 2048
	// MaxPayloadBytes bounds an immutable request snapshot.
	MaxPayloadBytes = 1 << 20
)

var (
	// ErrInvalidURL means an endpoint URL is not an absolute HTTP(S) URL.
	ErrInvalidURL = errors.New("webhook: invalid endpoint URL")
	// ErrSecretRequired means an endpoint has no signing secret.
	ErrSecretRequired = errors.New("webhook: a signing secret is required")
	// ErrSecretTooShort means a signing secret contains fewer than 32 bytes.
	ErrSecretTooShort = errors.New("webhook: a signing secret must contain at least 32 bytes")
	// ErrDeliveryNotFound means no delivery exists under the tenant and id.
	ErrDeliveryNotFound = errors.New("webhook: delivery not found")
	// ErrStaleClaim means another worker owns a newer claim.
	ErrStaleClaim = errors.New("webhook: delivery claim is stale")
)

// Event is one immutable fact to send. ID is stable across every endpoint.
type Event struct {
	ID          string
	Name        string
	Aggregate   string
	AggregateID string
	Payload     []byte
	OccurredAt  time.Time
}

// ManagerEndpoint is one stable callback destination of Manager.Dispatch.
//
// Deprecated: store an Endpoint through the Module and send to it with
// Module.Send, or build a call with Module.Outgoing.
type ManagerEndpoint struct {
	ID        string
	URL       string
	Headers   map[string]string
	SecretRef string
}

// Status is the durable state of a delivery Manager keeps.
//
// Deprecated: a Delivery of the Module carries its state as one of
// DeliveryPending, DeliveryProcessing, DeliveryDelivered and DeliveryFailed.
type Status string

// The states of a delivery Manager keeps.
//
// Deprecated: use DeliveryPending, DeliveryProcessing, DeliveryDelivered and
// DeliveryFailed, which have the same values.
const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
)

// ManagerDelivery is the immutable request and mutable audit state for one
// endpoint, as Manager and its Store keep it.
//
// Deprecated: the Module records a Delivery of its own, with its attempts.
type ManagerDelivery struct {
	ID             string
	TenantID       string
	EventID        string
	EventName      string
	EndpointID     string
	URL            string
	Headers        map[string]string
	SecretRef      string
	Body           []byte
	Status         Status
	Attempts       int
	Permanent      bool
	ResponseStatus int
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeliveredAt    time.Time
	LeaseUntil     time.Time
}

// Claim identifies one fenced attempt. Token and Version must both match when
// the result is settled.
//
// Deprecated: it belongs to Store, which the Module does not use.
type Claim struct {
	Delivery ManagerDelivery
	Token    string
	Version  int64
	LeaseEnd time.Time
}

// Result is a successful delivery result.
//
// Deprecated: it belongs to Store, which the Module does not use.
type Result struct {
	StatusCode int
	FinishedAt time.Time
}

// Failure classifies an unsuccessful attempt for persistence and queue retry.
//
// Deprecated: it belongs to Store, which the Module does not use; a
// DeliveryAttempt records what each attempt of the Module came to.
type Failure struct {
	Code       string
	StatusCode int
	Permanent  bool
	RetryAfter time.Duration
}

// Error returns the stable failure code without leaking a destination.
func (f Failure) Error() string { return f.Code }

// RetryAfterDuration tells the queue how long the remote endpoint asked to wait.
func (f Failure) RetryAfterDuration() time.Duration { return f.RetryAfter }

// Store owns durable delivery state and its fenced transitions.
//
// Deprecated: the Module keeps its deliveries in its own tables. Store remains
// for Manager, and Manager for the applications that still dispatch through
// it.
type Store interface {
	Transaction(context.Context, func(context.Context) error) error
	Create(context.Context, auth.Grant, ManagerDelivery) (created bool, err error)
	Find(context.Context, auth.Grant, string) (ManagerDelivery, error)
	Claim(context.Context, auth.Grant, string, time.Duration) (Claim, bool, error)
	Complete(context.Context, auth.Grant, Claim, Result) error
	Fail(context.Context, auth.Grant, Claim, Failure) error
	Prune(context.Context, auth.Grant, time.Time) (int64, error)
}

// SecretSet holds the active signing secret and older verification secrets.
type SecretSet struct {
	Current  []byte
	Previous [][]byte
}

// SecretProvider resolves an endpoint's signing secrets at delivery time.
type SecretProvider interface {
	Secrets(context.Context, auth.Grant, string) (SecretSet, error)
}

// StaticSecret is a fixed secret provider suitable for one application-wide key.
type StaticSecret struct{ set SecretSet }

// NewStaticSecret returns a provider with an active secret and optional older
// secrets accepted during rotation.
func NewStaticSecret(current []byte, previous ...[]byte) StaticSecret {
	set := SecretSet{Current: append([]byte(nil), current...)}
	for _, secret := range previous {
		set.Previous = append(set.Previous, append([]byte(nil), secret...))
	}
	return StaticSecret{set: set}
}

// Secrets returns defensive copies of the configured secrets.
func (s StaticSecret) Secrets(context.Context, auth.Grant, string) (SecretSet, error) {
	return NewStaticSecret(s.set.Current, s.set.Previous...).set, nil
}
