package fakes

import (
	"errors"
	"io"
	"sync"
)

// ExceptionHandler is what an [ExceptionHandlerFake] stands in for, and what
// it forwards to for anything it was not told to intercept.
type ExceptionHandler interface {
	// Report records the error, and returns an error of its own when
	// reporting it failed.
	Report(e error) error
	// ShouldReport reports whether the error is worth recording at all.
	ShouldReport(e error) bool
	// Render turns the error into a response for the given request.
	Render(request any, e error) any
	// RenderForConsole writes the error out for a terminal.
	RenderForConsole(output io.Writer, e error)
}

// WithoutExceptionHandling is satisfied by the handler a test installs when it
// turns exception handling off.
type WithoutExceptionHandling interface {
	// WithoutExceptionHandling reports that this handler is the one a test
	// installs to see every exception.
	WithoutExceptionHandling() bool
}

// ExceptionHandlerFake is the exception handler a test installs so that a
// reported exception is recorded instead of logged, and can be asserted on
// afterwards.
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

// NewExceptionHandlerFake builds a handler that records the named exception
// types and forwards the rest.
//
// With no exception types named, every exception is intercepted. A nil handler
// is allowed: it is only reached for by an exception this fake was not told to
// intercept.
func NewExceptionHandlerFake(handler ExceptionHandler, exceptions ...any) *ExceptionHandlerFake {
	return &ExceptionHandlerFake{handler: handler, exceptions: exceptions}
}

func (f *ExceptionHandlerFake) isFake() {}

// Handler returns the handler the fake stands in for, which may be nil.
func (f *ExceptionHandlerFake) Handler() ExceptionHandler {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handler
}

// AssertReported fails the test unless an exception of the given type was
// reported and passed the truth test. A nil truth test accepts any exception
// of the type.
//
// The type cannot be read off the truth test's parameter at run time, so it is
// named separately: the type first, the truth test second.
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

// AssertReportedCount fails the test unless exactly that many exceptions were
// reported.
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

// AssertNotReported fails the test when an exception of the given type was
// reported and passed the truth test.
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

// AssertNothingReported fails the test unless no exception was reported at
// all.
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

// Report records the exception, or hands it to the real handler when the fake
// was not told to intercept its type.
//
// It returns the exception when [ExceptionHandlerFake.ThrowOnReport] asked for
// that, and nil otherwise.
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

// isFakedException reports whether the fake was told to intercept this
// exception. A fake carrying no list intercepts everything.
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

// ShouldReport lets the real handler decide, unless that handler is the one a
// test installs to see every exception.
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

// Render forwards to the real handler, and returns nil when there is none.
func (f *ExceptionHandlerFake) Render(request any, e error) any {
	handler := f.Handler()
	if handler == nil {
		return nil
	}
	return handler.Render(request, e)
}

// RenderForConsole forwards to the real handler, and does nothing when there
// is none.
func (f *ExceptionHandlerFake) RenderForConsole(output io.Writer, e error) {
	if handler := f.Handler(); handler != nil {
		handler.RenderForConsole(output, e)
	}
}

// ThrowOnReport makes every later [ExceptionHandlerFake.Report] return the
// exception it was handed instead of swallowing it, and returns the fake.
func (f *ExceptionHandlerFake) ThrowOnReport() *ExceptionHandlerFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.throwOnReport = true
	return f
}

// ThrowFirstReported returns the first exception reported, so a test can fail
// with the cause rather than with a symptom. It returns nil when nothing was
// reported.
func (f *ExceptionHandlerFake) ThrowFirstReported() error {
	reported := f.Reported()
	if len(reported) == 0 {
		return nil
	}
	return reported[0]
}

// SetHandler sets the handler the fake forwards to, and returns the fake.
func (f *ExceptionHandlerFake) SetHandler(handler ExceptionHandler) *ExceptionHandlerFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
	return f
}

// Reported returns a copy of the exceptions recorded so far, in the order they
// were reported.
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
