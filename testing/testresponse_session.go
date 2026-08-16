package testing

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
	"github.com/arandu-io/hesape/support/arr"
	"github.com/arandu-io/hesape/view"
)

// This file is the third of the three TestResponse is written across: the
// session, the validation errors, the view and the streamed body.
//
// Where the errors come from is the one place this file is not a transcription.
// The PHP reads session()->get('errors') and session()->hasOldInput(), because a
// redirect from a rejected form leaves both in the session. This framework
// leaves both in a signed one-shot cookie -- session.Flash, for the reason it
// states: sign in, sign up and password reset are submitted by somebody who has
// no session at all. Reading the session here would be an assertion that never
// sees what the application actually wrote, so the error and old-input
// assertions read the flash instead, through TestResponse.Flash.
//
// AssertSessionHas, AssertSessionHasAll and AssertSessionMissing are unaffected:
// the session store is a general key/value store in both languages, and those
// three read it.

// AssertSessionHas answers to TestResponse::assertSessionHas.
//
// key stands for the PHP's `string|array`: a string, or a map[string]any or
// []string, which route to AssertSessionHasAll the way the PHP's is_array check
// does.
//
// value stands for the PHP's `$value = null`. Passing none asserts only that the
// key is there. Passing a func(any) bool stands for the PHP's Closure: it is
// handed what is in the session and has to answer true.
func (r *TestResponse) AssertSessionHas(key any, value ...any) *TestResponse {
	r.t.Helper()

	if bindings, ok := sessionBindings(key); ok {
		return r.AssertSessionHasAll(bindings)
	}

	name := fmt.Sprint(key)
	store := r.requireSession()
	if store == nil {
		return r
	}

	switch expected := first(value).(type) {
	case nil:
		assertTrue(r.t, store.Has(name),
			r.messageWithContext(fmt.Sprintf("Session is missing expected key [%s].", name)))
	case func(any) bool:
		assertTrue(r.t, expected(store.Get(name)),
			r.messageWithContext(fmt.Sprintf(
				"Failed asserting that the value at [%s] fulfills the expectations defined by the closure.", name)))
	default:
		assertEquals(r.t, expected, store.Get(name), r.messageWithContext(""))
	}

	return r
}

// AssertSessionHasAll answers to TestResponse::assertSessionHasAll.
//
// The PHP's `$bindings` is one array holding both shapes: an integer key means
// "this key is present", a string key means "this key holds that value". Go has
// no array that is both, so the presence-only entries are written with a nil
// value, which is the same nil AssertSessionHas already reads as "no value
// given".
func (r *TestResponse) AssertSessionHasAll(bindings map[string]any) *TestResponse {
	r.t.Helper()

	for _, key := range sortedStringKeys(bindings) {
		if bindings[key] == nil {
			r.AssertSessionHas(key)
			continue
		}
		r.AssertSessionHas(key, bindings[key])
	}
	return r
}

// AssertSessionHasInput answers to TestResponse::assertSessionHasInput.
//
// The PHP reads session()->hasOldInput(). Here the old input is on the flash
// cookie the rejected request wrote, so TestResponse.Flash has to be set.
//
// key and value carry the same three shapes AssertSessionHas takes.
func (r *TestResponse) AssertSessionHasInput(key any, value ...any) *TestResponse {
	r.t.Helper()

	if bindings, ok := sessionBindings(key); ok {
		for _, name := range sortedStringKeys(bindings) {
			if bindings[name] == nil {
				r.AssertSessionHasInput(name)
				continue
			}
			r.AssertSessionHasInput(name, bindings[name])
		}
		return r
	}

	name := fmt.Sprint(key)
	_, old := r.flash()

	switch expected := first(value).(type) {
	case nil:
		assertTrue(r.t, old.Has(name),
			r.messageWithContext(fmt.Sprintf("Session is missing expected key [%s].", name)))
	case func(any) bool:
		assertTrue(r.t, expected(oldInput(old, name)),
			r.messageWithContext(fmt.Sprintf(
				"Failed asserting that the old input at [%s] fulfills the expectations defined by the closure.", name)))
	default:
		assertEquals(r.t, expected, oldInput(old, name), r.messageWithContext(""))
	}

	return r
}

