package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
)

const (
	// ActionDispatch authorizes durable webhook dispatch and delivery access.
	ActionDispatch auth.Action = "webhook.dispatch"
	// QueueName is the default queue carrying webhook delivery jobs.
	QueueName = "webhooks"
	// DeliveryJobName is the stable worker handler name.
	DeliveryJobName = "webhook.deliver"
	// DefaultRetention is how long delivery audit records remain.
	DefaultRetention = 30 * 24 * time.Hour
	// MaxURLLength bounds a persisted endpoint URL.
	MaxURLLength = 2048
	// MaxPayloadBytes bounds an immutable request snapshot.
	MaxPayloadBytes = 1 << 20
)

var (
	// ErrInvalidURL means an endpoint URL is not an absolute HTTP(S) URL.
	ErrInvalidURL = errors.New("webhook: invalid endpoint URL")
	// ErrPayloadTooLarge means an event exceeds MaxPayloadBytes.
	ErrPayloadTooLarge = errors.New("webhook: event payload is too large")
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

// Endpoint is one stable callback destination.
type Endpoint struct {
	ID        string
	URL       string
	Headers   map[string]string
	SecretRef string
}

// Status is the durable state of a delivery.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
)

// Delivery is the immutable request and mutable audit state for one endpoint.
type Delivery struct {
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
type Claim struct {
	Delivery Delivery
	Token    string
	Version  int64
	LeaseEnd time.Time
}

// Result is a successful delivery result.
type Result struct {
	StatusCode int
	FinishedAt time.Time
}

// Failure classifies an unsuccessful attempt for persistence and queue retry.
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
type Store interface {
	Transaction(context.Context, func(context.Context) error) error
	Create(context.Context, auth.Grant, Delivery) (created bool, err error)
	Find(context.Context, auth.Grant, string) (Delivery, error)
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
