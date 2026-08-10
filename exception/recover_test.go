package exception_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/log"
)

// A panic carrying an error that says what it is is still an answer: the two
// ways a handler fails meet at the same classification.
func TestAPanickedRefusalIsStill403(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true})

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/admin", nil),
		func(http.ResponseWriter, *http.Request) { panic(fmt.Errorf("%w: admin.view", auth.ErrForbidden)) })

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	// Not the debug page: somebody claimed this, so it is an answer and not a
	// defect, even in development.
	if strings.Contains(rec.Body.String(), "arandu debug") {
		t.Fatal("a refusal was answered with the debug page")
	}
}

// net/http expects this value to keep unwinding: a client that hung up
// mid-response is not an application bug.
func TestErrAbortHandlerKeepsUnwinding(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true})
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })

	defer func() {
		v := recover()
		if v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()

	exception.Recover(h)(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("Recover swallowed http.ErrAbortHandler")
}

// A panic is news whatever it classifies as, and the log line is the only thread
// back to it once the page has been closed.
func TestAPanicIsAlwaysReported(t *testing.T) {
	logger, lines := log.Capture()
	h := exception.NewHandler(exception.Config{})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(log.Into(r.Context(), logger))

	serve(h, r, func(http.ResponseWriter, *http.Request) { panic(errors.New("boom")) })

	var found bool
	for _, rec := range lines.All() {
		if rec.Message == "recovered panic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the panic was not reported: %v", lines.All())
	}
}

// The Collector is created by the observability middleware, which runs INSIDE
// this one, so without the slot the debug page would show a stack and nothing
// else -- no queries, no dumps, which is the whole point of the page.
func TestTheCollectorReachesThePageFromInsideTheMiddleware(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true})

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil), func(_ http.ResponseWriter, r *http.Request) {
		// This is the observability middleware's job, done here to stand where
		// it stands: one layer further in than Recover.
		col := log.NewCollector("req-slot")
		ctx := log.WithCollector(r.Context(), col)
		col.RecordQuery("SELECT * FROM invoices", nil, 0, 1, nil)
		log.Dump(ctx, "checkpoint", 7)
		panic(errors.New("boom"))
	})

	body := rec.Body.String()
	for _, want := range []string{"req-slot", "FROM invoices", "checkpoint"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q, so the slot did not carry the Collector out", want)
		}
	}
}

// A request that did not panic must come out untouched.
func TestRecoverPassesASuccessfulRequestThrough(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true})

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("done"))
	})

	if rec.Code != http.StatusCreated || rec.Body.String() != "done" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}
