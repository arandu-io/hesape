package testing

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/collections"
)

// This file is the JSON half of Illuminate\Testing\TestResponse. Every
// assertion here is one call into AssertableJSONString, which is where the
// work is.

// DecodeResponseJSON answers to TestResponse::decodeResponseJson.
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

// JSON answers to TestResponse::json.
func (r *TestResponse) JSON(key ...string) any {
	r.t.Helper()
	return r.DecodeResponseJSON().JSON(key...)
}

// Collect answers to TestResponse::collect.
//
// The PHP wraps the decoded value in a Collection, which keeps string keys.
// collections.Collection is a slice, so an object at the path comes back as its
// values in key order and a list comes back as itself -- which is what the
// assertion this feeds is about either way.
func (r *TestResponse) Collect(key ...string) collections.Collection[any] {
	r.t.Helper()
	return collections.Collection[any](elements(r.JSON(key...)))
}

// AssertJSON answers to TestResponse::assertJson: the response carries at
// least this.
//
// The variadic strict stands for the PHP's `$strict = false`: === instead of
// ==, which is the difference between a payload carrying the number 1 and one
// carrying the string "1".
//
// The PHP's other form -- assertJson(fn (AssertableJson $json) => ...) -- is
// spelled fluent.FromAssertableJSONString(t, response.DecodeResponseJSON()).
// PHP overloads on the argument's type and Go has no union parameter; more to
// the point, Illuminate\Testing\Fluent uses Illuminate\Testing, so the closure
// form here would be an import cycle, which PHP allows and Go does not. See the
// doc comment on fluent.FromAssertableJSONString.
func (r *TestResponse) AssertJSON(value any, strict ...bool) *TestResponse {
	r.t.Helper()

	exact := len(strict) > 0 && strict[0]
	r.DecodeResponseJSON().AssertSubset(value, exact)
	return r
}

// AssertJSONPath answers to TestResponse::assertJsonPath.
func (r *TestResponse) AssertJSONPath(path string, expect any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertPath(path, expect)
	return r
}

// AssertJSONPathCanonicalizing answers to
// TestResponse::assertJsonPathCanonicalizing.
func (r *TestResponse) AssertJSONPathCanonicalizing(path string, expect any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertPathCanonicalizing(path, expect)
	return r
}

// AssertExactJSON answers to TestResponse::assertExactJson.
func (r *TestResponse) AssertExactJSON(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertExact(data)
	return r
}

// AssertSimilarJSON answers to TestResponse::assertSimilarJson.
func (r *TestResponse) AssertSimilarJSON(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertSimilar(data)
	return r
}

// AssertJSONFragment answers to TestResponse::assertJsonFragment.
func (r *TestResponse) AssertJSONFragment(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertFragment(data)
	return r
}

// AssertJSONFragments answers to TestResponse::assertJsonFragments.
func (r *TestResponse) AssertJSONFragments(data []any) *TestResponse {
	r.t.Helper()

	for _, fragment := range data {
		r.AssertJSONFragment(fragment)
	}
	return r
}

// AssertJSONMissing answers to TestResponse::assertJsonMissing.
//
// The variadic exact stands for the PHP's `$exact = false`.
func (r *TestResponse) AssertJSONMissing(data any, exact ...bool) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissing(data, len(exact) > 0 && exact[0])
	return r
}

// AssertJSONMissingExact answers to TestResponse::assertJsonMissingExact.
func (r *TestResponse) AssertJSONMissingExact(data any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissingExact(data)
	return r
}

// AssertJSONMissingPath answers to TestResponse::assertJsonMissingPath.
func (r *TestResponse) AssertJSONMissingPath(path string) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertMissingPath(path)
	return r
}

// AssertJSONStructure answers to TestResponse::assertJsonStructure.
//
// How a structure is written is on AssertableJSONString.AssertStructure. The
// variadic responseData stands for the PHP's second argument, which recursion
// uses and a test does not.
func (r *TestResponse) AssertJSONStructure(structure any, responseData ...any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertStructure(structure, first(responseData), false)
	return r
}

// AssertExactJSONStructure answers to
// TestResponse::assertExactJsonStructure: these keys, and no others.
func (r *TestResponse) AssertExactJSONStructure(structure any, responseData ...any) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertStructure(structure, first(responseData), true)
	return r
}

