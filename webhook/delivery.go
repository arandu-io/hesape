package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	httpclient "github.com/arandu-io/hesape/http/client"
	"github.com/arandu-io/hesape/queue/jobs"
)

// DeliverJob is the queued job that makes one attempt of a delivery.
const DeliverJob = "webhook.server.deliver"

// The infrastructure retries of an attempt's job: a database that did not
// answer, an attempt still holding its lease. A failed attempt is not one of
// them; the delivery's own plan schedules the next.
const (
	jobTries      = 10
	leaseHeadroom = 10 * time.Second
	maxErrorRunes = 500
)

var jobBackoff = []time.Duration{5 * time.Second, 30 * time.Second, time.Minute}

// The failure codes an attempt records. An HTTP answer outside 2xx is
// "http_status_" and its status.
const (
	FailureEndpointDeleted   = "endpoint_deleted"
	FailureSecretUnavailable = "secret_unavailable"
	FailureUnsupportedScheme = "unsupported_scheme"
	FailureRequestTooLarge   = "request_too_large"
	FailureUnsafeDestination = "unsafe_destination"
	FailureTooManyRedirects  = "too_many_redirects"
	FailureTimeout           = "request_timeout"
	FailureTransport         = "transport_failure"
	// FailureResponseTooLarge is an answer that announced more than the
	// client reads at all, which retrying does not shrink.
	FailureResponseTooLarge = "response_too_large"
	// FailureCanceled is an attempt whose context was canceled before it had
	// an answer, such as a worker stopping; another attempt may succeed.
	FailureCanceled = "request_canceled"
)

var errRedirectLimit = errors.New("webhook: redirect limit exceeded")

// errClaimLost is a settlement whose claim another attempt took over.
var errClaimLost = errors.New("webhook: the attempt's claim was taken over")

// ledger is where the deliveries the engine attempts are kept. The engine
// owns the attempt -- the request, its signature, the transport and what the
// answer means -- and a ledger owns everything around it: which delivery a job
// names, the claim that lets one worker attempt it, and what is written, and
// what the job answers the queue, once the attempt is over.
//
// There are two: nativeLedger, the Module's deliveries and their attempts,
// each retry scheduled by the delivery's own plan; and storeLedger, a Store of
// Manager, whose retries are the queue's.
type ledger interface {
	// claim takes the delivery the job names for one attempt. A nil take with
	// a nil error is a job with nothing left to attempt; an error is what the
	// job answers the queue.
	claim(ctx context.Context, g auth.Grant, j *jobs.Job) (*take, error)
	// settle writes what the attempt came to and answers what the job returns
	// to the queue.
	settle(ctx context.Context, g auth.Grant, t *take, r result) error
}

// take is one attempt a ledger hands the engine.
type take struct {
	// shot is the request to make.
	shot shot
	// policy is what the attempt is held to.
	policy Policy
	// client carries the request; nil builds it from the policy.
	client *http.Client
	// refused is a failure the ledger found before any request: nothing is
	// sent, and the attempt comes to it.
	refused *verdict
	// own is what the ledger needs back to settle the attempt.
	own any
}

// shot is the one request an attempt makes.
type shot struct {
	// id is the delivery's identity, the same on every attempt.
	id      string
	url     string
	dialect string
	// body is sent exactly as it was recorded.
	body []byte
	// headers are the extra headers, set before the dialect's own.
	headers map[string]string
	// keys are the secrets still signing, the current one first.
	keys            signing
	signatureHeader string
	aranduHeaders   bool
}

// verdict is what an attempt that did not deliver came to. Code is empty for
// an attempt that delivered. StatusCode is the answer's status, zero when
// there was none.
type verdict struct {
	Code       string
	StatusCode int
	Permanent  bool
	RetryAfter time.Duration
}

// result is what one attempt came to.
type result struct {
	verdict
	signedAt time.Time
	response string
	duration time.Duration
}

func (r result) delivered() bool { return r.Code == "" }

