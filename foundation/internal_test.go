package foundation

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/pipeline"
)

// TestTheFrameworksOwnRoutesDoNotSpendTheApplicationsBudget.
//
// The development reload asks once a second which process is answering, and
// that ran through the rate limit an application mounts for its own visitors --
// 60 of a 300-per-minute budget per open tab, shared, because the key falls
// back to the address for a request with no session.
//
// Ordinary browsing with a couple of tabs open therefore answered "too many
// requests: wait 32 seconds and try again", in plain text, on a page nobody had
// hammered. Reported from a browser, on /auth/login, while navigating normally.
func TestTheFrameworksOwnRoutesDoNotSpendTheApplicationsBudget(t *testing.T) {
	var counted int
	counting := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counted++
			next.ServeHTTP(w, r)
		})
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := pipeline.Chain[http.Handler](ok, exceptInternal(counting))

	for _, path := range []string{
		internalPrefix + "health",
		reloadPath,
		log.ConsolePath,
	} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if counted != 0 {
		t.Errorf("%d of the framework's own requests were charged to the application", counted)
	}

	// And the application's own traffic still is.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if counted != 1 {
		t.Errorf("the application's middleware ran %d times for one request to /, want 1", counted)
	}
}

// A middleware that refuses a request still refuses it on the application's own
// routes: exceptInternal skips the wrapper, it does not neuter it.
func TestApplicationMiddlewareStillAnswersOnApplicationRoutes(t *testing.T) {
	refusing := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		})
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := pipeline.Chain[http.Handler](ok, exceptInternal(refusing))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("the application's middleware answered %d on its own route, want 429", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, internalPrefix+"health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("the health probe answered %d, want 200: it was gated by the application", rec.Code)
	}
}

// TestTheConsoleIsClosedOutsideDevelopment.
//
// The recorder exists whenever a tracing secret is configured -- that is what
// makes production tracing possible at all. The console routes were mounted
// from the same condition, and the secret was checked only by the middleware
// that decides whether to RECORD. So anyone could GET /_arandu/debug with no
// session, no cookie and no header, and read the buffer: SQL with its bound
// arguments, dumps, event payloads, across every tenant.
func TestTheConsoleIsClosedOutsideDevelopment(t *testing.T) {
	var reached bool
	gated := requireTracingSecret("s3cret-operator-only", func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	// Anonymous, which is what an internet scan looks like.
	rec := httptest.NewRecorder()
	gated(rec, httptest.NewRequest(http.MethodGet, log.ConsolePath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("an anonymous request answered %d, want 404", rec.Code)
	}

	// A wrong secret behaves exactly like none, or the endpoint becomes an
	// oracle for guessing it.
	wrong := httptest.NewRequest(http.MethodGet, log.ConsolePath, nil)
	wrong.Header.Set(log.TracingHeader, "not-the-secret")
	rec = httptest.NewRecorder()
	gated(rec, wrong)
	if rec.Code != http.StatusNotFound {
		t.Errorf("a wrong secret answered %d, want 404", rec.Code)
	}
	if reached {
		t.Fatal("the console was reached without the secret")
	}

	// The operator, with the secret, still gets in -- otherwise the feature is
	// gone rather than fixed.
	right := httptest.NewRequest(http.MethodGet, log.ConsolePath, nil)
	right.Header.Set(log.TracingHeader, "s3cret-operator-only")
	rec = httptest.NewRecorder()
	gated(rec, right)
	if !reached {
		t.Error("the operator with the secret did not reach the console")
	}
}

// TestAnEmptySecretDoesNotOpenTheConsole: an empty secret is the zero value of
// the configuration, and treating it as "no gate" would open the console for
// every application that never set one.
func TestAnEmptySecretDoesNotOpenTheConsole(t *testing.T) {
	gated := requireTracingSecret("", func(w http.ResponseWriter, r *http.Request) {
		t.Error("the console answered with no secret configured")
	})

	for _, header := range []string{"", "anything"} {
		r := httptest.NewRequest(http.MethodGet, log.ConsolePath, nil)
		if header != "" {
			r.Header.Set(log.TracingHeader, header)
		}
		rec := httptest.NewRecorder()
		gated(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("with no secret configured and header %q, the console answered %d", header, rec.Code)
		}
	}
}
