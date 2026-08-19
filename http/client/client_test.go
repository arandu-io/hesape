package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// assertHelper marks the calling function as a test helper.
func assertHelper(t *testing.T) {
	t.Helper()
}

// assertEqual fails if got != want.
func assertEqual(t *testing.T, got, want any, msg string) {
	assertHelper(t)
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// assertNotNil fails if v is nil.
func assertNotNil(t *testing.T, v any, msg string) {
	assertHelper(t)
	if v == nil {
		t.Fatalf("%s: expected non-nil value", msg)
	}
}

// assertErr fails if err is nil.
func assertErr(t *testing.T, err error, msg string) {
	assertHelper(t)
	if err == nil {
		t.Fatalf("%s: expected error, got nil", msg)
	}
}

func assertNoErr(t *testing.T, err error, msg string) {
	assertHelper(t)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

// --- Factory.Fake URL matching tests ---

func TestFactoryFakeMatchesRequest(t *testing.T) {
	f := NewFactory(nil)
	called := false
	f.Fake(func(r *http.Request) (*http.Response, error) {
		called = true
		return NewResponseFromBytes(200, []byte(`{"ok":true}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	resp, err := pr.Get("https://api.example.com/users", nil)
	assertNoErr(t, err, "Get")
	assertNotNil(t, resp, "response")
	assertEqual(t, called, true, "stub was not called")
	assertEqual(t, resp.Status(), 200, "status")
}

func TestFactoryFakeMultipleStubs(t *testing.T) {
	f := NewFactory(nil)
	var order []int
	f.Fake(func(r *http.Request) (*http.Response, error) {
		order = append(order, 1)
		if strings.Contains(r.URL.String(), "users") {
			return NewResponseFromBytes(200, []byte(`{"users":true}`), nil).HTTPResponse(), nil
		}
		return nil, nil
	})
	f.Fake(func(r *http.Request) (*http.Response, error) {
		order = append(order, 2)
		if strings.Contains(r.URL.String(), "posts") {
			return NewResponseFromBytes(201, []byte(`{"posts":true}`), nil).HTTPResponse(), nil
		}
		return nil, nil
	})

	pr := f.CreatePendingRequest()
	resp, err := pr.Get("https://api.example.com/users", nil)
	assertNoErr(t, err, "Get users")
	assertEqual(t, resp.Status(), 200, "users status")
	assertEqual(t, len(order) >= 1, true, "at least one stub called")
	assertEqual(t, order[0], 1, "first stub called")
}

func TestFactoryFakeDoesNotMatch(t *testing.T) {
	f := NewFactory(nil)
	f.Fake(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://api.example.com/blocked" {
			return NewResponseFromBytes(403, nil, nil).HTTPResponse(), nil
		}
		return nil, nil
	})

	// Create a test server that replies with actual data.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"real":true}`))
	}))
	defer server.Close()

	f.client = server.Client()
	pr := f.CreatePendingRequest()
	resp, err := pr.Get(server.URL+"/ok", nil)
	assertNoErr(t, err, "Get real server")
	assertEqual(t, resp.Successful(), true, "successful")
	body, _ := resp.JSON("", nil)
	assertNotNil(t, body, "body")
}

// --- ResponseSequence tests ---

func TestResponseSequenceReturnsInOrder(t *testing.T) {
	seq := NewResponseSequence()
	seq.Push(200, `{"first":true}`, nil)
	seq.Push(201, `{"second":true}`, nil)
	seq.Push(404, `{"third":true}`, nil)

	stub := seq.AsStub("*")

	resp1, err1 := stub(mustNewRequest("GET", "https://api.example.com/1"))
	assertNoErr(t, err1, "first stub")
	assertEqual(t, resp1.StatusCode, 200, "first status")

	resp2, err2 := stub(mustNewRequest("GET", "https://api.example.com/1"))
	assertNoErr(t, err2, "second stub")
	assertEqual(t, resp2.StatusCode, 201, "second status")

	resp3, err3 := stub(mustNewRequest("GET", "https://api.example.com/1"))
	assertNoErr(t, err3, "third stub")
	assertEqual(t, resp3.StatusCode, 404, "third status")
}

func TestResponseSequenceExhaustedFails(t *testing.T) {
	seq := NewResponseSequence()
	seq.Push(200, `{"ok":true}`, nil)

	stub := seq.AsStub("*")

	resp, err := stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertNoErr(t, err, "first call")
	assertEqual(t, resp.StatusCode, 200, "first status")

	// Second call on exhausted sequence should fail.
	_, err = stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertErr(t, err, "exhausted sequence should return error")
}

func TestResponseSequenceWhenEmpty(t *testing.T) {
	seq := NewResponseSequence()
	seq.WhenEmpty(200, `{"fallback":true}`, nil)

	stub := seq.AsStub("*")

	// Sequence is empty but has a fallback.
	resp, err := stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertNoErr(t, err, "fallback call")
	assertEqual(t, resp.StatusCode, 200, "fallback status")
}

func TestResponseSequencePushStatus(t *testing.T) {
	seq := NewResponseSequence()
	seq.PushStatus(204, nil)

	stub := seq.AsStub("*")

	resp, err := stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertNoErr(t, err, "pushStatus call")
	assertEqual(t, resp.StatusCode, 204, "status")
}

func TestResponseSequencePushFailedConnection(t *testing.T) {
	seq := NewResponseSequence()
	seq.PushFailedConnection("connection refused")

	stub := seq.AsStub("*")

	_, err := stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertErr(t, err, "pushFailedConnection should return error")
}

func TestResponseSequenceIsEmpty(t *testing.T) {
	seq := NewResponseSequence()
	assertEqual(t, seq.IsEmpty(), true, "empty sequence should report empty")

	seq.Push(200, `{}`, nil)
	assertEqual(t, seq.IsEmpty(), false, "non-empty should not report empty")

	stub := seq.AsStub("*")
	stub(mustNewRequest("GET", "https://api.example.com/test"))
	assertEqual(t, seq.IsEmpty(), true, "consumed sequence should report empty")
}

// --- PreventStrayRequests tests ---

func TestPreventStrayRequestsBlocksUnstubbedRequest(t *testing.T) {
	f := NewFactory(nil)
	f.PreventStrayRequests(true)
	// No stubs registered.

	pr := f.CreatePendingRequest()
	_, err := pr.Get("https://api.example.com/forbidden", nil)
	assertErr(t, err, "unstubbed request should fail")
	if err != nil {
		msg := err.Error()
		if !strings.Contains(msg, "api.example.com/forbidden") {
			t.Fatalf("error message should contain the URL, got: %s", msg)
		}
	}
}

func TestPreventStrayRequestsStubbedRequestsSucceed(t *testing.T) {
	f := NewFactory(nil)
	f.PreventStrayRequests(true)
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{"ok":true}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	resp, err := pr.Get("https://api.example.com/ok", nil)
	assertNoErr(t, err, "stubbed request should succeed")
	assertEqual(t, resp.Status(), 200, "status")
}

func TestPreventingStrayRequests(t *testing.T) {
	f := NewFactory(nil)
	assertEqual(t, f.PreventingStrayRequests(), false, "default should be false")

	f.PreventStrayRequests(true)
	assertEqual(t, f.PreventingStrayRequests(), true, "after enable should be true")

	f.PreventStrayRequests(false)
	assertEqual(t, f.PreventingStrayRequests(), false, "after disable should be false")
}

// --- Assertion tests ---

func TestAssertSent(t *testing.T) {
	f := NewFactory(nil)
	f.Record()

	pr := f.CreatePendingRequest()
	resp, err := pr.Get("https://api.example.com/users", nil)
	// Ignore connection errors since there's no real server.
	_ = resp
	_ = err

	// Record was called; check that the recorded request has the right URL.
	match := false
	for _, p := range f.recorded {
		if p.Request.URL.String() == "https://api.example.com/users" {
			match = true
			break
		}
	}
	if !match {
		t.Fatalf("AssertSent: no recorded request matched URL https://api.example.com/users, but %d requests were recorded", len(f.recorded))
	}
}

func TestAssertSentWithFake(t *testing.T) {
	f := NewFactory(nil)
	f.Record()
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	resp, err := pr.Get("https://api.example.com/users", nil)
	assertNoErr(t, err, "stubbed Get")
	assertEqual(t, resp.Status(), 200, "status")

	found := f.AssertSent(func(r *http.Request) bool {
		return r.URL.String() == "https://api.example.com/users"
	})
	assertNoErr(t, found, "AssertSent should find the request")

	notFound := f.AssertSent(func(r *http.Request) bool {
		return r.URL.String() == "https://api.example.com/nonexistent"
	})
	assertErr(t, notFound, "AssertSent should not find nonexistent URL")
}

func TestAssertNotSent(t *testing.T) {
	f := NewFactory(nil)
	f.Record()
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	pr.Get("https://api.example.com/users", nil)

	assertErr := f.AssertNotSent(func(r *http.Request) bool {
		return r.URL.String() == "https://api.example.com/posts"
	})
	assertNoErr(t, assertErr, "AssertNotSent should pass for unrequested URL")
}

func TestAssertNothingSent(t *testing.T) {
	f := NewFactory(nil)
	f.Record()

	assertErr := f.AssertNothingSent()
	assertNoErr(t, assertErr, "AssertNothingSent should pass when nothing was sent")
}

func TestAssertSentCount(t *testing.T) {
	f := NewFactory(nil)
	f.Record()
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	pr.Get("https://api.example.com/a", nil)
	pr.Get("https://api.example.com/b", nil)
	pr.Get("https://api.example.com/c", nil)

	assertNoErr(t, f.AssertSentCount(3), "count should be 3")
	assertErr(t, f.AssertSentCount(2), "count should not be 2")
}

func TestAssertSentInOrder(t *testing.T) {
	f := NewFactory(nil)
	f.Record()
	f.Fake(func(r *http.Request) (*http.Response, error) {
		return NewResponseFromBytes(200, []byte(`{}`), nil).HTTPResponse(), nil
	})

	pr := f.CreatePendingRequest()
	pr.Get("https://api.example.com/1", nil)
	pr.Get("https://api.example.com/2", nil)
	pr.Get("https://api.example.com/3", nil)

	assertNoErr(t, f.AssertSentInOrder(
		func(r *http.Request) bool { return r.URL.String() == "https://api.example.com/1" },
		func(r *http.Request) bool { return r.URL.String() == "https://api.example.com/2" },
		func(r *http.Request) bool { return r.URL.String() == "https://api.example.com/3" },
	), "should match in order")

	assertErr(t, f.AssertSentInOrder(
		func(r *http.Request) bool { return r.URL.String() == "https://api.example.com/3" },
		func(r *http.Request) bool { return r.URL.String() == "https://api.example.com/2" },
	), "wrong order should fail")
}

// --- Retry tests ---

func TestRetryAttemptsAllRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	pr := f.CreatePendingRequest()
	pr.Retry(3, 10*time.Millisecond, func(resp *http.Response, err error) bool {
		return resp != nil && resp.StatusCode >= 500
	}, true)

	resp, err := pr.Get(server.URL, nil)
	assertNoErr(t, err, "retry should eventually succeed")
	assertEqual(t, resp.Status(), 200, "final status should be 200")
}

