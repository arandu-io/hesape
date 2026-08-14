package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/arandu-io/hesape/support"
)

// Response mirrors Illuminate\Http\Client\Response.
//
// It wraps *http.Response and adds the inspection, JSON decoding, and
// error-throwing methods that Illuminate provides. The body is read eagerly
// so that the caller can call Body, JSON, and Collect repeatedly without
// draining the stream.
type Response struct {
	resp    *http.Response
	body    []byte
	readErr error
	closed  bool

	// truncateExceptionsAt is Response::$truncateExceptionsAt, the PHP
	// int|false|null: nil defers to the RequestException static, a pointer to
	// zero is the PHP false, and a positive length cuts the body summary.
	truncateExceptionsAt *int
}

// NewResponse creates a Response from an *http.Response. The body is read
// eagerly.
func NewResponse(resp *http.Response) *Response {
	r := &Response{resp: resp}
	r.body, r.readErr = io.ReadAll(resp.Body)
	resp.Body.Close()
	return r
}

// NewResponseFromBytes creates a Response from raw bytes and a status code.
// This is useful in stubs and tests.
func NewResponseFromBytes(status int, body []byte, headers http.Header) *Response {
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "application/json")
	return &Response{
		resp: &http.Response{
			StatusCode: status,
			Header:     headers,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			// The body is carried on the *http.Response as well as on the
			// Response, because HTTPResponse hands this one out and a stub
			// returning it goes back through NewResponse -- which reads
			// resp.Body and would dereference nil. It is the round trip a
			// fake handler makes on every faked request.
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
		},
		body: body,
	}
}

// Body returns the response body as a string.
func (r *Response) Body() string {
	if r == nil {
		return ""
	}
	return string(r.body)
}

// JSON decodes the response body as JSON. If key is non-empty, it extracts
// a nested value using dot notation (e.g., "data.user.name").
func (r *Response) JSON(key string, defaultValue any) (any, error) {
	if r == nil {
		return defaultValue, fmt.Errorf("http client: nil response")
	}
	if r.readErr != nil {
		return defaultValue, r.readErr
	}
	var data any
	if err := json.Unmarshal(r.body, &data); err != nil {
		return defaultValue, fmt.Errorf("http client: json decode: %w", err)
	}
	if key == "" {
		return data, nil
	}
	result, err := dotGet(data, key)
	if err != nil {
		return defaultValue, err
	}
	return result, nil
}

// Object decodes the JSON body into the given value.
func (r *Response) Object(v any) error {
	if r == nil {
		return fmt.Errorf("http client: nil response")
	}
	if r.readErr != nil {
		return r.readErr
	}
	return json.Unmarshal(r.body, v)
}

// Collect decodes the JSON body and returns it as a slice of maps.
func (r *Response) Collect(key string) ([]map[string]any, error) {
	val, err := r.JSON(key, nil)
	if err != nil {
		return nil, err
	}
	if slice, ok := val.([]any); ok {
		result := make([]map[string]any, len(slice))
		for i, item := range slice {
			if m, ok := item.(map[string]any); ok {
				result[i] = m
			} else {
				return nil, fmt.Errorf("http client: expected array of objects at %q", key)
			}
		}
		return result, nil
	}
	return nil, fmt.Errorf("http client: expected array at %q", key)
}

// Resource returns the raw body bytes.
func (r *Response) Resource() []byte {
	if r == nil {
		return nil
	}
	return r.body
}

// Status returns the HTTP status code.
func (r *Response) Status() int {
	if r == nil || r.resp == nil {
		return 0
	}
	return r.resp.StatusCode
}

// Successful reports whether the status code is 2xx.
func (r *Response) Successful() bool {
	s := r.Status()
	return s >= 200 && s < 300
}

// OK reports whether the status is 200.
func (r *Response) OK() bool {
	return r.Status() == http.StatusOK
}

// Redirect reports whether the status code is 3xx.
func (r *Response) Redirect() bool {
	s := r.Status()
	return s >= 300 && s < 400
}

// Failed reports whether the response is a client or server error (4xx or 5xx).
func (r *Response) Failed() bool {
	return r.ClientError() || r.ServerError()
}

// ClientError reports whether the status code is 4xx.
func (r *Response) ClientError() bool {
	s := r.Status()
	return s >= 400 && s < 500
}

// ServerError reports whether the status code is >= 500.
func (r *Response) ServerError() bool {
	return r.Status() >= 500
}

// Header returns the value of a response header.
func (r *Response) Header(name string) string {
	if r == nil || r.resp == nil {
		return ""
	}
	return r.resp.Header.Get(name)
}