// engine makes the attempts: one request, signed in its dialect, on the
// guarded client, and what the answer means. It is the one way a delivery is
// sent, whichever ledger keeps it.
type engine struct {
	// client is the guarded client every attempt's client is a copy of.
	client *http.Client
	// prefix is the Arandu dialect's header prefix.
	prefix string
	now    func() time.Time
}

// newEngine builds the engine over the client the application gave, or the
// guarded one when custom is nil.
func newEngine(custom *http.Client, prefix string, now func() time.Time) (*engine, error) {
	client, err := newDeliveryClient(custom)
	if err != nil {
		return nil, err
	}
	return &engine{client: client, prefix: prefix, now: now}, nil
}

// run is one job: the ledger's claim, the attempt, the ledger's settlement.
func (e *engine) run(ctx context.Context, g auth.Grant, l ledger, j *jobs.Job) error {
	t, err := l.claim(ctx, g, j)
	if err != nil || t == nil {
		return err
	}
	return l.settle(ctx, g, t, e.attempt(ctx, t))
}

// clientFor is the client an attempt under the policy goes out on: the
// guarded client with no timeout of its own -- the attempt's context carries
// it -- following as many redirects as the policy allows, each to https when
// the policy says so.
func (e *engine) clientFor(policy Policy) *http.Client {
	client := *e.client
	client.Timeout = 0
	if policy.MaxRedirects == 0 {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return &client
	}
	limit, httpsOnly := policy.MaxRedirects, policy.HTTPSOnly
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= limit {
			return errRedirectLimit
		}
		if httpsOnly && next.URL.Scheme != "https" {
			return httpclient.ErrUnsupportedScheme
		}
		return nil
	}
	return &client
}

// attempt makes the take's request: the body as it was recorded, the extra
// headers, the dialect's signature under the secrets signing now.
func (e *engine) attempt(ctx context.Context, t *take) result {
	policy := t.policy
	r := result{signedAt: e.now().UTC().Truncate(time.Second)}
	settle := func(v verdict) result {
		if !policy.HonorRetryAfter {
			v.RetryAfter = 0
		}
		if policy.Retry == RetryEveryFailure && v.Code != "" && v.Code != FailureEndpointDeleted {
			v.Permanent = false
		}
		r.verdict = v
		return r
	}
	if t.refused != nil {
		return settle(*t.refused)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, policy.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, t.shot.url, bytes.NewReader(t.shot.body))
	if err != nil {
		return settle(verdict{Code: FailureUnsupportedScheme, Permanent: true})
	}
	for name, value := range t.shot.headers {
		request.Header.Set(name, value)
	}
	if err := sign(request.Header, t.shot, r.signedAt, e.prefix, policy.UserAgent); err != nil {
		return settle(verdict{Code: FailureSecretUnavailable, Permanent: true})
	}

	client := t.client
	if client == nil {
		client = e.clientFor(policy)
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		r.duration = time.Since(started)
		return settle(classify(err, 0, ""))
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, policy.MaxResponseBytes))
	_ = response.Body.Close()
	r.duration = time.Since(started)
	r.response = excerpt(raw)
	if readErr != nil {
		v := classify(readErr, 0, "")
		v.StatusCode = response.StatusCode
		return settle(v)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return settle(verdict{StatusCode: response.StatusCode})
	}
	return settle(classify(nil, response.StatusCode, response.Header.Get("Retry-After")))
}

