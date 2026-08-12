package broadcasting

import "fmt"

// BroadcastError is Illuminate\Broadcasting\BroadcastException.
//
// The name carries the second mechanical change of ADR 0044: PHP throws, Go
// returns an error, and a Go type that implements error is called an Error.
//
// Illuminate's class is an empty RuntimeException subclass; everything it
// carries is the message the thrower builds. RedisBroadcaster is the one place
// that raises it, and it raises it for a connection that failed -- so the cause
// is kept here rather than flattened into the message, because errors.Is on the
// driver's error is how a caller tells "the broker is down" from "the payload
// would not encode".
type BroadcastError struct {
	message string
	cause   error
}

// NewBroadcastError builds the error with a formatted message, which is how the
// PHP raises it: sprintf('Redis error: %s.', $e->getMessage()).
func NewBroadcastError(format string, args ...any) *BroadcastError {
	return &BroadcastError{message: fmt.Sprintf(format, args...)}
}

// WrapBroadcastError builds the error over the driver failure that caused it.
// The message is formatted as [NewBroadcastError] formats it.
func WrapBroadcastError(cause error, format string, args ...any) *BroadcastError {
	return &BroadcastError{message: fmt.Sprintf(format, args...), cause: cause}
}

// Error implements error with the exception's message, which is
// Exception::getMessage.
func (e *BroadcastError) Error() string { return e.message }

// Unwrap exposes the driver failure underneath to errors.Is and errors.As. It
// is nil when the error was raised without one.
func (e *BroadcastError) Unwrap() error { return e.cause }
