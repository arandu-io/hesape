package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/queue"
)

// The prefixes of a delivery's secret reference: a stored endpoint's own
// secrets, or one of Config.Server.Secrets by name.
const (
	endpointSecretRef = "endpoint:"
	namedSecretRef    = "secret:"
)

// maxFanout is how many endpoints one event reaches in one tenant: the first
// this many subscribed to it, by id, however many active endpoints are not.
const maxFanout = 500

// fanoutPage is how many active endpoints one read takes while the publisher
// looks for the ones subscribed to an event.
const fanoutPage = 500

// DefaultEvent is the event name of an Outgoing call that names none.
const DefaultEvent = "webhook.call"

// TestEvent is the event the test button sends.
const TestEvent = "webhook.test"

// The refusals of an Outgoing call.
var (
	// ErrURLNotSet is a call with no address.
	ErrURLNotSet = errors.New("webhook: the call has no URL")
	// ErrSecretNotSet is a call with no secret to sign it with.
	ErrSecretNotSet = errors.New("webhook: the call has no secret: name one of Config.Server.Secrets with UseSecret")
	// ErrUnknownSecret is a call naming a secret the configuration does not
	// have.
	ErrUnknownSecret = errors.New("webhook: no secret of that name in Config.Server.Secrets")
	// ErrReservedHeader is an extra header the delivery writes itself.
	ErrReservedHeader = errors.New("webhook: the header is written by the delivery itself")
)

// The refusals of a delivery asked for in code.
var (
	// ErrInvalidMessage is a message with no event, an id that cannot be
	// one, or a body that is not text.
	ErrInvalidMessage = errors.New("webhook: a message needs an event name, a body of UTF-8 text, and ids of letters, digits, '_', '.', ':' and '-'")
	// ErrPayloadTooLarge is a body above the policy's MaxRequestBytes, or an
	// event Manager.Dispatch is handed above MaxPayloadBytes.
	ErrPayloadTooLarge = errors.New("webhook: the payload is too large")
	// ErrHTTPSRequired is an http address under a policy that is HTTPSOnly.
	ErrHTTPSRequired = errors.New("webhook: the policy sends to https addresses only")
	// ErrDeliveryIDTaken is a delivery id already used for another event or
	// endpoint.
	ErrDeliveryIDTaken = statusError{status: http.StatusConflict, text: "webhook: the delivery id is already used by another delivery"}
)

// idPattern is what an id a caller chooses may be made of.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)

// sender is the sending side: the deliveries this module records, the jobs
// that make their attempts, and the endpoints they go to.
type sender struct {
	db         *database.DB
	queue      *queue.DatabaseQueue
	queueName  string
	sealer     *sealer
	audit      *auditor
	named      map[string]string
	allowed    map[string]bool
	allowEmpty bool
	custom     string
	policy     Policy
	engine     *engine
	now        func() time.Time
	retention  time.Duration
}

func newSender(db *database.DB, cfg Config, seal *sealer, audit *auditor) (*sender, error) {
	s := &sender{
		db: db, queue: queue.NewDatabaseQueue(db), queueName: cfg.Server.Queue, sealer: seal, audit: audit,
		named: map[string]string{}, custom: cfg.Server.HeaderPrefix,
		policy: cfg.Server.Policy.clone(), now: cfg.Server.Now, retention: cfg.Server.Retention,
		allowEmpty: cfg.Server.AllowEmptySubscription,
	}
	prefix := aranduPrefix
	if s.custom != "" {
		prefix = s.custom
	}
	engine, err := newEngine(cfg.Server.HTTPClient, prefix, s.clock)
	if err != nil {
		return nil, err
	}
	s.engine = engine
	for name, secret := range cfg.Server.Secrets {
		s.named[name] = secret
	}
	if len(cfg.Server.Events) > 0 {
		s.allowed = map[string]bool{}
		for _, name := range cfg.Server.Events {
			s.allowed[name] = true
		}
	}
	return s, nil
}

func (s *sender) clock() time.Time { return s.now().UTC() }

// admits reports whether the outbox may send an event of this name to any
// endpoint: every name when Config.Server.Events is empty, and only the names
// in it otherwise. It is asked before an endpoint's subscription, so neither
// AllEvents nor a row naming an event outside the list reaches past it.
func (s *sender) admits(event string) bool { return s.allowed == nil || s.allowed[event] }

