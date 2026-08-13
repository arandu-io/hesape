package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PendingRequest mirrors Illuminate\Http\Client\PendingRequest.
//
// It is the fluent builder for an outgoing HTTP request. Every method returns
// the PendingRequest itself so that callers chain: WithToken, WithHeader,
// Timeout, Retry, and finally Get, Post, Put, Patch, Delete, Head, or Send.
//
// The zero value is not usable; create one with NewPendingRequest or
// Factory.CreatePendingRequest.
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

	// beforeSending callbacks run just before the request is sent.
	beforeSending []func(*http.Request) error
	// afterResponse callbacks run after the Response is built.
	afterResponse []func(*Response) error

	// Dump enables request/response dumping to stdout.
	dump bool
	// DumpAndDie enables request/response dumping then exits.
	dumpAndDie bool

	// truncateExceptions is PendingRequest::$truncateExceptionsAt, the PHP
	// int|false|null: nil defers to the RequestException static, a pointer to
	// zero is the PHP false, and a positive length cuts the body summary.
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
		timeout:        30 * time.Second,
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

// Retry configures automatic retry on failure.
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

// Throw enables throwing on 4xx/5xx responses.
func (p *PendingRequest) Throw(callback func(*Response) error) *PendingRequest {
	if callback != nil {
		// Custom throw callback.
		p.afterResponse = append(p.afterResponse, func(r *Response) error {
			if r.Failed() {
				return callback(r)
			}
			return nil
		})
		return p
	}
	// Default: throw on any 4xx/5xx.
	p.afterResponse = append(p.afterResponse, func(r *Response) error {
		if r.Failed() {
			return NewRequestException(r, p.truncateExceptions)
		}
		return nil
	})
	return p
}

// TruncateExceptionsAt is PendingRequest::truncateExceptionsAt: cut the body
// summary of this request's exceptions at the given length.
func (p *PendingRequest) TruncateExceptionsAt(length int) *PendingRequest {
	p.truncateExceptions = &length
	return p
}

// DontTruncateExceptions is PendingRequest::dontTruncateExceptions: let the
// whole body into this request's exception messages.
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

