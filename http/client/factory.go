package client

import (
	"net/http"
)

// Factory mirrors Illuminate\Http\Client\Factory.
//
// It is the entry point for the HTTP client layer. The Factory holds global
// configuration — middleware, base options, stub callbacks — and issues
// PendingRequest instances through CreatePendingRequest. In a test, Fake
// installs a stub that intercepts every outgoing request; AssertSent
// verifies what was sent.
//
// The zero value is usable: it creates a PendingRequest backed by
// http.DefaultClient with no stubs and no middleware.
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

// NewFactory creates a Factory with the given HTTP client. If client is nil,
// http.DefaultClient is used.
func NewFactory(client *http.Client) *Factory {
	if client == nil {
		client = http.DefaultClient
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
