package fakes

import (
	"errors"
	"io"
	"sync"
)

// ExceptionHandler is Illuminate\Contracts\Debug\ExceptionHandler as this
// ecosystem spells it: what an ExceptionHandlerFake stands in for, and what it
// forwards to for anything it was not told to intercept.
//
// Report answers an error rather than nothing, because the PHP handler may
// throw out of report() -- which is what ExceptionHandlerFake::throwOnReport
// makes it do -- and Go returns what PHP throws.
type ExceptionHandler interface {
	// Report answers ExceptionHandler::report.
	Report(e error) error
	// ShouldReport answers ExceptionHandler::shouldReport.
	ShouldReport(e error) bool
	// Render answers ExceptionHandler::render.
	Render(request any, e error) any
	// RenderForConsole answers ExceptionHandler::renderForConsole. The PHP
	// takes a Symfony OutputInterface; here it is the writer that stands for
	// the console.
	RenderForConsole(output io.Writer, e error)
}

// WithoutExceptionHandling is answered by the handler a test installs when it
// turns exception handling off, and it answers
// Illuminate\Foundation\Testing\Concerns\WithoutExceptionHandlingHandler.
//
// The PHP asks `$this->handler instanceof WithoutExceptionHandlingHandler` and,
// when it is, reports everything regardless of what the handler would ignore --
// because a test that turned handling off wants to see the exception.
type WithoutExceptionHandling interface {
	// WithoutExceptionHandling answers that this handler is the one a test
	// installs to see every exception.
	WithoutExceptionHandling() bool
}

// ExceptionHandlerFake answers
// Illuminate\Support\Testing\Fakes\ExceptionHandlerFake: the exception handler
// a test installs so that a reported exception is recorded instead of logged,
// and can be asserted on afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex.
type ExceptionHandlerFake struct {
	mu            sync.Mutex
	handler       ExceptionHandler
	exceptions    []any
	reported      []error
	throwOnReport bool
}

// NewExceptionHandlerFake answers ExceptionHandlerFake::__construct.
//
// With no exception types named, every exception is intercepted, which is what
// an empty $exceptions means there. A nil handler is allowed: it is only
// reached for by an exception this fake was not told to intercept.
func NewExceptionHandlerFake(handler ExceptionHandler, exceptions ...any) *ExceptionHandlerFake {
	return &ExceptionHandlerFake{handler: handler, exceptions: exceptions}
}

func (f *ExceptionHandlerFake) isFake() {}

// Handler answers ExceptionHandlerFake::handler: the handler the fake stands in
// for.
func (f *ExceptionHandlerFake) Handler() ExceptionHandler {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handler
}

// AssertReported answers ExceptionHandlerFake::assertReported: an exception of
// the given type was reported.
//
// The type is named the way Go names one, with reflect.TypeFor[*NotFound]()
// where the PHP writes NotFound::class; a value of the type works too, and a
// func(e error) bool is the closure form, which the PHP reads the parameter
// type off. Go cannot read a parameter type at runtime, so a truth test that
// should also check the type takes the type beside it: pass both, with the
// type first and the truth test in the callback slot.
func (f *ExceptionHandlerFake) AssertReported(t TestingT, exception any, callback func(e error) bool) {
	t.Helper()

	reported := f.Reported()
	for _, e := range reported {
		if !instanceOf(e, exception) {
			continue
		}
		if callback != nil && !callback(e) {
			continue
		}
		return
	}

	t.Errorf(
		"AssertReported: the expected [%s] exception was not reported. %s reported:%s",
		tokenName(exception), countedWere(len(reported), "exception"), listOf(describeErrors(reported)),
	)
}

// AssertReportedCount answers ExceptionHandlerFake::assertReportedCount.
func (f *ExceptionHandlerFake) AssertReportedCount(t TestingT, count int) {
	t.Helper()

	reported := f.Reported()
	if len(reported) == count {
		return
	}

	t.Errorf(
		"AssertReportedCount: the total number of exceptions reported was %d instead of %d:%s",
		len(reported), count, listOf(describeErrors(reported)),
	)
}

// AssertNotReported answers ExceptionHandlerFake::assertNotReported.
func (f *ExceptionHandlerFake) AssertNotReported(t TestingT, exception any, callback func(e error) bool) {
	t.Helper()

	var found []error
	for _, e := range f.Reported() {
		if !instanceOf(e, exception) {
			continue
		}
		if callback != nil && !callback(e) {
			continue
		}
		found = append(found, e)
	}
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotReported: the expected [%s] exception was reported %d %s:%s",
		tokenName(exception), len(found), plural("time", len(found)), listOf(describeErrors(found)),
	)
}

