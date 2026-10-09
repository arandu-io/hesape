package exception

import (
	"errors"
	"net/http"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

// StatusPageExpired is 419, which is not in any RFC.
//
// 403 would be the standard answer and it is the wrong one: it says the
// account may not do this, when the account may, and the form is simply old.
const StatusPageExpired = 419

// HTTPError is an error that names the answer it wants.
//
// It is what Abort returns and the only thing an application uses to choose a
// status. The Message is shown to whoever made the request, in every
// environment, so it is written for them: it is the developer's own sentence.
type HTTPError struct {
	// Status is the HTTP status to answer with.
	Status int
	// Message is the sentence the person sees. Empty means the standard text
	// for the status.
	Message string
	// Err is the cause, when there was one. It is reported and never shown.
	Err error
	// Headers are what the answer carries besides the status: the Retry-After
	// of a 429, the WWW-Authenticate of a 401.
	//
	// Nothing here had it, so a 429 went out with no Retry-After and a client had
	// nothing to obey. Abort does not take them -- the common failure has none --
	// so an answer that carries headers is written as the value it is:
	//
	//	&exception.HTTPError{
	//		Status:  http.StatusTooManyRequests,
	//		Headers: http.Header{"Retry-After": {"30"}},
	//	}
	Headers http.Header
}

// Error is what makes an *HTTPError satisfy the error interface.
//
// It carries the status because this string ends up in a log line, where the
// number is the first thing anybody looks for.
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

// Unwrap exposes the cause, so errors.Is and errors.As reach through it.
func (e *HTTPError) Unwrap() error { return e.Err }

// HTTPStatus reports the status the error asks for.
//
// It is the method a routing layer looks for on any error, through errors.As
// over an interface rather than over this type, so an *HTTPError is answered
// with its own status by code that never imports this package. It returns
// Status as written: what to do with a value outside 400-599 is the caller's
// decision.
func (e *HTTPError) HTTPStatus() int { return e.Status }

// Abort builds a failure as a value rather than raising one.
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

// AbortIf is the abort_if() helper.
//
//	if err := exception.AbortIf(invoice.Locked, http.StatusConflict, "this invoice is closed"); err != nil {
//		return err
//	}
//
// Nothing here throws, so the caller returns the error -- which is why this
// reads as one line and not as the same if statement written twice.
func AbortIf(condition bool, status int, message string) error {
	if !condition {
		return nil
	}
	return Abort(status, message)
}

// AbortUnless is the abort_unless() helper.
//
//	if err := exception.AbortUnless(invoice != nil, http.StatusNotFound, ""); err != nil {
//		return err
//	}
func AbortUnless(condition bool, status int, message string) error {
	return AbortIf(!condition, status, message)
}

// StatusOf reads an error chain and answers two things at once: the HTTP status
// the error asks for, and whether it asked at all.
//
// It is the one table: every adapter that turns an error into a response asks
// it, the router's and this package's Handler alike, so the same error is
// answered with the same status wherever it surfaces. A second table beside it
// would be a second answer for the same error, and the failure that produces
// is invisible when one of them is wrong.
//
// The list is closed, and the order is the order below, first match wins:
//
//	an error with a method HTTPStatus() int        that status
//	validation.Errors holding at least one message  422
//	database.ErrRecordNotFound                      404
//	auth.ErrForbidden                               403
//	session.ErrTokenMismatch                        419
//	database.ErrUniqueViolation                     409
//
// The first row is how an error states its own status: an *HTTPError, which
// Abort builds; a *validation.ValidationException; the errors of
// hesape/http/exceptions, which is how a body over the size limit arrives as
// 413; and an application's own domain failure. It is matched by method set,
// so the type needs no import of this package, and it wins over the rows below
// because it is the explicit statement -- the sentinel under it may be the
// cause it wrapped. The status is returned as written, even outside 400-599:
// what to do with one that is not an error status is the caller's decision.
//
// validation.Errors with no message in it is not claimed. A handler that
// returned one did not ask whether anything failed, and a 422 with nothing to
// correct is the failure answering a rejected form exists to remove.
//
// model.ErrModelNotFound, and every other "not found" under database, answers
// errors.Is for database.ErrRecordNotFound, so all of them are the 404 row.
// auth.ErrForbidden is 403 rather than 404: the tenant filter has already
// decided what exists at all, so a status does not hide a resource. A token
// that does not match is 419, because the account may do this and the page is
// simply old. A duplicate key is 409, because it is the request that collided
// with a row already there; the engine decides it from its own error code,
// never from the message.
//
// errors.Is and errors.As walk the chain, so a sentinel wrapped with
// fmt.Errorf("loading invoice %d: %w", id, err) keeps its status, and the
// context it was wrapped with stays out of the answer.
//
// False means nobody claimed the error, which is a 500 and, in development,
// the debug page.
func StatusOf(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	var stated interface{ HTTPStatus() int }
	if errors.As(err, &stated) {
		return stated.HTTPStatus(), true
	}

	var rejected validation.Errors
	if errors.As(err, &rejected) && rejected.Any() {
		return http.StatusUnprocessableEntity, true
	}

	switch {
	case errors.Is(err, database.ErrRecordNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, auth.ErrForbidden):
		return http.StatusForbidden, true
	case errors.Is(err, session.ErrTokenMismatch):
		return StatusPageExpired, true
	case errors.Is(err, database.ErrUniqueViolation):
		return http.StatusConflict, true
	}
	return 0, false
}