// AssertSessionHasErrors answers to TestResponse::assertSessionHasErrors.
//
// keys stands for the PHP's `$keys = []`: nil or an empty slice asserts only
// that there are errors at all; a string or []string names the fields that must
// have one; a map[string]any pairs a field with the message it must carry.
//
// format and errorBag stand for `$format = null` and `$errorBag = 'default'`.
// The empty string means the PHP's default in both: MessageBag's own format, and
// the default bag. Two named parameters rather than one variadic, because they
// are two different things and a caller passing one string should not have to
// guess which.
//
// Named bags: the flash this framework writes carries one bag, so any name other
// than the default finds it empty. session.Flash says why -- what it carries is
// what one rejected request produced.
func (r *TestResponse) AssertSessionHasErrors(keys any, format string, errorBag string) *TestResponse {
	r.t.Helper()

	errors := r.requireErrors()
	assertTrue(r.t, errors.Any(),
		r.messageWithContext("Session is missing expected key [errors]."))

	bag := errors.GetBag(errorBag)

	for _, expected := range wrapErrors(keys) {
		if expected.first() == "" {
			assertTrue(r.t, bag.Has(expected.key),
				r.messageWithContext(fmt.Sprintf("Session missing error: %s", expected.key)))
			continue
		}
		assertContains(r.t, expected.first(), bag.Get(expected.key, formatOrDefault(format)...),
			r.messageWithContext(""))
	}

	return r
}

// AssertSessionHasErrorsIn answers to TestResponse::assertSessionHasErrorsIn:
// the same assertion with the bag named first.
func (r *TestResponse) AssertSessionHasErrorsIn(errorBag string, keys any, format string) *TestResponse {
	r.t.Helper()
	return r.AssertSessionHasErrors(keys, format, errorBag)
}

// AssertSessionHasNoErrors answers to TestResponse::assertSessionHasNoErrors.
//
// The failure prints every bag it found, because "the session has errors" is not
// an answer anybody can act on and the message that follows it is.
func (r *TestResponse) AssertSessionHasNoErrors() *TestResponse {
	r.t.Helper()

	errors := r.requireErrors()
	assertFalse(r.t, errors.Any(),
		r.messageWithContext("Session has unexpected errors: \n\n"+mustEncodePretty(bagsForMessage(errors))))
	return r
}

// AssertSessionDoesntHaveErrors answers to
// TestResponse::assertSessionDoesntHaveErrors.
//
// With no keys it is AssertSessionHasNoErrors. With keys it asserts about those
// fields only, and a response that produced no errors at all passes.
func (r *TestResponse) AssertSessionDoesntHaveErrors(keys any, format string, errorBag string) *TestResponse {
	r.t.Helper()

	expectations := wrapErrors(keys)
	if len(expectations) == 0 {
		return r.AssertSessionHasNoErrors()
	}

	errors := r.requireErrors()
	if !errors.Any() {
		return r
	}

	bag := errors.GetBag(errorBag)

	for _, expected := range expectations {
		if expected.first() == "" {
			assertFalse(r.t, bag.Has(expected.key),
				r.messageWithContext(fmt.Sprintf("Session has unexpected error: %s", expected.key)))
			continue
		}
		assertNotContains(r.t, expected.first(), bag.Get(expected.key, formatOrDefault(format)...),
			r.messageWithContext(""))
	}

	return r
}

// AssertSessionMissing answers to TestResponse::assertSessionMissing.
//
// key stands for the PHP's `string|array`.
func (r *TestResponse) AssertSessionMissing(key any) *TestResponse {
	r.t.Helper()

	store := r.requireSession()
	if store == nil {
		return r
	}

	for _, name := range wrapStrings(key) {
		assertFalse(r.t, store.Has(name),
			r.messageWithContext(fmt.Sprintf("Session has unexpected key [%s].", name)))
	}
	return r
}

