package access

import (
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// AuthorizationError is Illuminate\Auth\Access\AuthorizationException.
//
// The name carries the second mechanical change of ADR 0044: PHP throws, Go
// returns an error, and a Go type that implements error is called an Error. The
// methods keep the PHP names.
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

	// status is int|null in PHP, and the null matters: HasStatus is the question
	// the exception handler asks before choosing a status of its own.
	status *int
}

// DefaultDenialMessage is the message the PHP constructor falls back to when it
// is given none: 'This action is unauthorized.'
const DefaultDenialMessage = "This action is unauthorized."

// NewAuthorizationError is the constructor, __construct($message, $code, $previous).
//
// An empty message becomes DefaultDenialMessage and a nil code becomes 0, which
// is what `$message ?? '...'` and `$code ?: 0` do in the PHP.
func NewAuthorizationError(message string, code any, previous error) *AuthorizationError {
	if message == "" {
		message = DefaultDenialMessage
	}
	if code == nil {
		code = 0
	}

	return &AuthorizationError{message: message, code: code, previous: previous}
}

// Error implements error with the exception's message, which is Exception::getMessage.
func (e *AuthorizationError) Error() string { return e.message }

// Unwrap exposes the $previous of the constructor to errors.Is and errors.As.
func (e *AuthorizationError) Unwrap() error { return e.previous }

// Is reports that every AuthorizationError is auth.ErrForbidden.
//
// There is one authorization error in this framework and handlers translate it
// to 403 (RULE 9). This is the Gate's refusal joining it rather than opening a
// second one that every handler would have to learn about separately.
func (e *AuthorizationError) Is(target error) bool { return target == auth.ErrForbidden }

// Code is Exception::getCode, which the PHP constructor fills from its $code
// argument and toResponse reads back.
func (e *AuthorizationError) Code() any { return e.code }

// Response is AuthorizationException::response: the response the Gate produced,
// when the failure came from one. It is nil otherwise.
func (e *AuthorizationError) Response() *Response { return e.response }

// SetResponse is AuthorizationException::setResponse.
func (e *AuthorizationError) SetResponse(response *Response) *AuthorizationError {
	e.response = response

	return e
}

// WithStatus is AuthorizationException::withStatus. The status is int|null in
// PHP, so it is a pointer here; nil clears it.
func (e *AuthorizationError) WithStatus(status *int) *AuthorizationError {
	e.status = status

	return e
}

// AsNotFound is AuthorizationException::asNotFound.
func (e *AuthorizationError) AsNotFound() *AuthorizationError {
	status := http.StatusNotFound

	return e.WithStatus(&status)
}

// HasStatus is AuthorizationException::hasStatus.
func (e *AuthorizationError) HasStatus() bool { return e.status != nil }

// Status is AuthorizationException::status. A nil result is the PHP null.
func (e *AuthorizationError) Status() *int { return e.status }

// ToResponse is AuthorizationException::toResponse: the denial this failure
// stands for, built again from the message, the code and the status.
//
// It is what Gate.Inspect calls when an ability answers with a failure instead
// of a Response, which is where the PHP catches the exception.
func (e *AuthorizationError) ToResponse() *Response {
	return Deny(e.message, e.code).WithStatus(e.status)
}

// withPrevious records the cause behind this failure. It is unexported because
// the PHP takes $previous at construction and has no setter; Gate.Authorize
// needs one because the cause -- the sentence auth.Authorize builds, naming the
// action and the subject -- only exists after the AuthorizationError does.
func (e *AuthorizationError) withPrevious(previous error) *AuthorizationError {
	e.previous = previous

	return e
}
