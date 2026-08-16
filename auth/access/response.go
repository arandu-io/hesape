package access

import "net/http"

// Response is one authorization answer, carrying the sentence and the code that
// explain it.
//
// It is used through a pointer: WithStatus and AsNotFound set a field on the
// receiver and hand it back, and [Response.Authorize] reads that field again
// afterwards, so a value receiver would have dropped the status between the two
// calls.
type Response struct {
	allowed bool
	message string
	code    any

	// status is the HTTP status, and it is a pointer because no status at all is
	// not the same as 0: Authorize hands the status straight to the error, and
	// an unset one has to stay unset there so AuthorizationError.HasStatus can
	// answer no.
	status *int
}

// NewResponse returns an answer with the given message and code.
//
// An empty message and a nil code are both allowed, and mean that nothing was
// said beyond the answer itself.
func NewResponse(allowed bool, message string, code any) *Response {
	return &Response{allowed: allowed, message: message, code: code}
}

// Allow returns an answer that permits the action.
//
// It is a function and not a method on [HandlesAuthorization], because an
// unexported method promoted from an embedded struct is not callable by the
// package that embeds it.
func Allow(message string, code any) *Response {
	return NewResponse(true, message, code)
}

// Deny returns an answer that refuses the action. It is a function for the
// reason given on Allow.
func Deny(message string, code any) *Response {
	return NewResponse(false, message, code)
}

// DenyWithStatus is a refusal that answers with the given HTTP status.
func DenyWithStatus(status int, message string, code any) *Response {
	return Deny(message, code).WithStatus(&status)
}

// DenyAsNotFound is a denial that answers 404 instead of 403, for a resource
// whose existence is itself private.
func DenyAsNotFound(message string, code any) *Response {
	return DenyWithStatus(http.StatusNotFound, message, code)
}

// Allowed reports that the action was permitted.
func (r *Response) Allowed() bool { return r.allowed }

// Denied reports that it was refused.
func (r *Response) Denied() bool { return !r.Allowed() }

// Message is the sentence behind the answer.
func (r *Response) Message() string { return r.message }

// Code is the response code or reason, whatever the ability put there.
func (r *Response) Code() any { return r.code }

// Authorize fails when the response was a denial, and hands the response back
// when it was not.
//
// The error is an [AuthorizationError] carrying this response, its code and its
// status.
func (r *Response) Authorize() (*Response, error) {
	if r.Denied() {
		return nil, NewAuthorizationError(r.message, r.code, nil).SetResponse(r).WithStatus(r.status)
	}

	return r, nil
}

// WithStatus sets the HTTP status the answer carries. A nil pointer clears it.
func (r *Response) WithStatus(status *int) *Response {
	r.status = status

	return r
}

// AsNotFound sets the status to 404.
func (r *Response) AsNotFound() *Response {
	status := http.StatusNotFound

	return r.WithStatus(&status)
}

// Status is the HTTP status the answer carries, and nil when none was set.
func (r *Response) Status() *int { return r.status }

// ToArray is the answer as a map, for anything that serializes it.
func (r *Response) ToArray() map[string]any {
	return map[string]any{
		"allowed": r.Allowed(),
		"message": r.Message(),
		"code":    r.Code(),
	}
}

// String is the message on its own, which makes a Response a fmt.Stringer.
func (r *Response) String() string { return r.Message() }