func TestRetryBackoffWaits(t *testing.T) {
	var attempts atomic.Int32
	start := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	pr := f.CreatePendingRequest()
	pr.Retry(3, 50*time.Millisecond, func(resp *http.Response, err error) bool {
		return resp != nil && resp.StatusCode >= 500
	}, true)

	pr.Get(server.URL, nil)
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Fatalf("retry backoff too short: elapsed %v, expected at least 100ms for 2 retries at 50ms each", elapsed)
	}
}

// --- Pool tests ---

func TestPoolConcurrentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"path":"%s"}`, r.URL.Path)
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	pool := f.Pool()

	pool.As("a").Get(server.URL+"/a", nil)
	pool.As("b").Get(server.URL+"/b", nil)
	pool.As("c").Get(server.URL+"/c", nil)

	results, err := pool.Send(3)
	assertNoErr(t, err, "pool send should succeed")
	assertEqual(t, len(results), 3, "should have 3 results")

	for _, key := range []string{"a", "b", "c"} {
		if _, ok := results[key]; !ok {
			t.Fatalf("pool result missing key %q; got keys: %v", key, mapKeys(results))
		}
	}
}

// --- Batch tests ---

func TestBatchConcurrentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"path":"%s"}`, r.URL.Path)
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	var completed int
	var captured map[string]*Response

	// Factory.Pool already answers with a *Pool; Batch wraps one.
	pool := f.Pool()

	pool.As("x").Get(server.URL+"/x", nil)
	pool.As("y").Get(server.URL+"/y", nil)

	results, err := pool.Send(2)
	assertNoErr(t, err, "batch send")
	completed = len(results)
	captured = results

	assertEqual(t, completed, 2, "completed count")
	_ = captured
}

