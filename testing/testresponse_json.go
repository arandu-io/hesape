package testing

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/collections"
)

// The JSON assertions on [TestResponse]. Every one of them is a single call
// into [AssertableJSONString], which is where the work is.

// DecodeResponseJSON decodes the body and returns it as an
// [AssertableJSONString].
//
// A body that is not JSON is a failure here rather than three assertions later,
// and when the request logged an exception that exception is what the failure
// says: a route that threw answers with a stack trace or an error page, and
// "Invalid JSON was returned from the route" is the least useful true sentence
// about it.
func (r *TestResponse) DecodeResponseJSON() *AssertableJSONString {
	r.t.Helper()

	testJSON := NewAssertableJSONString(r.t, r.GetContent())

	if testJSON.Decoded() == nil {
		if len(r.Exceptions) > 0 && r.Exceptions[len(r.Exceptions)-1] != nil {
			fail(r.t, "%v", r.Exceptions[len(r.Exceptions)-1])
			return testJSON
		}
		fail(r.t, "Invalid JSON was returned from the route.")
	}

	return testJSON
}

// JSON returns the decoded payload, or the part of it at the given dot path.
// At most one path may be given.
func (r *TestResponse) JSON(key ...string) any {
	r.t.Helper()
	return r.DecodeResponseJSON().JSON(key...)
}

// Collect returns what is at the given dot path as a collection.
//
// collections.Collection is a slice, so an object at the path comes back as
// its values in key order and a list comes back as itself -- which is what the
// assertion this feeds is about either way.
func (r *TestResponse) Collect(key ...string) collections.Collection[any] {
	r.t.Helper()
	return collections.Collection[any](elements(r.JSON(key...)))
}

// AssertJSON asserts the response carries at least this. Passing strict as
// true asks for an exact match instead.
//
// See the doc comment on fluent.FromAssertableJSONString.
func (r *TestResponse) AssertJSON(value any, strict ...bool) *TestResponse {
	r.t.Helper()

	exact := len(strict) > 0 && strict[0]
	r.DecodeResponseJSON().AssertSubset(value, exact)
	return r
}

// AssertJSONPath asserts the value at the dot path is the one expected.
func (r *TestResponse) AssertJSONPath(path string, expect any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertPath(path, expect)
	return r
}

// AssertJSONPathCanonicalizing asserts the value at the dot path is the one
// expected once both sides are sorted, so order does not count.
func (r *TestResponse) AssertJSONPathCanonicalizing(path string, expect any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertPathCanonicalizing(path, expect)
	return r
}

// AssertExactJSON asserts the payload is exactly this, key for key.
func (r *TestResponse) AssertExactJSON(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertExact(data)
	return r
}

// AssertSimilarJSON asserts the payload is this once both sides are sorted, so
// the order of a list does not count.
func (r *TestResponse) AssertSimilarJSON(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertSimilar(data)
	return r
}

// AssertJSONFragment asserts the given key/value pairs appear somewhere in the
// payload.
func (r *TestResponse) AssertJSONFragment(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertFragment(data)
	return r
}

// AssertJSONFragments asserts each fragment appears somewhere in the payload.
func (r *TestResponse) AssertJSONFragments(data []any) *TestResponse {
	r.t.Helper()

	for _, fragment := range data {
		r.AssertJSONFragment(fragment)
	}
	return r
}

// AssertJSONMissing asserts the payload does not carry this. Passing exact as
// true compares the fragment whole rather than pair by pair.
func (r *TestResponse) AssertJSONMissing(data any, exact ...bool) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissing(data, len(exact) > 0 && exact[0])
	return r
}

// AssertJSONMissingExact asserts the payload does not carry this fragment
// whole.
func (r *TestResponse) AssertJSONMissingExact(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissingExact(data)
	return r
}

// AssertJSONMissingPath asserts nothing is at the dot path.
func (r *TestResponse) AssertJSONMissingPath(path string) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissingPath(path)
	return r
}

// AssertJSONStructure asserts the payload has the keys the structure names.
// Other keys are allowed.
//
// How a structure is written is on [AssertableJSONString.AssertStructure].
func (r *TestResponse) AssertJSONStructure(structure any, responseData ...any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertStructure(structure, first(responseData), false)
	return r
}

