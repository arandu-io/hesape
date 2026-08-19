package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/http/client/events"
)

// recordingDispatcher collects every event fired at it, so a test can assert
// what the client emitted and in which order.
type recordingDispatcher struct {
	mu    sync.Mutex
	fired []any
}

func (d *recordingDispatcher) Dispatch(event any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fired = append(d.fired, event)
}

func (d *recordingDispatcher) all() []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]any(nil), d.fired...)
}

// names is the type of each event fired, in order, for an assertion that reads
// as the sequence it is checking.
func (d *recordingDispatcher) names() []string {
	var out []string
	for _, e := range d.all() {
		switch e.(type) {
		case events.RequestSending:
			out = append(out, "RequestSending")
		case events.ResponseReceived:
			out = append(out, "ResponseReceived")
		case events.ConnectionFailed:
			out = append(out, "ConnectionFailed")
		default:
			out = append(out, "unknown")
		}
	}
	return out
}

func assertEventNames(t *testing.T, d *recordingDispatcher, want ...string) {
	t.Helper()
	got := d.names()
	if len(got) != len(want) {
		t.Fatalf("events fired: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events fired: got %v, want %v", got, want)
		}
	}
}

// TestASuccessfulRequestFiresRequestSendingThenResponseReceived. A listener
// registered through SetDispatcher runs, which is what the setter is for.
func TestASuccessfulRequestFiresRequestSendingThenResponseReceived(t *testing.T) {
	d := &recordingDispatcher{}
	f := NewFactory(nil).SetDispatcher(d)
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(201, []byte(`{"ok":true}`), nil).HTTPResponse(), nil
	})

	_, err := f.CreatePendingRequest().Get("https://api.example.com/users", nil)
	assertNoErr(t, err, "Get")

	assertEventNames(t, d, "RequestSending", "ResponseReceived")

	sending, ok := d.all()[0].(events.RequestSending)
	if !ok || sending.Request == nil {
		t.Fatalf("RequestSending carries no request: %#v", d.all()[0])
	}
	if got := sending.Request.URL.String(); got != "https://api.example.com/users" {
		t.Errorf("RequestSending carries the wrong URL: %s", got)
	}

	received, ok := d.all()[1].(events.ResponseReceived)
	if !ok || received.Response == nil || received.Request == nil {
		t.Fatalf("ResponseReceived carries no pair: %#v", d.all()[1])
	}
	if received.Response.StatusCode != 201 {
		t.Errorf("ResponseReceived carries the wrong status: %d", received.Response.StatusCode)
	}
}

// TestATransportFailureFiresConnectionFailed, against a server that is not
// listening any more, so the error comes from the transport and not from a
// stub standing in for it.
func TestATransportFailureFiresConnectionFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	client := server.Client()
	server.Close()

	d := &recordingDispatcher{}
	f := NewFactory(client).SetDispatcher(d)

	_, err := f.CreatePendingRequest().Get(url, nil)
	assertErr(t, err, "a request to a closed server should fail")

	assertEventNames(t, d, "RequestSending", "ConnectionFailed")

	failed, ok := d.all()[1].(events.ConnectionFailed)
	if !ok || failed.Exception == nil || failed.Request == nil {
		t.Fatalf("ConnectionFailed carries no failure: %#v", d.all()[1])
	}
}

// TestAStrayRequestDoesNotFireConnectionFailed. A missing stub is the test
// saying so, not an outage, and a listener counting outages must not count it.
func TestAStrayRequestDoesNotFireConnectionFailed(t *testing.T) {
	d := &recordingDispatcher{}
	f := NewFactory(nil).SetDispatcher(d)
	f.PreventStrayRequests(true)
	// A stub that answers another URL, so the request reaches the stray check
	// rather than falling through to the wire.
	f.Fake(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/allowed" {
			return NewResponseFromBytes(200, nil, nil).HTTPResponse(), nil
		}
		return nil, nil
	})

	_, err := f.CreatePendingRequest().Get("https://api.example.com/forbidden", nil)
	assertErr(t, err, "an unstubbed request should fail")

	var stray *StrayRequestError
	if !errors.As(err, &stray) {
		t.Fatalf("the request did not reach the stray check: %v", err)
	}
	assertEventNames(t, d, "RequestSending")
}

// TestEveryAttemptOfARetriedRequestFiresItsOwnEvents, because each attempt is
// a separate request on the wire and a listener that saw one would undercount
// what the process actually sent.
func TestEveryAttemptOfARetriedRequestFiresItsOwnEvents(t *testing.T) {
	d := &recordingDispatcher{}
	f := NewFactory(nil).SetDispatcher(d)
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(500, nil, nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	pr.Retry(3, 0, func(resp *http.Response, err error) bool { return true }, false)
	_, _ = pr.Get("https://api.example.com/flaky", nil)

	assertEventNames(t, d,
		"RequestSending", "ResponseReceived",
		"RequestSending", "ResponseReceived",
		"RequestSending", "ResponseReceived",
	)
}

// TestARequestWithoutADispatcherSends. Nobody listening is the common case,
// and it must not cost the caller a nil check or a panic.
func TestARequestWithoutADispatcherSends(t *testing.T) {
	f := NewFactory(nil)
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{}`), nil).HTTPResponse(), nil
	})

	resp, err := f.CreatePendingRequest().Get("https://api.example.com/quiet", nil)
	assertNoErr(t, err, "Get")
	assertEqual(t, resp.Status(), 200, "status")
	if f.GetDispatcher() != nil {
		t.Fatal("a factory with no dispatcher reports one")
	}
}