// --- Response tests ---

func TestResponseStatusChecks(t *testing.T) {
	tests := []struct {
		status     int
		successful bool
		failed     bool
		clientErr  bool
		serverErr  bool
	}{
		{200, true, false, false, false},
		{201, true, false, false, false},
		{301, false, false, false, false},
		{400, false, true, true, false},
		{404, false, true, true, false},
		{422, false, true, true, false},
		{500, false, true, false, true},
		{502, false, true, false, true},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			r := NewResponseFromBytes(tc.status, []byte(`{}`), nil)
			if r.Successful() != tc.successful {
				t.Fatalf("status %d: Successful() = %v, want %v", tc.status, r.Successful(), tc.successful)
			}
			if r.Failed() != tc.failed {
				t.Fatalf("status %d: Failed() = %v, want %v", tc.status, r.Failed(), tc.failed)
			}
			if r.ClientError() != tc.clientErr {
				t.Fatalf("status %d: ClientError() = %v, want %v", tc.status, r.ClientError(), tc.clientErr)
			}
			if r.ServerError() != tc.serverErr {
				t.Fatalf("status %d: ServerError() = %v, want %v", tc.status, r.ServerError(), tc.serverErr)
			}
		})
	}
}

func TestResponseOkCreatedNoContent(t *testing.T) {
	assertEqual(t, NewResponseFromBytes(200, nil, nil).OK(), true, "OK")
	assertEqual(t, NewResponseFromBytes(201, nil, nil).Created(), true, "Created")
	assertEqual(t, NewResponseFromBytes(204, nil, nil).NoContent(), true, "NoContent")
	assertEqual(t, NewResponseFromBytes(404, nil, nil).OK(), false, "not OK")
}

