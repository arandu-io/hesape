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

// The session, validation error, view and streamed-body assertions on
// [TestResponse].
//
// The validation errors and the old input are not in the session. They travel
// in a signed one-shot cookie -- session.Flash, for the reason it states: sign
// in, sign up and password reset are submitted by somebody who has no session
// at all. Reading the session for them would be an assertion that never sees
// what the application actually wrote, so those assertions read the flash
// through [TestResponse.Flash], and say so when it is not set.
//
// [TestResponse.AssertSessionHas], [TestResponse.AssertSessionHasAll] and
// [TestResponse.AssertSessionMissing] are unaffected: they read the session
// store, which is a general key/value store.

// AssertSessionHas asserts the session holds the key, and the value too when
// one is given.
//
// Passing no value asserts only that the key is there. A value of
// func(any) bool is called with what is stored and must report true. A key
// that is really a set of bindings is handed to [TestResponse.AssertSessionHasAll].
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

// AssertSessionHasAll asserts the session holds every one of these bindings.
//
// A nil value asks only that the key is there, which is the same nil
// [TestResponse.AssertSessionHas] reads as "no value given". Keys are walked
// in sorted order, so a failure message is stable.
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

// AssertSessionHasInput asserts the old input holds the key, and the value too
// when one is given.
//
// The old input is on the flash cookie the rejected request wrote, so
// [TestResponse.Flash] has to be set.
//
// key and value carry the same three shapes [TestResponse.AssertSessionHas]
// takes.
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

// AssertSessionHasErrors asserts these fields were rejected, and that the
// message about each contains what was named.
//
// format and errorBag are two named parameters rather than one variadic,
// because they are two different things and a caller passing one string should
// not have to guess which.
//
// The flash carries one bag, so any errorBag other than the default finds it
// empty. session.Flash says why -- what it carries is what one rejected
// request produced.
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

// AssertSessionHasErrorsIn is [TestResponse.AssertSessionHasErrors] with the
// bag named first.
func (r *TestResponse) AssertSessionHasErrorsIn(errorBag string, keys any, format string) *TestResponse {
	r.t.Helper()
	return r.AssertSessionHasErrors(keys, format, errorBag)
}

// AssertSessionHasNoErrors asserts nothing was rejected.
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

// AssertSessionDoesntHaveErrors asserts these fields were not rejected.
//
// With no keys it is [TestResponse.AssertSessionHasNoErrors]. With keys it
// asserts about those fields only, and a response that produced no errors at
// all passes.
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

// AssertSessionMissing asserts the session does not hold the key, or any of
// them when a list is given.
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

// AssertValid asserts these fields were not rejected, and with nil keys that
// nothing was.
//
// A JSON response is handed to [TestResponse.AssertJSONMissingValidationErrors]
// instead, reading responseKey, which defaults to "errors".
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

// AssertInvalid asserts these fields were rejected, and that the message about
// each contains what was named.
//
// A JSON response is handed to [TestResponse.AssertJSONValidationErrors]
// instead, reading responseKey, which defaults to "errors".
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

// AssertOnlyInvalid asserts these fields have errors and no others do.
//
// It is the half [TestResponse.AssertInvalid] is not. A rule that rejects one
// field too many passes AssertInvalid, because the field it was asked about is
// in the list.
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

// AssertViewIs asserts the handler rendered that view and not another.
func (r *TestResponse) AssertViewIs(value string) *TestResponse {
	r.t.Helper()

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return r
	}
	assertEquals(r.t, value, rendered.GetName(), r.messageWithContext(""))
	return r
}

// AssertViewHas asserts the view was bound this key, and this value when one
// is given.
//
// Passing no value asserts only that the key is there. A value of
// func(any) bool is called with what was bound and must report true. A key
// that is really a set of bindings is handed to [TestResponse.AssertViewHasAll].
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

// AssertViewHasAll asserts the view was bound every one of these. A nil value
// asks only that the key is there.
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

// AssertViewMissing asserts the view was not bound this key.
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

// ViewData returns one value the handler passed to the view, for an assertion
// this type does not have.
func (r *TestResponse) ViewData(key string) any {
	r.t.Helper()

	rendered := r.ensureResponseHasView()
	if rendered == nil {
		return nil
	}
	return rendered.GatherData()[key]
}

