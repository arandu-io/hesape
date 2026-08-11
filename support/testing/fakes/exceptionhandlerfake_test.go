package fakes

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type notFound struct{ Path string }

func (e *notFound) Error() string { return "not found: " + e.Path }

type tooManyRequests struct{}

func (e *tooManyRequests) Error() string { return "too many requests" }

// spyHandler records what the fake forwarded, and can be told to ignore an
// exception the way a real handler ignores one it does not report.
type spyHandler struct {
	mu       sync.Mutex
	reported []error
	rendered []error
	console  []error
	ignore   bool
	without  bool
}

func (h *spyHandler) Report(e error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reported = append(h.reported, e)
	return nil
}

func (h *spyHandler) ShouldReport(e error) bool { return !h.ignore }

func (h *spyHandler) Render(request any, e error) any {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rendered = append(h.rendered, e)
	return "a response"
}

func (h *spyHandler) RenderForConsole(output io.Writer, e error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.console = append(h.console, e)
	fmt.Fprint(output, e.Error())
}

func (h *spyHandler) WithoutExceptionHandling() bool { return h.without }

func TestExceptionHandlerFakeAssertReported(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	handler.Report(&notFound{Path: "/orders/1"})

	r := &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*notFound](), nil)
	assertPasses(t, r)

	r = &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*tooManyRequests](), nil)
	// The message has to carry what was reported instead, and the message of
	// each: two exceptions of one type are told apart by nothing else.
	assertFails(t, r, "tooManyRequests", "1 exception was reported", "not found: /orders/1")
}

func TestExceptionHandlerFakeAssertReportedWithATruthTest(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	handler.Report(&notFound{Path: "/orders/1"})

	r := &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*notFound](), func(e error) bool {
		return e.(*notFound).Path == "/orders/1"
	})
	assertPasses(t, r)

	r = &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*notFound](), func(e error) bool {
		return e.(*notFound).Path == "/orders/2"
	})
	assertFails(t, r, "was not reported")
}

func TestExceptionHandlerFakeAssertNotReportedAndNothingReported(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)

	r := &recorder{}
	handler.AssertNothingReported(r)
	assertPasses(t, r)

	r = &recorder{}
	handler.AssertNotReported(r, reflect.TypeFor[*notFound](), nil)
	assertPasses(t, r)

	r = &recorder{}
	handler.AssertReportedCount(r, 0)
	assertPasses(t, r)

	handler.Report(&notFound{Path: "/a"})

	r = &recorder{}
	handler.AssertNotReported(r, reflect.TypeFor[*notFound](), nil)
	assertFails(t, r, "was reported 1 time")

	r = &recorder{}
	handler.AssertNothingReported(r)
	assertFails(t, r, "the following exception was reported", "fakes.notFound")

	r = &recorder{}
	handler.AssertReportedCount(r, 2)
	assertFails(t, r, "was 1 instead of 2")
}

func TestExceptionHandlerFakeNamesAWrappedCause(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	handler.Report(fmt.Errorf("saving the order: %w", &notFound{Path: "/a"}))

	r := &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*tooManyRequests](), nil)
	// A wrapped cause is the first thing a reader looks for, and errors.Is is
	// how Go spells "the same exception, further up".
	assertFails(t, r, "wrapping [fakes.notFound]")
}

func TestExceptionHandlerFakeOnlyInterceptsTheTypesItWasNamed(t *testing.T) {
	t.Parallel()

	spy := &spyHandler{}
	handler := NewExceptionHandlerFake(spy, reflect.TypeFor[*notFound]())

	handler.Report(&notFound{Path: "/a"})
	handler.Report(&tooManyRequests{})

	if len(spy.reported) != 1 {
		t.Fatalf("the real handler got %d exceptions, want 1", len(spy.reported))
	}
	if _, ok := spy.reported[0].(*tooManyRequests); !ok {
		t.Errorf("the real handler got %T, want *fakes.tooManyRequests", spy.reported[0])
	}

	r := &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*notFound](), nil)
	assertPasses(t, r)

	r = &recorder{}
	handler.AssertNotReported(r, reflect.TypeFor[*tooManyRequests](), nil)
	assertPasses(t, r)
}

func TestExceptionHandlerFakeAsksTheHandlerWhetherToReport(t *testing.T) {
	t.Parallel()

	spy := &spyHandler{ignore: true}
	handler := NewExceptionHandlerFake(spy)
	handler.Report(&notFound{Path: "/a"})

	r := &recorder{}
	handler.AssertNothingReported(r)
	assertPasses(t, r)

	// A handler installed to see every exception overrides that, which is what
	// a test that turned exception handling off asked for.
	spy.without = true
	handler.Report(&notFound{Path: "/a"})

	r = &recorder{}
	handler.AssertReported(r, reflect.TypeFor[*notFound](), nil)
	assertPasses(t, r)
}

func TestExceptionHandlerFakeThrowOnReport(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	original := &notFound{Path: "/a"}

	if got := handler.Report(original); got != nil {
		t.Errorf("Report answered %v, want nothing until ThrowOnReport", got)
	}

	handler.ThrowOnReport()
	got := handler.Report(original)
	if !errors.Is(got, error(original)) {
		t.Errorf("Report answered %v, want the exception it was handed", got)
	}
}

func TestExceptionHandlerFakeThrowFirstReported(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	if got := handler.ThrowFirstReported(); got != nil {
		t.Errorf("ThrowFirstReported with nothing reported = %v, want nothing", got)
	}

	first := &notFound{Path: "/a"}
	handler.Report(first)
	handler.Report(&tooManyRequests{})

	if got := handler.ThrowFirstReported(); !errors.Is(got, error(first)) {
		t.Errorf("ThrowFirstReported = %v, want the first exception reported", got)
	}
}

func TestExceptionHandlerFakeForwardsRendering(t *testing.T) {
	t.Parallel()

	spy := &spyHandler{}
	handler := NewExceptionHandlerFake(spy)

	if got := handler.Render("a request", &notFound{Path: "/a"}); got != "a response" {
		t.Errorf("Render = %v, want what the real handler answered", got)
	}

	out := &strings.Builder{}
	handler.RenderForConsole(out, &notFound{Path: "/a"})
	if out.String() != "not found: /a" {
		t.Errorf("RenderForConsole wrote %q, want what the real handler writes", out.String())
	}

	// A fake with no handler behind it answers rather than panicking.
	alone := NewExceptionHandlerFake(nil)
	if alone.Render("a request", &notFound{}) != nil {
		t.Error("a fake with no handler should render nothing")
	}
	alone.RenderForConsole(io.Discard, &notFound{})
}

func TestExceptionHandlerFakeSetHandlerAndHandler(t *testing.T) {
	t.Parallel()

	first := &spyHandler{}
	second := &spyHandler{}
	handler := NewExceptionHandlerFake(first)

	if handler.Handler() != ExceptionHandler(first) {
		t.Error("Handler should answer the handler the fake was built with")
	}
	handler.SetHandler(second)
	if handler.Handler() != ExceptionHandler(second) {
		t.Error("SetHandler should replace the handler")
	}
}

func TestExceptionHandlerFakeIsSafeInParallel(t *testing.T) {
	t.Parallel()

	handler := NewExceptionHandlerFake(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler.Report(&notFound{Path: "/a"})
			handler.Reported()
		}()
	}
	wg.Wait()

	r := &recorder{}
	handler.AssertReportedCount(r, 50)
	assertPasses(t, r)
}
