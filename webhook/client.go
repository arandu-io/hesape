package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/arandu-io/hesape/session"
)

// ProcessJob is the queued job that hands a stored call to its receiver's
// Processor.
const ProcessJob = "webhook.process"

// Processor handles a stored call on the queue. g is the Grant of the call's tenant, for whatever the
// processing reads and writes. An error leaves the call failed, with the
// message kept, and the queue tries again until the receiver's Tries are
// spent; nil marks it processed.
type Processor interface {
	Process(ctx context.Context, g auth.Grant, call *Call) error
}

// ProcessorFunc adapts a function to Processor.
type ProcessorFunc func(ctx context.Context, g auth.Grant, call *Call) error

// Process calls f.
func (f ProcessorFunc) Process(ctx context.Context, g auth.Grant, call *Call) error {
	return f(ctx, g, call)
}

// SyncProcessor handles a stored call before the sender is answered, and
// says what to answer. g is the Grant of the call's tenant. An error leaves
// the call failed and answers Statuses.Failed; a result of 400 or above
// leaves it failed too, with the status as its message, and is answered as
// it is.
type SyncProcessor interface {
	ProcessNow(ctx context.Context, g auth.Grant, call *Call) (ProcessResult, error)
}

// SyncProcessorFunc adapts a function to SyncProcessor.
type SyncProcessorFunc func(ctx context.Context, g auth.Grant, call *Call) (ProcessResult, error)

// ProcessNow calls f.
func (f SyncProcessorFunc) ProcessNow(ctx context.Context, g auth.Grant, call *Call) (ProcessResult, error) {
	return f(ctx, g, call)
}

// ProcessResult is the answer a SyncProcessor gives the sender.
type ProcessResult struct {
	// Status is the answer's status. Zero means 200.
	Status int
	// Body is the answer's body, sent as it is; nil sends none.
	Body []byte
	// ContentType is the body's type. Empty means application/json when
	// there is a body.
	ContentType string
}

// Resolution is the record a request to a dynamic receiver is addressed to.
type Resolution struct {
	// Tenant is the record's tenant: whose call this is.
	Tenant string
	// Secret and PreviousSecrets are the record's signing secrets, the
	// previous ones still accepted during a rotation.
	Secret          string
	PreviousSecrets []string
	// Validator checks this record's calls. Nil means the receiver's.
	Validator SignatureValidator
	// Reference is kept on the call as Call.Reference, for the processor to
	// find the record again: an id, never a secret.
	Reference string
}

// Resolver finds the record a request to a dynamic receiver is addressed to.
// It reads the path with r.PathValue and nothing of the body, and answers an
// error when there is no such record, or it is not one that takes calls.
type Resolver interface {
	Resolve(r *http.Request) (Resolution, error)
}

// ResolverFunc adapts a function to Resolver.
type ResolverFunc func(r *http.Request) (Resolution, error)

// Resolve calls f.
func (f ResolverFunc) Resolve(r *http.Request) (Resolution, error) { return f(r) }

// ErrReceiverUnavailable is what a Resolver wraps when it cannot answer now
// -- what it reads from is not wired or not reachable -- rather than when the
// record does not exist. It answers Statuses.Unavailable.
var ErrReceiverUnavailable = errors.New("webhook: the receiver cannot resolve calls right now")

// Profile decides whether a call whose signature checked is stored and
// processed. A call it turns down is
// answered like any other and kept nowhere.
type Profile interface {
	ShouldProcess(r *http.Request, body []byte) bool
}

// ProfileFunc adapts a function to Profile.
type ProfileFunc func(r *http.Request, body []byte) bool

// ShouldProcess calls f.
func (f ProfileFunc) ShouldProcess(r *http.Request, body []byte) bool { return f(r, body) }

// ProcessEverything is the Profile that processes every call.
type ProcessEverything struct{}

// ShouldProcess answers true.
func (ProcessEverything) ShouldProcess(*http.Request, []byte) bool { return true }

// Responder answers the sender once a call is taken. call is nil when the Profile turned the call down.
type Responder interface {
	Respond(w http.ResponseWriter, r *http.Request, call *Call) error
}

// ResponderFunc adapts a function to Responder.
type ResponderFunc func(w http.ResponseWriter, r *http.Request, call *Call) error

// Respond calls f.
func (f ResponderFunc) Respond(w http.ResponseWriter, r *http.Request, call *Call) error {
	return f(w, r, call)
}