// Async enables async mode. In async mode, Get/Post/etc. return immediately.
func (p *PendingRequest) Async(async bool) *PendingRequest {
	p.async = async
	return p
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

// Get sends a GET request.
func (p *PendingRequest) Get(urlStr string, query map[string]string) (*Response, error) {
	return p.Send("GET", urlStr, query, nil)
}

// Post sends a POST request.
func (p *PendingRequest) Post(urlStr string, data any) (*Response, error) {
	return p.Send("POST", urlStr, nil, data)
}

// Put sends a PUT request.
func (p *PendingRequest) Put(urlStr string, data any) (*Response, error) {
	return p.Send("PUT", urlStr, nil, data)
}

// Patch sends a PATCH request.
func (p *PendingRequest) Patch(urlStr string, data any) (*Response, error) {
	return p.Send("PATCH", urlStr, nil, data)
}

// Delete sends a DELETE request.
func (p *PendingRequest) Delete(urlStr string, data any) (*Response, error) {
	return p.Send("DELETE", urlStr, nil, data)
}

// Head sends a HEAD request.
func (p *PendingRequest) Head(urlStr string, query map[string]string) (*Response, error) {
	return p.Send("HEAD", urlStr, query, nil)
}

// Send sends the request. It is the central method that all HTTP verb
// methods delegate to.
func (p *PendingRequest) Send(method, urlStr string, query map[string]string, data any) (*Response, error) {
	// Inside a pool, a verb records the call instead of making it, and the
	// pool makes them all at once when Send is called on it.
	//
	// PHP gets this from the promise its verbs return -- $pool->as('a')->get()
	// hands back something unresolved, and Http::pool resolves the lot. Go has
	// no promise, so the pending request knows which pool it belongs to and
	// answers nil until that pool runs. A caller outside a pool never sees it,
	// because a pending request only has a pool if the pool built it.
	if p.pool != nil {
		p.pool.record(p, method, urlStr, query, data)
		return nil, nil
	}

	// Build the URL.
	fullURL, err := p.buildURL(urlStr, query)
	if err != nil {
		return nil, fmt.Errorf("http client: building URL: %w", err)
	}

	// Build the request body.
	body, contentType, err := p.buildBody(data)
	if err != nil {
		return nil, fmt.Errorf("http client: building body: %w", err)
	}
	if contentType != "" && p.headers.Get("Content-Type") == "" {
		p.headers.Set("Content-Type", contentType)
	}

	// Build the *http.Request.
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
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
	for _, cb := range p.beforeSending {
		if err := cb(req); err != nil {
			return nil, fmt.Errorf("http client: before sending: %w", err)
		}
	}

	// Determine the client to use.
	client := http.DefaultClient
	if p.factory != nil && p.factory.client != nil {
		client = p.factory.client
	}
	// Make a copy so we don't mutate the shared client.
	cc := *client
	if p.maxRedirects == 0 {
		cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	cc.Timeout = p.timeout

	// Check stubs.
	if stubResp, stubErr := p.findStub(req); stubResp != nil || stubErr != nil {
		if p.factory != nil && p.factory.recording {
			p.factory.RecordRequestResponsePair(req, stubResp, stubErr)
		}
		if stubErr != nil {
			return nil, stubErr
		}
		resp := NewResponse(stubResp)
		for _, cb := range p.afterResponse {
			if err := cb(resp); err != nil {
				return nil, err
			}
		}
		return resp, nil
	}

	// Send the request.
	httpResp, err := cc.Do(req)
	if err != nil {
		if p.factory != nil && p.factory.recording {
			p.factory.RecordRequestResponsePair(req, nil, err)
		}
		return nil, fmt.Errorf("http client: %w", err)
	}

	// Record.
	if p.factory != nil && p.factory.recording {
		p.factory.RecordRequestResponsePair(req, httpResp, nil)
	}

	resp := NewResponse(httpResp)

	// Sink the body if configured.
	if p.sink != nil {
		if _, err := io.Copy(p.sink, httpResp.Body); err != nil {
			httpResp.Body.Close()
			return nil, fmt.Errorf("http client: sinking body: %w", err)
		}
		httpResp.Body.Close()
		_ = resp.Close()
	}

	// Run after-response callbacks.
	for _, cb := range p.afterResponse {
		if err := cb(resp); err != nil {
			httpResp.Body.Close()
			return nil, err
		}
	}

	// Retry if configured.
	if p.retryTimes > 0 && resp.Failed() && p.retryWhen != nil && p.retryWhen(httpResp, nil) {
		for i := 0; i < p.retryTimes; i++ {
			time.Sleep(p.retryDelay)
			httpResp.Body.Close()
			req2, _ := http.NewRequestWithContext(ctx, method, fullURL, body)
			req2.Header = req.Header
			httpResp, err = cc.Do(req2)
			if err != nil {
				continue
			}
			resp = NewResponse(httpResp)
			if !resp.Failed() || (p.retryWhen != nil && !p.retryWhen(httpResp, nil)) {
				break
			}
		}
		// If still failed after retries and throw is enabled, return error.
		if p.retryThrow && resp.Failed() {
			httpResp.Body.Close()
			return nil, NewRequestException(resp, p.truncateExceptions)
		}
	}

	return resp, nil
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

	// Check factory-level stubs.
	if p.factory == nil || len(p.factory.stubs) == 0 {
		return nil, nil
	}

	for _, stub := range p.factory.stubs {
		resp, err := stub(req)
		if resp != nil || err != nil {
			return resp, nil
		}
	}

	// No stub matched. Check stray prevention.
	if p.factory.preventStray || p.preventStray {
		// Check allowed URLs.
		for _, allowed := range p.factory.allowedStrayURLs {
			if req.URL.String() == allowed {
				return nil, nil
			}
		}
		for _, allowed := range p.allowStray {
			if req.URL.String() == allowed {
				return nil, nil
			}
		}
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
