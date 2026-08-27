package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arandu-io/hesape/http/client/events"
	"github.com/arandu-io/hesape/http/client/promises"
	"github.com/arandu-io/hesape/str"
)

// PendingRequest is the fluent builder for an outgoing HTTP request. Every
// method returns the PendingRequest itself so that callers chain: WithToken,
// WithHeader, Timeout, Retry, and finally Get, Post, Put, Patch, Delete,
// Head, or Send.
//
// The zero value is not usable; create one with [Factory.CreatePendingRequest].
type PendingRequest struct {
	factory *Factory

	// pool is set only on a request the pool itself built. It is what makes a
	// verb record instead of send -- see Send.
	pool *Pool

	// Request configuration.
	method  string
	baseURL string

	// Headers that are merged into every request.
	headers http.Header

	// Query parameters merged into the URL.
	query url.Values

	// URL parameters for URI template expansion.
	urlParams map[string]string

	// Body configuration.
	body          io.Reader
	bodyFormat    string // "json", "form", "multipart", or empty
	jsonData      any
	formData      url.Values
	multipartData *multipartBuffer

	// Authentication.
	token      string
	tokenType  string
	basicUser  string
	basicPass  string
	digestUser string
	digestPass string

	// Transport options.
	timeout        time.Duration
	connectTimeout time.Duration
	maxRedirects   int
	verifyTLS      bool
	sink           io.Writer

	// Retry configuration.
	retryTimes int
	retryDelay time.Duration
	retryWhen  func(*http.Response, error) bool
	retryThrow bool

	// Middleware.
	middleware         []func(*http.Request, http.RoundTripper) http.RoundTripper
	requestMiddleware  []func(*http.Request) error
	responseMiddleware []func(*http.Response) error

	// Stubs for this specific request.
	stubCallback StubCallback
	preventStray bool
	allowStray   []string

	// Attributes are arbitrary values attached to the request for later
	// inspection in stubs and assertions.
	attributes map[string]any

	// Async mode: when true, Get/Post/etc. return immediately.
	async bool

	// promise is what an asynchronous send left behind for the caller to
	// wait on.
	promise *promises.LazyPromise

	// beforeSending callbacks run just before the request is sent.
	beforeSending []func(*http.Request) error
	// afterResponse callbacks run after the Response is built.
	afterResponse []func(*Response) error

	// Dump enables request/response dumping to stdout.
	dump bool
	// DumpAndDie enables request/response dumping then exits.
	dumpAndDie bool

	// truncateExceptions is the per-request override of the truncation
	// length: nil defers to the package-level default, a pointer to zero
	// lets the whole body through, and a positive length cuts the body
	// summary.
	truncateExceptions *int
}

type multipartBuffer struct {
	buffer bytes.Buffer
	writer *multipart.Writer
	closed bool
}

func newPendingRequest(f *Factory) *PendingRequest {
	return &PendingRequest{
		factory:        f,
		headers:        http.Header{},
		query:          url.Values{},
		urlParams:      make(map[string]string),
		bodyFormat:     "json",
		timeout:        defaultTimeout,
		connectTimeout: 10 * time.Second,
		maxRedirects:   5,
		verifyTLS:      true,
		retryThrow:     true,
		attributes:     make(map[string]any),
	}
}

// BaseURL sets the base URL. Paths in Get/Post/etc. are resolved against it.
func (p *PendingRequest) BaseURL(u string) *PendingRequest {
	p.baseURL = strings.TrimRight(u, "/")
	return p
}

// WithBody sets the raw request body with a given Content-Type.
func (p *PendingRequest) WithBody(content io.Reader, contentType string) *PendingRequest {
	p.body = content
	p.headers.Set("Content-Type", contentType)
	return p
}

// AsJSON sets the body format to JSON (the default).
func (p *PendingRequest) AsJSON() *PendingRequest {
	p.bodyFormat = "json"
	return p
}

