package exception_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/log"
)

// reporting returns a handler with a captured logger wired into a context.
func reporting(t *testing.T, cfg exception.Config) (*exception.Handler, context.Context, *log.Records) {
	t.Helper()
	logger, lines := log.Capture()
	return exception.NewHandler(cfg), log.Into(context.Background(), logger), lines
}

func TestAbortIfOnlyFailsWhenTheConditionHolds(t *testing.T) {
	if err := exception.AbortIf(false, http.StatusConflict, "closed"); err != nil {
		t.Fatalf("AbortIf(false) = %v, want nil", err)
	}

	err := exception.AbortIf(true, http.StatusConflict, "closed")
	if status, known := exception.StatusOf(err); !known || status != http.StatusConflict {
		t.Fatalf("StatusOf = %d (%v), want 409", status, known)
	}
}

func TestAbortUnlessIsTheOtherWayRound(t *testing.T) {
	if err := exception.AbortUnless(true, http.StatusNotFound, ""); err != nil {
		t.Fatalf("AbortUnless(true) = %v, want nil", err)
	}

	err := exception.AbortUnless(false, http.StatusNotFound, "")
	if status, known := exception.StatusOf(err); !known || status != http.StatusNotFound {
		t.Fatalf("StatusOf = %d (%v), want 404", status, known)
	}
}

func TestIgnoreAndStopIgnoring(t *testing.T) {
	quiet := errors.New("the client hung up")
	h, ctx, lines := reporting(t, exception.Config{})

	h.Ignore(quiet)
	if h.ShouldReport(quiet) {
		t.Fatal("ShouldReport said yes about an error that was ignored")
	}
	h.Report(ctx, quiet)
	if lines.Len() != 0 {
		t.Fatalf("an ignored error was reported: %v", lines.All())
	}

	h.StopIgnoring(quiet)
	if !h.ShouldReport(quiet) {
		t.Fatal("StopIgnoring did not put the error back")
	}
	h.Report(ctx, quiet)
	if lines.Len() != 1 {
		t.Fatalf("%d lines, want the error reported once it stopped being ignored", lines.Len())
	}
}

func TestDontReportIsTheAliasOfIgnore(t *testing.T) {
	quiet := errors.New("quiet")
	h, ctx, lines := reporting(t, exception.Config{})

	h.DontReport(quiet)
	h.Report(ctx, quiet)

	if lines.Len() != 0 {
		t.Fatalf("DontReport did not silence the error: %v", lines.All())
	}
}

func TestDontReportDuplicates(t *testing.T) {
	failure := errors.New("the same failure twice")
	h, ctx, lines := reporting(t, exception.Config{})

	h.DontReportDuplicates()
	h.Report(ctx, failure)
	h.Report(ctx, failure)

	if lines.Len() != 1 {
		t.Fatalf("%d lines, want the duplicate dropped", lines.Len())
	}
}

// declined is an error type a callback can be written against.
type declined struct{ Reason string }

func (d *declined) Error() string { return "payment declined: " + d.Reason }

func TestReportableHandlesOnlyItsOwnType(t *testing.T) {
	var caught []string
	h, ctx, lines := reporting(t, exception.Config{})

	h.Reportable(func(err *declined) bool {
		caught = append(caught, err.Reason)
		return false
	})

	h.Report(ctx, &declined{Reason: "expired card"})
	h.Report(ctx, errors.New("something else"))

	if len(caught) != 1 || caught[0] != "expired card" {
		t.Fatalf("caught = %v, want only the declined error", caught)
	}
	if lines.Len() != 1 {
		t.Fatalf("%d lines, want the callback that returned false to have kept its error out of the log", lines.Len())
	}
}

func TestReportableStopStopsTheLog(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})

	h.Reportable(func(*declined) {}).Stop()
	h.Report(ctx, &declined{Reason: "expired card"})

	if lines.Len() != 0 {
		t.Fatalf("Stop did not stop the reporting: %v", lines.All())
	}
}

// reporting itself is what the error knows how to do here.
type selfReporting struct{ reported *bool }

