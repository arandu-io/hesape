// Package collections mirrors Illuminate\Collections.
//
// The files it answers to, in the clone at laravel_illuminate/collections:
//
//	Arr.php                     -> the arr subpackage
//	Collection.php              -> Collection[T] and the package functions here
//	Enumerable.php              -> the contract Collection implements
//	Traits/EnumeratesValues.php -> the concrete half of that contract
//	ItemNotFoundException.php   -> ErrItemNotFound
//	MultipleItemsFoundException -> ErrMultipleItemsFound
//
// Every exported symbol carries the name of the PHP method it answers to. A
// PHP name that reads badly in Go is still the PHP name: Illuminate's
// vocabulary is the point of this package, and a synonym would make the
// mapping unreadable.
//
// # Keys
//
// An Illuminate collection is an ordered map. A Collection[T] is an ordered
// list, and its keys are the positions 0..Count()-1, exactly as they are for a
// PHP collection built from a list. A method that is only meaningful when the
// keys are strings takes a map[string]T instead of a Collection[T]; those are
// package functions, and they are grouped in associative.go.
//
// # Methods that cannot be methods
//
// Go methods cannot declare type parameters. Every Illuminate method whose
// result changes the element type - Map, Pluck, GroupBy, Reduce, Sum and the
// rest - is therefore a package function taking the collection as its first
// argument, under its Illuminate name. This is the only reason any of them is
// not a method.
//
// # Callback arguments
//
// Where Illuminate passes ($value, $key) to a callback, the Go callback takes
// (value T, key int). Where Illuminate passes only ($value), so does Go.
//
// # The names that changed shape
//
// helpers.php spells its five path walkers in lower_snake_case, because they
// are global functions and not methods: data_get, data_has, data_set,
// data_fill and data_forget. They are here, in the arr subpackage, as DataGet,
// DataHas, DataSet, DataFill and DataForget -- the "capitalise to export" of
// ADR 0044 carried across the underscore, which is the one alteration this
// package makes to a name and is said again in arr/data.go where they live.
//
// # What is not here, and why
//
// Ten public methods of the component have no counterpart, and every one of
// them is reason 1 of the porting rule: a PHP language interface Go does not
// have. Nothing is skipped for any other reason.
//
//   - Collection::offsetExists, offsetGet, offsetSet and offsetUnset are
//     ArrayAccess -- the interface behind $collection[0] and unset(). Go has no
//     operator to overload. Each is one line in the PHP over $this->items, and
//     what it does is [Collection.Get], indexing the slice, [Collection.Put]
//     and [Collection.Forget].
//   - EnumeratesValues::jsonSerialize is JsonSerializable, which json_encode
//     calls. encoding/json calls MarshalJSON, and a Collection[T] is a slice,
//     so it already encodes as the JSON array the PHP produces. [ToJSON] and
//     [FromJSON] are the two directions written out.
//   - EnumeratesValues::getCachingIterator wraps getIterator in an SPL
//     CachingIterator so a foreach can look one element ahead. getIterator is
//     IteratorAggregate; the Go answer to both is range, and looking ahead is
//     [Sliding] or an index.
//   - EnumeratesValues::proxy registers a method name for the higher-order
//     message -- $collection->each->foo() -- which is delivered by __get and
//     resolved by name at run time. Go has no property hook and cannot go from
//     a method name to a method, so there is no proxy to register: the higher
//     order form is written as the callback it stands for.
//   - EnumeratesValues::escapeWhenCastingToString makes __toString escape HTML.
//     Go has no string cast to intercept; a collection reaches a template
//     through the view layer, which escapes what it renders.
//   - functions.php's enum_value returns $value->value for a BackedEnum and
//     $value->name for a UnitEnum. Both are language interfaces of PHP's enums,
//     and a Go constant is already the scalar the function exists to extract.
//   - Traits/TransformsToResourceCollection::toResourceCollection guesses a
//     JsonResource class from the element's class name, checks class_exists on
//     each candidate and reads PHP attributes with ReflectionClass. Go cannot
//     go from a name to a type at run time, so there is nothing to guess: the
//     caller names the resource collection, which is a value here.
package collections