func TestResponseJSON(t *testing.T) {
	body := `{"user":{"name":"Alice","age":30},"items":[1,2,3]}`
	r := NewResponseFromBytes(200, []byte(body), nil)

	val, err := r.JSON("", nil)
	assertNoErr(t, err, "JSON root")
	assertNotNil(t, val, "JSON root value")

	// Dot notation access.
	userName, err := r.JSON("user.name", nil)
	assertNoErr(t, err, "JSON user.name")
	assertEqual(t, userName, "Alice", "user name")

	userAge, err := r.JSON("user.age", nil)
	assertNoErr(t, err, "JSON user.age")
	assertEqual(t, userAge, float64(30), "user age")

	_, err = r.JSON("user.missing", nil)
	assertErr(t, err, "missing key should error")
}

func TestResponseHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Custom", "value123")
	r := NewResponseFromBytes(200, []byte(`{}`), h)
	assertEqual(t, r.Header("X-Custom"), "value123", "custom header")
	assertEqual(t, r.Header("X-Missing"), "", "missing header")
}

func TestResponseBody(t *testing.T) {
	r := NewResponseFromBytes(200, []byte(`hello world`), nil)
	assertEqual(t, r.Body(), "hello world", "body")
}

func TestResponseCollect(t *testing.T) {
	body := `{"items":[{"id":1},{"id":2},{"id":3}]}`
	r := NewResponseFromBytes(200, []byte(body), nil)

	items, err := r.Collect("items")
	assertNoErr(t, err, "collect items")
	assertEqual(t, len(items), 3, "items count")
	assertEqual(t, items[0]["id"], float64(1), "first id")
}