// AssertStreamed asserts the response was streamed.
//
// It reads two signals: what the handler recorded on [TestResponse.Streamed],
// and the chunked transfer encoding a streamed body arrives under.
func (r *TestResponse) AssertStreamed() *TestResponse {
	r.t.Helper()

	assertTrue(r.t, r.isStreamed(),
		r.messageWithContext("Expected the response to be streamed, but it wasn't."))
	return r
}

// AssertNotStreamed asserts the response was not streamed.
func (r *TestResponse) AssertNotStreamed() *TestResponse {
	r.t.Helper()

	assertFalse(r.t, r.isStreamed(),
		r.messageWithContext("Response was unexpectedly streamed."))
	return r
}

// AssertStreamedContent asserts the streamed body is exactly this.
func (r *TestResponse) AssertStreamedContent(value string) *TestResponse {
	r.t.Helper()

	assertSame(r.t, value, r.StreamedContent(), r.messageWithContext(""))
	return r
}

// AssertStreamedJSONContent asserts the streamed body is that value encoded.
func (r *TestResponse) AssertStreamedJSONContent(value any) *TestResponse {
	r.t.Helper()
	return r.AssertStreamedContent(mustEncode(value))
}

// StreamedContent returns the streamed body.
//
// The body was already read off the wire by [NewTestResponse], so it is simply
// the body.
func (r *TestResponse) StreamedContent() string { return r.GetContent() }

// OffsetExists reports whether the key is in the view's data, or failing that
// in the decoded JSON body.
//
// It is one of the four Offset methods, which are how a test reaches for a
// value by key.
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

// OffsetGet returns the view's data at the key, or the decoded JSON body's.
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

// OffsetSet always fails the test: a response is what came back, and there is
// nothing to write to.
//
// It fails rather than doing nothing, because a test that reached here has a
// bug in it and carrying on would hide where.
func (r *TestResponse) OffsetSet(offset string, value any) {
	r.t.Helper()
	fail(r.t, "Response data may not be mutated using array access.")
}

// OffsetUnset always fails the test, for the same reason
// [TestResponse.OffsetSet] does.
func (r *TestResponse) OffsetUnset(offset string) {
	r.t.Helper()
	fail(r.t, "Response data may not be mutated using array access.")
}

// responseView returns the view the handler rendered, or nil. It hands back
// the view rather than a bool, because every caller wants it straight after.
func (r *TestResponse) responseView() *view.View {
	rendered, _ := r.Original.(*view.View)
	return rendered
}

// ensureResponseHasView returns the rendered view, failing the test when the
// response is not one. The view assertions open with it.
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

// respondsWithJSON reports whether the response is JSON. AssertValid,
// AssertInvalid and AssertOnlyInvalid open with it to pick which half to run.
func (r *TestResponse) respondsWithJSON() bool {
	if r.BaseResponse == nil {
		return false
	}
	return r.Headers().Get("Content-Type") == "application/json"
}

// requireErrors reads the validation errors off the flash the response wrote.
//
// It answers with an empty bag rather than nil for a response that flashed
// nothing, because [TestResponse.AssertSessionHasNoErrors] and
// [TestResponse.AssertValid] have to pass on one.
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
// this flash trustworthy".
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

// sessionBindings reads a key that is really a set of bindings, which is the
// branch the session and view assertions open with. A list of names becomes a
// map to nil, which reads as "no value given".
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

// oldInput reads one field back out of the old input the flash carried.
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

// formatOrDefault turns an optional format string into the variadic argument
// MessageBag.Get takes. An empty format means "use the default".
func formatOrDefault(format string) []string {
	if format == "" {
		return nil
	}
	return []string{format}
}

// orDefault returns value, or fallback when value is empty.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// bagsForMessage flattens every error bag to its messages, which is what
// [TestResponse.AssertSessionHasNoErrors] prints on failure.
func bagsForMessage(errors *support.ViewErrorBag) map[string][]string {
	out := map[string][]string{}
	for name, bag := range errors.GetBags() {
		out[name] = bag.All()
	}
	return out
}