// AsForm sets the body format to application/x-www-form-urlencoded.
func (p *PendingRequest) AsForm() *PendingRequest {
	p.bodyFormat = "form"
	return p
}

// AsMultipart sets the body format to multipart/form-data.
func (p *PendingRequest) AsMultipart() *PendingRequest {
	p.bodyFormat = "multipart"
	return p
}

// Attach adds a file to the multipart form data.
func (p *PendingRequest) Attach(name, contents, filename string, headers map[string]string) *PendingRequest {
	if p.multipartData == nil {
		p.multipartData = newMultipartBuffer()
	}
	// Write as a form field if no filename is given.
	if filename == "" {
		p.multipartData.writer.WriteField(name, contents)
		return p
	}
	part, err := p.multipartData.writer.CreateFormFile(name, filename)
	if err != nil {
		// Best-effort; errors surface when the request is built.
		return p
	}
	part.Write([]byte(contents))
	return p
}

// BodyFormat sets the body format explicitly.
func (p *PendingRequest) BodyFormat(format string) *PendingRequest {
	p.bodyFormat = format
	return p
}

// WithQueryParameters adds query parameters to the request URL.
func (p *PendingRequest) WithQueryParameters(params map[string]string) *PendingRequest {
	for k, v := range params {
		p.query.Set(k, v)
	}
	return p
}

// ContentType sets the Content-Type header.
func (p *PendingRequest) ContentType(ct string) *PendingRequest {
	p.headers.Set("Content-Type", ct)
	return p
}

// AcceptJSON sets the Accept header to application/json.
func (p *PendingRequest) AcceptJSON() *PendingRequest {
	p.headers.Set("Accept", "application/json")
	return p
}

// Accept sets the Accept header.
func (p *PendingRequest) Accept(ct string) *PendingRequest {
	p.headers.Set("Accept", ct)
	return p
}

// WithHeaders merges the given headers into the request headers.
func (p *PendingRequest) WithHeaders(headers map[string]string) *PendingRequest {
	for k, v := range headers {
		p.headers.Set(k, v)
	}
	return p
}

// WithHeader sets a single header.
func (p *PendingRequest) WithHeader(name, value string) *PendingRequest {
	p.headers.Set(name, value)
	return p
}

// ReplaceHeaders replaces all headers.
func (p *PendingRequest) ReplaceHeaders(headers map[string]string) *PendingRequest {
	p.headers = http.Header{}
	for k, v := range headers {
		p.headers.Set(k, v)
	}
	return p
}

// WithBasicAuth sets HTTP Basic authentication.
func (p *PendingRequest) WithBasicAuth(username, password string) *PendingRequest {
	p.basicUser = username
	p.basicPass = password
	return p
}

// WithDigestAuth sets HTTP Digest authentication.
func (p *PendingRequest) WithDigestAuth(username, password string) *PendingRequest {
	p.digestUser = username
	p.digestPass = password
	return p
}

// WithToken sets a Bearer token (or custom token type).
func (p *PendingRequest) WithToken(token string, tokenType string) *PendingRequest {
	if tokenType == "" {
		tokenType = "Bearer"
	}
	p.token = token
	p.tokenType = tokenType
	return p
}

// WithUserAgent sets the User-Agent header.
func (p *PendingRequest) WithUserAgent(ua string) *PendingRequest {
	p.headers.Set("User-Agent", ua)
	return p
}

// WithURLParameters sets URL template parameters.
func (p *PendingRequest) WithURLParameters(params map[string]string) *PendingRequest {
	for k, v := range params {
		p.urlParams[k] = v
	}
	return p
}

// WithCookies sets cookies for the request domain.
func (p *PendingRequest) WithCookies(cookies []*http.Cookie, domain string) *PendingRequest {
	for _, c := range cookies {
		p.headers.Add("Cookie", c.String())
	}
	return p
}

