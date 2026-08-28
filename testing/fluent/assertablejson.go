package fluent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/collections/arr"
	htesting "github.com/arandu-io/hesape/testing"
)

// AssertableJSON is a JSON payload asserted about one property at a time,
// where every property has to be accounted for.
//
// The accounting is the point. A fragment assertion says "these pairs are in
// there"; this says "these are the properties, and there are no others", which
// is the assertion that notices the field somebody added without meaning to
// publish it. [AssertableJSON.Etc] is how a scope opts out of it, and
// [AssertableJSON.Interacted] is what enforces it.
type AssertableJSON struct {
	t htesting.T

	// props are the properties in the current scope.
	props map[string]any

	// keys is the order props are walked in.
	//
	// A Go map has no order and encoding/json does not keep the document's, so a
	// list keeps its index order and an object is walked sorted: not the order
	// the response was written in, but the same order on every run, which is what
	// a test needs from it.
	keys []string

	// path is the dotted path to the current scope, empty at the root.
	path string

	// interacted are the properties the test has named in this scope.
	interacted []string
}

// FromArray wraps an already decoded payload.
func FromArray(t htesting.T, data any) *AssertableJSON {
	props, keys := toProps(data)
	return &AssertableJSON{t: t, props: props, keys: keys}
}

// FromAssertableJSONString wraps a decoded response payload.
//
// It is the entry point to the property-by-property form, where the whole-
// payload form stays on TestResponse.AssertJSON:
//
//	json := fluent.FromAssertableJSONString(t, response.DecodeResponseJSON())
//	json.Has("data", 3).Where("data.0.name", "Alice")
//	json.Interacted()
//
// Interacted is the call that must close the sequence. It is what fails when
// the response carries a property the test never named.
func FromAssertableJSONString(t htesting.T, json *htesting.AssertableJSONString) *AssertableJSON {
	return FromArray(t, json.JSON())
}

// ToArray returns the properties in the current scope.
func (a *AssertableJSON) ToArray() map[string]any { return a.props }

// dotPath returns the dotted path to a key in the current scope, for a failure
// message that names where in the payload it was looking.
func (a *AssertableJSON) dotPath(key ...string) string {
	name := ""
	if len(key) > 0 {
		name = key[0]
	}
	if a.path == "" {
		return name
	}
	return strings.TrimRight(a.path+"."+name, ".")
}

// prop reads a property of the current scope, or the whole scope when no key
// is given.
func (a *AssertableJSON) prop(key ...string) any {
	if len(key) == 0 || key[0] == "" {
		return a.props
	}
	held, _ := arr.Get(a.props, key[0])
	return held
}

// scope asserts about what is under a key, with the same accounting. It fails
// when the property is not something that can be descended into.
func (a *AssertableJSON) scope(key string, callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	value := a.prop(key)
	path := a.dotPath(key)

	htesting.AssertIsArray(a.t, value, fmt.Sprintf("Property [%s] is not scopeable.", path))

	props, keys := toProps(value)
	scope := &AssertableJSON{t: a.t, props: props, keys: keys, path: path}
	callback(scope)
	scope.Interacted()

	return a
}

// Scope asserts about what is under a key, with the same accounting, and
// records that the key was named.
//
// It exists so a test that only wants to descend does not have to assert the
// property exists first.
func (a *AssertableJSON) Scope(key string, callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()
	a.interactsWith(key)
	return a.scope(key, callback)
}

// First asserts about the first child of this scope. An empty scope fails.
func (a *AssertableJSON) First(callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	path := a.dotPath()
	message := "Cannot scope directly onto the first element of the root level because it is empty."
	if path != "" {
		message = fmt.Sprintf("Cannot scope directly onto the first element of property [%s] because it is empty.", path)
	}
	htesting.AssertNotEmpty(a.t, a.props, message)

	if len(a.keys) == 0 {
		return a
	}
	key := a.keys[0]

	a.interactsWith(key)

	return a.scope(key, callback)
}

// Each asserts the same thing about every child of this scope. An empty scope
// fails.
func (a *AssertableJSON) Each(callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	path := a.dotPath()
	message := "Cannot scope directly onto each element of the root level because it is empty."
	if path != "" {
		message = fmt.Sprintf("Cannot scope directly onto each element of property [%s] because it is empty.", path)
	}
	htesting.AssertNotEmpty(a.t, a.props, message)

	for _, key := range a.keys {
		a.interactsWith(key)
		a.scope(key, callback)
	}

	return a
}

// toProps reads a decoded value as the properties a scope is made of, with the
// order to walk them in. A list is keyed by its indices; anything that is
// neither map nor list is an empty scope.
func toProps(v any) (map[string]any, []string) {
	switch value := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return value, keys

	case []any:
		props := make(map[string]any, len(value))
		keys := make([]string, 0, len(value))
		for i, item := range value {
			key := strconv.Itoa(i)
			props[key] = item
			keys = append(keys, key)
		}
		return props, keys

	default:
		return map[string]any{}, nil
	}
}

// asString reads a key that may be a name or an index as the name it uses. A
// list's names are its indices.
func asString(key any) string {
	switch value := key.(type) {
	case string:
		return value
	case int:
		return strconv.Itoa(value)
	default:
		return fmt.Sprint(value)
	}
}

// asInt reads a key as the size it means, or -1 when it is not a number.
func asInt(key any) int {
	switch value := key.(type) {
	case int:
		return value
	case string:
		n, err := strconv.Atoi(value)
		if err != nil {
			return -1
		}
		return n
	default:
		return -1
	}
}

func sortedAnyMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// joinComma renders a list of names for a failure message.
func joinComma(values []string) string { return strings.Join(values, ", ") }