// AssertExactJSONStructure asserts the payload has the keys the structure
// names and no others.
func (r *TestResponse) AssertExactJSONStructure(structure any, responseData ...any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertStructure(structure, first(responseData), true)
	return r
}

// AssertJSONCount asserts the payload, or what is at the given dot path, has
// the expected number of entries.
func (r *TestResponse) AssertJSONCount(count int, key ...string) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertCount(count, key...)
	return r
}

// AssertJSONIsArray asserts what is at the key is a JSON array.
func (r *TestResponse) AssertJSONIsArray(key ...string) *TestResponse {
	r.t.Helper()

	_, isList := r.JSON(key...).([]any)
	assertTrue(r.t, isList, "")
	return r
}

// AssertJSONIsObject asserts what is at the key is a JSON object.
func (r *TestResponse) AssertJSONIsObject(key ...string) *TestResponse {
	r.t.Helper()

	_, isObject := r.JSON(key...).(map[string]any)
	assertTrue(r.t, isObject, "")
	return r
}

// AssertJSONValidationErrors asserts the payload rejected these fields, and
// said these things about them.
//
//	"email"                                    the field was rejected
//	[]string{"email", "name"}                  both were
//	map[string]any{"email": "must be unique"}  and this is in the message
func (r *TestResponse) AssertJSONValidationErrors(errors any, responseKey ...string) *TestResponse {
	r.t.Helper()

	key := firstOr(responseKey, "errors")
	expected := wrapErrors(errors)

	assertNotEmpty(r.t, expected, "No validation errors were provided.")

	jsonErrors := errorsAt(r.JSON(), key)
	errorMessage := jsonErrorMessage(jsonErrors)

	for _, want := range expected {
		r.AssertJSONValidationErrorFor(want.key, key)

		for _, expectedMessage := range want.messages {
			if containsMessage(jsonErrors[want.key], expectedMessage) {
				continue
			}
			fail(r.t, "Failed to find a validation error in the response for key and message: '%s' => '%s'\n\n%s",
				want.key, expectedMessage, errorMessage)
		}
	}

	return r
}

// AssertOnlyJSONValidationErrors asserts these fields were rejected and no
// others.
func (r *TestResponse) AssertOnlyJSONValidationErrors(errors any, responseKey ...string) *TestResponse {
	r.t.Helper()

	key := firstOr(responseKey, "errors")
	r.AssertJSONValidationErrors(errors, key)

	jsonErrors := errorsAt(r.JSON(), key)

	expected := map[string]bool{}
	for _, want := range wrapErrors(errors) {
		expected[want.key] = true
	}

	var unexpected []string
	for _, name := range sortedStringKeys(jsonErrors) {
		if !expected[name] {
			unexpected = append(unexpected, "'"+name+"'")
		}
	}

	assertTrue(r.t, len(unexpected) == 0,
		"Response has unexpected validation errors: "+strings.Join(unexpected, ", "))
	return r
}

// AssertJSONValidationErrorFor asserts the payload rejected this one field.
func (r *TestResponse) AssertJSONValidationErrorFor(key string, responseKey ...string) *TestResponse {
	r.t.Helper()

	jsonErrors := errorsAt(r.JSON(), firstOr(responseKey, "errors"))

	_, found := jsonErrors[key]
	assertTrue(r.t, found, fmt.Sprintf(
		"Failed to find a validation error in the response for key: '%s'\n\n%s", key, jsonErrorMessage(jsonErrors)))
	return r
}

// AssertJSONMissingValidationErrors asserts none of these fields was rejected.
// A nil argument asserts there were no validation errors at all, and an empty
// body passes.
func (r *TestResponse) AssertJSONMissingValidationErrors(keys any, responseKey ...string) *TestResponse {
	r.t.Helper()

	if r.GetContent() == "" {
		return r
	}

	key := firstOr(responseKey, "errors")

	decoded := NewAssertableJSONString(r.t, r.GetContent()).Decoded()
	if !dataHas(decoded, key) {
		return r
	}

	jsonErrors := errorsAt(decoded, key)

	if keys == nil {
		if len(jsonErrors) > 0 {
			fail(r.t, "Response has unexpected validation errors: \n\n%s", mustEncodePretty(jsonErrors))
		}
		return r
	}

	for _, want := range wrapErrors(keys) {
		_, found := jsonErrors[want.key]
		assertFalse(r.t, found, fmt.Sprintf("Found unexpected validation error for key: '%s'", want.key))
	}

	return r
}