// RespondOK is the Responder a receiver has by default: 200 {"message":"ok"}.
type RespondOK struct{}

// Respond writes the answer.
func (RespondOK) Respond(w http.ResponseWriter, _ *http.Request, _ *Call) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"message":"ok"}`))
	return err
}

// The refusals of a receiver. Each answers the sender with the status it
// carries and no detail.
var (
	// ErrInvalidSignature is a call whose signature does not check. It is
	// answered 403: sending
	// the same bytes again cannot make it valid, so a 5xx would only invite
	// the sender to retry.
	ErrInvalidSignature = statusError{status: http.StatusForbidden, text: "webhook: the signature is invalid"}
	// ErrBodyTooLarge is a body above the receiver's MaxBodyBytes.
	ErrBodyTooLarge = statusError{status: http.StatusRequestEntityTooLarge, text: "webhook: the body is too large"}
)

type statusError struct {
	status int
	text   string
}

func (e statusError) Error() string   { return e.text }
func (e statusError) HTTPStatus() int { return e.status }

// processMessage is the queued job's payload.
type processMessage struct {
	CallID string `json:"call_id"`
}

// receiver is one configured Receiver with what it needs at request time.
type receiver struct {
	Receiver
	secrets SecretSet
	pattern string
}

// client is the receiving side.
type client struct {
	db        *database.DB
	audit     *auditor
	queue     *queue.DatabaseQueue
	queueName string
	receivers map[string]*receiver
	now       func() time.Time
}

func newClient(db *database.DB, cfg Config, audit *auditor) *client {
	c := &client{
		db: db, audit: audit, queue: queue.NewDatabaseQueue(db), queueName: cfg.Server.Queue,
		receivers: map[string]*receiver{}, now: func() time.Time { return time.Now().UTC() },
	}
	for _, r := range cfg.Receivers {
		set := SecretSet{Current: []byte(r.Secret)}
		for _, previous := range r.PreviousSecrets {
			set.Previous = append(set.Previous, []byte(previous))
		}
		c.receivers[r.Name] = &receiver{Receiver: r, secrets: set, pattern: r.route(cfg.ReceivePrefix)}
	}
	return c
}

// refusal is a receiver's answer to a call it does not take.
func refusal(status int, text string) error { return statusError{status: status, text: text} }

// addressed is who a call is for and what signs it: the receiver's own, or
// the record Resolve found.
type addressed struct {
	tenant    string
	secrets   SecretSet
	validator SignatureValidator
	reference string
}

// address resolves the record a request is for. A static receiver is its
// own record.
func (c *client) address(r *http.Request, rc *receiver) (addressed, error) {
	if rc.Resolve == nil {
		return addressed{tenant: rc.Tenant, secrets: rc.secrets, validator: rc.Validator}, nil
	}
	found, err := rc.Resolve.Resolve(r)
	switch {
	case errors.Is(err, ErrReceiverUnavailable):
		return addressed{}, refusal(rc.Statuses.Unavailable, "webhook: the receiver is not available")
	case err != nil || !auth.ValidTenant(found.Tenant):
		return addressed{}, refusal(rc.Statuses.Unresolved, "webhook: no record takes this call")
	}
	out := addressed{tenant: found.Tenant, validator: rc.Validator, reference: found.Reference,
		secrets: SecretSet{Current: []byte(found.Secret)}}
	for _, previous := range found.PreviousSecrets {
		out.secrets.Previous = append(out.secrets.Previous, []byte(previous))
	}
	if found.Validator != nil {
		out.validator = found.Validator
	}
	return out, nil
}

// receive answers a call, and a refusal as the receiver's RefusalBody says:
// returned for the router to answer, or written as the status alone.
func (c *client) receive(w http.ResponseWriter, r *http.Request, rc *receiver) error {
	err := c.take(w, r, rc)
	var refused statusError
	if rc.RefusalBody == RefusalEmpty && errors.As(err, &refused) {
		w.WriteHeader(refused.status)
		return nil
	}
	return err
}

// take is the pipeline of one call: the record the call is
// addressed to, the signature, the profile, the stored call and its
// processing, the answer. A validator that reads only headers is asked
// before the body is read, so a call it refuses is refused whatever its body.
func (c *client) take(w http.ResponseWriter, r *http.Request, rc *receiver) error {
	to, err := c.address(r, rc)
	if err != nil {
		return err
	}
	invalid := func() error {
		if rc.OnInvalidSignature != nil {
			rc.OnInvalidSignature(r, rc.Name)
		}
		if rc.Statuses.InvalidSignature == ErrInvalidSignature.status {
			return ErrInvalidSignature
		}
		return refusal(rc.Statuses.InvalidSignature, ErrInvalidSignature.text)
	}
	checked := false
	if headers, ok := to.validator.(headerValidator); ok && headers.readsHeadersOnly() {
		if !to.validator.Valid(r, nil, to.secrets) {
			return invalid()
		}
		checked = true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, rc.MaxBodyBytes+1))
	if err != nil {
		return refusal(rc.Statuses.BadRequest, "webhook: the body could not be read")
	}
	if len(body) == 0 && rc.ReencodeForm && isForm(r) {
		if r.PostForm == nil {
			if err := r.ParseForm(); err != nil {
				return refusal(rc.Statuses.BadRequest, "webhook: the form could not be read")
			}
		}
		body = []byte(r.PostForm.Encode())
	}
	if int64(len(body)) > rc.MaxBodyBytes {
		if rc.Statuses.TooLarge == ErrBodyTooLarge.status {
			return ErrBodyTooLarge
		}
		return refusal(rc.Statuses.TooLarge, ErrBodyTooLarge.text)
	}
	if !checked && !to.validator.Valid(r, body, to.secrets) {
		return invalid()
	}
	if len(body) == 0 && rc.Statuses.EmptyBody != 0 {
		return refusal(rc.Statuses.EmptyBody, "webhook: the call has no body")
	}
	if !rc.Profile.ShouldProcess(r, body) {
		if rc.ProcessSync != nil {
			w.WriteHeader(http.StatusOK)
			return nil
		}
		return rc.Respond.Respond(w, r, nil)
	}
	key := deliveryKey(rc, to.validator, r, body)
	if rc.ProcessSync != nil {
		return c.processNow(w, r, rc, to, body, key)
	}
	call, err := c.store(r.Context(), r, rc, to, body, key, true)
	var repeat repeated
	if errors.As(err, &repeat) {
		// Stored and queued the first time: the sender did not see that
		// answer, and is given it again rather than a second processing.
		return rc.Respond.Respond(w, r, repeat.call)
	}
	if err != nil {
		return err
	}
	return rc.Respond.Respond(w, r, call)
}

// deliveryKey is what tells a repeat of the call from another call: the key
// the receiver's DedupeKey reads, or else the delivery id the validator's
// signature covered, and empty when there is neither. A key wider than the
// column is kept as its SHA-256, which is as unique and fits.
func deliveryKey(rc *receiver, v SignatureValidator, r *http.Request, body []byte) string {
	key := ""
	if rc.DedupeKey != nil {
		key = rc.DedupeKey(r, body)
	} else if id, ok := v.(identifier); ok {
		key = id.deliveryID(r)
	}
	if len(key) > maxDeliveryIDLen || !utf8.ValidString(key) {
		sum := sha256.Sum256([]byte(key))
		key = "sha256:" + hex.EncodeToString(sum[:])
	}
	return key
}

// repeated is a call whose delivery id the receiver already holds, carrying
// the call that holds it.
type repeated struct{ call *Call }

func (repeated) Error() string { return "webhook: this delivery was already received" }

// isForm reports whether the request says its body is a form.
func isForm(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded")
}

// processNow stores the call, runs the receiver's SyncProcessor on it, keeps
// the outcome, and answers what the processor said.
//
// A repeat of a delivery already processed is answered 200 with no body and
// not processed again. A repeat of one whose processing failed -- the sender
// saw the failure and tries again with the same id -- is processed again, by
// whichever copy wins the claim on it. A repeat of one still being processed
// is answered Statuses.Unavailable, which the sender retries: answering 200
// there would report as done a delivery whose processing may yet fail.
func (c *client) processNow(w http.ResponseWriter, r *http.Request, rc *receiver, to addressed, body []byte, key string) error {
	call, err := c.store(r.Context(), r, rc, to, body, key, false)
	var repeat repeated
	if errors.As(err, &repeat) {
		if call, err = c.retake(r.Context(), repeat.call); err != nil {
			return err
		}
		switch {
		case call == nil && repeat.call.Status == CallProcessed:
			w.WriteHeader(http.StatusOK)
			return nil
		case call == nil:
			return refusal(rc.Statuses.Unavailable, "webhook: this delivery is being processed")
		}
	} else if err != nil {
		return err
	}
	result, failure := c.runSync(r.Context(), rc, call)
	if failure != nil {
		return refusal(rc.Statuses.Failed, "webhook: the call could not be processed")
	}
	status := result.Status
	if status == 0 {
		status = http.StatusOK
	}
	if result.Body != nil {
		kind := result.ContentType
		if kind == "" {
			kind = "application/json"
		}
		w.Header().Set("Content-Type", kind)
	}
	w.WriteHeader(status)
	if result.Body != nil {
		_, err = w.Write(result.Body)
	}
	return err
}

// runSync runs a SyncProcessor on a stored call and keeps its outcome on the
// call. The error is the processing's, or the call's own save.
func (c *client) runSync(ctx context.Context, rc *receiver, call *Call) (ProcessResult, error) {
	//arandu:system-grant the call was stored in the tenant its receiver or its resolved record names; its processing runs there.
	g := auth.SystemGrant(CallReceive, call.TenantID)
	call.Attempts++
	result, failure := rc.ProcessSync.ProcessNow(ctx, g, call)
	switch {
	case failure != nil:
		call.Failed(failure, c.now())
	case result.Status >= http.StatusBadRequest:
		call.Failed(fmt.Errorf("webhook: the processor answered %d", result.Status), c.now())
	default:
		call.Processed(c.now())
	}
	if _, err := call.Save(ctx, g); err != nil {
		return result, errors.Join(failure, err)
	}
	return result, failure
}

// retake claims a stored call whose synchronous processing failed, so a
// repeat of its delivery processes it again, and answers nil when there is
// nothing to claim: the call was processed, or is being processed, or another
// repeat claimed it first. The claim is one conditional update, so of two
// repeats that arrive together exactly one processes the call.
func (c *client) retake(ctx context.Context, call *Call) (*Call, error) {
	if call.Status != CallFailed {
		return nil, nil
	}
	//arandu:system-grant the call was stored in this tenant by a request whose signature checked; a repeat of the same signed delivery claims it there.
	g := auth.SystemGrant(CallReceive, call.TenantID)
	claimed, err := Calls(c.db).WhereKey(call.ID).Where("status", "=", CallFailed).
		Update(ctx, g, map[string]any{"status": CallReceived, "updated_at": c.now()})
	if err != nil || claimed != 1 {
		return nil, err
	}
	call.Status = CallReceived
	return call, nil
}

// store keeps the call and, when queued, its processing, in one
// transaction: a call is never stored without its job, nor queued without
// its row.
//
// A call with a delivery key its receiver already holds for the tenant is not
// stored again: the unique index refuses the second row, whichever of two
// copies arrived first, and store answers repeated with the call that holds
// the key.
func (c *client) store(ctx context.Context, r *http.Request, rc *receiver, to addressed, body []byte, key string, queued bool) (*Call, error) {
	//arandu:system-grant the signature checked against the secret the application configured for this receiver, or for the record its Resolve found, and the tenant is that receiver's or that record's; nothing in the request chose it.
	g := auth.SystemGrant(CallReceive, to.tenant)
	call, err := Calls(c.db).New()
	if err != nil {
		return nil, err
	}
	if call.ID, err = database.NewID(); err != nil {
		return nil, err
	}
	now := c.now()
	call.TenantID, call.Name, call.Method, call.Reference = auth.Tenant(g), rc.Name, r.Method, to.reference
	call.URL, call.HeaderData, call.Payload = keptURL(r, rc), keptHeaders(r.Header, rc.StoreHeaders, secretHeadersOf(to.validator)), string(body)
	received := r.Header.Clone()
	call.RemoteAddr, call.HeadersSHA256, call.received = clip(r.RemoteAddr, 255), HeadersDigest(r.Header), &received
	call.Status, call.ProcessedAt, call.CreatedAt, call.UpdatedAt = CallReceived, now, now, now
	if key != "" {
		call.DeliveryID = &key
	}
	err = database.Transaction(ctx, c.db, func(ctx context.Context) error {
		if _, err := call.Save(ctx, g); err != nil {
			return err
		}
		if !queued {
			return nil
		}
		return c.enqueue(ctx, rc, call)
	})
	if key != "" && errors.Is(err, database.ErrUniqueViolation) {
		held, readErr := Calls(c.db).Where("name", "=", rc.Name).Where("delivery_id", "=", key).First(ctx, g)
		if readErr != nil {
			return nil, errors.Join(err, readErr)
		}
		if held != nil {
			return nil, repeated{call: held}
		}
	}
	if err != nil {
		return nil, err
	}
	return call, nil
}

// enqueue queues the processing of a stored call.
func (c *client) enqueue(ctx context.Context, rc *receiver, call *Call) error {
	//arandu:system-grant the job runs in the call's own tenant, which the receiver's configuration named when the call was stored.
	g := auth.SystemGrant(CallReceive, call.TenantID)
	job, err := jobs.New(g, c.queueName, ProcessJob, processMessage{CallID: call.ID})
	if err != nil {
		return err
	}
	job.Attributes.Tries = rc.Tries
	job.Attributes.Backoff = backoff(rc.Tries)
	return c.queue.Push(ctx, g, job)
}

// backoff is the waits between the processings of a call: 10, 100, 1000 and
// 10000 seconds, then a day.
func backoff(tries int) []time.Duration {
	out := make([]time.Duration, 0, tries)
	wait := 10 * time.Second
	for i := 1; i < tries; i++ {
		if i > 4 {
			out = append(out, 24*time.Hour)
			continue
		}
		out = append(out, wait)
		wait *= 10
	}
	return out
}

// handle is the job: the call, its receiver's Processor, the outcome kept.
func (c *client) handle(ctx context.Context, g auth.Grant, j *jobs.Job) error {
	if err := g.Check(CallReceive); err != nil {
		return err
	}
	var msg processMessage
	if err := j.Decode(&msg); err != nil {
		return err
	}
	call, err := Calls(c.db).WhereKey(msg.CallID).First(ctx, g)
	if err != nil {
		return err
	}
	if call == nil {
		return nil // cleaned since: nothing left to process
	}
	rc, ok := c.receivers[call.Name]
	if !ok {
		return errors.New("webhook: no receiver named " + call.Name + " is configured any more")
	}
	if rc.Process == nil {
		// A call of a synchronous receiver, sent to its processor again.
		_, failure := c.runSync(ctx, rc, call)
		return failure
	}
	call.Attempts++
	failure := rc.Process.Process(ctx, g, call)
	if failure != nil {
		call.Failed(failure, c.now())
	} else {
		call.Processed(c.now())
	}
	if _, err := call.Save(ctx, g); err != nil {
		return errors.Join(failure, err)
	}
	return failure
}

// keptURL is the address the call arrived at, as the receiver keeps it, with
// the value of a credential in its query redacted.
func keptURL(r *http.Request, rc *receiver) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	switch rc.URL {
	case URLNone:
		return ""
	case URLRoute:
		return clip(scheme+"://"+r.Host+rc.pattern, MaxURLLength)
	}
	arrived := *r.URL
	if arrived.RawQuery != "" {
		arrived.RawQuery = redactQuery(arrived.RawQuery)
	}
	return clip(scheme+"://"+r.Host+arrived.RequestURI(), MaxURLLength)
}

// credentialParams are query parameters that carry a credential in a callback
// address although session.IsSecretField, which reads form fields, does not
// call them secret: ?key=, ?sig=, ?signature=.
var credentialParams = map[string]bool{"key": true, "sig": true, "signature": true}

// redactQuery is a raw query with the value of every credential parameter
// redacted -- a name session.IsSecretField calls secret, such as token,
// access_token or api_key, or one of credentialParams -- and every other pair
// kept as it arrived, in its order.
func redactQuery(raw string) string {
	pairs := strings.Split(raw, "&")
	for i, pair := range pairs {
		name, _, _ := strings.Cut(pair, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		if name != "" && (credentialParams[strings.ToLower(name)] || session.IsSecretField(name)) {
			pairs[i] = pair[:strings.IndexByte(pair+"=", '=')] + "=" + redacted
		}
	}
	return strings.Join(pairs, "&")
}

// clip cuts a text to a column's width.
func clip(text string, width int) string {
	if len(text) > width {
		return text[:width]
	}
	return text
}

// secretHeadersOf is the headers a validator reads its secret from, which a
// stored call keeps redacted.
func secretHeadersOf(v SignatureValidator) []string {
	if reader, ok := v.(interface{ secretHeaders() []string }); ok {
		return reader.secretHeaders()
	}
	return nil
}
