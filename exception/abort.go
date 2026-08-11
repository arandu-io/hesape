package exception

import (
	"errors"
	"net/http"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
)

// StatusPageExpired is 419, which is not in any RFC.
//
// Laravel answers an expired or missing CSRF token with it, and the whole
// ecosystem -- browsers, proxies, monitoring dashboards, the person reading the
// access log -- has learned to read 419 as "your page went stale, reload it".
// 403 would be the standard answer and it is the wrong one: it says the account
// may not do this, when the account may, and the form is simply old.
const StatusPageExpired = 419

// HTTPError is an error that names the answer it wants.
//
// It is what Abort returns and the only thing an application uses to choose a
// status. The Message is shown to whoever made the request, in every
// environment, so it is written for them: it is the developer's own sentence,
// the way Laravel's abort(403, '...') is, and never a driver's error text.
type HTTPError struct {
	// Status is the HTTP status to answer with.
	Status int
	// Message is the sentence the person sees. Empty means the standard text
	// for the status.
	Message string
	// Err is the cause, when there was one. It is reported and never shown.
	Err error
}

// Error implements error. It carries the status because this string ends up in
// a log line, where the number is the first thing anybody looks for.
func (e *HTTPError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = statusMessage(e.Status)
	}
	out := statusTitle(e.Status) + ": " + msg
	if e.Err != nil {
		out += ": " + e.Err.Error()
	}
	return out
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *HTTPError) Unwrap() error { return e.Err }

// Abort is Laravel's abort() helper, as a value.
//
//	return exception.Abort(http.StatusNotFound, "no invoice with that number")
//
// There is no method on the request context, which is where the audit found the
// previous attempt at this -- a helper nothing could reach is a helper that does
// not exist. An error is reachable from every handler, from a service three
// calls down, and from a job.
//
// An empty message means the standard sentence for the status, so the common
// case is exception.Abort(404, "").
//
// To carry a cause, wrap it: fmt.Errorf("loading invoice: %w", exception.Abort(...))
// keeps the status -- StatusOf walks the chain -- and puts the context in the
// log without putting it on the page.
func Abort(status int, message string) error {
	return &HTTPError{Status: status, Message: message}
}

// AbortIf is Laravel's abort_if(): the failure when the condition holds, and
// nil when it does not.
//
//	if err := exception.AbortIf(invoice.Locked, http.StatusConflict, "this invoice is closed"); err != nil {
//		return err
//	}
//
// The PHP throws, so its body ends at the call. Nothing here throws, so the
// caller returns the error -- which is why this reads as one line and not as
// the same if statement written twice.
func AbortIf(condition bool, status int, message string) error {
	if !condition {
		return nil
	}
	return Abort(status, message)
}

// AbortUnless is Laravel's abort_unless(): the failure when the condition does
// not hold.
//
//	if err := exception.AbortUnless(invoice != nil, http.StatusNotFound, ""); err != nil {
//		return err
//	}
func AbortUnless(condition bool, status int, message string) error {
	return AbortIf(!condition, status, message)
}

// StatusOf reports the HTTP status an error asks for, and whether it asked.
//
// It is what the routing layer calls with whatever a controller action
// returned. False means nobody claimed the error, which is a 500 and, in
// development, the debug page.
func StatusOf(err error) (int, bool) { return classify(err) }

// classify is the closed table of what the collection's own errors mean.
//
// Closed is the point (RULE 15). Every entry here is a sentinel this collection
// declares, so the list cannot grow with an application's own errors -- those
// say what they want with Abort, which is the one mechanism, and cannot end up
// with two ways to mean 403.
//
// Order matters: an *HTTPError anywhere in the chain wins, because it is the
// explicit statement and the sentinel below it may be the cause it wrapped.
func classify(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status, true
	}

	switch {
	// A policy refused, or a repository was reached without a Grant. Both are
	// auth.ErrForbidden, and both are 403 rather than 404: this collection does
	// not hide the existence of a resource behind a status, because the tenant
	// filter has already decided what exists at all (RULE 14).
	case errors.Is(err, auth.ErrForbidden):
		return http.StatusForbidden, true
	case errors.Is(err, session.ErrTokenMismatch):
		return StatusPageExpired, true
	}

	return 0, false
}
