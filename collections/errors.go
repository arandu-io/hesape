package collections

import (
	"errors"
	"fmt"
)

// ErrItemNotFound answers to Illuminate\Support\ItemNotFoundException.
//
// Sole and FirstOrFail return it when no item passes the filter.
var ErrItemNotFound = errors.New("collections: item not found")

// ErrMultipleItemsFound answers to
// Illuminate\Support\MultipleItemsFoundException.
//
// Sole returns it when more than one item passes the filter. The PHP exception
// carries the count; wrap it with fmt.Errorf to carry the same detail.
var ErrMultipleItemsFound = errors.New("collections: multiple items found")

// MultipleItemsFoundError answers to
// Illuminate\Support\MultipleItemsFoundException as a value rather than a
// sentinel, because the PHP exception carries the count and exposes it through
// getCount().
//
// It unwraps to ErrMultipleItemsFound, so errors.Is keeps working on the
// sentinel and errors.As reaches the count.
type MultipleItemsFoundError struct {
	// Count is the $count the PHP constructor stores.
	Count int
}

// GetCount answers to MultipleItemsFoundException::getCount.
func (e *MultipleItemsFoundError) GetCount() int { return e.Count }

// Error renders the message the PHP exception builds from the count.
func (e *MultipleItemsFoundError) Error() string {
	return fmt.Sprintf("%v: %d items were found", ErrMultipleItemsFound, e.Count)
}

// Unwrap reports ErrMultipleItemsFound, so that errors.Is matches the sentinel.
func (e *MultipleItemsFoundError) Unwrap() error { return ErrMultipleItemsFound }

// ErrInvalidArgument answers to the InvalidArgumentException that Collection
// throws from nth, split, splitIn, sliding, shift and random.
var ErrInvalidArgument = errors.New("collections: invalid argument")

// ErrUnexpectedValue answers to the UnexpectedValueException that
// EnumeratesValues::reduceSpread throws when the reducer does not return the
// expected shape.
var ErrUnexpectedValue = errors.New("collections: unexpected value")