// MaxRedirects sets how many redirects the request follows before it gives
// up.
func (p *PendingRequest) MaxRedirects(max int) *PendingRequest {
	p.maxRedirects = max
	return p
}

// WithoutRedirecting disables automatic redirect following.
func (p *PendingRequest) WithoutRedirecting() *PendingRequest {
	p.maxRedirects = 0
	return p
}

// WithoutVerifying disables TLS certificate verification.
func (p *PendingRequest) WithoutVerifying() *PendingRequest {
	p.verifyTLS = false
	return p
}

// Sink directs the response body to the given writer instead of reading it
// into memory.
func (p *PendingRequest) Sink(w io.Writer) *PendingRequest {
	p.sink = w
	return p
}

// Timeout sets the request timeout.
func (p *PendingRequest) Timeout(d time.Duration) *PendingRequest {
	p.timeout = d
	return p
}

// ConnectTimeout sets the connection timeout.
func (p *PendingRequest) ConnectTimeout(d time.Duration) *PendingRequest {
	p.connectTimeout = d
	return p
}

// Retry sets how many times the request may be attempted.
//
// times is a total and includes the first send: Retry(3) makes at most
// three requests, not four. delay is what is waited between them, and it is
// abandoned as soon as the context passed to the verb is done.
//
// when decides whether a failure is worth repeating and is handed the raw
// response, if one arrived, and the failure itself -- a *RequestException when
// the response failed, or the transport's error when the connection did. A nil
// when repeats every failure, which is the ordinary way to call this.
//
// throw: when more than one attempt was asked for and the last one still
// failed, the failure comes back as an error rather than as a response. It
// is on by default.
func (p *PendingRequest) Retry(times int, delay time.Duration, when func(*http.Response, error) bool, throw bool) *PendingRequest {
	p.retryTimes = times
	p.retryDelay = delay
	p.retryWhen = when
	p.retryThrow = throw
	return p
}

// WithOptions sets arbitrary options on the underlying HTTP client.
func (p *PendingRequest) WithOptions(fn func(*http.Client)) *PendingRequest {
	if p.factory != nil && p.factory.client != nil {
		fn(p.factory.client)
	}
	return p
}

// WithMiddleware adds transport-level middleware.
func (p *PendingRequest) WithMiddleware(mw ...func(*http.Request, http.RoundTripper) http.RoundTripper) *PendingRequest {
	p.middleware = append(p.middleware, mw...)
	return p
}

// WithRequestMiddleware adds request-level middleware (runs before send).
func (p *PendingRequest) WithRequestMiddleware(mw ...func(*http.Request) error) *PendingRequest {
	p.requestMiddleware = append(p.requestMiddleware, mw...)
	return p
}

// WithResponseMiddleware adds response-level middleware (runs after receive).
func (p *PendingRequest) WithResponseMiddleware(mw ...func(*http.Response) error) *PendingRequest {
	p.responseMiddleware = append(p.responseMiddleware, mw...)
	return p
}

// WithAttributes attaches arbitrary attributes to the request.
func (p *PendingRequest) WithAttributes(attrs map[string]any) *PendingRequest {
	for k, v := range attrs {
		p.attributes[k] = v
	}
	return p
}

// BeforeSending registers a callback that runs just before the request is sent.
func (p *PendingRequest) BeforeSending(callback func(*http.Request) error) *PendingRequest {
	p.beforeSending = append(p.beforeSending, callback)
	return p
}

// AfterResponse registers a callback that runs after the Response is built.
func (p *PendingRequest) AfterResponse(callback func(*Response) error) *PendingRequest {
	p.afterResponse = append(p.afterResponse, callback)
	return p
}

