package exception_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/log"
)

// serve runs fn behind Recover and returns what the client got.
//
// The tests go through the middleware rather than calling the page directly,
// because the page is no longer something anybody calls: it is what the Handler
// falls back to, and the path from a panic to the page is the thing worth
// proving.
func serve(h *exception.Handler, r *http.Request, fn http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	exception.Recover(h)(fn).ServeHTTP(rec, r)
	return rec
}

func devHandler() *exception.Handler {
	return exception.NewHandler(exception.Config{Dev: true, Editor: "vscode"})
}

// withCollector puts a collector on the request, the way the observability
// middleware does one layer in.
func withCollector(r *http.Request, col *log.Collector) *http.Request {
	return r.WithContext(log.WithCollector(r.Context(), col))
}

func TestDebugPageShowsQueriesDumpsAndEvents(t *testing.T) {
	col := log.NewCollector("req-abc")
	col.RecordQuery(`SELECT * FROM invoices WHERE id = $1`, []any{"inv-1"}, 3*time.Millisecond, 1, nil)
	col.RecordEvent("invoice.viewed", "inv-1")
	col.RecordExternal("POST", "https://payments.test/charge", 502, 900*time.Millisecond)

	r := withCollector(httptest.NewRequest(http.MethodGet, "/invoices/inv-1", nil), col)
	rec := serve(devHandler(), r, func(_ http.ResponseWriter, r *http.Request) {
		log.Dump(r.Context(), "invoice", map[string]string{"number": "42"})
		panic(errors.New("boom"))
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"FROM invoices",        // the query
		"inv-1",                // its context
		"invoice",              // the dump label
		"number",               // the dumped value
		"invoice.viewed",       // the event
		"payments.test/charge", // the outbound call
		"req-abc",              // the request id
		"GET",                  // the method
		"/invoices/inv-1",      // the path
		"vscode://file",        // the open-in-editor link
		`<html lang="en">`,     // the product speaks English
		"development only",     // and says where it is allowed to exist
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the debug page does not mention %q", want)
		}
	}
}

// TestDebugPageSuggestsTheCause is the difference between showing data and being
// useful: the page has to name the likely cause.
func TestDebugPageSuggestsTheCause(t *testing.T) {
	cases := []struct {
		name     string
		panicVal any
		prepare  func(*log.Collector)
		want     string
	}{
		{
			name:     "missing grant",
			panicVal: errors.New("arandu: action not authorized: missing grant for auth.user.view"),
			want:     "auth.Authorize",
		},
		{
			name:     "grant for another action",
			panicVal: errors.New("arandu: action not authorized: grant issued for a, used on b"),
			want:     "issued for one action",
		},
		{
			// Not a CSRF case. That one asserted a panic that cannot happen --
			// a token mismatch is classified as 419 and answered with a status
			// page -- and the branch behind it matched "CSRFToken" too, so it
			// answered a missing method with somebody else's fix. See
			// hints_test.go.
			name:     "the migrations never ran here",
			panicVal: errors.New(`ERROR: relation "posts" does not exist (SQLSTATE 42P01)`),
			want:     "aru migrate",
		},
		{
			name:     "n plus one",
			panicVal: errors.New("boom"),
			prepare: func(c *log.Collector) {
				for range 5 {
					c.RecordQuery(`SELECT * FROM addresses WHERE user_id = $1`, nil, time.Millisecond, 1, nil)
				}
			},
			// The literal text is "Likely N+1", which html/template writes as
			// "N&#43;1", so the assertion matches the part that survives escaping.
			want: "the same statement ran 5 or more times",
		},
		{
			name:     "slow query",
			panicVal: errors.New("boom"),
			prepare: func(c *log.Collector) {
				c.RecordQuery(`SELECT * FROM ledger`, nil, 900*time.Millisecond, 1, nil)
			},
			want: "Check the index",
		},
		{
			name:     "failed query before the panic",
			panicVal: errors.New("boom"),
			prepare: func(c *log.Collector) {
				c.RecordQuery(`SELECT 1`, nil, time.Millisecond, 0, errors.New("connection reset"))
			},
			want: "A query failed before this panic",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			col := log.NewCollector("req-1")
			if c.prepare != nil {
				c.prepare(col)
			}
			r := withCollector(httptest.NewRequest(http.MethodPost, "/", nil), col)

			rec := serve(devHandler(), r, func(http.ResponseWriter, *http.Request) { panic(c.panicVal) })

			if body := rec.Body.String(); !strings.Contains(body, c.want) {
				t.Fatalf("no diagnosis mentioning %q", c.want)
			}
		})
	}
}