// AssertValid answers to TestResponse::assertValid.
//
// A JSON response is handed to AssertJSONMissingValidationErrors, the way the
// PHP hands it over when the content type says application/json.
//
// keys stands for `$keys = null`: nil asserts that there are no errors at all,
// and a string or []string names the fields that must not have one. errorBag
// and responseKey stand for `'default'` and `'errors'`; the empty string means
// the PHP's default in both.
func (r *TestResponse) AssertValid(keys any, errorBag string, responseKey string) *TestResponse {
	r.t.Helper()

	if r.respondsWithJSON() {
		return r.AssertJSONMissingValidationErrors(keys, orDefault(responseKey, "errors"))
	}

	messages := r.requireErrors().GetBag(errorBag).Messages()
	if len(messages) == 0 {
		return r
	}

	if keys == nil {
		fail(r.t, "%s", r.messageWithContext(
			"Response has unexpected validation errors: \n\n"+mustEncodePretty(messages)))
		return r
	}

	for _, key := range wrapStrings(keys) {
		_, found := messages[key]
		assertFalse(r.t, found,
			r.messageWithContext(fmt.Sprintf("Found unexpected validation error for key: '%s'", key)))
	}

	return r
}

// AssertInvalid answers to TestResponse::assertInvalid.
//
// A JSON response is handed to AssertJSONValidationErrors.
//
// errors stands for the PHP's `$errors = null`: a string or []string names the
// fields that must have an error, and a map[string]any pairs a field with a
// message it must carry -- matched by substring, as Str::contains does.
func (r *TestResponse) AssertInvalid(errors any, errorBag string, responseKey string) *TestResponse {
	r.t.Helper()

	if r.respondsWithJSON() {
		return r.AssertJSONValidationErrors(errors, orDefault(responseKey, "errors"))
	}

	bag := r.requireErrors()
	assertTrue(r.t, bag.Any(),
		r.messageWithContext("Session is missing expected key [errors]."))

	messages := bag.GetBag(errorBag).Messages()

	context := "Response does not have validation errors in the session."
	if len(messages) > 0 {
		context = "Response has the following validation errors in the session:\n\n" +
			mustEncodePretty(messages) + "\n"
	}

	for _, expected := range wrapErrors(errors) {
		found, ok := messages[expected.key]
		assertTrue(r.t, ok, r.messageWithContext(fmt.Sprintf(
			"Failed to find a validation error in session for key: '%s'\n\n%s", expected.key, context)))

		if expected.first() == "" {
			continue
		}
		if !containsMessage(found, expected.first()) {
			fail(r.t, "%s", r.messageWithContext(fmt.Sprintf(
				"Failed to find a validation error for key and message: '%s' => '%s'\n\n%s",
				expected.key, expected.first(), context)))
		}
	}

	return r
}

// AssertOnlyInvalid answers to TestResponse::assertOnlyInvalid: these fields
// have errors and no others do.
//
// It is the half AssertInvalid is not. A rule that rejects one field too many
// passes AssertInvalid, because the field it was asked about is in the list.
func (r *TestResponse) AssertOnlyInvalid(errors any, errorBag string, responseKey string) *TestResponse {
	r.t.Helper()

	if r.respondsWithJSON() {
		return r.AssertOnlyJSONValidationErrors(errors, orDefault(responseKey, "errors"))
	}

	bag := r.requireErrors()
	assertTrue(r.t, bag.Any(),
		r.messageWithContext("Session is missing expected key [errors]."))

	messages := bag.GetBag(errorBag).Messages()

	expected := make([]string, 0, len(messages))
	for _, e := range wrapErrors(errors) {
		expected = append(expected, e.key)
	}

	unexpected := make([]string, 0, len(messages))
	for _, key := range sortedStringKeys(messages) {
		if !slices.Contains(expected, key) {
			unexpected = append(unexpected, "'"+key+"'")
		}
	}

	assertTrue(r.t, len(unexpected) == 0,
		r.messageWithContext("Response has unexpected validation errors: "+strings.Join(unexpected, ", ")))
	return r
}