// classify is what an attempt that did not deliver came to: cause is the
// error that left it with no answer, or nil when it had one, and then status
// is that answer's and retryAfter its Retry-After header.
//
// Another attempt may fix a transport error, a timeout, a canceled attempt,
// 408, 425, 429 and the 5xx; it cannot fix an address that is refused, a
// scheme that is not http or https, too many redirects, an answer larger than
// the client reads, or any other status, so those are permanent.
func classify(cause error, status int, retryAfter string) verdict {
	if cause != nil {
		switch {
		case errors.Is(cause, httpclient.ErrInternalAddress):
			return verdict{Code: FailureUnsafeDestination, Permanent: true}
		case errors.Is(cause, httpclient.ErrResponseTooLarge):
			return verdict{Code: FailureResponseTooLarge, Permanent: true}
		case errors.Is(cause, httpclient.ErrUnsupportedScheme):
			return verdict{Code: FailureUnsupportedScheme, Permanent: true}
		case errors.Is(cause, errRedirectLimit):
			return verdict{Code: FailureTooManyRedirects, Permanent: true}
		case errors.Is(cause, context.Canceled):
			return verdict{Code: FailureCanceled}
		case errors.Is(cause, context.DeadlineExceeded):
			return verdict{Code: FailureTimeout}
		}
		return verdict{Code: FailureTransport}
	}
	v := verdict{Code: "http_status_" + strconv.Itoa(status), StatusCode: status}
	retryable := status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError && status <= 599
	v.Permanent = !retryable
	if retryable {
		v.RetryAfter = parseRetryAfter(retryAfter, time.Now())
	}
	return v
}

// parseRetryAfter reads a Retry-After header, in seconds or as an HTTP date,
// as the wait it asks for from now. Anything else asks for none.
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

// excerpt is the start of an answer, as one line of valid text.
func excerpt(raw []byte) string {
	text := strings.ToValidUTF8(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return ' '
		}
		return r
	}, string(raw)), "")
	if utf8.RuneCountInString(text) <= maxErrorRunes {
		return text
	}
	return string([]rune(text)[:maxErrorRunes])
}

// deliverMessage is the job's payload: the delivery, how many attempts it
// had when this one was scheduled, and what scheduled it. A job whose count
// no longer matches the delivery is stale and does nothing, so a redelivery
// and the attempt it overtook never both send.
type deliverMessage struct {
	DeliveryID string `json:"delivery_id"`
	Attempts   int64  `json:"attempts"`
	Trigger    string `json:"trigger"`
}

// waitError asks the queue to run the job again after a while, through the
// RetryAfterDuration the worker reads.
type waitError struct {
	reason string
	wait   time.Duration
}

func (e waitError) Error() string                     { return "webhook: " + e.reason }
func (e waitError) RetryAfterDuration() time.Duration { return e.wait }

// schedule queues the attempt after the given count, due at the given time,
// under the dispatch Grant of the tenant.
func (s *sender) schedule(ctx context.Context, tenant, deliveryID string, attempts int64, trigger string, at time.Time, timeout time.Duration) error {
	if trigger == "" {
		trigger = AttemptScheduled
	}
	//arandu:system-grant the delivery was recorded in this tenant under DeliveryDispatch, by the application or after a policy authorized the act; its attempts run under the same action.
	g := auth.SystemGrant(DeliveryDispatch, tenant)
	job, err := jobs.New(g, s.queueName, DeliverJob, deliverMessage{DeliveryID: deliveryID, Attempts: attempts, Trigger: trigger})
	if err != nil {
		return err
	}
	job.RunAt = at
	job.Attributes.Tries = jobTries
	job.Attributes.Backoff = jobBackoff
	job.Attributes.Timeout = timeout + 2*leaseHeadroom
	return s.queue.Push(ctx, g, job)
}

// handle is the job: the engine's attempt of a delivery of the Module.
func (s *sender) handle(ctx context.Context, g auth.Grant, j *jobs.Job) error {
	return s.engine.run(ctx, g, nativeLedger{s: s}, j)
}

// nativeLedger is the Module's deliveries: webhook_server_deliveries, the
// attempts beside them, and the endpoints they go to. A delivery carries its
// own plan, and a failed attempt that has one left schedules the next.
type nativeLedger struct{ s *sender }

// nativeTake is what a native attempt is settled with.
type nativeTake struct {
	delivery *Delivery
	endpoint *Endpoint
	token    string
	n        int64
	trigger  string
}