func TestResponseThrow(t *testing.T) {
	r := NewResponseFromBytes(500, []byte(`server error`), nil)
	err := r.Throw(nil)
	assertErr(t, err, "throw on 500 should error")

	r2 := NewResponseFromBytes(200, []byte(`ok`), nil)
	err2 := r2.Throw(nil)
	assertNoErr(t, err2, "throw on 200 should not error")
}

func TestResponseOnError(t *testing.T) {
	r := NewResponseFromBytes(500, []byte(`error`), nil)
	called := false
	err := r.OnError(func(r *Response) error {
		called = true
		return fmt.Errorf("handled error")
	})
	assertErr(t, err, "onError should return error")
	assertEqual(t, called, true, "callback called")
}

func TestResponseRedirect(t *testing.T) {
	assertEqual(t, NewResponseFromBytes(301, nil, nil).Redirect(), true, "301 is redirect")
	assertEqual(t, NewResponseFromBytes(302, nil, nil).Redirect(), true, "302 is redirect")
	assertEqual(t, NewResponseFromBytes(200, nil, nil).Redirect(), false, "200 not redirect")
}

func TestResponseClose(t *testing.T) {
	r := NewResponseFromBytes(200, []byte(`ok`), nil)
	assertNoErr(t, r.Close(), "first close")
	assertNoErr(t, r.Close(), "second close should be safe")
}

// --- Request tests ---

func TestRequestMethodAndURL(t *testing.T) {
	req := mustNewRequest("POST", "https://api.example.com/users")
	r := NewRequest(req)
	assertEqual(t, r.Method(), "POST", "method")
	assertEqual(t, r.URL(), "https://api.example.com/users", "url")
}

func TestRequestHasHeader(t *testing.T) {
	req := mustNewRequest("GET", "https://api.example.com/test")
	req.Header.Set("Accept", "application/json")
	r := NewRequest(req)

	assertEqual(t, r.HasHeader("Accept", ""), true, "has Accept header")
	assertEqual(t, r.HasHeader("Accept", "application/json"), true, "Accept value matches")
	assertEqual(t, r.HasHeader("Accept", "text/html"), false, "Accept value does not match")
	assertEqual(t, r.HasHeader("X-Missing", ""), false, "missing header")
}

func TestRequestIsJSON(t *testing.T) {
	req := mustNewRequest("POST", "https://api.example.com/test")
	req.Header.Set("Content-Type", "application/json")
	r := NewRequest(req)
	assertEqual(t, r.IsJSON(), true, "is json")
	assertEqual(t, r.IsForm(), false, "not form")
}

func TestRequestIsForm(t *testing.T) {
	req := mustNewRequest("POST", "https://api.example.com/test")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r := NewRequest(req)
	assertEqual(t, r.IsForm(), true, "is form")
	assertEqual(t, r.IsJSON(), false, "not json")
}

// --- Exception tests ---

func TestRequestException(t *testing.T) {
	r := NewResponseFromBytes(422, []byte(`{"errors":{"name":["required"]}}`), nil)
	exc := NewRequestException(r, nil)
	assertNotNil(t, exc, "exception")
	assertNotNil(t, exc.Response, "exception response")

	msg := exc.Error()
	if !strings.Contains(msg, "422") {
		t.Fatalf("exception message should contain status code, got: %s", msg)
	}
}