// Headers returns all response headers.
func (r *Response) Headers() http.Header {
	if r == nil || r.resp == nil {
		return nil
	}
	return r.resp.Header
}

// Cookies returns the cookies set by the response.
func (r *Response) Cookies() []*http.Cookie {
	if r == nil || r.resp == nil {
		return nil
	}
	return r.resp.Cookies()
}

// EffectiveURI returns the effective request URI (after redirects).
func (r *Response) EffectiveURI() string {
	if r == nil || r.resp == nil || r.resp.Request == nil {
		return ""
	}
	return r.resp.Request.URL.String()
}

// OnError calls the callback if the response indicates a failure.
func (r *Response) OnError(callback func(*Response) error) error {
	if r.Failed() {
		return callback(r)
	}
	return nil
}

// Close closes the response body. It is safe to call multiple times.
func (r *Response) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	return nil
}

// Throw is Response::throw: the RequestException a failed response carries,
// or nil when it did not fail.
//
// The callback is a side effect and never a substitute. The PHP wraps it in
// tap() -- throw tap($this->toException(), fn ($e) => $callback($this, $e)) --
// so the exception goes up whatever the callback returns, and the documented
// idiom ->throw(fn ($r, $e) => Log::error(...)) logs *and* throws. This
// returned the callback's own error instead, so the idiom's nil made a 500
// disappear; it also handed the callback one argument where the PHP hands it
// two, the response and the exception.
func (r *Response) Throw(callback func(*Response, *RequestException)) error {
	if !r.Failed() {
		return nil
	}
	exception := r.ToException()
	if callback != nil {
		callback(r, exception)
	}
	return exception
}

// ToException is Response::toException: the RequestException this response
// would throw, or nil when it did not fail.
//
// The PHP returns RequestException|null; a nil *RequestException is the null.
// Callers that assign it to an error must check for nil before returning it,
// because a nil pointer in a non-nil interface is not a nil error.
func (r *Response) ToException() *RequestException {
	if !r.Failed() {
		return nil
	}
	return NewRequestException(r, r.truncateExceptionsAt)
}

// TruncateExceptionsAt is Response::truncateExceptionsAt: cut the body summary
// of this response's exception at the given length.
func (r *Response) TruncateExceptionsAt(length int) *Response {
	r.truncateExceptionsAt = &length
	return r
}

// DontTruncateExceptions is Response::dontTruncateExceptions: let the whole
// body into this response's exception message.
func (r *Response) DontTruncateExceptions() *Response {
	zero := 0
	r.truncateExceptionsAt = &zero
	return r
}

// ThrowIf is Response::throwIf: throw when the condition holds and the
// response failed. The callback is the side effect [Response.Throw] describes.
func (r *Response) ThrowIf(condition bool, callback func(*Response, *RequestException)) error {
	if condition {
		return r.Throw(callback)
	}
	return nil
}

// ThrowUnless calls throw unless the condition is true.
func (r *Response) ThrowUnless(condition bool) error {
	return r.ThrowIf(!condition, nil)
}

// ThrowIfStatus is Response::throwIfStatus: throw when the response carries
// the given status code.
//
// It does not consult failed(). The PHP throws a RequestException the moment
// the status matches, whatever the status is, which is what makes
// throwIfStatus(201) a usable assertion. This went through Throw, which
// returns nil for anything below 400, so every status a caller could name
// under 400 was silently accepted -- the mirror image of throwUnlessStatus,
// which was already right.
func (r *Response) ThrowIfStatus(statusCode int) error {
	if r.Status() == statusCode {
		return NewRequestException(r, r.truncateExceptionsAt)
	}
	return nil
}

// ThrowUnlessStatus is Response::throwUnlessStatus: throw unless the response
// carries the given status code.
func (r *Response) ThrowUnlessStatus(statusCode int) error {
	if r.Status() == statusCode {
		return nil
	}
	return NewRequestException(r, r.truncateExceptionsAt)
}

// ThrowIfClientError is Response::throwIfClientError.
func (r *Response) ThrowIfClientError() error {
	if r.ClientError() {
		return r.Throw(nil)
	}
	return nil
}

// ThrowIfServerError is Response::throwIfServerError.
func (r *Response) ThrowIfServerError() error {
	if r.ServerError() {
		return r.Throw(nil)
	}
	return nil
}

// HTTPResponse returns the underlying *http.Response.
func (r *Response) HTTPResponse() *http.Response {
	return r.resp
}

// Created reports whether the status is 201.
func (r *Response) Created() bool { return r.Status() == http.StatusCreated }