// AssertViewIs answers to TestResponse::assertViewIs: the handler rendered that
// view and not another.
func (r *TestResponse) AssertViewIs(value string) *TestResponse {
	r.t.Helper()

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return r
	}
	assertEquals(r.t, value, rendered.GetName(), r.messageWithContext(""))
	return r
}

// AssertViewHas answers to TestResponse::assertViewHas.
//
// key stands for the PHP's `string|array`, and takes dot notation the way
// Arr::get does. value stands for `$value = null`, and a func(any) bool stands
// for the PHP's Closure.
//
// The PHP has two more branches, for an Eloquent model and an Eloquent
// collection, which compare by key rather than by value. There is no Active
// Record here -- 00-meta/DOC-architecture.md rejected it -- so a record is compared
// like any other value.
func (r *TestResponse) AssertViewHas(key any, value ...any) *TestResponse {
	r.t.Helper()

	if bindings, ok := sessionBindings(key); ok {
		return r.AssertViewHasAll(bindings)
	}

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return r
	}

	name := fmt.Sprint(key)
	data := rendered.GatherData()
	actual := arr.Get(data, name, nil)

	switch expected := first(value).(type) {
	case nil:
		assertTrue(r.t, arr.Has(data, name),
			r.messageWithContext(fmt.Sprintf("Failed asserting that the data contains the key [%s].", name)))
	case func(any) bool:
		assertTrue(r.t, expected(actual),
			r.messageWithContext(fmt.Sprintf(
				"Failed asserting that the value at [%s] fulfills the expectations defined by the closure.", name)))
	default:
		assertEquals(r.t, expected, actual,
			r.messageWithContext(fmt.Sprintf("Failed asserting that [%s] matches the expected value.", name)))
	}

	return r
}

// AssertViewHasAll answers to TestResponse::assertViewHasAll. A nil value asks
// only that the key is there, which is what the PHP's integer key does.
func (r *TestResponse) AssertViewHasAll(bindings map[string]any) *TestResponse {
	r.t.Helper()

	for _, key := range sortedStringKeys(bindings) {
		if bindings[key] == nil {
			r.AssertViewHas(key)
			continue
		}
		r.AssertViewHas(key, bindings[key])
	}
	return r
}

// AssertViewMissing answers to TestResponse::assertViewMissing.
func (r *TestResponse) AssertViewMissing(key string) *TestResponse {
	r.t.Helper()

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return r
	}
	assertFalse(r.t, arr.Has(rendered.GatherData(), key),
		r.messageWithContext(fmt.Sprintf("Failed asserting that the data does not contain the key [%s].", key)))
	return r
}

// ViewData answers to TestResponse::viewData: one value the handler passed to
// the view, for an assertion this class does not have.
func (r *TestResponse) ViewData(key string) any {
	r.t.Helper()

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return nil
	}
	return rendered.GatherData()[key]
}

// AssertStreamed answers to TestResponse::assertStreamed.
//
// The PHP asks whether the response is a StreamedResponse or a
// StreamedJsonResponse. Go has one response type, so this reads the two signals
// that survive: what the handler recorded on TestResponse.Streamed, and the
// chunked transfer encoding a streamed body arrives under.
func (r *TestResponse) AssertStreamed() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isStreamed(),
		r.messageWithContext("Expected the response to be streamed, but it wasn't."))
	return r
}

// AssertNotStreamed answers to TestResponse::assertNotStreamed.
func (r *TestResponse) AssertNotStreamed() *TestResponse {
	r.t.Helper()

	assertFalse(r.t, r.isStreamed(),
		r.messageWithContext("Response was unexpectedly streamed."))
	return r
}

