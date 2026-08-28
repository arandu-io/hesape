package fluent

import (
	"fmt"

	"github.com/arandu-io/hesape/collections/arr"
	htesting "github.com/arandu-io/hesape/testing"
)

// The existence and size assertions on [AssertableJSON].

// Count asserts the scope, or the property at a key, has that many entries.
//
// Without it, key is the expected size of the current scope; with it, key
// names the property and length is its expected size.
func (a *AssertableJSON) Count(key any, length ...int) *AssertableJSON {
	a.t.Helper()

	if len(length) == 0 {
		path := a.dotPath()
		message := "Root level does not have the expected size."
		if path != "" {
			message = fmt.Sprintf("Property [%s] does not have the expected size.", path)
		}
		htesting.AssertCount(a.t, asInt(key), a.prop(), message)
		return a
	}

	htesting.AssertCount(a.t, length[0], a.prop(asString(key)),
		fmt.Sprintf("Property [%s] does not have the expected size.", a.dotPath(asString(key))))
	return a
}

// CountBetween asserts the scope has at least minimum and at most maximum
// entries.
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

	htesting.AssertGreaterThanOrEqual(a.t, minimum, size, lower)
	htesting.AssertLessThanOrEqual(a.t, maximum, size, upper)
	return a
}

// Has asserts the property exists, and optionally that it has that many
// entries or holds what the callback asserts.
//
//	Has("data")                       the property exists
//	Has("data", 3)                    it exists and holds three
//	Has("data", func(j) { ... })      it exists, and this is true of what is in it
//	Has("data", 3, func(j) { ... })   it holds three, and this is true of the first
//
// An int key with nothing after it is [AssertableJSON.Count].
func (a *AssertableJSON) Has(key any, args ...any) *AssertableJSON {
	a.t.Helper()

	if _, isInt := key.(int); isInt && len(args) == 0 {
		return a.Count(key)
	}

	name := asString(key)

	htesting.AssertTrue(a.t, arr.Has(a.props, name),
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

// HasAll asserts every one of the properties exists.
//
// A string argument is a bare name, a []string is several of them, and a
// map[string]int is a name with the size it must have:
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

// HasAny asserts at least one of the properties exists.
func (a *AssertableJSON) HasAny(keys ...string) *AssertableJSON {
	a.t.Helper()

	htesting.AssertTrue(a.t, arr.HasAny(a.props, keys...),
		fmt.Sprintf("None of properties [%s] exist.", joinComma(keys)))

	for _, key := range keys {
		a.interactsWith(key)
	}

	return a
}

// MissingAll asserts none of the properties is there.
func (a *AssertableJSON) MissingAll(keys ...string) *AssertableJSON {
	a.t.Helper()

	for _, key := range keys {
		a.Missing(key)
	}
	return a
}

// Missing asserts the property is not there.
//
// It is the half that catches a leak -- a password hash in a user payload, an
// internal note on a public resource -- and [AssertableJSON.Etc] turns the
// whole accounting off, so a scope that says Etc should still say Missing
// about what must not be there.
func (a *AssertableJSON) Missing(key string) *AssertableJSON {
	a.t.Helper()

	htesting.AssertNotTrue(a.t, arr.Has(a.props, key),
		fmt.Sprintf("Property [%s] was found while it was expected to be missing.", a.dotPath(key)))
	return a
}

// readHasArgs sorts the variadic tail of [AssertableJSON.Has] into the
// expected size and the callback, in either order.
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
