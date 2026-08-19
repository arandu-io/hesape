package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/str"
)

// Factory is the entry point for the HTTP client layer. It holds global
// configuration — middleware, base options, stub callbacks — and issues
// PendingRequest instances through CreatePendingRequest. In a test, Fake
// installs a stub that intercepts every outgoing request; AssertSent
// verifies what was sent.
//
// The zero value is usable: it creates a PendingRequest with no stubs and no
// middleware, on a client that has a deadline and refuses a destination inside
// the network. See AllowInternalHosts and MaxResponseBytes for what it refuses
// and how to widen it.
type Factory struct {
	// Global middleware applied to every request-response cycle.
	globalMiddleware []func(*http.Request, http.RoundTripper) http.RoundTripper
	// Global request middleware applied before sending.
	globalRequestMiddleware []func(*http.Request) error
	// Global response middleware applied after receiving.
	globalResponseMiddleware []func(*http.Response) error

	client *http.Client

	// Stub callbacks registered via Fake. When non-nil, every outgoing
	// request is routed through these callbacks instead of the wire.
	stubs []StubCallback

	// sequences holds ResponseSequence instances for isEmpty checking.
	sequences []*ResponseSequence

	// When true, any request that does not match a stub callback fails
	// with a StrayRequestError.
	preventStray bool

	// allowedStrayURLs permits specific URLs to bypass preventStray when set.
	allowedStrayURLs []string

	// recorded holds pairs of sent Request and received Response for
	// later assertion.
	recorded []RecordedPair

	// recording, when true, stores every request-response pair.
	recording bool

	// dispatcher is the events the client fires into.
	dispatcher Dispatcher

	// allowedInternalHosts are the hosts allowed to resolve to an address
	// inside the network. See AllowInternalHosts.
	allowedInternalHosts []string

	// maxResponseBytes caps the body read back. Zero means
	// DefaultMaxResponseBytes. See MaxResponseBytes.
	maxResponseBytes int64
}

// Dispatcher is the one method the HTTP client needs of an event dispatcher.
//
// It is one method, and it is declared here rather than imported, so that a
// binary that only sends requests does not drag an event bus in behind them.
//
// A dispatcher runs on the calling goroutine, so a listener that blocks blocks
// the request. Anything slow belongs on a queue.
type Dispatcher interface {
	// Dispatch delivers one event to whoever is listening.
	//
	// It returns nothing: a listener that fails must not fail the request that
	// fired it, and an attempt that had to decide what to do with a listener's
	// error would have to decide it three times.
	Dispatch(event any)
}

// SetDispatcher sets the dispatcher the client fires its events into. It is
// a setter rather than a constructor argument, since most callers never
// need one.
//
// Every attempt fires RequestSending before it goes out, and then exactly one
// of ResponseReceived or ConnectionFailed. A retried request fires the three
// once per attempt, because each attempt is a separate request on the wire.
func (f *Factory) SetDispatcher(dispatcher Dispatcher) *Factory {
	f.dispatcher = dispatcher
	return f
}

// GetDispatcher is the dispatcher the client fires into, or nil when
// nobody is listening.
func (f *Factory) GetDispatcher() Dispatcher {
	return f.dispatcher
}

// event fires one event, if anybody is listening.
//
// The receiver may be nil: a PendingRequest built without a Factory has
// nowhere to fire, and that is not a reason for the send to stop.
func (f *Factory) event(e any) {
	if f == nil || f.dispatcher == nil {
		return
	}
	f.dispatcher.Dispatch(e)
}

// GetGlobalMiddleware is the middleware [Factory.GlobalMiddleware] added.
func (f *Factory) GetGlobalMiddleware() []func(*http.Request, http.RoundTripper) http.RoundTripper {
	return f.globalMiddleware
}

// StubCallback receives the outgoing *http.Request and returns a response
// or an error. Return (nil, nil) to let the request fall through to the
// next stub or to the wire.
type StubCallback func(*http.Request) (*http.Response, error)

// RecordedPair is a request-response pair stored during recording.
type RecordedPair struct {
	Request  *http.Request
	Response *http.Response
	Error    error
}

// NewFactory creates a Factory with the given HTTP client.
//
// A nil client asks for the one this package builds: it has a deadline, and its
// transport refuses to connect to an address inside the network. A client
// passed in keeps its own transport and therefore its own answer to where a
// request may connect; one with no transport set is given this package's.
func NewFactory(client *http.Client) *Factory {
	if client == nil {
		client = &http.Client{Transport: defaultTransport, Timeout: defaultTimeout}
	}
	return &Factory{client: client}
}