// AssertNothingReported answers ExceptionHandlerFake::assertNothingReported.
func (f *ExceptionHandlerFake) AssertNothingReported(t TestingT) {
	t.Helper()

	reported := f.Reported()
	if len(reported) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingReported: the following %s reported:%s",
		plural("exception was", len(reported)), listOf(describeErrors(reported)),
	)
}

// Report answers ExceptionHandlerFake::report: the exception is recorded, or
// handed to the real handler when this fake was not told to intercept its type.
//
// It answers the exception when ThrowOnReport asked for it, which is what the
// PHP throws there; nil otherwise.
func (f *ExceptionHandlerFake) Report(e error) error {
	if !f.isFakedException(e) {
		if handler := f.Handler(); handler != nil {
			return handler.Report(e)
		}
		return nil
	}

	if !f.ShouldReport(e) {
		return nil
	}

	f.mu.Lock()
	f.reported = append(f.reported, e)
	shouldThrow := f.throwOnReport
	f.mu.Unlock()

	if shouldThrow {
		return e
	}
	return nil
}

// isFakedException answers ExceptionHandlerFake::isFakedException: a fake
// without a list intercepts everything.
func (f *ExceptionHandlerFake) isFakedException(e error) bool {
	f.mu.Lock()
	exceptions := append([]any(nil), f.exceptions...)
	f.mu.Unlock()

	if len(exceptions) == 0 {
		return true
	}
	for _, token := range exceptions {
		if instanceOf(e, token) {
			return true
		}
	}
	return false
}

// ShouldReport answers ExceptionHandlerFake::shouldReport: the real handler
// decides, unless it is the one a test installs to see every exception.
//
// A fake with no handler behind it reports everything, because there is nobody
// left to say otherwise and an exception silently dropped by a fake is the
// worst answer of the three.
func (f *ExceptionHandlerFake) ShouldReport(e error) bool {
	handler := f.Handler()
	if handler == nil {
		return true
	}
	if without, ok := handler.(WithoutExceptionHandling); ok && without.WithoutExceptionHandling() {
		return true
	}
	return handler.ShouldReport(e)
}

// Render answers ExceptionHandlerFake::render, forwarded to the real handler.
func (f *ExceptionHandlerFake) Render(request any, e error) any {
	handler := f.Handler()
	if handler == nil {
		return nil
	}
	return handler.Render(request, e)
}

// RenderForConsole answers ExceptionHandlerFake::renderForConsole, forwarded.
func (f *ExceptionHandlerFake) RenderForConsole(output io.Writer, e error) {
	if handler := f.Handler(); handler != nil {
		handler.RenderForConsole(output, e)
	}
}

// ThrowOnReport answers ExceptionHandlerFake::throwOnReport: from here on,
// Report answers the exception it was handed instead of swallowing it.
func (f *ExceptionHandlerFake) ThrowOnReport() *ExceptionHandlerFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.throwOnReport = true
	return f
}

// ThrowFirstReported answers ExceptionHandlerFake::throwFirstReported: the
// first exception reported, so that a test can fail with the cause rather than
// with a symptom. It answers nil when nothing was reported, which is where the
// PHP's loop falls through and returns self.
func (f *ExceptionHandlerFake) ThrowFirstReported() error {
	reported := f.Reported()
	if len(reported) == 0 {
		return nil
	}
	return reported[0]
}

// SetHandler answers ExceptionHandlerFake::setHandler.
func (f *ExceptionHandlerFake) SetHandler(handler ExceptionHandler) *ExceptionHandlerFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
	return f
}

// Reported answers ExceptionHandlerFake::$reported, the exceptions recorded so
// far, in the order they were reported.
//
// PHP reads the protected property from the assertions inside the class; a Go
// test that wants to look at what was reported has no other way in, and a fake
// whose records can only be asserted on is a fake that fails opaquely.
func (f *ExceptionHandlerFake) Reported() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]error(nil), f.reported...)
}

// describeErrors renders the exceptions a failure message ends with: the type
// and the message, because two exceptions of one type are told apart by the
// message and by nothing else.
func describeErrors(reported []error) []string {
	lines := make([]string, 0, len(reported))
	for _, e := range reported {
		line := "[" + className(e) + "]"
		if e != nil && e.Error() != "" {
			line += " " + e.Error()
		}
		if unwrapped := errors.Unwrap(e); unwrapped != nil {
			line += " (wrapping [" + className(unwrapped) + "])"
		}
		lines = append(lines, line)
	}
	return lines
}
