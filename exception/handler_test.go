package exception_test

import (
	"context"
	"encoding/json"
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

// render is the routing layer's side of this package: a controller returned an
// error and something has to answer it.
func render(h *exception.Handler, r *http.Request, err error) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Render(rec, r, err)
	return rec
}

func TestAReturnedErrorGetsTheStatusItAskedFor(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	r := httptest.NewRequest(http.MethodGet, "/invoices/inv-1", nil)

	rec := render(h, r, exception.Abort(http.StatusNotFound, "no invoice with that number"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no invoice with that number") {
		t.Fatal("the page does not carry the message the handler wrote")
	}
	if !strings.Contains(body, "Not Found") {
		t.Fatal("the page does not name the status")
	}
}

// A refusal from a policy is 403 and not 500, which is the change this package
// exists to make.
func TestARefusalIsAnAnswerAndNotAFailure(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)

	rec := render(h, r, fmt.Errorf("%w: admin.view", auth.ErrForbidden))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	// The policy's own words are for the log, not for the page: they were
	// written for whoever is debugging.
	if strings.Contains(rec.Body.String(), "admin.view") {
		t.Fatal("the page leaked the internal reason for the refusal")
	}
}

// Only an *HTTPError message leaves the process. Everything else is the
// standard sentence, in development as much as in production.
func TestAnUnclaimedErrorNeverReachesThePage(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := render(h, r, errors.New("dial tcp 10.0.0.4:5432: connection refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.4") {
		t.Fatal("the page leaked the address of the database")
	}
}

func TestAJSONRequestGetsJSON(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	r := httptest.NewRequest(http.MethodGet, "/api/invoices/inv-1", nil)
	r.Header.Set("Accept", "application/json")
	r = withCollector(r, log.NewCollector("req-json"))

	rec := render(h, r, exception.Abort(http.StatusNotFound, "no invoice with that number"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body struct {
		Status    int    `json:"status"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}
	if body.Status != http.StatusNotFound || body.Message != "no invoice with that number" {
		t.Fatalf("body = %+v", body)
	}
	if body.RequestID != "req-json" {
		t.Fatalf("request_id = %q, and without it nothing connects this to the log", body.RequestID)
	}
}

// htmx sends X-Requested-With and swaps HTML. Answering it with JSON would put
// a JSON document inside a div.
func TestHTMXGetsAPageAndNotJSON(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	r := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("X-Requested-With", "XMLHttpRequest")

	rec := render(h, r, exception.Abort(http.StatusForbidden, ""))

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want HTML", ct)
	}
}

// DontReport is a list of sentinels and not a list of statuses: a 404 from a
// bad link and a 404 from a repository that lost a row are the same status and
// different news.
func TestDontReportSilencesOnlyWhatItNames(t *testing.T) {
	quiet := errors.New("the client hung up")

	logger, lines := log.Capture()
	h := exception.NewHandler(exception.Config{DontReport: []error{quiet}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(log.Into(r.Context(), logger))

	h.Render(httptest.NewRecorder(), r, fmt.Errorf("writing the response: %w", quiet))
	if lines.Len() != 0 {
		t.Fatalf("a silenced error was reported: %v", lines.All())
	}

	h.Render(httptest.NewRecorder(), r, errors.New("something else"))
	if lines.Len() == 0 {
		t.Fatal("an error nobody silenced went unreported")
	}
}

// stubViews is an application that ships its own errors/404.
type stubViews struct {
	has  string
	err  error
	seen exception.PageData
}

func (s *stubViews) Has(name string) bool { return name == s.has }

func (s *stubViews) Render(_ context.Context, w http.ResponseWriter, status int, _ string, data any) error {
	d, ok := data.(exception.PageData)
	if !ok {
		return fmt.Errorf("the page data is %T and not exception.PageData", data)
	}
	s.seen = d
	if s.err != nil {
		return s.err
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte("the application's own 404"))
	return nil
}

// The audit found there was no errors/ view directory anywhere, so a 404 was an
// empty body. The built-in pages answer by default; this is the override.
func TestTheApplicationCanShipItsOwnErrorPage(t *testing.T) {
	views := &stubViews{has: "errors/404"}
	h := exception.NewHandler(exception.Config{Views: views})
	r := withCollector(httptest.NewRequest(http.MethodGet, "/nowhere", nil), log.NewCollector("req-7"))

	rec := render(h, r, exception.Abort(http.StatusNotFound, "no invoice with that number"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "the application's own 404") {
		t.Fatal("the application's page was not used")
	}
	if views.seen.RequestID != "req-7" || views.seen.Message != "no invoice with that number" {
		t.Fatalf("the page data reached the view incomplete: %+v", views.seen)
	}
}

// A view that failed before writing anything can still be replaced. An error
// page that fails is the one page that must not.
func TestABrokenApplicationPageFallsBackToTheBuiltInOne(t *testing.T) {
	views := &stubViews{has: "errors/500", err: errors.New("undefined field on the page data")}
	h := exception.NewHandler(exception.Config{Views: views})
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := render(h, r, errors.New("boom"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Fatal("the built-in page did not answer for the broken one")
	}
}

// A status the application has no view for falls straight through, which is why
// Has exists: guessing from a failed render is guessing after the fact.
func TestAStatusWithNoApplicationViewUsesTheBuiltInPage(t *testing.T) {
	h := exception.NewHandler(exception.Config{Views: &stubViews{has: "errors/404"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := render(h, r, exception.Abort(http.StatusServiceUnavailable, ""))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatalf("the built-in 503 did not answer: %s", rec.Body.String())
	}
}

func TestRenderForConsole(t *testing.T) {
	h := exception.NewHandler(exception.Config{})

	var claimed, unclaimed strings.Builder
	h.RenderForConsole(&claimed, exception.Abort(http.StatusForbidden, "this account may not run that"))
	h.RenderForConsole(&unclaimed, errors.New("dial tcp: connection refused"))

	if !strings.Contains(claimed.String(), "this account may not run that") || !strings.Contains(claimed.String(), "403") {
		t.Fatalf("claimed = %q", claimed.String())
	}
	// A command is run by whoever operates the application, so there is nothing
	// to withhold: the error text is what they need.
	if !strings.Contains(unclaimed.String(), "connection refused") {
		t.Fatalf("unclaimed = %q", unclaimed.String())
	}
}

// A status page is one person's answer about one account. A shared cache handing
// it to somebody else is a 404 that stays missing after the page is created.
func TestAStatusPageIsNeverCached(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	rec := render(h, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, ""))

	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestRenderIgnoresANilError(t *testing.T) {
	rec := render(exception.NewHandler(exception.Config{}), httptest.NewRequest(http.MethodGet, "/", nil), nil)

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("a nil error wrote %d and %q", rec.Code, rec.Body.String())
	}
}
