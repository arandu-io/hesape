// Package contracts mirrors Illuminate\Console\Contracts.
//
// The files it answers to, in the clone at
// laravel_illuminate/console/Contracts:
//
//	NewLineAware.php
package contracts

// NewLineAware is an output that knows how many line endings it just wrote.
//
// It answers Illuminate\Console\Contracts\NewLineAware. It exists for one
// reader: the Line component, which puts a blank line above itself unless the
// previous write already left one, so two labelled lines in a row are separated
// by one blank line and never by two.
type NewLineAware interface {
	// NewLinesWritten is how many line endings the last write left behind.
	NewLinesWritten() int

	// NewLineWritten reports whether the last write ended a line.
	//
	// The PHP marks it deprecated in favour of NewLinesWritten, and it is here
	// because the interface still declares it.
	NewLineWritten() bool
}