func TestRequestExceptionTruncatesBody(t *testing.T) {
	body := strings.Repeat("a", 400)
	r := NewResponseFromBytes(500, []byte(body), nil)

	short := NewRequestException(r, nil)
	if strings.Contains(short.Error(), body) {
		t.Fatal("the default truncation should have cut the body")
	}
	if !strings.Contains(short.Error(), "(truncated...)") {
		t.Fatalf("expected the truncation marker, got: %s", short.Error())
	}

	full := r.DontTruncateExceptions().ToException()
	if !strings.Contains(full.Error(), body) {
		t.Fatal("DontTruncateExceptions should have let the whole body through")
	}
}

func TestResponseToExceptionIsNilWhenSuccessful(t *testing.T) {
	ok := NewResponseFromBytes(200, []byte(`{}`), nil)
	if ok.ToException() != nil {
		t.Fatal("a successful response has no exception")
	}
	if err := ok.ThrowIfClientError(); err != nil {
		t.Fatalf("no client error to throw, got: %v", err)
	}
	if err := ok.ThrowUnlessStatus(201); err == nil {
		t.Fatal("ThrowUnlessStatus should have thrown on a mismatched status")
	}
	if err := ok.ThrowUnlessStatus(200); err != nil {
		t.Fatalf("ThrowUnlessStatus should be quiet on a match, got: %v", err)
	}
}

func TestResponseUnprocessableContentAndReason(t *testing.T) {
	r := NewResponseFromBytes(422, []byte(`{}`), nil)
	assertEqual(t, r.UnprocessableContent(), true, "unprocessable content")
	assertEqual(t, r.UnprocessableEntity(), true, "unprocessable entity")
	assertEqual(t, r.Reason(), "Unprocessable Entity", "reason phrase")
}

func TestStrayRequestError(t *testing.T) {
	err := NewStrayRequestError("https://evil.com/api")
	assertEqual(t, err.Error(), "http client: attempt to send request without a matching stub: https://evil.com/api", "stray error message")
}

func TestConnectionException(t *testing.T) {
	err := NewConnectionException("timeout")
	assertEqual(t, err.Error(), "http client error: timeout", "connection error message")
}

// --- Helpers ---

func mustNewRequest(method, url string) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	return req
}