// AssertStreamedContent answers to TestResponse::assertStreamedContent.
func (r *TestResponse) AssertStreamedContent(value string) *TestResponse {
	r.t.Helper()

	assertSame(r.t, value, r.StreamedContent(), r.messageWithContext(""))
	return r
}

// AssertStreamedJSONContent answers to
// TestResponse::assertStreamedJsonContent: the streamed body is that value
// encoded.
func (r *TestResponse) AssertStreamedJSONContent(value any) *TestResponse {
	r.t.Helper()
	return r.AssertStreamedContent(mustEncode(value))
}

// StreamedContent answers to TestResponse::streamedContent.
//
// The PHP buffers the output the response writes when it is sent, because a
// StreamedResponse has no body until then. Here the body was already read off
// the wire by NewTestResponse, so it is the body.
func (r *TestResponse) StreamedContent() string { return r.GetContent() }

// OffsetExists answers to TestResponse::offsetExists, the ArrayAccess method
// `isset($response['name'])` reaches.
//
// PHP has no way to write `$response['name']`; Go has no way to overload it. The
// four Offset methods are the interface written out, which is what a test that
// reaches for a value by key calls instead.
func (r *TestResponse) OffsetExists(offset string) bool {
	if rendered := r.responseView(); rendered != nil {
		_, found := rendered.GatherData()[offset]
		return found
	}
	decoded, ok := r.JSON().(map[string]any)
	if !ok {
		return false
	}
	_, found := decoded[offset]
	return found
}

// OffsetGet answers to TestResponse::offsetGet: the view's data by key, or the
// decoded JSON body's.
func (r *TestResponse) OffsetGet(offset string) any {
	r.t.Helper()

	if rendered := r.responseView(); rendered != nil {
		return r.ViewData(offset)
	}
	decoded, ok := r.JSON().(map[string]any)
	if !ok {
		return nil
	}
	return decoded[offset]
}

// OffsetSet answers to TestResponse::offsetSet, which throws: a response is what
// came back and there is nothing to write to.
//
// The PHP throws LogicException. This fails the test with the same sentence,
// because a test that reached here has a bug in it and carrying on would hide
// where.
func (r *TestResponse) OffsetSet(offset string, value any) {
	r.t.Helper()
	fail(r.t, "Response data may not be mutated using array access.")
}

// OffsetUnset answers to TestResponse::offsetUnset, which throws for the same
// reason OffsetSet does.
func (r *TestResponse) OffsetUnset(offset string) {
	r.t.Helper()
	fail(r.t, "Response data may not be mutated using array access.")
}

// responseView answers to TestResponse::responseHasView, with the view itself
// rather than a bool: every caller of the PHP's wants it straight after.
func (r *TestResponse) responseView() *view.View {
	rendered, _ := r.Original.(*view.View)
	return rendered
}

// ensureResponseHasView answers to TestResponse::ensureResponseHasView.
func (r *TestResponse) ensureResponseHasView() *view.View {
	r.t.Helper()

	rendered := r.responseView()
	if rendered == nil {
		fail(r.t, "%s", r.messageWithContext(
			"The response is not a view: TestResponse.Original holds no *view.View. "+
				"It is what the handler returned before it became bytes, and the assertView family reads it."))
	}
	return rendered
}

// isStreamed is the check AssertStreamed and AssertNotStreamed share.
func (r *TestResponse) isStreamed() bool {
	if r.Streamed {
		return true
	}
	if r.BaseResponse == nil {
		return false
	}
	return slices.Contains(r.BaseResponse.TransferEncoding, "chunked")
}

// respondsWithJSON answers to the `Content-Type === 'application/json'` check
// AssertValid, AssertInvalid and AssertOnlyInvalid open with.
func (r *TestResponse) respondsWithJSON() bool {
	if r.BaseResponse == nil {
		return false
	}
	return r.Headers().Get("Content-Type") == "application/json"
}