// TestSensitiveHeadersAreRedacted: a screenshot of an error page is the easiest
// way in the world to leak a session, so redaction applies even in development.
func TestSensitiveHeadersAreRedacted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cookie", "arandu_session=super-secret")
	r.Header.Set("Authorization", "Bearer super-secret")
	r.Header.Set("X-CSRF-Token", "super-secret")
	r.Header.Set("X-Arandu-Trace", "super-secret")
	r.Header.Set("Accept-Language", "en")

	rec := serve(devHandler(), r, func(http.ResponseWriter, *http.Request) { panic(errors.New("boom")) })

	body := rec.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Fatal("a sensitive header value reached the page")
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatal("the page must show that a value was redacted, not hide the header")
	}
	if !strings.Contains(body, "Accept-Language") {
		t.Fatal("harmless headers must still be visible")
	}
}

// TestDebugPageWithoutCollector covers the panic that happens before the
// observability middleware runs.
func TestDebugPageWithoutCollector(t *testing.T) {
	rec := serve(devHandler(), httptest.NewRequest(http.MethodGet, "/", nil),
		func(http.ResponseWriter, *http.Request) { panic("raw string panic") })

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "raw string panic") {
		t.Fatal("the page must show the panic value even with no Collector")
	}
}

func TestDumpPageAnswersOK(t *testing.T) {
	col := log.NewCollector("req-1")
	r := withCollector(httptest.NewRequest(http.MethodGet, "/", nil), col)

	rec := serve(devHandler(), r, func(_ http.ResponseWriter, r *http.Request) {
		log.DumpDie(r.Context(), "checkpoint", 7)
	})

	// Dump-and-die is a deliberate stop, not a failure.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "checkpoint") {
		t.Fatal("the dump page must show the dump")
	}
}

// TestTheDebugPageIsDevelopmentOnly is the absolute rule of this package.
func TestTheDebugPageIsDevelopmentOnly(t *testing.T) {
	col := log.NewCollector("req-9")
	col.RecordQuery(`SELECT * FROM secrets`, nil, time.Millisecond, 1, nil)
	r := withCollector(httptest.NewRequest(http.MethodGet, "/", nil), col)

	rec := serve(exception.NewHandler(exception.Config{}), r,
		func(http.ResponseWriter, *http.Request) { panic(errors.New("the database password is hunter2")) })

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"hunter2", "FROM secrets", "arandu debug", "Stack"} {
		if strings.Contains(body, leak) {
			t.Errorf("the production page leaked %q", leak)
		}
	}
	if !strings.Contains(body, "req-9") {
		t.Error("the production page must carry the request id, or nothing connects it to the log")
	}
}

// TestCaptureMarksApplicationFrames: the page opens app frames and collapses
// collection and runtime ones, which is what makes a stack readable.
func TestCaptureMarksApplicationFrames(t *testing.T) {
	frames := exception.Capture(1, "github.com/arandu-io/hesape/exception_test")

	if len(frames) == 0 {
		t.Fatal("Capture returned no frames")
	}
	var firstApp *exception.StackFrame
	for i, f := range frames {
		if strings.HasPrefix(f.Func, "github.com/arandu-io/hesape/exception.") && f.IsApp {
			t.Errorf("collection frame marked as application code: %s", f.Func)
		}
		if strings.HasPrefix(f.Func, "runtime.") && f.IsApp {
			t.Errorf("runtime frame marked as application code: %s", f.Func)
		}
		if f.IsApp && firstApp == nil {
			firstApp = &frames[i]
		}
	}

	if firstApp == nil {
		t.Fatal("no frame was marked as application code")
	}
	// The snippet is what makes the page readable without leaving the browser.
	if firstApp.Snippet == nil {
		t.Fatalf("the application frame %s carries no source snippet", firstApp.Func)
	}
	if firstApp.SnipTop == 0 {
		t.Fatal("the snippet must know which line it starts at, or the highlight is meaningless")
	}
}

// TestTheStackNamesTheLineThatPanicked: the skip count in Recover is the whole
// value of capturing there rather than in the Handler, and it is off-by-one bait.
func TestTheStackNamesTheLineThatPanicked(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true, AppModule: "github.com/arandu-io/hesape/exception_test"})

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil),
		func(http.ResponseWriter, *http.Request) { panic(errors.New("boom")) })

	if !strings.Contains(rec.Body.String(), "TestTheStackNamesTheLineThatPanicked") {
		t.Fatal("the stack does not reach the function that panicked")
	}
}