// expectedError is one entry of what the validation assertions take: the
// field, and what the message about it has to contain.
type expectedError struct {
	key      string
	messages []string
}

// first is the message to compare against, or empty when the caller named a
// field without one.
//
// The slice behind it is here because wrapErrors also accepts
// map[string][]string, which is the shape a message bag hands back, and
// dropping the rest of it at the door would make the two disagree.
func (e expectedError) first() string {
	if len(e.messages) == 0 {
		return ""
	}
	return e.messages[0]
}

// wrapErrors normalises what the validation assertions take -- a field name, a
// list of them, or a map from field to message or messages -- into a list of
// [expectedError]. Map keys come out sorted, so a failure message is stable.
func wrapErrors(errors any) []expectedError {
	switch value := errors.(type) {
	case nil:
		return nil

	case string:
		return []expectedError{{key: value}}

	case []string:
		out := make([]expectedError, 0, len(value))
		for _, name := range value {
			out = append(out, expectedError{key: name})
		}
		return out

	case map[string][]string:
		out := make([]expectedError, 0, len(value))
		for _, name := range sortedStringKeys(value) {
			out = append(out, expectedError{key: name, messages: value[name]})
		}
		return out

	case map[string]string:
		out := make([]expectedError, 0, len(value))
		names := make([]string, 0, len(value))
		for name := range value {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			out = append(out, expectedError{key: name, messages: []string{value[name]}})
		}
		return out

	case map[string]any:
		out := make([]expectedError, 0, len(value))
		for _, name := range sortedMapKeys(value) {
			out = append(out, expectedError{key: name, messages: wrapStrings(value[name])})
		}
		return out

	case []any:
		out := make([]expectedError, 0, len(value))
		for _, item := range value {
			out = append(out, wrapErrors(item)...)
		}
		return out

	default:
		return []expectedError{{key: fmt.Sprint(value)}}
	}
}

// errorsAt reads the bag at the given key of a decoded payload as the
// field-to-messages map every branch below walks. A missing key is empty.
func errorsAt(decoded any, responseKey string) map[string][]string {
	return normaliseErrors(dataGet(decoded, responseKey))
}

// normaliseErrors reads a bag of validation errors, however it was written, as
// field to messages.
//
// Here the errors may have come back from a JSON body, from validation.Errors
// or from a support.ViewErrorBag, and all three are the same fact.
func normaliseErrors(value any) map[string][]string {
	out := map[string][]string{}

	switch bag := value.(type) {
	case nil:
		return out

	case map[string][]string:
		for key, messages := range bag {
			out[key] = messages
		}
		return out

	case map[string]any:
		for key, messages := range bag {
			out[key] = wrapStrings(messages)
		}
		return out

	case map[string]string:
		for key, message := range bag {
			out[key] = []string{message}
		}
		return out
	}

	// Anything else with a GetMessages of its own: support.MessageBag,
	// support.ViewErrorBag and validation.Errors all have one.
	if bag, ok := value.(interface{ GetMessages() map[string][]string }); ok {
		for key, messages := range bag.GetMessages() {
			out[key] = messages
		}
	}
	return out
}

// containsMessage reports whether any of one field's messages contains the
// expected text.
func containsMessage(messages []string, expected string) bool {
	for _, message := range messages {
		if strings.Contains(message, expected) {
			return true
		}
	}
	return false
}

// jsonErrorMessage is the tail the validation assertions append to a failure:
// every error the payload carried, so the reader sees what was there instead.
func jsonErrorMessage(errors map[string][]string) string {
	if len(errors) == 0 {
		return "Response does not have JSON validation errors."
	}
	return fmt.Sprintf("Response has the following JSON validation errors:\n\n%s\n", mustEncodePretty(errors))
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

// first returns the single optional value, or nil.
func first(values []any) any {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

// firstOr returns the single optional string, or def when it is absent or
// empty.
func firstOr(values []string, def string) string {
	if len(values) == 0 || values[0] == "" {
		return def
	}
	return values[0]
}