// GlobalMiddleware adds middleware applied to every request-response cycle.
func (f *Factory) GlobalMiddleware(middleware ...func(*http.Request, http.RoundTripper) http.RoundTripper) *Factory {
	f.globalMiddleware = append(f.globalMiddleware, middleware...)
	return f
}

// GlobalRequestMiddleware adds middleware applied before every request is sent.
func (f *Factory) GlobalRequestMiddleware(middleware ...func(*http.Request) error) *Factory {
	f.globalRequestMiddleware = append(f.globalRequestMiddleware, middleware...)
	return f
}

// GlobalResponseMiddleware adds middleware applied after every response is received.
func (f *Factory) GlobalResponseMiddleware(middleware ...func(*http.Response) error) *Factory {
	f.globalResponseMiddleware = append(f.globalResponseMiddleware, middleware...)
	return f
}

// CreatePendingRequest creates a new PendingRequest backed by this Factory.
func (f *Factory) CreatePendingRequest() *PendingRequest {
	return newPendingRequest(f)
}

// Fake installs stub callbacks that intercept outgoing requests.
// When stubs are installed, requests are routed through them instead of
// the wire. Each callback receives the *http.Request; return a
// (*http.Response, nil) to simulate a successful response, or
// (nil, error) for a connection failure.
//
//	factory.Fake(func(r *http.Request) (*http.Response, error) {
//	    return client.NewResponse(200, `{"ok":true}`), nil
//	})
func (f *Factory) Fake(callbacks ...StubCallback) *Factory {
	f.stubs = append(f.stubs, callbacks...)
	return f
}

// FakeSequence registers a ResponseSequence for the given URL pattern.
// Each call to a matching URL consumes the next response in the sequence.
func (f *Factory) FakeSequence(urlPattern string) *ResponseSequence {
	seq := NewResponseSequence()
	f.stubs = append(f.stubs, seq.AsStub(urlPattern))
	f.sequences = append(f.sequences, seq)
	return seq
}

// Stub registers a stub callback for requests matching the given URL.
//
// Deprecated: use Fake instead, which covers the same use case without the
// URL-matching layer that duplicates what callers already do in the callback.
func (f *Factory) Stub(callback StubCallback) *Factory {
	return f.Fake(callback)
}

// PreventStrayRequests enables prevention of requests that do not match
// any stub. When enabled, an unmatched request fails with StrayRequestError.
func (f *Factory) PreventStrayRequests(prevent bool) *Factory {
	f.preventStray = prevent
	return f
}

// PreventingStrayRequests reports whether stray request prevention is active.
func (f *Factory) PreventingStrayRequests() bool {
	return f.preventStray
}

// AllowStrayRequests allows stray requests, optionally limited to specific URLs.
// When only is empty, all stray requests are allowed.
func (f *Factory) AllowStrayRequests(only ...string) *Factory {
	f.preventStray = false
	f.allowedStrayURLs = only
	return f
}

// Record enables request-response recording. Every sent request and its
// response (or error) is stored for later assertion.
func (f *Factory) Record() *Factory {
	f.recording = true
	return f
}

// RecordRequestResponsePair stores a request-response pair.
func (f *Factory) RecordRequestResponsePair(req *http.Request, resp *http.Response, err error) {
	if !f.recording {
		return
	}
	f.recorded = append(f.recorded, RecordedPair{
		Request:  req,
		Response: resp,
		Error:    err,
	})
}

// Recorded returns the recorded request-response pairs, optionally filtered
// by the callback. If callback is nil, all pairs are returned.
func (f *Factory) Recorded(callback func(RecordedPair) bool) []RecordedPair {
	if callback == nil {
		return f.recorded
	}
	var out []RecordedPair
	for _, p := range f.recorded {
		if callback(p) {
			out = append(out, p)
		}
	}
	return out
}

// AssertSent asserts that a request matching the callback was sent.
// The callback receives the sent *http.Request and returns true if it matches.
func (f *Factory) AssertSent(callback func(*http.Request) bool) error {
	for _, p := range f.recorded {
		if callback(p.Request) {
			return nil
		}
	}
	return &AssertionError{Message: "expected a matching request to have been sent, but none was found"}
}

// AssertNotSent asserts that no request matching the callback was sent.
func (f *Factory) AssertNotSent(callback func(*http.Request) bool) error {
	for _, p := range f.recorded {
		if callback(p.Request) {
			return &AssertionError{Message: "expected no matching request to have been sent, but one was found"}
		}
	}
	return nil
}

// AssertNothingSent asserts that no requests were sent at all.
func (f *Factory) AssertNothingSent() error {
	if len(f.recorded) > 0 {
		return &AssertionError{Message: "expected no requests to have been sent"}
	}
	return nil
}