// claim takes the delivery the job names, fenced by the attempt count the job
// carries, so a job overtaken by another does nothing.
func (l nativeLedger) claim(ctx context.Context, g auth.Grant, j *jobs.Job) (*take, error) {
	s := l.s
	if err := g.Check(DeliveryDispatch); err != nil {
		return nil, err
	}
	var msg deliverMessage
	if err := j.Decode(&msg); err != nil {
		return nil, err
	}
	row, err := Deliveries(s.db).WhereKey(msg.DeliveryID).First(ctx, g)
	if err != nil || row == nil {
		return nil, err // nil: cleaned since, nothing left to send
	}
	var endpoint *Endpoint
	if strings.HasPrefix(row.SecretRef, endpointSecretRef) {
		if endpoint, err = Endpoints(s.db).WhereKey(row.EndpointID).First(ctx, g); err != nil {
			return nil, err
		}
	}
	policy := s.policy.clone()
	if endpoint != nil {
		policy = s.policyOf(endpoint)
	}

	now := s.clock()
	token, err := database.NewID()
	if err != nil {
		return nil, err
	}
	claimed, err := Deliveries(s.db).WhereKey(row.ID).Where(func(q *DeliveryQuery) {
		q.Where(func(due *DeliveryQuery) {
			due.Where("status", "=", DeliveryPending).Where("attempts", "=", msg.Attempts).Where("next_attempt_at", "<=", now)
		}).OrWhere(func(abandoned *DeliveryQuery) {
			abandoned.Where("status", "=", DeliveryProcessing).Where("attempts", "=", msg.Attempts+1).Where("lease_until", "<=", now)
		})
	}).Update(ctx, g, map[string]any{
		"status": DeliveryProcessing, "claim_token": token, "lease_until": now.Add(policy.Timeout + leaseHeadroom),
		"attempts": msg.Attempts + 1,
	})
	if err != nil {
		return nil, err
	}
	if claimed == 0 {
		return nil, s.unclaimed(ctx, g, row.ID, msg, now)
	}
	if row, err = Deliveries(s.db).WhereKey(row.ID).First(ctx, g); err != nil || row == nil {
		return nil, err
	}

	// An endpoint turned off keeps its deliveries waiting: the attempt is
	// given back, and turning the endpoint on queues them again. A delivery
	// somebody asked for goes whatever the endpoint's state.
	if endpoint != nil && !endpoint.Active && msg.Trigger != AttemptManual {
		_, err := Deliveries(s.db).WhereKey(row.ID).Where("claim_token", "=", token).Update(ctx, g, map[string]any{
			"status": DeliveryPending, "attempts": msg.Attempts, "claim_token": nil, "lease_until": nil,
		})
		return nil, err
	}

	t := &take{policy: policy, own: nativeTake{delivery: row, endpoint: endpoint, token: token, n: msg.Attempts + 1, trigger: msg.Trigger}}
	t.shot, t.refused = s.prepare(row, endpoint, policy, now)
	return t, nil
}

// settle writes the attempt, and swallows a settlement another attempt took
// over: that one writes its own.
func (l nativeLedger) settle(ctx context.Context, g auth.Grant, t *take, r result) error {
	own := t.own.(nativeTake)
	err := l.s.settle(ctx, g, own.delivery, own.endpoint, t.policy, own.token, own.n, own.trigger, r)
	if errors.Is(err, errClaimLost) {
		return nil
	}
	return err
}

// prepare is the request a claimed delivery makes, or the failure it comes to
// before one: its endpoint deleted, an address the policy refuses, a body the
// policy refuses, or secrets and headers that cannot be opened.
func (s *sender) prepare(d *Delivery, endpoint *Endpoint, policy Policy, now time.Time) (shot, *verdict) {
	refuse := func(code string) (shot, *verdict) { return shot{}, &verdict{Code: code, Permanent: true} }
	if strings.HasPrefix(d.SecretRef, endpointSecretRef) && endpoint == nil {
		return refuse(FailureEndpointDeleted)
	}
	if policy.HTTPSOnly && !strings.HasPrefix(strings.ToLower(d.URL), "https://") {
		return refuse(FailureUnsupportedScheme)
	}
	if int64(len(d.Body)) > policy.MaxRequestBytes {
		return refuse(FailureRequestTooLarge)
	}
	keys, err := s.signingFor(d, endpoint, now.Truncate(time.Second))
	if err != nil {
		return refuse(FailureSecretUnavailable)
	}
	extra := map[string]string{}
	if d.Headers != "" {
		plain, err := s.sealer.open(d.TenantID, d.ID, "delivery-headers", d.Headers)
		if err != nil || jsonInto(plain, &extra) != nil {
			return refuse(FailureSecretUnavailable)
		}
	}
	return shot{
		id: d.ID, url: d.URL, dialect: d.Dialect, body: []byte(d.Body), headers: extra, keys: keys,
		signatureHeader: d.SignatureHeader, aranduHeaders: bool(d.AranduHeaders),
	}, nil
}