func (s *selfReporting) Error() string { return "reported itself" }
func (s *selfReporting) Report() bool  { *s.reported = true; return true }

func TestAnErrorThatReportsItselfIsNotLoggedAgain(t *testing.T) {
	var reported bool
	h, ctx, lines := reporting(t, exception.Config{})

	h.Report(ctx, &selfReporting{reported: &reported})

	if !reported {
		t.Fatal("the error's own Report was not called")
	}
	if lines.Len() != 0 {
		t.Fatalf("the error was logged as well as reporting itself: %v", lines.All())
	}
}

func TestMapTurnsOneErrorIntoAnother(t *testing.T) {
	missing := errors.New("no rows")
	h := exception.NewHandler(exception.Config{})
	h.Map(missing, func(error) error { return exception.Abort(http.StatusNotFound, "no invoice with that number") })

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/invoices/1", nil), missing)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the mapped 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no invoice with that number") {
		t.Fatalf("the mapped message is not on the page: %s", rec.Body.String())
	}
}

func TestLevelOverridesTheLevelTheStatusWouldPick(t *testing.T) {
	noisy := errors.New("the third-party API was slow again")
	h, ctx, lines := reporting(t, exception.Config{})

	h.Level(noisy, slog.LevelWarn)
	h.Report(ctx, noisy)

	all := lines.All()
	if len(all) != 1 || all[0].Level != slog.LevelWarn {
		t.Fatalf("lines = %v, want one line at WARN", all)
	}
}

func TestBuildContextUsingPutsFieldsOnTheLine(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})

	h.BuildContextUsing(func(error, map[string]any) map[string]any {
		return map[string]any{"tenant": "acme"}
	})
	h.Report(ctx, errors.New("failed"))

	all := lines.All()
	if len(all) != 1 || all[0].Attrs["tenant"] != "acme" {
		t.Fatalf("attrs = %v, want the context callback's field", all)
	}
}

func TestRenderableAnswersBeforeTheBuiltInPages(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.Renderable(func(err *declined, w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(err.Reason))
		return true
	})

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), &declined{Reason: "expired card"})

	if rec.Code != http.StatusPaymentRequired || rec.Body.String() != "expired card" {
		t.Fatalf("status = %d, body = %q, want the callback's answer", rec.Code, rec.Body.String())
	}
}

func TestRenderableThatDeclinesLetsTheBuiltInPageAnswer(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.Renderable(func(*declined, http.ResponseWriter, *http.Request) bool { return false })

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, ""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the built-in 404", rec.Code)
	}
}

func TestShouldRenderJSONWhenWinsOverTheDefault(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.ShouldRenderJSONWhen(func(*http.Request, error) bool { return true })

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, ""))

	if got := rec.Header().Get("Content-Type"); got != exception.ProblemContentType {
		t.Fatalf("Content-Type = %q, want the problem document", got)
	}
}

func TestRespondUsingGetsTheLastWord(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.RespondUsing(func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("X-Answered-By", "the application")
	})

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, ""))

	if got := rec.Header().Get("X-Answered-By"); got != "the application" {
		t.Fatalf("X-Answered-By = %q, want the callback to have run", got)
	}
}

func TestDontFlashNeverCarriesTheSecretsBack(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.DontFlash("card_number")

	kept := h.FlashableInput(map[string]any{
		"email":       "a@b.c",
		"password":    "hunter2",
		"card_number": "4111",
	})

	if len(kept) != 1 || kept["email"] != "a@b.c" {
		t.Fatalf("kept = %v, want only the email", kept)
	}
}

func TestThrottleUsingCapsHowOftenAnErrorIsReported(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})

	h.ThrottleUsing(func(*declined) exception.Throttle {
		return exception.Throttle{Key: "declined", MaxAttempts: 2, Decay: time.Minute}
	})

	for range 5 {
		h.Report(ctx, &declined{Reason: "expired card"})
	}

	if lines.Len() != 2 {
		t.Fatalf("%d lines, want the throttle to have stopped at two", lines.Len())
	}
}

