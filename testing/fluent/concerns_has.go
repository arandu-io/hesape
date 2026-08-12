package fluent

import (
	"fmt"

	"github.com/arandu-io/hesape/support/arr"
	hesapetesting "github.com/arandu-io/hesape/testing"
)

// This file answers to Illuminate\Testing\Fluent\Concerns\Has.
//
// The PHP is a trait mixed into AssertableJson. Go has no traits, so the
// methods are on AssertableJSON itself and the file keeps the trait's name.

// Count answers to Has::count: the scope, or the property at a key, has that
// many entries.
//
// The variadic length stands for the PHP's `?int $length = null`. Without it,
// key is the expected size of the current scope; with it, key names the
// property and length is its expected size.
func (a *AssertableJSON) Count(key any, length ...int) *AssertableJSON {
	a.t.Helper()

	if len(length) == 0 {
		path := a.dotPath()
		message := "Root level does not have the expected size."
		if path != "" {
			message = fmt.Sprintf("Property [%s] does not have the expected size.", path)
		}
		hesapetesting.AssertCount(a.t, asInt(key), a.prop(), message)
		return a
	}

	hesapetesting.AssertCount(a.t, length[0], a.prop(asString(key)),
		fmt.Sprintf("Property [%s] does not have the expected size.", a.dotPath(asString(key))))
	return a
}

// CountBetween answers to Has::countBetween.
func (a *AssertableJSON) CountBetween(minimum, maximum int) *AssertableJSON {
	a.t.Helper()

	path := a.dotPath()
	size := len(a.props)

	lower := fmt.Sprintf("Root level size is not greater than or equal to [%d].", minimum)
	upper := fmt.Sprintf("Root level size is not less than or equal to [%d].", maximum)
	if path != "" {
		lower = fmt.Sprintf("Property [%s] size is not greater than or equal to [%d].", path, minimum)
		upper = fmt.Sprintf("Property [%s] size is not less than or equal to [%d].", path, maximum)
	}

	hesapetesting.AssertGreaterThanOrEqual(a.t, minimum, size, lower)
	hesapetesting.AssertLessThanOrEqual(a.t, maximum, size, upper)
	return a
}

// Has answers to Has::has: the property exists, and optionally has that many
// entries or holds what the callback asserts.
//
// The variadic argument stands for the PHP's `$length = null, ?Closure
// $callback = null`, which is three calls in one method:
//
//	Has("data")                       the property exists
//	Has("data", 3)                    it exists and holds three
//	Has("data", func(j) { ... })      it exists, and this is true of what is in it
//	Has("data", 3, func(j) { ... })   it holds three, and this is true of the first
//
// An int key with nothing after it is Count, which is what the PHP does with
// has(3).
func (a *AssertableJSON) Has(key any, args ...any) *AssertableJSON {
	a.t.Helper()

	if _, isInt := key.(int); isInt && len(args) == 0 {
		return a.Count(key)
	}

	name := asString(key)

	hesapetesting.AssertTrue(a.t, arr.Has(a.props, name),
		fmt.Sprintf("Property [%s] does not exist.", a.dotPath(name)))

	a.interactsWith(name)

	length, hasLength, callback := readHasArgs(args)

	if callback != nil {
		return a.scope(name, func(scope *AssertableJSON) {
			if hasLength {
				scope.Count(length)
			}
			scope.First(callback)
			scope.Etc()
		})
	}

	if hasLength {
		return a.Count(name, length)
	}

	return a
}

// HasAll answers to Has::hasAll: every one of the properties exists.
//
// The PHP takes one array or a list of arguments, and the array mixes bare
// names with name-to-size pairs. Go has no such array, so a string argument is
// a bare name and a map[string]int argument is the pairs:
//
//	HasAll("id", "name")
//	HasAll(map[string]int{"roles": 2}, "name")
func (a *AssertableJSON) HasAll(keys ...any) *AssertableJSON {
	a.t.Helper()

	for _, key := range keys {
		switch value := key.(type) {
		case map[string]int:
			for _, name := range sortedIntMapKeys(value) {
				a.Has(name, value[name])
			}
		case []string:
			for _, name := range value {
				a.Has(name)
			}
		default:
			a.Has(asString(key))
		}
	}

	return a
}

// HasAny answers to Has::hasAny: at least one of the properties exists.
func (a *AssertableJSON) HasAny(keys ...string) *AssertableJSON {
	a.t.Helper()

	hesapetesting.AssertTrue(a.t, arr.HasAny(a.props, keys...),
		fmt.Sprintf("None of properties [%s] exist.", joinComma(keys)))

	for _, key := range keys {
		a.interactsWith(key)
	}

	return a
}

// MissingAll answers to Has::missingAll.
func (a *AssertableJSON) MissingAll(keys ...string) *AssertableJSON {
	a.t.Helper()

	for _, key := range keys {
		a.Missing(key)
	}
	return a
}

// Missing answers to Has::missing: the property is not there.
//
// It is the half that catches a leak -- a password hash in a user payload, an
// internal note on a public resource -- and Etc is what turns the whole
// accounting off, so a scope that says Etc should still say Missing about what
// must not be there.
func (a *AssertableJSON) Missing(key string) *AssertableJSON {
	a.t.Helper()

	hesapetesting.AssertNotTrue(a.t, arr.Has(a.props, key),
		fmt.Sprintf("Property [%s] was found while it was expected to be missing.", a.dotPath(key)))
	return a
}

// readHasArgs sorts the variadic tail of Has into the PHP's $length and
// $callback.
func readHasArgs(args []any) (length int, hasLength bool, callback func(*AssertableJSON)) {
	for _, arg := range args {
		switch value := arg.(type) {
		case int:
			length, hasLength = value, true
		case func(*AssertableJSON):
			callback = value
		}
	}
	return length, hasLength, callback
}
