package exceptions

import (
	stdhttp "net/http"
)

// Response is the slice of Illuminate\Http\Response that HttpResponseException
// carries.
//
// It is an interface and not the concrete hesape/http.Response for one reason:
// hesape/http throws these, so naming its type here would be a cycle. The two
// methods are what a handler that catches one reaches for -- the status to send
// and the body to write.
type Response interface {
	// Status is ResponseTrait::status.
	Status() int
	// Content is ResponseTrait::content.
	Content() string
}

// HTTPError is Symfony's HttpKernel HttpException, the base the exceptions in
// this package extend.
//
// The PHP hierarchy is HttpException -> MalformedUrlException,
// PostTooLargeException, ThrottleRequestsException. Go has no class
// inheritance, so it is a struct the three embed, and errors.As finds it in
// any of them.
type HTTPError struct {
	// StatusCode is the status the exception answers with.
	StatusCode int
	// Message is the sentence shown with it.
	Message string
	// Headers are the response headers the exception carries.
	Headers stdhttp.Header
	// Code is the exception code the PHP constructor takes.
	Code int
	// Previous is the error this one wraps, PHP's $previous.
	Previous error
}

// Error implements error.
func (e *HTTPError) Error() string {
	if e.Message == "" {
		return stdhttp.StatusText(e.StatusCode)
	}
	return e.Message
}

// Unwrap returns the wrapped error, so errors.Is and errors.As reach PHP's
// $previous.
func (e *HTTPError) Unwrap() error { return e.Previous }

// GetStatusCode is HttpException::getStatusCode.
func (e *HTTPError) GetStatusCode() int { return e.StatusCode }

// GetHeaders is HttpException::getHeaders.
func (e *HTTPError) GetHeaders() stdhttp.Header { return e.Headers }

// HttpResponseException mirrors
// Illuminate\Http\Exceptions\HttpResponseException: an error carrying a
// response that was built already, so the layer that catches it sends that
// response instead of rendering one.
type HttpResponseException struct {
	response Response
	previous error
}

// NewHttpResponseException is HttpResponseException::__construct. The PHP takes
// an optional $previous; the variadic is that optional argument.
func NewHttpResponseException(response Response, previous ...error) *HttpResponseException {
	e := &HttpResponseException{response: response}
	if len(previous) > 0 {
		e.previous = previous[0]
	}
	return e
}

// Error implements error. The PHP takes its message from $previous, and has
// none of its own when there is no previous.
func (e *HttpResponseException) Error() string {
	if e.previous != nil {
		return e.previous.Error()
	}
	return "http: a response was thrown"
}

// Unwrap returns PHP's $previous.
func (e *HttpResponseException) Unwrap() error { return e.previous }

// GetResponse is HttpResponseException::getResponse: the response to send.
func (e *HttpResponseException) GetResponse() Response { return e.response }

// MalformedUrlException mirrors
// Illuminate\Http\Exceptions\MalformedUrlException: 400 with "Malformed URL.".
type MalformedUrlException struct{ HTTPError }

// NewMalformedUrlException is MalformedUrlException::__construct. It takes no
// arguments, as the PHP does not.
func NewMalformedUrlException() *MalformedUrlException {
	return &MalformedUrlException{HTTPError{
		StatusCode: stdhttp.StatusBadRequest,
		Message:    "Malformed URL.",
	}}
}

// PostTooLargeException mirrors
// Illuminate\Http\Exceptions\PostTooLargeException: 413.
type PostTooLargeException struct{ HTTPError }

// NewPostTooLargeException is PostTooLargeException::__construct.
func NewPostTooLargeException(message string, previous error, headers stdhttp.Header, code int) *PostTooLargeException {
	return &PostTooLargeException{HTTPError{
		StatusCode: stdhttp.StatusRequestEntityTooLarge,
		Message:    message,
		Headers:    headers,
		Code:       code,
		Previous:   previous,
	}}
}

// ThrottleRequestsException mirrors
// Illuminate\Http\Exceptions\ThrottleRequestsException: 429.
type ThrottleRequestsException struct{ HTTPError }

// NewThrottleRequestsException is ThrottleRequestsException::__construct.
func NewThrottleRequestsException(message string, previous error, headers stdhttp.Header, code int) *ThrottleRequestsException {
	return &ThrottleRequestsException{HTTPError{
		StatusCode: stdhttp.StatusTooManyRequests,
		Message:    message,
		Headers:    headers,
		Code:       code,
		Previous:   previous,
	}}
}

// OriginMismatchException mirrors
// Illuminate\Http\Exceptions\OriginMismatchException: the redirect target was
// not on the origin the request arrived on. The PHP class has no body.
type OriginMismatchException struct {
	// Message is the sentence, empty in the PHP.
	Message string
}

// NewOriginMismatchException builds an OriginMismatchException.
func NewOriginMismatchException(message string) *OriginMismatchException {
	return &OriginMismatchException{Message: message}
}

// Error implements error.
func (e *OriginMismatchException) Error() string {
	if e.Message == "" {
		return "http: the redirect target is not on this origin"
	}
	return e.Message
}
