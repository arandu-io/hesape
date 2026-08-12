package access

import "net/http"

// Response is Illuminate\Auth\Access\Response: one authorization answer,
// carrying the sentence and the code that explain it.
//
// It is used through a pointer because the PHP object is. WithStatus and
// AsNotFound set a field on the receiver and hand it back, and Response::authorize
// reads that field again afterwards, so a value receiver would have dropped the
// status between the two calls.
//
// The PHP class declares Arrayable and Stringable. Those are ToArray and String
// here, and nothing is declared: Go satisfies an interface structurally.
type Response struct {
	allowed bool
	message string
	code    any

	// status is the HTTP status, which PHP types as int|null. A nil pointer is
	// that null, and it is not the same as 0: Response::authorize hands the
	// status straight to the exception, and an unset status has to stay unset
	// there so AuthorizationError.HasStatus can answer no.
	status *int
}

// NewResponse is the Response constructor, __construct($allowed, $message, $code).
//
// The PHP defaults the message to the empty string and the code to null; Go has
// no default arguments, so both are always passed. An empty message and a nil
// code mean the same thing they mean in PHP.
func NewResponse(allowed bool, message string, code any) *Response {
	return &Response{allowed: allowed, message: message, code: code}
}

// Allow is Response::allow.
//
// It is also the allow() of the HandlesAuthorization trait: that method is
// protected and forwards here, and an unexported method promoted from an
// embedded struct is not callable by the package that embeds it, so the
// function is the only form of it that works.
func Allow(message string, code any) *Response {
	return NewResponse(true, message, code)
}

// Deny is Response::deny. It is also the trait's protected deny(), for the
// reason given on Allow.
func Deny(message string, code any) *Response {
	return NewResponse(false, message, code)
}

// DenyWithStatus is Response::denyWithStatus.
func DenyWithStatus(status int, message string, code any) *Response {
	return Deny(message, code).WithStatus(&status)
}

// DenyAsNotFound is Response::denyAsNotFound: a denial that answers 404 instead
// of 403, for a resource whose existence is itself private.
func DenyAsNotFound(message string, code any) *Response {
	return DenyWithStatus(http.StatusNotFound, message, code)
}

// Allowed is Response::allowed.
func (r *Response) Allowed() bool { return r.allowed }

// Denied is Response::denied.
func (r *Response) Denied() bool { return !r.Allowed() }

// Message is Response::message.
func (r *Response) Message() string { return r.message }

// Code is Response::code: the response code or reason, which PHP types as mixed.
func (r *Response) Code() any { return r.code }

// Authorize is Response::authorize: it fails when the response was a denial,
// and hands the response back when it was not.
//
// The PHP throws, so this returns (T, error) -- the second mechanical change of
// ADR 0044. The error is an AuthorizationError carrying this response, its code
// and its status, exactly as the PHP exception does.
func (r *Response) Authorize() (*Response, error) {
	if r.Denied() {
		return nil, NewAuthorizationError(r.message, r.code, nil).SetResponse(r).WithStatus(r.status)
	}

	return r, nil
}

// WithStatus is Response::withStatus. The status is int|null in PHP, so it is a
// pointer here; nil clears it.
func (r *Response) WithStatus(status *int) *Response {
	r.status = status

	return r
}

// AsNotFound is Response::asNotFound.
func (r *Response) AsNotFound() *Response {
	status := http.StatusNotFound

	return r.WithStatus(&status)
}

// Status is Response::status. A nil result is the PHP null: no status was set.
func (r *Response) Status() *int { return r.status }

// ToArray is Response::toArray.
func (r *Response) ToArray() map[string]any {
	return map[string]any{
		"allowed": r.Allowed(),
		"message": r.Message(),
		"code":    r.Code(),
	}
}

// String is Response::__toString: the message on its own. It makes a Response a
// fmt.Stringer, which is what the PHP Stringable interface bought.
func (r *Response) String() string { return r.Message() }