// Accepted reports whether the status is 202.
func (r *Response) Accepted() bool { return r.Status() == http.StatusAccepted }

// NoContent reports whether the status is 204.
func (r *Response) NoContent() bool { return r.Status() == http.StatusNoContent }

// MovedPermanently reports whether the status is 301.
func (r *Response) MovedPermanently() bool { return r.Status() == http.StatusMovedPermanently }

// Found reports whether the status is 302.
func (r *Response) Found() bool { return r.Status() == http.StatusFound }

// NotModified reports whether the status is 304.
func (r *Response) NotModified() bool { return r.Status() == http.StatusNotModified }

// BadRequest reports whether the status is 400.
func (r *Response) BadRequest() bool { return r.Status() == http.StatusBadRequest }

// Unauthorized reports whether the status is 401.
func (r *Response) Unauthorized() bool { return r.Status() == http.StatusUnauthorized }

// PaymentRequired reports whether the status is 402.
func (r *Response) PaymentRequired() bool { return r.Status() == http.StatusPaymentRequired }

// Forbidden reports whether the status is 403.
func (r *Response) Forbidden() bool { return r.Status() == http.StatusForbidden }

// NotFound reports whether the status is 404.
func (r *Response) NotFound() bool { return r.Status() == http.StatusNotFound }

// RequestTimeout reports whether the status is 408.
func (r *Response) RequestTimeout() bool { return r.Status() == http.StatusRequestTimeout }

// Conflict reports whether the status is 409.
func (r *Response) Conflict() bool { return r.Status() == http.StatusConflict }

// UnprocessableEntity reports whether the status is 422.
func (r *Response) UnprocessableEntity() bool { return r.Status() == http.StatusUnprocessableEntity }

// TooManyRequests reports whether the status is 429.
func (r *Response) TooManyRequests() bool { return r.Status() == http.StatusTooManyRequests }

// UnprocessableContent is DeterminesStatusCode::unprocessableContent: the 422
// check under the name the RFC gave it. [Response.UnprocessableEntity] is the
// same check under the name the older RFC gave it, and the PHP keeps both for
// the same reason.
func (r *Response) UnprocessableContent() bool {
	return r.Status() == http.StatusUnprocessableEntity
}

// Reason is Response::reason: the reason phrase the server sent.
//
// The PHP reads it off the PSR-7 response, which keeps whatever the server
// wrote; net/http keeps the whole status line, so the code is trimmed off the
// front of it. When there is no status line -- a stubbed response -- the
// registered text for the code stands in, which is what the server would have
// sent.
func (r *Response) Reason() string {
	if r == nil || r.resp == nil {
		return ""
	}
	line := r.resp.Status
	code := fmt.Sprintf("%d ", r.resp.StatusCode)
	if len(line) > len(code) && line[:len(code)] == code {
		return line[len(code):]
	}
	if line != "" {
		return line
	}
	return http.StatusText(r.resp.StatusCode)
}

// DumpHeaders is Response::dumpHeaders: write the response headers out where
// the process can see them.
func (r *Response) DumpHeaders() *Response {
	support.Dump(r.Headers())
	return r
}

// DdHeaders is Response::ddHeaders: dump the headers and end the process.
func (r *Response) DdHeaders() {
	support.Dd(r.Headers())
}

// StatusText returns the HTTP status text.
func (r *Response) StatusText() string {
	if r == nil || r.resp == nil {
		return ""
	}
	return http.StatusText(r.resp.StatusCode)
}

// HandlerStats returns basic timing information about the request.
func (r *Response) HandlerStats() map[string]any {
	return map[string]any{
		"status":  r.Status(),
		"headers": r.Headers(),
	}
}

// dotGet extracts a nested value from JSON using dot notation.
func dotGet(data any, key string) (any, error) {
	if key == "" {
		return data, nil
	}
	parts := splitDot(key)
	current := data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("http client: cannot index into non-object for key %q", part)
		}
		var found bool
		current, found = m[part]
		if !found {
			return nil, fmt.Errorf("http client: key %q not found", part)
		}
	}
	return current, nil
}

func splitDot(key string) []string {
	var parts []string
	for {
		idx := indexDot(key)
		if idx < 0 {
			parts = append(parts, key)
			break
		}
		parts = append(parts, key[:idx])
		key = key[idx+1:]
	}
	return parts
}

func indexDot(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			// Handle escape: \. is a literal dot.
			if i > 0 && s[i-1] == '\\' {
				continue
			}
			return i
		}
	}
	return -1
}
