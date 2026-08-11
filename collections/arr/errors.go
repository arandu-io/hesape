package arr

import "github.com/arandu-io/hesape/collections"

// The sentinels are the ones the parent package already declares, not copies of
// them: Arr::sole throws the same ItemNotFoundException that Collection::sole
// throws, and a caller that reaches for errors.Is should not have to know which
// of the two produced the error. Assigning the value rather than restating it
// keeps errors.Is matching across both names.
var (
	// ErrInvalidArgument is collections.ErrInvalidArgument, the
	// InvalidArgumentException that Arr::random, Arr::array, Arr::from and the
	// typed readers throw.
	ErrInvalidArgument = collections.ErrInvalidArgument

	// ErrItemNotFound is collections.ErrItemNotFound, the
	// ItemNotFoundException that Arr::sole throws when nothing matches.
	ErrItemNotFound = collections.ErrItemNotFound

	// ErrMultipleItemsFound is collections.ErrMultipleItemsFound, the
	// MultipleItemsFoundException that Arr::sole throws when more than one
	// item matches.
	ErrMultipleItemsFound = collections.ErrMultipleItemsFound
)