// Throw turns a failed response into an error.
//
// The callback is a side effect only, never a substitute for building the
// exception: it used to run in place of returning it, so a callback that
// only logged made the failure vanish instead of surfacing as an error.
func (p *PendingRequest) Throw(callback func(*Response, *RequestException)) *PendingRequest {
	p.afterResponse = append(p.afterResponse, func(r *Response) error {
		if !r.Failed() {
			return nil
		}
		exception := NewRequestException(r, p.truncateExceptions)
		if callback != nil {
			callback(r, exception)
		}
		return exception
	})
	return p
}

// TruncateExceptionsAt cuts the body summary of this request's exceptions
// at the given length.
func (p *PendingRequest) TruncateExceptionsAt(length int) *PendingRequest {
	p.truncateExceptions = &length
	return p
}

// DontTruncateExceptions lets the whole body into this request's exception
// messages.
func (p *PendingRequest) DontTruncateExceptions() *PendingRequest {
	zero := 0
	p.truncateExceptions = &zero
	return p
}

// ThrowIf enables conditional throwing on failed responses.
func (p *PendingRequest) ThrowIf(condition bool) *PendingRequest {
	if condition {
		return p.Throw(nil)
	}
	return p
}

// ThrowUnless enables throwing unless the condition is true.
func (p *PendingRequest) ThrowUnless(condition bool) *PendingRequest {
	return p.ThrowIf(!condition)
}

// Dump enables request/response dumping.
func (p *PendingRequest) Dump() *PendingRequest {
	p.dump = true
	return p
}

// Dd enables request/response dumping and then exits.
func (p *PendingRequest) Dd() *PendingRequest {
	p.dumpAndDie = true
	return p
}

// Async sets whether to send without waiting.
//
// A verb called on an asynchronous request returns (nil, nil) and leaves a
// promise on [PendingRequest.GetPromise]; waiting on that promise is what makes
// the request go out, and what hands back the response.
func (p *PendingRequest) Async(async bool) *PendingRequest {
	p.async = async
	return p
}

// GetPromise is the promise this request left behind, or nil when it was
// not sent asynchronously.
//
// Wait on it for a (*Response, error): the value is the *Response.
func (p *PendingRequest) GetPromise() promises.Promise {
	if p.promise == nil {
		return nil
	}
	return p.promise
}

// StubCallback registers a stub callback for this specific request.
func (p *PendingRequest) Stub(callback StubCallback) *PendingRequest {
	p.stubCallback = callback
	return p
}

// PreventStrayRequests enables stray request prevention for this request.
func (p *PendingRequest) PreventStrayRequests(prevent bool) *PendingRequest {
	p.preventStray = prevent
	return p
}

// AllowStrayRequests allows stray requests for specific URLs.
func (p *PendingRequest) AllowStrayRequests(only []string) *PendingRequest {
	p.allowStray = only
	return p
}

// SetClient sets the underlying HTTP client.
func (p *PendingRequest) SetClient(client *http.Client) *PendingRequest {
	if p.factory == nil {
		p.factory = NewFactory(client)
	} else {
		p.factory.client = client
	}
	return p
}

// GetOptions returns the current option configuration.
func (p *PendingRequest) GetOptions() map[string]any {
	return map[string]any{
		"timeout":         p.timeout,
		"connect_timeout": p.connectTimeout,
		"max_redirects":   p.maxRedirects,
		"verify_tls":      p.verifyTLS,
	}
}

// MergeOptions is this request's options with the given ones laid over
// them, later maps winning.
//
// Nothing in [PendingRequest.GetOptions] nests, so this is a flat merge.
func (p *PendingRequest) MergeOptions(options ...map[string]any) map[string]any {
	merged := p.GetOptions()
	for _, option := range options {
		for key, value := range option {
			merged[key] = value
		}
	}
	return merged
}

// SetHandler sets the transport the request goes out on: the
// http.RoundTripper on the client, which is where a caller puts a
// recording or an offline transport.
//
// It installs a copy rather than writing the transport into the client it was
// handed, so that a client shared with anything else keeps the transport it
// had.
func (p *PendingRequest) SetHandler(handler http.RoundTripper) *PendingRequest {
	client := *p.BuildClient()
	client.Transport = handler
	return p.SetClient(&client)
}

