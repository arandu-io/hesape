package testing

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// AssertableJSONString answers to Illuminate\Testing\AssertableJsonString: a
// JSON payload with the assertions worth making about it.
//
// It is what TestResponse::decodeResponseJson hands back, and every assertJson*
// on TestResponse is one call into here.
type AssertableJSONString struct {
	t T

	// JSONValue answers to the public $json: what was handed in, before it was
	// decoded. The field is named for what it holds because JSON is the
	// method that reads the decoded half.
	JSONValue any

	// decoded answers to $decoded.
	decoded any
}

// NewAssertableJSONString answers to AssertableJsonString::__construct.
//
// A string is decoded; anything else is taken as the already decoded value,
// which is what the PHP does for an array, a Jsonable and a JsonSerializable at
// once -- Go has no three interfaces to tell apart, so the values that are not
// strings are canonicalised into the shape json.Unmarshal would have produced.
//
// A string that is not JSON leaves decoded nil, which is what json_decode
// returning null means, and it is the state decodeResponseJson checks for.
func NewAssertableJSONString(t T, jsonable any) *AssertableJSONString {
	a := &AssertableJSONString{t: t, JSONValue: jsonable}

	switch value := jsonable.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			a.decoded = decoded
		}
	case []byte:
		var decoded any
		if err := json.Unmarshal(value, &decoded); err == nil {
			a.decoded = decoded
		}
	default:
		a.decoded = canonical(jsonable)
	}

	return a
}

// Decoded is the decoded payload, which the PHP reads as the protected
// $decoded from inside the class and through data_get from outside.
func (a *AssertableJSONString) Decoded() any { return a.decoded }

// JSON answers to AssertableJsonString::json: read a value by dotted path, or
// the whole payload when no path is given.
//
// The variadic key stands for the PHP's `$key = null`, whose absence means the
// whole payload.
func (a *AssertableJSONString) JSON(key ...string) any {
	if len(key) == 0 || key[0] == "" {
		return a.decoded
	}
	return dataGet(a.decoded, key[0])
}

// AssertCount answers to AssertableJsonString::assertCount.
func (a *AssertableJSONString) AssertCount(count int, key ...string) *AssertableJSONString {
	a.t.Helper()

	message := fmt.Sprintf("Failed to assert that the response count matched the expected %d", count)
	if len(key) > 0 && key[0] != "" {
		assertCount(a.t, count, dataGet(a.decoded, key[0]), message)
		return a
	}
	assertCount(a.t, count, a.decoded, message)
	return a
}

// AssertExact answers to AssertableJsonString::assertExact: the payload is the
// given value and nothing else.
//
// The PHP flattens both sides with Arr::dot, sorts the keys and rebuilds, so
// that two payloads differing only in the order their keys were written compare
// equal. encoding/json already writes object keys in sorted order, so encoding
// both sides is the same normalisation with one step instead of three.
func (a *AssertableJSONString) AssertExact(data any) *AssertableJSONString {
	a.t.Helper()

	assertEquals(a.t,
		mustEncodePretty(canonical(data)),
		mustEncodePretty(a.decoded),
		"")
	return a
}

// AssertSimilar answers to AssertableJsonString::assertSimilar: the same
// payload, in any order.
func (a *AssertableJSONString) AssertSimilar(data any) *AssertableJSONString {
	a.t.Helper()

	assertEquals(a.t,
		mustEncode(sortRecursive(data)),
		mustEncode(sortRecursive(a.decoded)),
		"")
	return a
}

// AssertFragment answers to AssertableJsonString::assertFragment: every pair in
// the given value appears somewhere in the payload.
//
// The mechanism is the PHP's, and it is worth knowing about: the payload is
// encoded to one sorted string and each pair is looked for as text. It is why a
// fragment matches at any depth, and why it can match a pair that is nested
// somewhere the test did not mean.
func (a *AssertableJSONString) AssertFragment(data any) *AssertableJSONString {
	a.t.Helper()

	actual := mustEncode(sortRecursive(a.decoded))

	for _, pair := range pairs(sortRecursive(data)) {
		expected := jsonSearchStrings(pair.key, pair.value)

		assertTrue(a.t, containsAny(actual, expected), fmt.Sprintf(
			"Unable to find JSON fragment: \n\n[%s]\n\nwithin\n\n[%s].",
			mustEncode(pair.asMap()), actual))
	}

	return a
}

