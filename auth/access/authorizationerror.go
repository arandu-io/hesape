package access

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// AuthorizationError is the failure a refused authorization answers with.
//
// It answers auth.ErrForbidden. That is not decoration: exception.StatusOf reads
// errors.Is(err, auth.ErrForbidden) to choose 403, and an authorization failure
// that did not answer it would be rendered as a 500 -- the one denial in the
// framework that leaks a stack trace to whoever was denied.
type AuthorizationError struct {
	message  string
	code     any
	previous error
	response *Response

	// status is a pointer because no status at all is not the same as 0:
	// HasStatus is the question the exception handler asks before choosing a
	// status of its own.
	status *int
}

// DefaultDenialMessage is what an error built with no message says.
const DefaultDenialMessage = "This action is unauthorized."

// NewAuthorizationError returns the failure.
//
// An empty message becomes [DefaultDenialMessage], and a nil code becomes 0.
func NewAuthorizationError(message string, code any, previous error) *AuthorizationError {
	if message == "" {
		message = DefaultDenialMessage
	}
	if code == nil {
		code = 0
	}

	return &AuthorizationError{message: message, code: code, previous: previous}
}

// Error is the sentence the failure answers with.
func (e *AuthorizationError) Error() string { return e.message }

// Unwrap exposes the cause behind this failure to errors.Is and errors.As.
func (e *AuthorizationError) Unwrap() error { return e.previous }

// Is reports that every AuthorizationError is auth.ErrForbidden.
//
// There is one authorization error in this framework and handlers translate it
// to 403. This is the Gate's refusal joining it rather than opening a second
// one that every handler would have to learn about separately.
func (e *AuthorizationError) Is(target error) bool { return target == auth.ErrForbidden }

// Code is the response code or reason the failure carries.
func (e *AuthorizationError) Code() any { return e.code }

// Response is the response the Gate produced, when the failure came from one.
// It is nil otherwise.
func (e *AuthorizationError) Response() *Response { return e.response }

// SetResponse attaches that response, and returns the failure.
func (e *AuthorizationError) SetResponse(response *Response) *AuthorizationError {
	e.response = response

	return e
}

// WithStatus sets the HTTP status the failure carries. A nil pointer clears it.
func (e *AuthorizationError) WithStatus(status *int) *AuthorizationError {
	e.status = status

	return e
}

// AsNotFound sets that status to 404.
func (e *AuthorizationError) AsNotFound() *AuthorizationError {
	status := http.StatusNotFound

	return e.WithStatus(&status)
}

// HasStatus reports that a status was set at all.
func (e *AuthorizationError) HasStatus() bool { return e.status != nil }

// Status is that status, and nil when none was set.
func (e *AuthorizationError) Status() *int { return e.status }

// ToResponse is the denial this failure stands for, built again from the
// message, the code and the status.
//
// It is what [Gate.Inspect] calls when an ability answers with a failure
// instead of a Response.
func (e *AuthorizationError) ToResponse() *Response {
	return Deny(e.message, e.code).WithStatus(e.status)
}

// withPrevious records the cause behind this failure. It is unexported and not
// a constructor argument because the cause -- the sentence auth.Authorize
// builds, naming the action and the subject -- only exists after the
// AuthorizationError does.
func (e *AuthorizationError) withPrevious(previous error) *AuthorizationError {
	e.previous = previous

	return e
}