// BuildClient is the client this request will go out on.
//
// A request configured with none answers with the one this package shares,
// which is not a client to change in place: copy it, the way
// [PendingRequest.CreateClient] does.
func (p *PendingRequest) BuildClient() *http.Client {
	if p.factory != nil && p.factory.client != nil {
		return p.factory.client
	}
	return defaultClient
}

// CreateClient is a client configured the way this request needs it,
// without touching the shared one.
//
// It takes the transport for the reason [PendingRequest.SetHandler] gives.
// A nil transport keeps whatever the client already had.
//
// Whatever the transport turns out to be, it is wrapped in the factory's guard:
// the scheme, the destination and the size of the answer are settled on every
// hop, redirects included.
func (p *PendingRequest) CreateClient(handler http.RoundTripper) *http.Client {
	client := *p.BuildClient()
	if handler != nil {
		client.Transport = handler
	}
	if client.Transport == nil {
		client.Transport = defaultTransport
	}
	client.Transport = &guardedTransport{next: client.Transport, guard: p.factory.guard()}
	client.Timeout = p.timeout
	if p.maxRedirects == 0 {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		max := p.maxRedirects
		client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
			if len(via) >= max {
				return fmt.Errorf("http client: stopped after %d redirects", max)
			}
			return nil
		}
	}
	return &client
}

// RunBeforeSendingCallbacks runs what [PendingRequest.BeforeSending]
// registered, in the order it was registered, mutating req in place. It
// returns only what went wrong.
func (p *PendingRequest) RunBeforeSendingCallbacks(req *http.Request) error {
	for _, callback := range p.beforeSending {
		if err := callback(req); err != nil {
			return err
		}
	}
	return nil
}

// IsAllowedRequestUrl reports whether a request to this URL may leave the
// process when no stub matched it.
//
// Every URL is allowed until stray request prevention is on. After that only
// the patterns given to [PendingRequest.AllowStrayRequests] or
// [Factory.AllowStrayRequests] are, matched the way [str.Is] matches: a
// literal string, or one with * standing for any run of characters.
func (p *PendingRequest) IsAllowedRequestUrl(url string) bool {
	preventing := p.preventStray || (p.factory != nil && p.factory.preventStray)
	if !preventing {
		return true
	}
	allowed := p.allowStray
	if p.factory != nil {
		allowed = append(append([]string{}, allowed...), p.factory.allowedStrayURLs...)
	}
	return str.Is(allowed, url, false)
}

// Get sends a GET request.
func (p *PendingRequest) Get(ctx context.Context, urlStr string, query map[string]string) (*Response, error) {
	return p.Send(ctx, "GET", urlStr, query, nil)
}

// Post sends a POST request.
func (p *PendingRequest) Post(ctx context.Context, urlStr string, data any) (*Response, error) {
	return p.Send(ctx, "POST", urlStr, nil, data)
}

// Put sends a PUT request.
func (p *PendingRequest) Put(ctx context.Context, urlStr string, data any) (*Response, error) {
	return p.Send(ctx, "PUT", urlStr, nil, data)
}

// Patch sends a PATCH request.
func (p *PendingRequest) Patch(ctx context.Context, urlStr string, data any) (*Response, error) {
	return p.Send(ctx, "PATCH", urlStr, nil, data)
}

// Delete sends a DELETE request.
func (p *PendingRequest) Delete(ctx context.Context, urlStr string, data any) (*Response, error) {
	return p.Send(ctx, "DELETE", urlStr, nil, data)
}

// Head sends a HEAD request.
func (p *PendingRequest) Head(ctx context.Context, urlStr string, query map[string]string) (*Response, error) {
	return p.Send(ctx, "HEAD", urlStr, query, nil)
}