// AssertMissing answers to AssertableJsonString::assertMissing.
func (a *AssertableJSONString) AssertMissing(data any, exact bool) *AssertableJSONString {
	a.t.Helper()

	if exact {
		return a.AssertMissingExact(data)
	}

	actual := mustEncode(sortRecursive(a.decoded))

	for _, pair := range pairs(sortRecursive(data)) {
		unexpected := jsonSearchStrings(pair.key, pair.value)

		assertFalse(a.t, containsAny(actual, unexpected), fmt.Sprintf(
			"Found unexpected JSON fragment: \n\n[%s]\n\nwithin\n\n[%s].",
			mustEncode(pair.asMap()), actual))
	}

	return a
}

// AssertMissingExact answers to AssertableJsonString::assertMissingExact: the
// payload does not carry the whole of the given value.
//
// One pair missing is enough, which is what makes it the exact form: assertMissing
// fails when any pair is present, this one only when they all are.
func (a *AssertableJSONString) AssertMissingExact(data any) *AssertableJSONString {
	a.t.Helper()

	actual := mustEncode(sortRecursive(a.decoded))

	for _, pair := range pairs(sortRecursive(data)) {
		if !containsAny(actual, jsonSearchStrings(pair.key, pair.value)) {
			return a
		}
	}

	fail(a.t, "Found unexpected JSON fragment: \n\n[%s]\n\nwithin\n\n[%s].",
		mustEncode(canonical(data)), actual)
	return a
}

// AssertMissingPath answers to AssertableJsonString::assertMissingPath.
func (a *AssertableJSONString) AssertMissingPath(path string) *AssertableJSONString {
	a.t.Helper()

	assertFalse(a.t, dataHas(a.decoded, path), "")
	return a
}

// AssertPath answers to AssertableJsonString::assertPath: the value at the path
// is the expected one, of the expected type.
//
// expect stands for the PHP's mixed: a value, or a func(any) bool where the PHP
// takes a Closure. The closure form is a truth test over what is at the path,
// and it is how a test says "an id, whatever it turned out to be".
func (a *AssertableJSONString) AssertPath(path string, expect any) *AssertableJSONString {
	a.t.Helper()

	if test, ok := expect.(func(any) bool); ok {
		assertTrue(a.t, test(a.JSON(path)), "")
		return a
	}

	assertSame(a.t, expect, a.JSON(path), "")
	return a
}

// AssertPathCanonicalizing answers to
// AssertableJsonString::assertPathCanonicalizing.
func (a *AssertableJSONString) AssertPathCanonicalizing(path string, expect any) *AssertableJSONString {
	a.t.Helper()

	assertEqualsCanonicalizing(a.t, expect, a.JSON(path), "")
	return a
}

// AssertStructure answers to AssertableJsonString::assertStructure: the payload
// has these keys, nested this way.
//
// # How a structure is written
//
// A PHP structure array mixes integer keys with string keys in one literal, and
// Go has no such array. The two halves are written separately:
//
//   - []any or []string is the integer-keyed half: every element is a key the
//     payload has to carry, and nothing is said about what is under it.
//   - map[string]any is the string-keyed half: every key has to be carried, and
//     its value is the structure of what is under it. A nil value says nothing
//     about what is under it, which is how the integer-keyed spelling is
//     written inside a map.
//
// The key "*" is the wildcard, and it applies the structure under it to every
// element of the payload at that level.
//
// responseData stands for the PHP's second argument: the value to check instead
// of this payload, which is how the wildcard recurses. Pass nil from a test.
func (a *AssertableJSONString) AssertStructure(structure, responseData any, exact bool) *AssertableJSONString {
	a.t.Helper()

	if structure == nil {
		return a.AssertSimilar(a.decoded)
	}

	if responseData != nil {
		return NewAssertableJSONString(a.t, canonical(responseData)).AssertStructure(structure, nil, exact)
	}

	if exact {
		assertIsArray(a.t, a.decoded, "")

		keys := structureKeys(structure)
		if len(keys) != 1 || keys[0] != "*" {
			assertEquals(a.t, keys, sortedKeys(a.decoded), "")
		}
	}

	switch s := structure.(type) {
	case map[string]any:
		for _, key := range sortedMapKeys(s) {
			value := s[key]

			if key == "*" {
				assertIsArray(a.t, a.decoded, "")

				for _, item := range elements(a.decoded) {
					a.AssertStructure(value, orEmpty(item), exact)
				}
				continue
			}

			assertArrayHasKey(a.t, key, a.decoded, "")

			if value == nil {
				continue
			}
			a.AssertStructure(value, orEmpty(dataGet(a.decoded, key)), exact)
		}

	default:
		for _, key := range structureKeys(structure) {
			assertArrayHasKey(a.t, key, a.decoded, "")
		}
	}

	return a
}

// AssertSubset answers to AssertableJsonString::assertSubset.
func (a *AssertableJSONString) AssertSubset(data any, strict bool) *AssertableJSONString {
	a.t.Helper()

	if err := AssertArraySubset(a.t, canonical(data), a.decoded, strict, a.assertJSONMessage(data)); err != nil {
		fail(a.t, "%v", err)
	}
	return a
}