// policyOf is the policy an endpoint's deliveries are made under: its own,
// or the server's.
func (s *sender) policyOf(e *Endpoint) Policy {
	if e != nil && e.Policy != "" {
		if own, err := decodePolicy(e.Policy); err == nil && own != nil && own.Validate() == nil {
			return *own
		}
	}
	return s.policy.clone()
}

// reserved reports whether an extra header is one a delivery writes.
func (s *sender) reserved(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "content-type", "content-length", "user-agent", "host", "transfer-encoding",
		"webhook-id", "webhook-timestamp", "webhook-signature":
		return true
	}
	if strings.HasPrefix(lower, "x-arandu-") {
		return true
	}
	return s.custom != "" && strings.HasPrefix(lower, strings.ToLower(s.custom)+"-")
}

// outgoing is one delivery about to be recorded.
type outgoing struct {
	id, eventID, event       string
	endpointID, url, dialect string
	body                     []byte
	headers                  map[string]string
	secretRef                string
	signatureHeader          string
	aranduHeaders            bool
	policy                   Policy
	trigger                  string
}

// target is a delivery to a stored endpoint, with its headers opened.
func (s *sender) target(row *Endpoint) (outgoing, error) {
	headers := map[string]string{}
	if row.Headers != "" {
		plain, err := s.sealer.open(row.TenantID, row.ID, "headers", row.Headers)
		if err != nil {
			return outgoing{}, err
		}
		if err := json.Unmarshal([]byte(plain), &headers); err != nil {
			return outgoing{}, err
		}
	}
	return outgoing{
		endpointID: row.ID, url: row.URL, dialect: row.Dialect, headers: headers,
		secretRef: endpointSecretRef + row.ID, signatureHeader: row.SignatureHeader,
		aranduHeaders: bool(row.AranduHeaders), policy: s.policyOf(row), trigger: AttemptScheduled,
	}, nil
}

// newDeliveryID is a delivery id the module makes: msg_ and a new id for a
// Standard Webhooks endpoint, which is how its ids are written, and a new id
// for the others.
func newDeliveryID(dialect string) (string, error) {
	id, err := database.NewID()
	if err != nil {
		return "", err
	}
	if dialect == DialectStandardWebhooks {
		return "msg_" + id, nil
	}
	return id, nil
}