// Send sends the request. It is the central method that all HTTP verb
// methods delegate to.
//
// The context governs the whole call: every attempt is made under a context
// derived from it, and so is the wait between two attempts. Cancelling it ends
// the request that is in flight rather than waiting for the destination to
// answer, and the error it returns matches [context.Canceled] or
// [context.DeadlineExceeded]. [PendingRequest.Timeout] is a ceiling laid on top
// of it, per attempt; whichever expires first ends the attempt.
//
// A nil context is refused rather than replaced with a background one: a call
// that cannot be cancelled is the thing this parameter exists to prevent.
func (p *PendingRequest) Send(ctx context.Context, method, urlStr string, query map[string]string, data any) (*Response, error) {
	if ctx == nil {
		return nil, errors.New("http client: nil context")
	}

	// Inside a pool, a verb records the call instead of making it, and the
	// pool makes them all at once when Send is called on it.
	//
	// The pending request knows which pool it belongs to and answers nil
	// until that pool runs. A caller outside a pool never sees it, because a
	// pending request only has a pool if the pool built it.
	if p.pool != nil {
		p.pool.record(ctx, p, method, urlStr, query, data)
		return nil, nil
	}

	// An asynchronous request leaves a promise behind instead of a response.
	// Nothing goes out until somebody waits on it -- see [promises.LazyPromise].
	//
	// The context is the one handed to the verb, not one taken at the moment
	// the promise is waited on: a caller that cancelled before waiting has
	// already said the request is not wanted.
	if p.async {
		p.async = false
		p.promise = promises.NewLazyPromise(func() promises.Promise {
			deferred := promises.NewDeferred(nil)
			go func() {
				response, err := p.Send(ctx, method, urlStr, query, data)
				if err != nil {
					_ = deferred.Reject(err)
					return
				}
				_ = deferred.Resolve(response)
			}()
			return deferred
		})
		return nil, nil
	}

	// Build the URL.
	fullURL, err := p.buildURL(urlStr, query)
	if err != nil {
		return nil, fmt.Errorf("http client: building URL: %w", err)
	}

	// Build the request body.
	//
	// It is buffered rather than streamed because an attempt consumes the
	// reader and a retry needs the same bytes again. The old loop reused the
	// spent reader, so every repeated request went out with an empty body.
	body, contentType, err := p.buildBody(data)
	if err != nil {
		return nil, fmt.Errorf("http client: building body: %w", err)
	}
	var payload []byte
	if body != nil {
		if payload, err = io.ReadAll(body); err != nil {
			return nil, fmt.Errorf("http client: reading body: %w", err)
		}
	}
	if contentType != "" && p.headers.Get("Content-Type") == "" {
		p.headers.Set("Content-Type", contentType)
	}

	// The client these requests go out on, copied so the shared one is left
	// alone.
	cc := p.CreateClient(nil)

	// The number of attempts, which is a total and not a number of extras:
	// Retry's times counts the first send. Retry was configured with zero
	// until somebody asked for it, and zero still means one send.
	attempts := p.retryTimes
	if attempts < 1 {
		attempts = 1
	}

	// One deadline per attempt, released when Send returns. A single deadline
	// spanning every attempt would spend the caller's budget on the attempts
	// that already failed.
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	for attempt := 1; ; attempt++ {
		// Derived from the caller's context, so cancelling it ends the attempt
		// in flight. It used to be derived from context.Background(), which
		// meant no caller could ever stop a request once it had left.
		attemptCtx, cancel := context.WithTimeout(ctx, p.timeout)
		cancels = append(cancels, cancel)

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(attemptCtx, method, fullURL, reader)
		if err != nil {
			return nil, fmt.Errorf("http client: creating request: %w", err)
		}

		// Apply headers.
		req.Header = p.headers.Clone()

		// Apply auth.
		if p.token != "" {
			req.Header.Set("Authorization", p.tokenType+" "+p.token)
		}
		if p.basicUser != "" {
			req.SetBasicAuth(p.basicUser, p.basicPass)
		}

		// Run before-sending callbacks.
		if err := p.RunBeforeSendingCallbacks(req); err != nil {
			return nil, fmt.Errorf("http client: before sending: %w", err)
		}

		resp, httpResp, attemptErr := p.attempt(cc, req)

		// A stray request is the test saying a stub is missing. Repeating it
		// only makes the same complaint three times.
		var stray *StrayRequestError
		if errors.As(attemptErr, &stray) {
			return nil, attemptErr
		}

		// What went wrong on this attempt, which is either the transport
		// failing or the response having failed. The retry callback is handed
		// the transport's error in the first case and the response's own
		// RequestException in the second; it used to be handed nil for both,
		// so a when callback could never actually see why an attempt failed.
		failure := attemptErr
		if failure == nil && resp != nil && resp.Failed() {
			failure = NewRequestException(resp, p.truncateExceptions)
		}
		if failure == nil {
			return resp, nil
		}

		// Whether to try again. A nil callback means true -- retry every
		// failure -- not the retry being switched off: Retry(3, ...) with no
		// callback is the ordinary way to call it, and it used to make exactly
		// one request.
		shouldRetry := true
		if p.retryWhen != nil {
			shouldRetry = p.retryWhen(httpResp, failure)
		}

		if attempt < attempts && shouldRetry {
			if p.retryDelay > 0 {
				// The wait is part of the call, so it ends when the call is
				// cancelled. A plain sleep held a cancelled caller for the
				// whole delay, and a long delay is exactly when a caller
				// gives up.
				timer := time.NewTimer(p.retryDelay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return nil, fmt.Errorf("http client: %w while waiting to retry, after: %w", ctx.Err(), failure)
				}
			}
			continue
		}

		// Out of attempts. A transport failure is returned as it is; a failed
		// response is turned into an exception only when more than one attempt
		// was asked for and throwing was left on.
		if attemptErr != nil {
			return nil, attemptErr
		}
		if attempts > 1 && p.retryThrow {
			return nil, failure
		}
		return resp, nil
	}
}