// Count answers to AssertableJsonString::count.
func (a *AssertableJSONString) Count() int {
	n, _ := count(a.decoded)
	return n
}

// assertJSONMessage answers to AssertableJsonString::assertJsonMessage.
func (a *AssertableJSONString) assertJSONMessage(data any) string {
	return fmt.Sprintf("Unable to find JSON: \n\n[%s]\n\nwithin response JSON:\n\n[%s].\n\n",
		mustEncodePretty(canonical(data)), mustEncodePretty(a.decoded))
}

// pair is one key and value of a PHP array being walked. The key is a string
// for a map and an int for a list, which is the distinction jsonSearchStrings
// needs.
type pair struct {
	key   any
	value any
}

// asMap renders the pair the way the failure message encodes it: [$key => $value].
func (p pair) asMap() any {
	if key, ok := p.key.(string); ok {
		return map[string]any{key: p.value}
	}
	return []any{p.value}
}

// pairs answers to the `foreach ($array as $key => $value)` the fragment
// assertions run, over both PHP array shapes. Map keys come out sorted, so a
// failure names the same pair on every run.
func pairs(v any) []pair {
	switch value := v.(type) {
	case map[string]any:
		out := make([]pair, 0, len(value))
		for _, key := range sortedMapKeys(value) {
			out = append(out, pair{key: key, value: value[key]})
		}
		return out
	case []any:
		out := make([]pair, 0, len(value))
		for i, item := range value {
			out = append(out, pair{key: i, value: item})
		}
		return out
	default:
		return nil
	}
}

// jsonSearchStrings answers to AssertableJsonString::jsonSearchStrings: the
// encoded pair, without its enclosing brackets, followed by each of the three
// characters that can end it inside a larger document.
func jsonSearchStrings(key, value any) []string {
	needle := mustEncode(pair{key: key, value: value}.asMap())
	if len(needle) >= 2 {
		needle = needle[1 : len(needle)-1]
	}
	return []string{needle + "]", needle + "}", needle + ","}
}

// containsAny answers to Str::contains with an iterable of needles.
func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// structureKeys answers to the `(new Collection($structure))->map(...)->values()`
// in assertStructure: the key each entry requires, sorted.
func structureKeys(structure any) []string {
	var keys []string

	switch s := structure.(type) {
	case map[string]any:
		keys = append(keys, sortedMapKeys(s)...)
	case []string:
		keys = append(keys, s...)
	case []any:
		for _, item := range s {
			if name, ok := item.(string); ok {
				keys = append(keys, name)
			}
		}
	}

	sort.Strings(keys)
	return keys
}

// sortedKeys is the keys of a decoded payload, sorted, for the exact structure
// comparison. A list answers with its indices written out, which is what
// (new Collection($list))->keys() gives in the PHP.
func sortedKeys(v any) []string {
	switch value := v.(type) {
	case map[string]any:
		return sortedMapKeys(value)
	case []any:
		keys := make([]string, len(value))
		for i := range value {
			keys[i] = strconv.Itoa(i)
		}
		sort.Strings(keys)
		return keys
	default:
		return nil
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// elements is the values of a decoded payload, in order: what the PHP's
// `foreach ($this->decoded as $responseDataItem)` walks.
func elements(v any) []any {
	switch value := v.(type) {
	case []any:
		return value
	case map[string]any:
		out := make([]any, 0, len(value))
		for _, key := range sortedMapKeys(value) {
			out = append(out, value[key])
		}
		return out
	default:
		return nil
	}
}

// orEmpty keeps a nil from being read as "no second argument" by
// AssertStructure, which is a different instruction.
func orEmpty(v any) any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

// dataGet answers to data_get: read a value out of a decoded payload by dotted
// path.
//
// arr.Get is the same walk and is used everywhere a map is the root; it takes a
// map[string]any, and a JSON response is as often a list at the root, which is
// the whole of why this is written here.
func dataGet(root any, key string) any {
	if key == "" {
		return root
	}

	current := root
	for _, segment := range strings.Split(key, ".") {
		next, ok := descend(current, segment)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// dataHas answers to Arr::has over a decoded payload.
func dataHas(root any, key string) bool {
	if key == "" {
		return false
	}

	current := root
	for _, segment := range strings.Split(key, ".") {
		next, ok := descend(current, segment)
		if !ok {
			return false
		}
		current = next
	}
	return true
}

func descend(current any, segment string) (any, bool) {
	switch c := current.(type) {
	case map[string]any:
		v, ok := c[segment]
		return v, ok
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(c) {
			return nil, false
		}
		return c[i], true
	default:
		return nil, false
	}
}