// unclaimed answers a job that found its delivery not claimable: an attempt
// still in progress, or a delivery not due yet, is asked for again later;
// anything else is a job overtaken by another, which does nothing.
func (s *sender) unclaimed(ctx context.Context, g auth.Grant, id string, msg deliverMessage, now time.Time) error {
	row, err := Deliveries(s.db).WhereKey(id).First(ctx, g)
	if err != nil || row == nil {
		return err
	}
	switch {
	case row.Status == DeliveryProcessing && row.Attempts == msg.Attempts+1 && row.LeaseUntil != nil && row.LeaseUntil.After(now):
		return waitError{reason: "the delivery's attempt is still in progress", wait: row.LeaseUntil.Sub(now)}
	case row.Status == DeliveryPending && row.Attempts == msg.Attempts && row.NextAttemptAt != nil && row.NextAttemptAt.After(now):
		return waitError{reason: "the delivery's attempt is not due yet", wait: row.NextAttemptAt.Sub(now)}
	}
	return nil
}

// settle writes what the attempt came to, in one transaction: the delivery,
// fenced by the claim; its attempt; its next attempt queued when it has one;
// and the endpoint's count of deliveries in a row that gave up.
func (s *sender) settle(ctx context.Context, g auth.Grant, d *Delivery, endpoint *Endpoint, policy Policy, token string, n int64, trigger string, r result) error {
	now := s.clock()
	return database.Transaction(ctx, s.db, func(ctx context.Context) error {
		values := map[string]any{"claim_token": nil, "lease_until": nil, "response_status": nil}
		if r.StatusCode != 0 {
			values["response_status"] = r.StatusCode
		}
		var next time.Time
		gaveUp := false
		switch {
		case r.delivered():
			values["status"], values["delivered_at"], values["last_error"] = DeliveryDelivered, now, nil
			values["next_attempt_at"], values["permanent"] = nil, Flag(false)
		case !r.Permanent && n < d.MaxAttempts:
			wait := d.WaitAfter(n)
			if policy.HonorRetryAfter && r.RetryAfter > wait {
				wait = r.RetryAfter
			}
			next = now.Add(wait)
			values["status"], values["next_attempt_at"], values["last_error"] = DeliveryPending, next, r.Code
		default:
			gaveUp = true
			values["status"], values["next_attempt_at"], values["last_error"] = DeliveryFailed, nil, r.Code
			values["permanent"] = Flag(r.Permanent)
		}
		settled, err := Deliveries(s.db).WhereKey(d.ID).Where("claim_token", "=", token).Update(ctx, g, values)
		if err != nil {
			return err
		}
		if settled == 0 {
			return errClaimLost
		}
		if err := s.recordAttempt(ctx, g, d, n, trigger, r, now); err != nil {
			return err
		}
		if !next.IsZero() {
			if err := s.schedule(ctx, auth.Tenant(g), d.ID, n, AttemptScheduled, next, policy.Timeout); err != nil {
				return err
			}
		}
		if endpoint == nil {
			return nil
		}
		return s.settleEndpoint(ctx, g, endpoint, policy, r.delivered(), gaveUp && trigger != AttemptManual && r.Code != FailureEndpointDeleted, now)
	})
}