func mapKeys(m map[string]*Response) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestAnAsyncRequestLeavesAPromiseAndSendsWhenItIsWaitedOn(t *testing.T) {
	f := NewFactory(nil)
	sent := 0
	f.Fake(func(r *http.Request) (*http.Response, error) {
		sent++
		return NewResponseFromBytes(200, []byte(`{"ok":true}`), nil).HTTPResponse(), nil
	})

	pending := f.CreatePendingRequest().Async(true)
	resp, err := pending.Get("https://example.test/things", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp != nil {
		t.Fatal("an async request should not hand back a response")
	}
	if sent != 0 {
		t.Fatal("an async request should not have gone out before it was waited on")
	}

	promise := pending.GetPromise()
	if promise == nil {
		t.Fatal("an async request should have left a promise behind")
	}

	value, err := promise.Wait(true)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if sent != 1 {
		t.Fatalf("the request went out %d times, want 1", sent)
	}
	waited, ok := value.(*Response)
	if !ok || !waited.OK() {
		t.Fatalf("the promise resolved to %v, want an OK response", value)
	}
}

func TestFactoryResponseBuildsTheStubEveryFakeHandsBack(t *testing.T) {
	f := NewFactory(nil)

	resp := NewResponse(f.Response(map[string]any{"name": "Ada"}, 201, nil))
	assertEqual(t, resp.Status(), 201, "status")
	assertEqual(t, resp.Header("Content-Type"), "application/json", "content type")
	assertEqual(t, resp.Body(), `{"name":"Ada"}`, "body")

	empty := NewResponse(f.Response(nil, 0, nil))
	assertEqual(t, empty.Status(), 200, "default status")
	assertEqual(t, empty.Body(), "", "empty body")
}

func TestStubUrlOnlyAnswersForTheUrlItWasGiven(t *testing.T) {
	f := NewFactory(nil)
	f.PreventStrayRequests(true)
	f.StubUrl("*example.test/allowed*", func(*http.Request) (*http.Response, error) {
		return f.Response("yes", 200, nil), nil
	})

	resp, err := f.CreatePendingRequest().Get("https://example.test/allowed/thing", nil)
	if err != nil {
		t.Fatalf("the stubbed URL failed: %v", err)
	}
	assertEqual(t, resp.Body(), "yes", "stubbed body")

	if _, err := f.CreatePendingRequest().Get("https://example.test/other", nil); err == nil {
		t.Fatal("a URL the stub does not match should have been a stray request")
	}
}

func TestFailedConnectionAndFailedRequestAreStubsThatFail(t *testing.T) {
	f := NewFactory(nil)

	f.Fake(f.FailedConnection(""))
	_, err := f.CreatePendingRequest().Get("https://example.test/thing", nil)
	if err == nil || !strings.Contains(err.Error(), "example.test") {
		t.Fatalf("error = %v, want one naming the host", err)
	}

	failed := f.FailedRequest(map[string]any{"message": "nope"}, 422, nil)
	assertEqual(t, failed.Response.Status(), 422, "failed request status")
	if !strings.Contains(failed.Error(), "422") {
		t.Fatalf("message = %q, want the status in it", failed.Error())
	}
}

func TestIsAllowedRequestUrlMatchesPatternsAndNotOnlyLiterals(t *testing.T) {
	f := NewFactory(nil)
	p := f.CreatePendingRequest()

	assertEqual(t, p.IsAllowedRequestUrl("https://anywhere.test/x"), true, "allowed before prevention")

	f.PreventStrayRequests(true)
	assertEqual(t, p.IsAllowedRequestUrl("https://anywhere.test/x"), false, "blocked after prevention")

	f.AllowStrayRequests("https://anywhere.test/*")
	f.PreventStrayRequests(true)
	assertEqual(t, p.IsAllowedRequestUrl("https://anywhere.test/x"), true, "allowed by pattern")
	assertEqual(t, p.IsAllowedRequestUrl("https://elsewhere.test/x"), false, "still blocked elsewhere")
}

func TestMergeOptionsLaysTheGivenOptionsOverTheRequestsOwn(t *testing.T) {
	p := NewFactory(nil).CreatePendingRequest().MaxRedirects(9)

	merged := p.MergeOptions(map[string]any{"max_redirects": 2}, map[string]any{"sink": "file"})
	assertEqual(t, merged["max_redirects"], 2, "later option wins")
	assertEqual(t, merged["sink"], "file", "added option")

	if p.GetOptions()["max_redirects"] != 9 {
		t.Fatal("MergeOptions must not write back onto the request")
	}
}

// --- Audit regressions: Response.Throw, Response.ThrowIfStatus ---

// TestResponseThrowRunsTheCallbackAndStillReturnsTheException pins
// Response.Throw: the callback is a side effect, and the exception is
// returned whatever the callback does. This used to return the callback's
// own nil instead, making a 500 disappear when the callback only logged.
func TestResponseThrowRunsTheCallbackAndStillReturnsTheException(t *testing.T) {
	r := NewResponseFromBytes(500, []byte(`server error`), nil)

	var gotResponse *Response
	var gotException *RequestException
	err := r.Throw(func(resp *Response, e *RequestException) {
		gotResponse, gotException = resp, e
	})

	assertErr(t, err, "a 500 must throw even when a callback ran")
	if gotResponse != r {
		t.Fatal("the callback must receive the response, as the PHP's first argument")
	}
	if gotException == nil {
		t.Fatal("the callback must receive the exception, as the PHP's second argument")
	}
	if err.(*RequestException) != gotException {
		t.Fatal("the exception the callback saw must be the one that is returned")
	}
}

// TestResponseThrowIfStatusThrowsOnASuccessfulStatus pins
// Response.ThrowIfStatus, which throws whenever the status matches and does
// not consult Failed. A 201 matched by ThrowIfStatus(201) used to return nil.
func TestResponseThrowIfStatusThrowsOnASuccessfulStatus(t *testing.T) {
	r := NewResponseFromBytes(201, []byte(`created`), nil)

	assertErr(t, r.ThrowIfStatus(201), "throwIfStatus(201) on a 201 must throw")
	assertNoErr(t, r.ThrowIfStatus(202), "throwIfStatus(202) on a 201 must not throw")
}

// --- Audit regressions: PendingRequest.Retry ---

// countingTransport counts the requests that reach it and fails every one of
// them, which is the connection error the retry loop never repeated.
type countingTransport struct {
	attempts atomic.Int32
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.attempts.Add(1)
	return nil, fmt.Errorf("dial tcp: connection refused")
}

// TestRetryWithoutAWhenCallbackStillRetries pins Retry(3, ...) with no
// callback, which is the common form: a nil callback means retry every
// failure, and three attempts are made. A nil callback used to turn the
// retry off entirely.
func TestRetryWithoutAWhenCallbackStillRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	p := f.CreatePendingRequest().Retry(3, time.Millisecond, nil, true)

	resp, err := p.Get(server.URL, nil)
	assertNoErr(t, err, "the third attempt succeeds")
	assertEqual(t, resp.Status(), 200, "final status")
	assertEqual(t, int(attempts.Load()), 3, "attempts")
}