// attempt sends one request and builds its Response.
//
// A stub stands in for the transport when one matches; otherwise the
// request goes out for real. The response is recorded and the
// after-response callbacks run. The error it returns is the attempt's
// failure -- a transport error, a stubbed connection failure, or whatever an
// after-response callback objected to -- and the caller decides whether that
// failure is worth repeating.
func (p *PendingRequest) attempt(cc *http.Client, req *http.Request) (*Response, *http.Response, error) {
	p.factory.event(events.RequestSending{Request: req})

	httpResp, err := p.findStub(req)
	if httpResp == nil && err == nil {
		httpResp, err = cc.Do(req)
		if err != nil {
			err = fmt.Errorf("http client: %w", err)
		}
	}

	if p.factory != nil && p.factory.recording {
		p.factory.RecordRequestResponsePair(req, httpResp, err)
	}
	if err != nil {
		// A stray request is the test saying a stub is missing, not a
		// connection that failed. Reporting it as one would put a listener
		// that counts outages in the business of counting missing stubs.
		var stray *StrayRequestError
		if !errors.As(err, &stray) {
			p.factory.event(events.ConnectionFailed{Request: req, Exception: err})
		}
		return nil, nil, err
	}

	p.factory.event(events.ResponseReceived{Request: req, Response: httpResp})

	resp := NewResponse(httpResp)

	// A body that could not be read whole is not an answer. It is reported as
	// the attempt's failure rather than left on the Response, where only a
	// caller that decoded it would ever meet it -- and a body stopped at the
	// size limit decodes as a shorter document rather than as an error.
	if resp.readErr != nil {
		return resp, httpResp, fmt.Errorf("http client: reading response body: %w", resp.readErr)
	}

	// Sink the body if configured.
	if p.sink != nil {
		if _, err := io.Copy(p.sink, httpResp.Body); err != nil {
			httpResp.Body.Close()
			return nil, httpResp, fmt.Errorf("http client: sinking body: %w", err)
		}
		httpResp.Body.Close()
		_ = resp.Close()
	}

	// Run after-response callbacks.
	for _, cb := range p.afterResponse {
		if err := cb(resp); err != nil {
			httpResp.Body.Close()
			return resp, httpResp, err
		}
	}

	return resp, httpResp, nil
}

