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
package collections
