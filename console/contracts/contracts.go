// Package contracts holds the small interfaces a console type can implement
// so another package can act on it without importing the concrete type.
package contracts

// NewLineAware is an output that knows how many line endings it just wrote.
//
// It exists for one reader: the Line component, which puts a blank line
// above itself unless the previous write already left one, so two labelled
// lines in a row are separated by one blank line and never by two.
type NewLineAware interface {
	// NewLinesWritten is how many line endings the last write left behind.
	NewLinesWritten() int

	// NewLineWritten reports whether the last write ended a line.
	NewLineWritten() bool
}
