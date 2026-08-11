package exceptions

import "fmt"

// BackedEnumCaseNotFoundError is the error when a parameter value does not
// match any case of the expected backed enum.
//
// It mirrors Illuminate\Routing\Exceptions\BackedEnumCaseNotFoundException.
type BackedEnumCaseNotFoundError struct {
	Class string
	Value string
}

func (e *BackedEnumCaseNotFoundError) Error() string {
	return fmt.Sprintf("routing: no case of %q matched %q", e.Class, e.Value)
}

// InvalidSignatureError is returned when a signed URL fails verification.
//
// It mirrors Illuminate\Routing\Exceptions\InvalidSignatureException.
type InvalidSignatureError struct {
	Message string
}

func (e *InvalidSignatureError) Error() string {
	return "routing: invalid signature: " + e.Message
}

// MissingRateLimiterError is returned when a named rate limiter has not
// been registered.
//
// It mirrors Illuminate\Routing\Exceptions\MissingRateLimiterException.
type MissingRateLimiterError struct {
	Name string
}

func (e *MissingRateLimiterError) Error() string {
	return "routing: rate limiter " + e.Name + " has not been registered"
}

// StreamedResponseError wraps an error thrown during streamed response
// generation.
//
// It mirrors Illuminate\Routing\Exceptions\StreamedResponseException.
type StreamedResponseError struct {
	Err error
}

func (e *StreamedResponseError) Error() string {
	return "routing: streamed response failed: " + e.Err.Error()
}

func (e *StreamedResponseError) Unwrap() error { return e.Err }

// URLGenerationError is returned when a route URL cannot be built because
// one or more required parameters are missing.
//
// It mirrors Illuminate\Routing\Exceptions\UrlGenerationException.
type URLGenerationError struct {
	Route      string
	Parameters []string
}

func (e *URLGenerationError) Error() string {
	return fmt.Sprintf("routing: missing required parameters for %q: %v", e.Route, e.Parameters)
}