// Pool executes a callback that adds requests to a Pool and sends them
// concurrently.
func (p *PendingRequest) Pool(callback func(*Pool), concurrency int) (map[string]*Response, error) {
	pool := NewPool(p.factory)
	callback(pool)
	return pool.Send(concurrency)
}

// Batch executes a callback that adds requests to a Batch and sends them
// concurrently with progress and error callbacks.
func (p *PendingRequest) Batch(callback func(*Batch)) *Batch {
	batch := NewBatch(p.factory)
	callback(batch)
	return batch
}

func (p *PendingRequest) buildURL(path string, query map[string]string) (string, error) {
	u := path
	if p.baseURL != "" && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		u = p.baseURL + "/" + strings.TrimLeft(path, "/")
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}

	// Merge query parameters.
	q := parsed.Query()
	for k, v := range p.query {
		q[k] = v
	}
	for k, v := range query {
		q.Set(k, v)
	}
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

func (p *PendingRequest) buildBody(data any) (io.Reader, string, error) {
	if p.body != nil {
		return p.body, "", nil
	}

	switch p.bodyFormat {
	case "json":
		if data == nil {
			return nil, "application/json", nil
		}
		b, err := json.Marshal(data)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(b), "application/json", nil
	case "form":
		form := url.Values{}
		if m, ok := data.(map[string]string); ok {
			for k, v := range m {
				form.Set(k, v)
			}
		} else if data != nil {
			// Marshal struct to url.Values via JSON round-trip.
			b, _ := json.Marshal(data)
			var flat map[string]any
			json.Unmarshal(b, &flat)
			for k, v := range flat {
				form.Set(k, fmt.Sprintf("%v", v))
			}
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	case "multipart":
		if p.multipartData != nil {
			p.multipartData.writer.Close()
			return &p.multipartData.buffer, p.multipartData.writer.FormDataContentType(), nil
		}
		return nil, "multipart/form-data", nil
	default:
		if data == nil {
			return nil, "", nil
		}
		b, err := json.Marshal(data)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(b), "application/json", nil
	}
}

func (p *PendingRequest) findStub(req *http.Request) (*http.Response, error) {
	// Check request-level stub first.
	if p.stubCallback != nil {
		return p.stubCallback(req)
	}

	var stubs []StubCallback
	if p.factory != nil {
		stubs = p.factory.stubs
	}
	for _, stub := range stubs {
		resp, err := stub(req)
		if resp != nil || err != nil {
			// The error is the stub's answer and not a fault in finding one: a
			// stub exists to say what the call did, and refusing to connect is
			// one of the things a call does.
			return resp, err
		}
	}

	// No stub matched, and that is where prevention belongs -- before the
	// request can leave rather than after a stub declined it.
	//
	// It used to sit below a return that fired when no stub was registered at
	// all, so a test that prevented stray requests and stubbed nothing let
	// every request reach the network. That is the configuration a test reaches
	// for first, and the one where failing open is worst: the check that exists
	// to keep a suite off the network was skipped exactly when the suite had
	// told it nothing else.
	if !p.IsAllowedRequestUrl(req.URL.String()) {
		return nil, NewStrayRequestError(req.URL.String())
	}

	return nil, nil
}

func newMultipartBuffer() *multipartBuffer {
	var buf bytes.Buffer
	return &multipartBuffer{
		buffer: buf,
		writer: multipart.NewWriter(&buf),
	}
}