// TestAThrottleOfNoAttemptsNeverReports: MaxAttempts was read as "no throttle
// asked for" and the error was reported every time -- the opposite answer.
func TestAThrottleOfNoAttemptsNeverReports(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})

	h.ThrottleUsing(func(*declined) exception.Throttle {
		return exception.Throttle{Key: "silenced"}
	})

	for range 3 {
		h.Report(ctx, &declined{Reason: "expired card"})
	}

	if lines.Len() != 0 {
		t.Fatalf("%d lines, want none: a throttle of no attempts reports nothing", lines.Len())
	}
}

// TestAnUnlimitedThrottleReportsEveryTime pins the fallback used when no
// throttle callback answered.
func TestAnUnlimitedThrottleReportsEveryTime(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})

	h.ThrottleUsing(func(*declined) exception.Throttle { return exception.Unlimited })

	for range 3 {
		h.Report(ctx, &declined{Reason: "expired card"})
	}

	if lines.Len() != 3 {
		t.Fatalf("%d lines, want all three", lines.Len())
	}
}

// causes is an error that carries several others, which makes it a struct with
// a slice in it -- and a value of that type cannot be compared with ==.
//
// Go's way of asking "is this that error" about a type like this is an Is
// method.
type causes struct{ of []error }

func (c causes) Error() string { return "several things went wrong" }

func (c causes) Is(target error) bool { _, ok := target.(causes); return ok }

// TestIgnoreAcceptsAnErrorThatCannotBeCompared: the list membership test was
// item == target, and == on two interfaces panics when their dynamic type is the
// same and not comparable. Two errors of one such type took the process down
// inside Ignore -- while reportThrowable next door already guarded against
// exactly this.
func TestIgnoreAcceptsAnErrorThatCannotBeCompared(t *testing.T) {
	first := causes{of: []error{errors.New("a")}}
	second := causes{of: []error{errors.New("b")}}

	h, ctx, lines := reporting(t, exception.Config{})
	h.Ignore(first)
	h.Ignore(second)

	h.Report(ctx, first)
	h.Report(ctx, second)
	if lines.Len() != 0 {
		t.Fatalf("%d lines, want both ignored: %v", lines.Len(), lines.All())
	}

	h.StopIgnoring(first)
	h.Report(ctx, first)
	if lines.Len() != 1 {
		t.Fatalf("%d lines, want the error back in the log once it stopped being ignored", lines.Len())
	}
}

// TestLevelRegisteredTwiceKeepsTheLast: the levels were a list read from the
// front, so the first registration won.
func TestLevelRegisteredTwiceKeepsTheLast(t *testing.T) {
	noisy := errors.New("the third-party API was slow again")
	h, ctx, lines := reporting(t, exception.Config{})

	h.Level(noisy, slog.LevelWarn)
	h.Level(noisy, slog.LevelDebug)
	h.Report(ctx, noisy)

	all := lines.All()
	if len(all) != 1 || all[0].Level != slog.LevelDebug {
		t.Fatalf("lines = %v, want one line at DEBUG, which is what was registered last", all)
	}
}

// TestRenderDoesNotReport: it reported on the way in.
func TestRenderDoesNotReport(t *testing.T) {
	h, ctx, lines := reporting(t, exception.Config{})
	failure := exception.Abort(http.StatusConflict, "this invoice is closed")

	h.Report(ctx, failure)
	h.Render(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx), failure)

	if lines.Len() != 1 {
		t.Fatalf("%d lines, want the one the caller asked for: %v", lines.Len(), lines.All())
	}
}

// TestRespondUsingReachesTheClient: the callback ran after the response had been
// written, and a header set after WriteHeader never leaves the process. The
// recorder's Result() is the snapshot taken when the status line went out, which
// is what the client actually got.
func TestRespondUsingReachesTheClient(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.RespondUsing(func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("X-Answered-By", "the application")
	})

	rec := httptest.NewRecorder()
	h.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), exception.Abort(http.StatusNotFound, ""))

	if got := rec.Result().Header.Get("X-Answered-By"); got != "the application" {
		t.Fatalf("the client got X-Answered-By = %q, want the callback's header", got)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the page to have answered as well", rec.Code)
	}
}