// AssertJSONCount answers to TestResponse::assertJsonCount.
func (r *TestResponse) AssertJSONCount(count int, key ...string) *TestResponse {
	r.t.Helper()

	r.DecodeResponseJSON().AssertCount(count, key...)
	return r
}

// AssertJSONIsArray answers to TestResponse::assertJsonIsArray: what is at the
// key is a JSON array.
func (r *TestResponse) AssertJSONIsArray(key ...string) *TestResponse {
	r.t.Helper()

	_, isList := r.JSON(key...).([]any)
	assertTrue(r.t, isList, "")
	return r
}

// AssertJSONIsObject answers to TestResponse::assertJsonIsObject.
func (r *TestResponse) AssertJSONIsObject(key ...string) *TestResponse {
	r.t.Helper()

	_, isObject := r.JSON(key...).(map[string]any)
	assertTrue(r.t, isObject, "")
	return r
}

// AssertJSONValidationErrors answers to
// TestResponse::assertJsonValidationErrors: the payload rejected these fields,
// and said these things about them.
//
// errors stands for the PHP's `string|array`:
//
//	"email"                                    the field was rejected
//	[]string{"email", "name"}                  both were
//	map[string]any{"email": "must be unique"}  and this is in the message
//
// The variadic responseKey stands for the PHP's `$responseKey = 'errors'`.
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

// AssertOnlyJSONValidationErrors answers to
// TestResponse::assertOnlyJsonValidationErrors: these fields were rejected and
// no others.
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

// AssertJSONValidationErrorFor answers to
// TestResponse::assertJsonValidationErrorFor.
func (r *TestResponse) AssertJSONValidationErrorFor(key string, responseKey ...string) *TestResponse {
	r.t.Helper()

	jsonErrors := errorsAt(r.JSON(), firstOr(responseKey, "errors"))

	_, found := jsonErrors[key]
	assertTrue(r.t, found, fmt.Sprintf(
		"Failed to find a validation error in the response for key: '%s'\n\n%s", key, jsonErrorMessage(jsonErrors)))
	return r
}

// AssertJSONMissingValidationErrors answers to
// TestResponse::assertJsonMissingValidationErrors: none of these fields was
// rejected.
//
// keys nil is the PHP's `$keys = null`: no field was rejected at all.
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

// expectedError is one entry of the `string|array $errors` the validation
// assertions take: the field, and what the message about it has to contain.
type expectedError struct {
	key      string
	messages []string
}

// first is the message the PHP compares against, or empty when the caller named
// a field without one.
//
// The PHP body of assertSessionHasErrors reads a pair as one key and one
// message -- is_int($key) tells a bare name from a name-and-message pair -- so
// a single message is what an assertion needs. The slice is here because
// wrapErrors also accepts map[string][]string, which is the shape a message bag
// hands back, and dropping the rest of it at the door would make the two
// disagree.
func (e expectedError) first() string {
	if len(e.messages) == 0 {
		return ""
	}
	return e.messages[0]
}

// wrapErrors answers to Arr::wrap over that argument, and to the `is_int($key)`
// branch that tells a bare field name from a field-and-message pair.
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

// errorsAt answers to `Arr::get($this->json(), $responseKey) ?? []`, read as
// the field-to-messages map every branch below walks.
func errorsAt(decoded any, responseKey string) map[string][]string {
	return normaliseErrors(dataGet(decoded, responseKey))
}

// normaliseErrors reads a bag of validation errors, however it was written, as
// field to messages.
//
// The PHP has one shape because a MessageBag has one shape. Here the errors may
// have come back from a JSON body, from validation.Errors or from a
// support.ViewErrorBag, and all three are the same fact.
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
	// support.ViewErrorBag and validation.Errors all have one, and it is the
	// method the PHP calls at this point too.
	if bag, ok := value.(interface{ GetMessages() map[string][]string }); ok {
		for key, messages := range bag.GetMessages() {
			out[key] = messages
		}
	}
	return out
}

// containsMessage answers to the Str::contains loop the validation assertions
// run over one field's messages.
func containsMessage(messages []string, expected string) bool {
	for _, message := range messages {
		if strings.Contains(message, expected) {
			return true
		}
	}
	return false
}

// jsonErrorMessage answers to the $errorMessage the validation assertions build.
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

// first answers to a PHP parameter whose default is null.
func first(values []any) any {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

// firstOr answers to a PHP parameter with a default value.
func firstOr(values []string, def string) string {
	if len(values) == 0 || values[0] == "" {
		return def
	}
	return values[0]
}