// requireErrors reads the validation errors off the flash the response wrote.
//
// It answers with an empty bag rather than nil for a response that flashed
// nothing, because AssertSessionHasNoErrors and AssertValid have to pass on one.
func (r *TestResponse) requireErrors() *support.ViewErrorBag {
	r.t.Helper()

	errors, _ := r.flash()
	bag := support.NewViewErrorBag()
	if len(errors) > 0 {
		bag.Put(support.DefaultErrorBag, support.NewMessageBag(errors))
	}
	return bag
}

// flash reads the one-shot cookie back off the response.
//
// It goes through session.Flash.Take rather than decoding the cookie here, so
// there is one verifier and not two: Take is what checks the signature, the
// purpose and the expiry, and a second decoder would be a second answer to "is
// this flash trustworthy" (RULE 9).
//
// Take spends the flash the way a page read does, which needs a GET asking for
// HTML and somewhere to write the clearing cookie. Neither leaves this function.
func (r *TestResponse) flash() (map[string][]string, url.Values) {
	r.t.Helper()

	if r.Flash == nil {
		fail(r.t, "The response was asked about validation errors or old input and TestResponse.Flash is not set; "+
			"this framework writes both to a signed cookie rather than to the session, "+
			"so the assertion needs the session.Flash built over the same application key.")
		return nil, nil
	}
	if r.BaseResponse == nil {
		return nil, nil
	}

	var carried *http.Cookie
	for _, c := range r.BaseResponse.Cookies() {
		if c.Name == session.FlashCookieName && c.Value != "" {
			carried = c
		}
	}
	if carried == nil {
		return nil, nil
	}

	read := &http.Request{Method: http.MethodGet, Header: http.Header{}}
	read.Header.Set("Accept", "text/html")
	read.AddCookie(carried)

	errors, old, ok := r.Flash.Take(discardWriter{}, read)
	if !ok {
		return nil, nil
	}
	return errors, old
}

// discardWriter is somewhere for the clearing cookie to go. session.Flash.Take
// spends the flash it read, and a test asserting about a response has no
// response of its own to spend it on.
type discardWriter struct{}

func (discardWriter) Header() http.Header         { return http.Header{} }
func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardWriter) WriteHeader(int)             {}

// sessionBindings answers to the `is_array($key)` branch the session and view
// assertions open with: the key that is really a set of bindings.
func sessionBindings(key any) (map[string]any, bool) {
	switch v := key.(type) {
	case map[string]any:
		return v, true
	case map[string]string:
		out := make(map[string]any, len(v))
		for name, value := range v {
			out[name] = value
		}
		return out, true
	case []string:
		out := make(map[string]any, len(v))
		for _, name := range v {
			out[name] = nil
		}
		return out, true
	default:
		return nil, false
	}
}

// oldInput answers to Store::getOldInput over what the flash carried.
//
// A field the form sent once reads back as a string, which is what the page that
// redisplays it puts in the value attribute; a field sent more than once reads
// back as the list, because that is a set of checkboxes and one of them is not
// the answer.
func oldInput(old url.Values, key string) any {
	values, found := old[key]
	switch {
	case !found:
		return nil
	case len(values) == 1:
		return values[0]
	default:
		return values
	}
}

// formatOrDefault turns the PHP's `$format = null` into the variadic
// MessageBag.Get takes.
func formatOrDefault(format string) []string {
	if format == "" {
		return nil
	}
	return []string{format}
}

// orDefault stands for a PHP parameter default that is not the empty string.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// bagsForMessage answers to the closure assertSessionHasNoErrors builds its
// failure message from.
func bagsForMessage(errors *support.ViewErrorBag) map[string][]string {
	out := map[string][]string{}
	for name, bag := range errors.GetBags() {
		out[name] = bag.All()
	}
	return out
}
