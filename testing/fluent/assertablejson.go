package fluent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/support/arr"
	hesapetesting "github.com/arandu-io/hesape/testing"
)

// AssertableJSON answers to Illuminate\Testing\Fluent\AssertableJson: a JSON
// payload asserted about one property at a time, where every property has to be
// accounted for.
//
// The accounting is the point. assertJson says "these pairs are in there";
// this says "these are the properties, and there are no others", which is the
// assertion that notices the field somebody added without meaning to publish
// it. Etc is how a scope opts out of it.
type AssertableJSON struct {
	t hesapetesting.T

	// props answers to $props: the properties in the current scope.
	props map[string]any

	// keys is the order props are walked in.
	//
	// A PHP array keeps the order its keys were inserted in, and first() and
	// each() rely on it. A Go map has no order and encoding/json does not keep
	// the document's, so a list keeps its index order and an object is walked
	// sorted: not the order the response was written in, but the same order on
	// every run, which is what a test needs from it.
	keys []string

	// path answers to $path: the dotted path to the current scope. Empty is the
	// PHP's null, which is the root.
	path string

	// interacted answers to $interacted.
	interacted []string
}

// FromArray answers to AssertableJson::fromArray.
func FromArray(t hesapetesting.T, data any) *AssertableJSON {
	props, keys := toProps(data)
	return &AssertableJSON{t: t, props: props, keys: keys}
}

// FromAssertableJSONString answers to
// AssertableJson::fromAssertableJsonString.
//
// It is also the spelling of TestResponse::assertJson's closure form. The PHP
// overloads assertJson on the type of its argument -- an array is a subset
// assertion, a closure is this -- and Go has neither the union parameter nor
// the import cycle that overload would need, because Illuminate\Testing\Fluent
// uses Illuminate\Testing and assertJson would have to use Fluent back. So the
// array form stays on TestResponse::AssertJSON and the closure form is written
// here:
//
//	json := fluent.FromAssertableJSONString(t, response.DecodeResponseJSON())
//	json.Has("data", 3).Where("data.0.name", "Alice")
//	json.Interacted()
//
// Interacted is the call assertJson makes for you at the end of the closure. It
// is what fails when the response carries a property the test never named.
func FromAssertableJSONString(t hesapetesting.T, json *hesapetesting.AssertableJSONString) *AssertableJSON {
	return FromArray(t, json.JSON())
}

// ToArray answers to AssertableJson::toArray.
func (a *AssertableJSON) ToArray() map[string]any { return a.props }

// dotPath answers to AssertableJson::dotPath.
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

// prop answers to AssertableJson::prop.
func (a *AssertableJSON) prop(key ...string) any {
	if len(key) == 0 || key[0] == "" {
		return a.props
	}
	return arr.Get(a.props, key[0], nil)
}

// scope answers to AssertableJson::scope: assert about what is under a key,
// with the same accounting.
func (a *AssertableJSON) scope(key string, callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	value := a.prop(key)
	path := a.dotPath(key)

	hesapetesting.AssertIsArray(a.t, value, fmt.Sprintf("Property [%s] is not scopeable.", path))

	props, keys := toProps(value)
	scope := &AssertableJSON{t: a.t, props: props, keys: keys, path: path}
	callback(scope)
	scope.Interacted()

	return a
}

// Scope is scope() reachable from a test, which the PHP reaches through has()
// with a closure. It is the same call, and it is here because a test that only
// wants to descend should not have to say has() twice.
func (a *AssertableJSON) Scope(key string, callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()
	a.interactsWith(key)
	return a.scope(key, callback)
}

// First answers to AssertableJson::first: assert about the first child.
func (a *AssertableJSON) First(callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	path := a.dotPath()
	message := "Cannot scope directly onto the first element of the root level because it is empty."
	if path != "" {
		message = fmt.Sprintf("Cannot scope directly onto the first element of property [%s] because it is empty.", path)
	}
	hesapetesting.AssertNotEmpty(a.t, a.props, message)

	if len(a.keys) == 0 {
		return a
	}
	key := a.keys[0]

	a.interactsWith(key)

	return a.scope(key, callback)
}

// Each answers to AssertableJson::each: assert the same thing about every
// child.
func (a *AssertableJSON) Each(callback func(*AssertableJSON)) *AssertableJSON {
	a.t.Helper()

	path := a.dotPath()
	message := "Cannot scope directly onto each element of the root level because it is empty."
	if path != "" {
		message = fmt.Sprintf("Cannot scope directly onto each element of property [%s] because it is empty.", path)
	}
	hesapetesting.AssertNotEmpty(a.t, a.props, message)

	for _, key := range a.keys {
		a.interactsWith(key)
		a.scope(key, callback)
	}

	return a
}

// toProps reads a decoded value as the array a scope is made of.
//
// A PHP array is a list and a map at once, so a list becomes its indices as
// keys -- which is exactly what json_decode(..., true) hands the PHP for a JSON
// array, and what makes has('0') and first() work on one.
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

// asString reads a PHP `string|int $key` as the key it names. A property is
// reached by name in both shapes, and a list's names are its indices.
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

// asInt reads a PHP `string|int` as the size it means.
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

// joinComma answers to implode(', ', $keys).
func joinComma(values []string) string { return strings.Join(values, ", ") }