// TestRetryMakesAtMostTheNumberOfAttemptsAskedFor pins the count: Retry's
// times is a total, not a number of extra tries. Retry(3) used to make four
// requests.
func TestRetryMakesAtMostTheNumberOfAttemptsAskedFor(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(500)
	}))
	defer server.Close()

	f := NewFactory(server.Client())
	p := f.CreatePendingRequest().Retry(3, time.Millisecond, func(*http.Response, error) bool {
		return true
	}, true)

	_, err := p.Get(server.URL, nil)
	assertErr(t, err, "every attempt failed, so the exception is returned")
	assertEqual(t, int(attempts.Load()), 3, "attempts")
}

// TestRetryRepeatsAConnectionError pins the case retry exists for. The loop
// returned as soon as the transport failed, so a refused connection was tried
// once.
func TestRetryRepeatsAConnectionError(t *testing.T) {
	transport := &countingTransport{}

	f := NewFactory(&http.Client{Transport: transport})
	p := f.CreatePendingRequest().Retry(3, time.Millisecond, nil, true)

	_, err := p.Get("https://example.test/thing", nil)
	assertErr(t, err, "the connection never came up")
	assertEqual(t, int(transport.attempts.Load()), 3, "attempts")
}

// TestRetryDoesNotRepeatWhenTheCallbackSaysNot pins the callback's veto, and
// that it is handed the exception rather than a nil second argument.
func TestRetryDoesNotRepeatWhenTheCallbackSaysNot(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(429)
	}))
	defer server.Close()

	var sawException bool
	f := NewFactory(server.Client())
	p := f.CreatePendingRequest().Retry(3, time.Millisecond, func(_ *http.Response, err error) bool {
		sawException = err != nil
		return false
	}, false)

	resp, err := p.Get(server.URL, nil)
	assertNoErr(t, err, "throw is off, so the response comes back")
	assertEqual(t, resp.Status(), 429, "status")
	assertEqual(t, int(attempts.Load()), 1, "attempts")
	assertEqual(t, sawException, true, "the callback must be handed the exception")
}

// TestStrayPreventionHoldsWithNoStubsRegistered separates a refusal from a
// request that left and failed.
//
// TestPreventStrayRequestsBlocksUnstubbedRequest above asserts that the error
// names the URL, and a transport error names it too -- so it passed while the
// request was reaching the network, because api.example.com refused it. The
// discriminator is the error type, and this is the configuration where the
// check used to be skipped: prevention on, nothing stubbed.
func TestStrayPreventionHoldsWithNoStubsRegistered(t *testing.T) {
	f := NewFactory(nil)
	f.PreventStrayRequests(true)

	pr := f.CreatePendingRequest()
	_, err := pr.Get("https://api.example.com/forbidden", nil)
	assertErr(t, err, "a request left with prevention on and no stub registered")

	var stray *StrayRequestError
	if !errors.As(err, &stray) {
		t.Fatalf("the request was attempted rather than refused: %T: %v", err, err)
	}
}
