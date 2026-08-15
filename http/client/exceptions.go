package client

import (
	"fmt"
	"sync"
)

// HttpClientException is the base exception type for HTTP client errors.
type HttpClientException struct {
	Message string
}

func (e *HttpClientException) Error() string {
	return "http client error: " + e.Message
}

// ConnectionException is returned when the request never reached the server:
// the host did not resolve, the connection was refused, or the transport gave
// up before a status line came back. There is no [Response] to inspect,
// because none arrived.
//
// It is not the same failure as a response that arrived carrying a failing
// status -- that one is a [RequestException]. The retry callback is handed
// this error when the transport failed and the other one when the server
// answered badly, so a caller that treats them alike is retrying two different
// problems with one policy.
//
// Answers Illuminate\Http\Client\ConnectionException.
type ConnectionException struct {
	HttpClientException
}

// NewConnectionException creates a ConnectionException.
func NewConnectionException(message string) *ConnectionException {
	return &ConnectionException{HttpClientException{Message: message}}
}

// DefaultRequestExceptionTruncateAt is RequestException::$truncateAt in its
// initial state: 120 characters of body summary in the message.
const DefaultRequestExceptionTruncateAt = 120

// requestExceptionTruncateAt is RequestException::$truncateAt, the static the
// three class methods below write. Zero is the PHP false -- "do not truncate"
// -- because the PHP type is int<1, max>|false and zero is not a length any
// caller can ask for. A mutex guards it because a Go test suite runs with
// -race and PHP statics have no concurrency to speak of.
var requestExceptionTruncateAt = struct {
	sync.RWMutex
	length int
}{length: DefaultRequestExceptionTruncateAt}

// RequestException is returned when a response did come back and the caller
// asked for a failing one to be treated as an error: [Response.Throw],
// [Response.ThrowIfStatus] and the retry loop that ran out of attempts all
// build one. The response stays on the value, so whatever handles the error
// can still read the status, the headers and the body.
//
// Its message is the status code followed by a summary of the body, cut at the
// truncation length so that one log line does not swallow a whole HTML error
// page. TruncateExceptionsAt decides that length when it is set on the
// instance; otherwise the package-level [TruncateAt] does.
//
// Answers Illuminate\Http\Client\RequestException.
type RequestException struct {
	HttpClientException
	Response *Response

	// TruncateExceptionsAt is RequestException::$truncateExceptionsAt: the
	// per-instance override of the static. Nil is the PHP null, meaning the
	// static decides; a pointer to zero is the PHP false.
	TruncateExceptionsAt *int

	// HasBeenSummarized is RequestException::$hasBeenSummarized.
	HasBeenSummarized bool
}

// Truncate is RequestException::truncate: restore the default truncation
// length for every request exception built from here on.
func Truncate() {
	TruncateAt(DefaultRequestExceptionTruncateAt)
}

// TruncateAt is RequestException::truncateAt: set the truncation length for
// every request exception built from here on.
func TruncateAt(length int) {
	requestExceptionTruncateAt.Lock()
	requestExceptionTruncateAt.length = length
	requestExceptionTruncateAt.Unlock()
}

// DontTruncate is RequestException::dontTruncate: let the whole body into the
// message of every request exception built from here on.
func DontTruncate() {
	TruncateAt(0)
}

// globalTruncateAt reads the static.
func globalTruncateAt() int {
	requestExceptionTruncateAt.RLock()
	defer requestExceptionTruncateAt.RUnlock()
	return requestExceptionTruncateAt.length
}

// NewRequestException creates a RequestException from a failed Response.
//
// It answers to RequestException::__construct. truncateExceptionsAt is the
// PHP int|false|null: nil defers to [TruncateAt], a pointer to zero is the
// PHP false and lets the whole body through, and a pointer to a positive
// length cuts the body summary there.
func NewRequestException(resp *Response, truncateExceptionsAt *int) *RequestException {
	e := &RequestException{
		Response:             resp,
		TruncateExceptionsAt: truncateExceptionsAt,
	}
	e.Message = e.prepareMessage(resp)
	return e
}

// prepareMessage is RequestException::prepareMessage.
func (e *RequestException) prepareMessage(resp *Response) string {
	message := formatAssertion("HTTP request returned status code %d", resp.Status())

	length := globalTruncateAt()
	if e.TruncateExceptionsAt != nil {
		length = *e.TruncateExceptionsAt
	}

	summary := resp.Body()
	if length > 0 && len(summary) > length {
		summary = summary[:length] + " (truncated...)"
	}
	if summary == "" {
		return message
	}
	return message + ":\n" + summary + "\n"
}

// Report is RequestException::report: rebuild the message from the response
// once, and tell the handler it is not done reporting.
//
// The PHP returns false so that the framework's exception handler carries on
// with its own reporting; the Go returns the same bool for the same reason.
func (e *RequestException) Report() bool {
	if !e.HasBeenSummarized {
		e.Message = e.prepareMessage(e.Response)
		e.HasBeenSummarized = true
	}
	return false
}

// StrayRequestError mirrors Illuminate\Http\Client\StrayRequestException.
// It is returned when a request is made that does not match any stub and
// stray request prevention is enabled.
type StrayRequestError struct {
	URI string
}

// NewStrayRequestError creates a StrayRequestError.
func NewStrayRequestError(uri string) *StrayRequestError {
	return &StrayRequestError{URI: uri}
}

func (e *StrayRequestError) Error() string {
	return "http client: attempt to send request without a matching stub: " + e.URI
}

// BatchInProgressError is returned by [Batch.Send] when that batch has already
// been sent. A batch collects its requests once and dispatches them once, so
// the second call is a mistake in the calling code rather than a transient
// failure -- going ahead with it would put every request in the batch on the
// wire a second time.
//
// Answers Illuminate\Http\Client\BatchInProgressException.
type BatchInProgressError struct {
	HttpClientException
}

// NewBatchInProgressError creates a BatchInProgressError.
func NewBatchInProgressError() *BatchInProgressError {
	return &BatchInProgressError{HttpClientException{Message: "batch is already in progress"}}
}

func formatAssertion(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