// recordAttempt writes the attempt's history row.
func (s *sender) recordAttempt(ctx context.Context, g auth.Grant, d *Delivery, n int64, trigger string, r result, now time.Time) error {
	row, err := DeliveryAttempts(s.db).New()
	if err != nil {
		return err
	}
	if row.ID, err = database.NewID(); err != nil {
		return err
	}
	row.TenantID = auth.Tenant(g)
	row.DeliveryID, row.EndpointID, row.Attempt, row.Trigger = d.ID, d.EndpointID, n, trigger
	row.Succeeded, row.Error, row.Response = Flag(r.delivered()), r.Code, r.response
	if r.StatusCode != 0 {
		status := int64(r.StatusCode)
		row.ResponseStatus = &status
	}
	row.DurationMS, row.SignedAt, row.CreatedAt = r.duration.Milliseconds(), r.signedAt, now
	_, err = row.Save(ctx, g)
	return err
}

// settleEndpoint moves the endpoint's count of deliveries in a row that gave
// up, atomically, and turns it off once the policy's DisableAfter is
// reached. The turning off is recorded like any other act on it.
func (s *sender) settleEndpoint(ctx context.Context, g auth.Grant, endpoint *Endpoint, policy Policy, delivered, gaveUp bool, now time.Time) error {
	switch {
	case delivered:
		_, err := Endpoints(s.db).WhereKey(endpoint.ID).Where("consecutive_failures", ">", 0).Update(ctx, g, map[string]any{"consecutive_failures": 0})
		return err
	case !gaveUp:
		return nil
	}
	if _, err := Endpoints(s.db).WhereKey(endpoint.ID).Increment(ctx, g, "consecutive_failures", 1, nil); err != nil {
		return err
	}
	row, err := Endpoints(s.db).WhereKey(endpoint.ID).First(ctx, g)
	if err != nil || row == nil || !row.ShouldDisable(policy.DisableAfter) {
		return err
	}
	row.Disable(DisabledByFailures, now)
	turned, err := Endpoints(s.db).WhereKey(row.ID).Where("active", "=", Flag(true)).Update(ctx, g, map[string]any{
		"active": row.Active, "disabled_at": row.DisabledAt, "disabled_reason": row.DisabledReason,
	})
	if err != nil || turned == 0 {
		return err
	}
	return s.audit.record(ctx, g, EventEndpointDisabled, aggregateEndpoint, row.ID, map[string]any{
		"reason": DisabledByFailures, "consecutive_failures": row.ConsecutiveFailures,
	})
}

// resume queues again the deliveries an endpoint kept waiting while it was
// off.
func (s *sender) resume(ctx context.Context, g auth.Grant, endpoint *Endpoint) error {
	policy := s.policyOf(endpoint)
	now := s.clock()
	return Deliveries(s.db).Where("endpoint_id", "=", endpoint.ID).Where("status", "=", DeliveryPending).OrderBy("id").
		Chunk(ctx, g, 200, func(rows DeliveryCollection, _ int) (bool, error) {
			for _, row := range rows {
				at := now
				if row.NextAttemptAt != nil && row.NextAttemptAt.After(now) {
					at = *row.NextAttemptAt
				}
				if err := s.schedule(ctx, auth.Tenant(g), row.ID, row.Attempts, AttemptScheduled, at, policy.Timeout); err != nil {
					return false, err
				}
			}
			return true, nil
		})
}

// clean removes the deliveries that are settled and older than the cutoff,
// and the attempts older than it, in the Grant's tenant.
func (s *sender) clean(ctx context.Context, g auth.Grant, cutoff time.Time) error {
	if err := g.Check(DeliveryClean); err != nil {
		return err
	}
	if _, err := DeliveryAttempts(s.db).Where("created_at", "<", cutoff).Delete(ctx, g); err != nil {
		return err
	}
	_, err := Deliveries(s.db).Where("created_at", "<", cutoff).
		WhereIn("status", []any{DeliveryDelivered, DeliveryFailed}).Delete(ctx, g)
	return err
}

func jsonInto(text string, v any) error {
	if err := json.Unmarshal([]byte(text), v); err != nil {
		return fmt.Errorf("webhook: the sealed headers are not readable: %w", err)
	}
	return nil
}