// AssertSentCount asserts that exactly count requests were sent.
func (f *Factory) AssertSentCount(count int) error {
	if len(f.recorded) != count {
		return &AssertionError{
			Message: formatAssertion("expected %d requests to have been sent, but %d were sent", count, len(f.recorded)),
		}
	}
	return nil
}

// AssertSentInOrder asserts that requests matching the given callbacks were
// sent in the specified order.
func (f *Factory) AssertSentInOrder(callbacks ...func(*http.Request) bool) error {
	ci := 0
	for _, p := range f.recorded {
		if ci >= len(callbacks) {
			break
		}
		if callbacks[ci](p.Request) {
			ci++
		}
	}
	if ci < len(callbacks) {
		return &AssertionError{
			Message: formatAssertion("expected %d matching requests in order, but only %d matched", len(callbacks), ci),
		}
	}
	return nil
}

// AssertSequencesAreEmpty asserts that all registered ResponseSequences
// have been fully consumed.
func (f *Factory) AssertSequencesAreEmpty() error {
	for _, seq := range f.sequences {
		if !seq.IsEmpty() {
			return &AssertionError{Message: "expected all response sequences to be empty"}
		}
	}
	return nil
}

// Response is a stub response for a fake to hand back.
//
// A nil body produces an empty body; anything that is not a string or a
// []byte is JSON-encoded and the Content-Type header is set.
//
// It is a method and not a package function because the package already
// has a type named Response.
func (f *Factory) Response(body any, status int, headers http.Header) *http.Response {
	if status == 0 {
		status = http.StatusOK
	}
	if headers == nil {
		headers = http.Header{}
	}

	var raw []byte
	switch v := body.(type) {
	case nil:
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			// A body the caller cannot encode is a bug in the test, and a
			// stub that swallowed it would fail somewhere else entirely.
			raw = []byte(`{"error":` + strconv.Quote(err.Error()) + `}`)
		} else {
			raw = encoded
		}
		headers.Set("Content-Type", "application/json")
	}

	return &http.Response{
		StatusCode:    status,
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		Header:        headers,
		Body:          io.NopCloser(bytes.NewReader(raw)),
		ContentLength: int64(len(raw)),
	}
}

// FailedRequest is the RequestException a stub hands back when it wants
// the caller to see a failure it can inspect.
func (f *Factory) FailedRequest(body any, status int, headers http.Header) *RequestException {
	return NewRequestException(NewResponse(f.Response(body, status, headers)), nil)
}

// FailedConnection is a stub that fails to connect.
//
// When no message is given, the error names the host instead, because a
// test that asserts on the text is asserting on the host.
func (f *Factory) FailedConnection(message string) StubCallback {
	return func(req *http.Request) (*http.Response, error) {
		if message != "" {
			return nil, NewConnectionException(message)
		}
		host := ""
		if req != nil && req.URL != nil {
			host = req.URL.Host
		}
		return nil, NewConnectionException("could not resolve host: " + host)
	}
}

// Sequence is a ResponseSequence the caller installs itself.
//
// [Factory.FakeSequence] is the same sequence already wired to a URL pattern.
// This one is registered for AssertSequencesAreEmpty and handed back bare,
// for a caller that builds its own stub around it.
func (f *Factory) Sequence() *ResponseSequence {
	seq := NewResponseSequence()
	f.sequences = append(f.sequences, seq)
	return seq
}

// StubUrl fakes one URL pattern with one canned response.
//
// It accepts the StubCallback that a status, a body, a closure or a
// sequence would all collapse into, and [Factory.Response] builds the
// canned one.
func (f *Factory) StubUrl(urlPattern string, callback StubCallback) *Factory {
	return f.Fake(func(req *http.Request) (*http.Response, error) {
		// A pattern without a leading wildcard still matches a URL that
		// carries a scheme and a host.
		pattern := urlPattern
		if !strings.HasPrefix(pattern, "*") {
			pattern = "*" + pattern
		}
		if !str.Is([]string{pattern}, req.URL.String(), false) {
			return nil, nil
		}
		return callback(req)
	})
}

// Pool creates a Pool for issuing concurrent requests.
func (f *Factory) Pool() *Pool {
	return NewPool(f)
}

// GlobalOptions sets global options on the underlying HTTP client.
func (f *Factory) GlobalOptions(fn func(*http.Client)) *Factory {
	fn(f.client)
	return f
}

// Client returns the underlying *http.Client.
func (f *Factory) Client() *http.Client {
	return f.client
}

// AssertionError is returned by assertion methods when the assertion fails.
type AssertionError struct {
	Message string
}

func (e *AssertionError) Error() string {
	return "http client assertion failed: " + e.Message
}
