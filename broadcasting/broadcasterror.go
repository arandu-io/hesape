package broadcasting

import "fmt"

// BroadcastError is what a driver returns when a broadcast could not be
// published.
//
// The cause is kept rather than flattened into the message, because errors.Is
// on the driver's error is how a caller tells "the broker is down" from "the
// payload would not encode".
type BroadcastError struct {
	message string
	cause   error
}

// NewBroadcastError builds the error with a formatted message.
func NewBroadcastError(format string, args ...any) *BroadcastError {
	return &BroadcastError{message: fmt.Sprintf(format, args...)}
}

// WrapBroadcastError builds the error over the driver failure that caused it.
// The message is formatted as [NewBroadcastError] formats it.
func WrapBroadcastError(cause error, format string, args ...any) *BroadcastError {
	return &BroadcastError{message: fmt.Sprintf(format, args...), cause: cause}
}

// Error implements error with the message the error was built with.
func (e *BroadcastError) Error() string { return e.message }

// Unwrap exposes the driver failure underneath to errors.Is and errors.As. It
// is nil when the error was raised without one.
func (e *BroadcastError) Unwrap() error { return e.cause }
