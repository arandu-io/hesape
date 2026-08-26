package image

import (
	"errors"
	"fmt"
)

// ErrImage is the sentinel every error this package returns wraps.
//
// This package can fail for many reasons -- an unsupported format, an
// undecodable stream, a resize with no dimension -- and every error it
// returns wraps this one sentinel, so a caller writes a single check:
//
//	if errors.Is(err, image.ErrImage) { ... }
//
// It is a sentinel and not a struct type for the reason
// model.ErrModelNotFound is one: the caller wants to know which failure it
// is from the message, and to classify it from the sentinel, and a struct would
// make it name a type to do either.
var ErrImage = errors.New("image")

// fail builds a wrapped [ErrImage] with the given message.
func fail(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrImage, fmt.Sprintf(format, args...))
}

// failWith is fail for an error that already exists, keeping both in the chain:
// errors.Is finds ErrImage and the cause both.
func failWith(cause error, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %w", ErrImage, fmt.Sprintf(format, args...), cause)
}
