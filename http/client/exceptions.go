package client

import "fmt"

// HttpClientException is the base exception type for HTTP client errors.
type HttpClientException struct {
	Message string
}

func (e *HttpClientException) Error() string {
	return "http client error: " + e.Message
}

// ConnectionException mirrors Illuminate\Http\Client\ConnectionException.
type ConnectionException struct {
	HttpClientException
}

// NewConnectionException creates a ConnectionException.
func NewConnectionException(message string) *ConnectionException {
	return &ConnectionException{HttpClientException{Message: message}}
}

// RequestException mirrors Illuminate\Http\Client\RequestException.
type RequestException struct {
	HttpClientException
	Response *Response
}

// NewRequestException creates a RequestException from a failed Response.
// If truncateExceptionsAt > 0, the message is truncated to that length.
func NewRequestException(resp *Response, truncateAt int) *RequestException {
	body := resp.Body()
	if truncateAt > 0 && len(body) > truncateAt {
		body = body[:truncateAt] + "..."
	}
	msg := formatAssertion(
		"HTTP request returned status code %d:\n%s",
		resp.Status(),
		body,
	)
	return &RequestException{
		HttpClientException: HttpClientException{Message: msg},
		Response:            resp,
	}
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

// BatchInProgressError mirrors Illuminate\Http\Client\BatchInProgressException.
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