// record writes the deliveries and queues their first attempts, in one
// transaction. One event reaches one endpoint once: a delivery already
// recorded for the same event and endpoint is answered instead of a second.
func (s *sender) record(ctx context.Context, g auth.Grant, items []outgoing) ([]*Delivery, error) {
	if err := g.Check(DeliveryDispatch); err != nil {
		return nil, err
	}
	tenant := auth.Tenant(g)
	if !auth.ValidTenant(tenant) {
		return nil, fmt.Errorf("%w: the Grant carries no valid tenant", auth.ErrForbidden)
	}
	for i := range items {
		item := &items[i]
		if item.id == "" {
			id, err := newDeliveryID(item.dialect)
			if err != nil {
				return nil, err
			}
			item.id = id
		}
		if item.eventID == "" {
			item.eventID = item.id
		}
		if !validID(item.id, maxDeliveryIDLen) || !validID(item.eventID, maxDeliveryIDLen) ||
			strings.TrimSpace(item.event) == "" || len(item.event) > 255 || !utf8.Valid(item.body) {
			return nil, ErrInvalidMessage
		}
		address, err := NormalizeURL(item.url)
		if err != nil {
			return nil, err
		}
		if item.policy.HTTPSOnly && !strings.HasPrefix(address, "https://") {
			return nil, ErrHTTPSRequired
		}
		if int64(len(item.body)) > item.policy.MaxRequestBytes {
			return nil, ErrPayloadTooLarge
		}
		item.url = address
	}
	var out []*Delivery
	err := database.Transaction(ctx, s.db, func(ctx context.Context) error {
		for _, item := range items {
			row, err := s.insert(ctx, g, item)
			if err != nil {
				return err
			}
			out = append(out, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// insert writes one delivery unless the event already reached the endpoint,
// and queues its first attempt when it is new. Two that race past the first
// read both queue an attempt, and the claim lets one of them go out.
func (s *sender) insert(ctx context.Context, g auth.Grant, item outgoing) (*Delivery, error) {
	existing, err := Deliveries(s.db).Where("event_id", "=", item.eventID).Where("endpoint_id", "=", item.endpointID).First(ctx, g)
	if err != nil || existing != nil {
		return existing, err
	}
	now := s.clock()
	headers := ""
	if len(item.headers) > 0 {
		encoded, err := json.Marshal(item.headers)
		if err != nil {
			return nil, err
		}
		if headers, err = s.sealer.seal(auth.Tenant(g), item.id, "delivery-headers", string(encoded)); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(item.body)
	if _, err := Deliveries(s.db).InsertOrIgnore(ctx, g, map[string]any{
		"id": item.id, "tenant_id": auth.Tenant(g), "event_id": item.eventID, "event_name": item.event,
		"endpoint_id": item.endpointID, "url": item.url, "dialect": item.dialect,
		"body": string(item.body), "body_sha256": hex.EncodeToString(sum[:]), "headers": headers,
		"secret_ref": item.secretRef, "signature_header": item.signatureHeader,
		"include_arandu_headers": Flag(item.aranduHeaders), "status": DeliveryPending,
		"attempts": 0, "max_attempts": item.policy.Tries, "backoff": encodeWaits(item.policy.plan()),
		"permanent": Flag(false), "next_attempt_at": now, "created_at": now, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	row, err := Deliveries(s.db).Where("event_id", "=", item.eventID).Where("endpoint_id", "=", item.endpointID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ID != item.id {
		// Another delivery holds the id, or a concurrent one recorded the
		// event first: either way this one was not written.
		if row == nil {
			return nil, ErrDeliveryIDTaken
		}
		return row, nil
	}
	if err := s.schedule(ctx, auth.Tenant(g), row.ID, 0, item.trigger, now, item.policy.Timeout); err != nil {
		return nil, err
	}
	return row, nil
}

func validID(id string, max int) bool { return len(id) <= max && idPattern.MatchString(id) }

// Message is one delivery asked for in code, to a stored endpoint.
type Message struct {
	// ID is the delivery's id: the Delivery-Id of the Arandu dialect and the
	// Webhook-Id of Standard Webhooks, the same on every attempt and every
	// redelivery. Up to 128 letters, digits, '_', '.', ':' and '-'. Empty
	// makes one. One id is one delivery: a message to two endpoints takes
	// two ids.
	ID string
	// EventID is what the delivery is for; a second message with the same
	// EventID to the same endpoint is answered with the first delivery.
	// Empty means ID.
	EventID string
	// Event is the event's name.
	Event string
	// Body is sent exactly as it is, on every attempt. It is UTF-8 text,
	// JSON for every dialect this module speaks.
	Body []byte
}

// send records one message to one of the tenant's endpoints, whatever it
// subscribes to.
func (s *sender) send(ctx context.Context, g auth.Grant, endpointID string, msg Message) (*Delivery, error) {
	if err := g.Check(DeliveryDispatch); err != nil {
		return nil, err
	}
	row, err := Endpoints(s.db).WhereKey(endpointID).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	item, err := s.target(row)
	if err != nil {
		return nil, err
	}
	item.id, item.eventID, item.event, item.body = msg.ID, msg.EventID, msg.Event, append([]byte(nil), msg.Body...)
	rows, err := s.record(ctx, g, []outgoing{item})
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

// publisher is the events outbox's listener: each event goes to the
// tenant's active endpoints subscribed to it.
type publisher struct{ s *sender }

var _ events.Publisher = publisher{}

// Publish records one delivery per endpoint subscribed to the event, keeping
// the event's id, so the relay publishing it again records nothing new. The
// module's own management events are not delivered, and neither is an event
// outside Config.Server.Events when that list is set.
func (p publisher) Publish(ctx context.Context, stored events.Stored) error {
	if auditEvent(stored.Name) || !p.s.admits(stored.Name) {
		return nil
	}
	//arandu:system-grant the event was committed to the outbox in this tenant by the application; its deliveries go to that tenant's own endpoints.
	g := auth.SystemGrant(DeliveryDispatch, stored.TenantID)
	if !auth.ValidTenant(stored.TenantID) {
		return fmt.Errorf("%w: the event carries no valid tenant", auth.ErrForbidden)
	}
	rows, err := subscribed(func(after string) ([]*Endpoint, error) {
		query := Endpoints(p.s.db).Where("active", "=", Flag(true))
		if after != "" {
			query = query.Where("id", ">", after)
		}
		return query.OrderBy("id").Limit(fanoutPage).Get(ctx, g)
	}, stored.Name)
	if err != nil {
		return err
	}
	var items []outgoing
	for _, row := range rows {
		item, err := p.s.target(row)
		if err != nil {
			return err
		}
		item.eventID, item.event, item.body = stored.ID, stored.Name, []byte(stored.Payload)
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	_, err = p.s.record(ctx, g, items)
	return err
}

// signingFor is what an attempt signs with, read when it is made: the
// endpoint's current secret and the ones still signing after a rotation,
// newest first, or the named secret of Server.Secrets.
func (s *sender) signingFor(d *Delivery, endpoint *Endpoint, now time.Time) (signing, error) {
	switch {
	case strings.HasPrefix(d.SecretRef, namedSecretRef):
		secret, ok := s.named[strings.TrimPrefix(d.SecretRef, namedSecretRef)]
		if !ok {
			return signing{}, ErrUnknownSecret
		}
		return signing{secrets: []string{secret}}, nil
	case strings.HasPrefix(d.SecretRef, endpointSecretRef):
		if endpoint == nil {
			return signing{}, ErrNotFound
		}
		current, err := s.sealer.open(endpoint.TenantID, endpoint.ID, "secret", endpoint.Secret)
		if err != nil {
			return signing{}, err
		}
		previous, err := s.sealer.openPrevious(endpoint.TenantID, endpoint.ID, endpoint.PreviousSecrets)
		if err != nil {
			return signing{}, err
		}
		out := signing{secrets: []string{current}}
		for _, p := range stillSigning(previous, now) {
			out.secrets = append(out.secrets, p.Secret)
		}
		return out, nil
	}
	return signing{}, fmt.Errorf("webhook: %q is not a secret reference of this module", d.SecretRef)
}

// stillSigning is the previous secrets that have not retired at now, the
// latest to retire first.
func stillSigning(previous []PreviousSecret, now time.Time) []PreviousSecret {
	var out []PreviousSecret
	for _, p := range previous {
		if now.Before(p.RetiresAt) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RetiresAt.After(out[j].RetiresAt) })
	return out
}

// Outgoing is one call built in code:
//
//	err := webhooks.Outgoing().
//		URL("https://example.com/hooks").
//		Event("order.paid").
//		Payload(map[string]any{"order": id}).
//		UseSecret("partners").
//		Dispatch(ctx, g)
//
// Dispatch records the delivery and its queued attempt in one transaction;
// the worker sends it, signed, under Server.Policy. g carries
// DeliveryDispatch for the tenant the call belongs to: the application
// decided to send, and that decision is the Grant.
type Outgoing struct {
	s        *sender
	url      string
	event    string
	id       string
	body     []byte
	secret   string
	headers  map[string]string
	hmac     string
	noArandu bool
	err      error
}

// URL sets the address.
func (o *Outgoing) URL(u string) *Outgoing { o.url = u; return o }

// Event names the call. Empty means DefaultEvent.
func (o *Outgoing) Event(name string) *Outgoing { o.event = name; return o }

// UUID sets the call's identity. A call dispatched twice with one identity to
// one address is delivered once. Empty means a new one.
func (o *Outgoing) UUID(id string) *Outgoing { o.id = id; return o }

// Payload sets the body to v, encoded as JSON.
func (o *Outgoing) Payload(v any) *Outgoing {
	body, err := json.Marshal(v)
	if err != nil {
		o.err = fmt.Errorf("webhook: the payload is not JSON: %w", err)
		return o
	}
	o.body = body
	return o
}

// RawBody sets the body as it is. It is
// sent as application/json.
func (o *Outgoing) RawBody(body []byte) *Outgoing {
	o.body = append([]byte(nil), body...)
	return o
}

// UseSecret names the secret of Config.Server.Secrets the call is signed
// with.
func (o *Outgoing) UseSecret(name string) *Outgoing { o.secret = name; return o }

// WithHeaders adds extra headers.
func (o *Outgoing) WithHeaders(headers map[string]string) *Outgoing {
	if o.headers == nil {
		o.headers = map[string]string{}
	}
	for name, value := range headers {
		o.headers[name] = value
	}
	return o
}

// SignWithHMAC adds the DialectHMAC signature -- the hex
// HMAC-SHA256 of the body -- in the header named, beside the Arandu one.
// Empty means DefaultSignatureHeader.
func (o *Outgoing) SignWithHMAC(header string) *Outgoing {
	if header == "" {
		header = DefaultSignatureHeader
	}
	o.hmac = header
	return o
}

// WithoutAranduHeaders sends the HMAC signature SignWithHMAC adds alone,
// without the Arandu signature, timestamp and delivery id headers.
func (o *Outgoing) WithoutAranduHeaders() *Outgoing { o.noArandu = true; return o }

// DispatchIf dispatches when cond holds.
func (o *Outgoing) DispatchIf(ctx context.Context, cond bool, g auth.Grant) error {
	if !cond {
		return nil
	}
	return o.Dispatch(ctx, g)
}

// DispatchUnless dispatches unless cond holds.
func (o *Outgoing) DispatchUnless(ctx context.Context, cond bool, g auth.Grant) error {
	return o.DispatchIf(ctx, !cond, g)
}

// Dispatch records the call and queues its delivery.
func (o *Outgoing) Dispatch(ctx context.Context, g auth.Grant) error {
	if o.err != nil {
		return o.err
	}
	if strings.TrimSpace(o.url) == "" {
		return ErrURLNotSet
	}
	address, err := NormalizeURL(o.url)
	if err != nil {
		return err
	}
	if o.secret == "" {
		return ErrSecretNotSet
	}
	if _, ok := o.s.named[o.secret]; !ok {
		return ErrUnknownSecret
	}
	if o.noArandu && o.hmac == "" {
		return errors.New("webhook: WithoutAranduHeaders leaves a call unsigned unless SignWithHMAC names a header")
	}
	if len(o.headers) > MaxHeaders {
		return fmt.Errorf("webhook: a call carries at most %d extra headers", MaxHeaders)
	}
	headers := map[string]string{}
	for name, value := range o.headers {
		if o.s.reserved(name) || strings.EqualFold(name, o.hmac) {
			return fmt.Errorf("%w: %s", ErrReservedHeader, name)
		}
		headers[http.CanonicalHeaderKey(name)] = value
	}
	body := o.body
	if body == nil {
		body = []byte("{}")
	}
	event := o.event
	if event == "" {
		event = DefaultEvent
	}
	eventID := o.id
	if eventID == "" {
		if eventID, err = database.NewID(); err != nil {
			return err
		}
	}
	dialect := DialectArandu
	if o.hmac != "" {
		dialect = DialectHMAC
	}
	sum := sha256.Sum256([]byte(address))
	_, err = o.s.record(ctx, g, []outgoing{{
		eventID: eventID, event: event, endpointID: "url:" + hex.EncodeToString(sum[:16]), url: address,
		dialect: dialect, body: body, headers: headers, secretRef: namedSecretRef + o.secret,
		signatureHeader: o.hmac, aranduHeaders: !o.noArandu, policy: o.s.policy.clone(), trigger: AttemptScheduled,
	}})
	return err
}

// subscribed reads the active endpoints a page at a time, by id, through
// page -- which answers the ones after the id it is given, "" for the first --
// and keeps those subscribed to event, up to maxFanout. The subscription is a
// list in a text column, so it is read here rather than in the statement; the
// pages are what keep an endpoint subscribed behind maxFanout others that are
// not from being left out.
func subscribed(page func(after string) ([]*Endpoint, error), event string) ([]*Endpoint, error) {
	var out []*Endpoint
	after := ""
	for {
		rows, err := page(after)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !row.Receives(event) {
				continue
			}
			if out = append(out, row); len(out) == maxFanout {
				return out, nil
			}
		}
		if len(rows) < fanoutPage {
			return out, nil
		}
		after = rows[len(rows)-1].ID
	}
}
