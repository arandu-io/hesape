package collections

import "errors"

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

// ErrInvalidArgument answers to the InvalidArgumentException that Collection
// throws from nth, split, splitIn, sliding, shift and random.
var ErrInvalidArgument = errors.New("collections: invalid argument")

// ErrUnexpectedValue answers to the UnexpectedValueException that
// EnumeratesValues::reduceSpread throws when the reducer does not return the
// expected shape.
var ErrUnexpectedValue = errors.New("collections: unexpected value")
