package exception_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
)

func TestErrorRegistersInFrontAndPushErrorBehind(t *testing.T) {
	var order []string
	h := exception.NewHandler(exception.Config{})

	h.Error(func(error, int, bool) any { order = append(order, "first"); return nil })
	h.PushError(func(error, int, bool) any { order = append(order, "last"); return nil })
	h.Error(func(error, int, bool) any { order = append(order, "front"); return nil })

	h.HandleException(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("failed"))

	want := []string{"front", "first", "last"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestAHandlerThatAnswersStopsTheOnesBehindIt(t *testing.T) {
	var ran int
	h := exception.NewHandler(exception.Config{})

	h.PushError(func(error, int, bool) any { ran++; return "answered" })
	h.PushError(func(error, int, bool) any { ran++; return nil })

	response := h.HandleException(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("failed"))

	if response != "answered" {
		t.Fatalf("response = %v, want the first handler's answer", response)
	}
	if ran != 1 {
		t.Fatalf("ran = %d, want the handler behind the answer to be skipped", ran)
	}
}

// A handler that fails while handling a failure must not take the process with
// it: the PHP wraps each one to avoid a white screen of death.
func TestAHandlerThatPanicsDoesNotTakeTheProcessWithIt(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.PushError(func(error, int, bool) any { panic("the handler itself failed") })

	response := h.HandleException(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("failed"))

	answer, _ := response.(string)
	if !strings.HasPrefix(answer, "Error in exception handler") {
		t.Fatalf("response = %v, want the formatted handler failure", response)
	}
}

func TestAHandlerFailureSaysNothingOutsideDevelopment(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: false})
	h.PushError(func(error, int, bool) any { panic("the secret is /etc/passwd") })

	response := h.HandleException(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("failed"))

	if response != "Error in exception handler." {
		t.Fatalf("response = %v, want the sentence with nothing in it", response)
	}
}

func TestMissingOnlyFiresFor404(t *testing.T) {
	var seen []int
	h := exception.NewHandler(exception.Config{})
	h.Missing(func(err error) any {
		status, _ := exception.StatusOf(err)
		seen = append(seen, status)
		return "handled"
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	h.HandleException(w, r, exception.Abort(http.StatusForbidden, ""))
	h.HandleException(w, r, exception.Abort(http.StatusNotFound, ""))

	if len(seen) != 1 || seen[0] != http.StatusNotFound {
		t.Fatalf("seen = %v, want only the 404", seen)
	}
}

func TestFatalOnlyFiresForWhatNobodyClaimed(t *testing.T) {
	var ran int
	h := exception.NewHandler(exception.Config{})
	h.Fatal(func(error) any { ran++; return "handled" })

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	h.HandleException(w, r, exception.Abort(http.StatusNotFound, ""))
	if ran != 0 {
		t.Fatal("Fatal fired for a status the application chose")
	}

	h.HandleException(w, r, errors.New("nobody claimed this"))
	if ran != 1 {
		t.Fatalf("ran = %d, want Fatal to fire once", ran)
	}
}

func TestHandleExceptionFallsBackToTheDisplayer(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	rec := httptest.NewRecorder()

	h.HandleException(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, "no invoice"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the status page", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no invoice") {
		t.Fatalf("the message is not on the page: %s", rec.Body.String())
	}
}

func TestHandleConsoleRunsTheStackWithFromConsoleSet(t *testing.T) {
	var seen bool
	h := exception.NewHandler(exception.Config{})
	h.PushError(func(_ error, _ int, fromConsole bool) any { seen = fromConsole; return "handled" })

	if got := h.HandleConsole(errors.New("failed")); got != "handled" {
		t.Fatalf("HandleConsole = %v, want the handler's answer", got)
	}
	if !seen {
		t.Fatal("the handler was not told it was running for a command")
	}
}

func TestTheDisplayerFollowsTheDebugSwitch(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	if _, ok := h.Displayer().(*exception.PlainDisplayer); !ok {
		t.Fatalf("displayer = %T, want the plain one outside development", h.Displayer())
	}

	h.SetDebug(true)
	if _, ok := h.Displayer().(*exception.DebugDisplayer); !ok {
		t.Fatalf("displayer = %T, want the debug one in development", h.Displayer())
	}
}

// Nothing that reveals the inside of the process may be reachable when debug is
// off, and the plain displayer is what enforces it.
func TestThePlainDisplayerLeaksNothing(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	rec := httptest.NewRecorder()

	h.Displayer().Display(rec, httptest.NewRequest(http.MethodGet, "/", nil),
		errors.New("pq: password authentication failed for user \"arandu\""))

	if strings.Contains(rec.Body.String(), "password authentication") {
		t.Fatalf("the driver's error text reached the page: %s", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRunningInConsole(t *testing.T) {
	if exception.NewHandler(exception.Config{}).RunningInConsole() {
		t.Fatal("a handler serving requests said it was running in the console")
	}
	if !exception.NewHandler(exception.Config{Console: true}).RunningInConsole() {
		t.Fatal("a handler running a command said it was not")
	}
}

// Register("testing") leaves a panic to the test that provoked it: a page
// nobody is looking at hides the failure the test exists to show.
func TestRegisterInTestingLetsThePanicThrough(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	middleware := h.Register("testing")

	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("the handler failed")
	}))

	defer func() {
		if recover() == nil {
			t.Fatal("the panic was swallowed in the testing environment")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRegisterOutsideTestingDrawsThePage(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	middleware := h.Register("production")

	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("the handler failed")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want the 500 page", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "the handler failed") {
		t.Fatalf("the panic value reached the page: %s", rec.Body.String())
	}
}

// TestPlainDisplayerCarriesTheErrorsHeaders: the PHP's PlainDisplayer copies
// $exception->getHeaders() onto the response, and this dropped them -- which
// turns a 429 into a 429 nobody can retry correctly and a 401 into one no client
// knows how to answer.
func TestPlainDisplayerCarriesTheErrorsHeaders(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	err := &exception.HTTPError{
		Status:  http.StatusTooManyRequests,
		Headers: http.Header{"Retry-After": {"30"}},
	}

	rec := httptest.NewRecorder()
	h.Displayer().Display(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)

	if got := rec.Result().Header.Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want the 30 the error asked for", got)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

// TestDebugDisplayerAnswersTheStatusTheErrorAskedFor: both PHP displayers
// answer $exception->getStatusCode(), and this one always answered 500 -- so a
// 404 in development was a 500 to every client, and a test written against the
// status could not tell the two apart.
func TestDebugDisplayerAnswersTheStatusTheErrorAskedFor(t *testing.T) {
	h := exception.NewHandler(exception.Config{Dev: true})

	rec := httptest.NewRecorder()
	h.Displayer().Display(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, "no invoice"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the 404 the error asked for", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Displayer().Display(rec, httptest.NewRequest(http.MethodGet, "/", nil), errors.New("nobody claimed this"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an error nobody claimed", rec.Code)
	}
}
