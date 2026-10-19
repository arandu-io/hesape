package session

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/arandu-io/hesape/encryption"
)

// FlashCookieName carries the messages and the typed input of a rejected
// request, between the request that was rejected and the page it is sent back
// to.
//
// Fixed, for the reason CookieName is fixed: one half of the framework writes it
// and another half reads it, and two parts of a project that disagree about the
// name is a form that comes back with nothing on it -- which is the exact
// failure this exists to remove.
const FlashCookieName = "arandu_flash"

// FlashLifetime is how long the messages are worth keeping.
//
// The gap it has to cover is one redirect, which is milliseconds. Five minutes
// is slack for a browser that was closed mid-navigation and reopened, and short
// enough that a message cannot resurface on a page nobody submitted -- which is
// the failure that makes flashed state untrustworthy. The cookie is cleared on
// the read anyway; this is what covers the read that never happens.
const FlashLifetime = 5 * time.Minute

// MaxFlashBytes is the budget for the signed cookie value.
//
// A browser drops a cookie over about 4096 bytes SILENTLY -- no error, no
// warning, nothing in the log. Past that limit the flash is discarded by the
// browser and the screen says nothing again, which is the bug this whole path
// exists to fix, arrived at by a different route. So the budget is enforced
// here, where something can be done about it: see Flash.Write for what is given
// up first.
const MaxFlashBytes = 3500

// flashPurpose binds the signature, so a token minted to verify an e-mail
// address cannot be pasted in here and turned into messages on a page. See
// encryption.Signer.
const flashPurpose = "session.flash"

// maxFlashMessage is the longest single message worth carrying, in bytes.
//
// Messages are sentences written by the framework or by a rule set, not typed by
// a visitor, so this is not a limit anybody meets in normal use. It exists so
// that the trimming in Write terminates with something rather than with a cookie
// the browser throws away: one field carrying one message of at most this length
// always fits in MaxFlashBytes.
const maxFlashMessage = 512

// The three parts of the payload. A single url.Values carries them all, with
// the key prefixed by which part it belongs to, because url.Values.Encode sorts
// its keys -- so the same errors and the same input always produce the same
// bytes, and a test can assert on the cookie instead of on a decoded copy of it.
const (
	flashErrorPrefix   = "e:"
	flashOldPrefix     = "o:"
	flashMessagePrefix = "m:"
	// flashRevealKey lists the fields of Old a request declared revealed, so
	// the read applies the same exception the write did. See WriteRevealing.
	flashRevealKey = "r:"
)

// Flashed is everything one flash cookie carries to the page the browser is
// redirected to.
//
// Errors and Old are what a rejected request produced, and Write puts them
// there. Messages are what a request that succeeded has to say about it --
// "Profile saved.", under "status" -- and WriteMessages puts them there. A
// response writes one or the other: a request is either refused or not.
type Flashed struct {
	// Errors is the validation messages of the rejected request, by field.
	Errors map[string][]string
	// Old is what was typed on it, without the fields IsSecretField and
	// Withhold keep out.
	Old url.Values
	// Messages is the sentences a request that succeeded left for the next
	// page, by key.
	Messages map[string]string
}

// empty reports whether there is nothing in it to draw.
func (f Flashed) empty() bool {
	return len(f.Errors) == 0 && len(f.Old) == 0 && len(f.Messages) == 0
}

// The rules below decide which field's VALUE never goes back in the browser.
//
// A name is caught when putting its value back on the page would redisplay a
// credential, a one-time code, a payment card's secrets or a request token that
// is already stale by the time the redirect lands. The two ways of being wrong
// cost different amounts, and that is what decides how wide the rules are: a
// name caught by mistake comes back as an empty box somebody retypes, while a
// name missed comes back as a secret written into the HTML and into the old
// input a debug dump prints. So the rules are written to over-catch.
//
// Every rule reads the name the same way: lowercased, cut into words at an
// underscore, a hyphen, a dot, a bracket or a change from lower to upper case,
// and joined back without separators. newPassword, new-password, new_password
// and NEW_PASSWORD are one name to every rule, which an exact list of spellings
// never was: it caught new_password and let newPassword through.
//
// What is dropped is the value alone. The MESSAGES for these fields are flashed
// like any other -- an empty password box that does not say why it was rejected
// is the failure being fixed here, not the fix.

// secretNames are whole names, compared after the separators are removed.
//
// code is here because that is what a prompt asking for one calls its field
// when it asks for nothing else. The cost is a checkout that redraws its coupon
// box empty, the cheap side of the trade above. The plurals are here because a
// screen that issues a set names the set; tokens is here and not among the
// words, because max_tokens is a number somebody typed.
var secretNames = map[string]bool{
	"tokens":  true,
	"secrets": true,
	"code":    true,
	"pan":     true,
}

// secretWords are words that make a field secret wherever they stand in its
// name: client_secret, accessToken, card-pin, login_otp, card_cvv. Each is the
// credential itself, whatever words a form author puts around it.
//
// They are matched as whole words and not as substrings, and that is what keeps
// short ones off ordinary fields: pin is not found in shipping or pinned, and
// otp is not found in laptop.
//
// secret is here although it is an ordinary English word, and the cost is real:
// a checkbox called is_secret comes back unticked. That is the cheap side of the
// trade, written down so the next reader knows the name was weighed rather than
// copied in.
var secretWords = map[string]bool{
	"password":   true,
	"passwords":  true,
	"passwd":     true,
	"pwd":        true,
	"passphrase": true,
	"senha":      true,
	"secret":     true,
	"token":      true,
	"otp":        true,
	"totp":       true,
	"pin":        true,
	"cvv":        true,
	"cvv2":       true,
	"cvc":        true,
	"cvc2":       true,
	"apikey":     true,
}

// secretFragments are found anywhere in the joined name, so they catch a name
// whose author ran the words together: newpassword, senhaatual, xapikey,
// cardnumber. Only fragments no ordinary field name contains belong here --
// token and secret do not qualify (tokenizer, secretary), and they are caught as
// words and as endings instead.
var secretFragments = []string{
	"password",
	"passwd",
	"passphrase",
	"senha",
	"apikey",
	"privatekey",
	"secretkey",
	"cardnumber",
	"creditcard",
	"cvv",
	"cvc",
}

// secretEndings catch a credential at the end of a joined name with no
// separator before it: csrftoken, clientsecret, loginotp.
//
// The credential codes are here too, one at a time. What decides whether a
// field ending in code holds a credential is the word in FRONT of it, and on
// that side the ordinary ones are the majority -- postal_code, country_code,
// area_code, status_code, currency_code, promo_code are all values somebody
// typed and expects to get back. A rule that empties a postcode box is a rule
// that gets removed within the week, and removing it would take the recovery
// code out with it. So each credential code is named, and caught behind any
// qualifier.
var secretEndings = []string{
	"token",
	"secret",
	"otp",
	"pwd",
	"recoverycode",
	"recoverycodes",
	"backupcode",
	"backupcodes",
	"twofactorcode",
	"otpcode",
	"mfacode",
	"verificationcode",
	"confirmationcode",
	"securitycode",
	"cardcode",
	"emailcode",
	"authenticatorcode",
	"smscode",
	"resetcode",
	"logincode",
	"passcode",
}

// Flash carries the messages and the input of a rejected request, or the
// sentences of one that succeeded, across the one redirect that follows it.
//
// It is a signed one-shot cookie rather than session state on purpose. The
// forms that need it most -- sign in, sign up, password reset -- are
// submitted by somebody who has no session at all, which is the same reason
// CSRF.Binding binds a token to a guest cookie when there is no session id. A
// flash on the session would work everywhere except on the three screens it was
// built for.
//
// It is not a bag of arbitrary values either. What it carries is what a
// rejected request produced -- the errors, and what was typed -- or the
// sentences a request that succeeded left for the page it redirects to, such as
// "status". All of it is text for a person to read once. A general-purpose
// "flash anything" would be a second way to get data onto a page, next to the
// controller filling a struct.
type Flash struct {
	signer *encryption.Signer
	secure bool
	// withheld is the fields the application named with Withhold, each cut
	// into its words. It is replaced whole, never changed in place, so a
	// request reading it while Withhold runs reads one list or the other.
	withheld atomic.Pointer[[][]string]
}

// Withhold names fields of the application's own forms whose value is never
// written to the flash, beside the ones IsSecretField already keeps out: a
// document number, a phone, anything the application decides is personal data
// that should not travel in a cookie that is signed but not encrypted. The
// person retypes the field after a refusal; its messages still come back.
//
// A name is read as IsSecretField reads one -- case-insensitively, cut into
// words at separators and at a change of case -- and withholds every field
// whose words contain its words in a run: Withhold("mobile") withholds
// mobile, Mobile, billing_mobile, mobilePhone and user[mobile]. Calls add to
// the list. Call it while the application is wired, before it serves; it is
// safe to call at any time, and a request already writing its flash may miss
// the names one call adds.
func (f *Flash) Withhold(fields ...string) {
	for {
		current := f.withheld.Load()
		var next [][]string
		if current != nil {
			next = append(next, (*current)...)
		}
		for _, field := range fields {
			if words := fieldWords(field); len(words) > 0 {
				next = append(next, words)
			}
		}
		if f.withheld.CompareAndSwap(current, &next) {
			return
		}
	}
}

// withholds reports whether a field is one Withhold named.
func (f *Flash) withholds(field string) bool {
	list := f.withheld.Load()
	if list == nil {
		return false
	}
	words := fieldWords(field)
	for _, named := range *list {
		if containsRun(words, named) {
			return true
		}
	}
	return false
}

// containsRun reports whether run appears in words, in order and adjacent.
func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		match := true
		for j := range run {
			if words[i+j] != run[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// NewFlash returns a Flash over the application key.
//
// The same key as the session, the CSRF token and the signed links, because
// they are the same secret: an attacker who has it does not need four. Pass
// secure=false only in development -- without the Secure attribute the cookie
// travels over plain HTTP, and it carries what somebody typed into a form.
func NewFlash(appKey []byte, secure bool) *Flash {
	return &Flash{signer: encryption.NewSigner(appKey), secure: secure}
}

// Write puts the messages and what was typed in the browser, for the page
// the rejected request is about to be redirected to, on a signed cookie
// instead of the session. A request with no session -- sign in, sign up,
// password reset -- would get nothing from session state, which is why
// this is a cookie.
//
// errs is map[string][]string rather than validation.Errors so that this package
// keeps importing nothing above it. validation.Errors IS that type and assigns
// to it directly.
//
// # What it gives up when it does not fit
//
// Everything below MaxFlashBytes is kept. Over it, in this order:
//
//  1. old input, longest value first. A field redisplayed empty is an
//     inconvenience; a field with no message on it is the bug.
//  2. all but the first message of each field. The first one is the one that
//     names what to change.
//  3. fields, from the end of the sorted order, until it fits.
//
// It writes nothing when there is nothing to write, so no caller needs the
// branch.
func (f *Flash) Write(w http.ResponseWriter, errs map[string][]string, old url.Values) {
	f.WriteRevealing(w, errs, old, nil)
}

// WriteRevealing is Write for a request that declared fields whose value goes
// back although IsSecretField keeps them out: a discount's code, which is a
// coupon somebody typed and not a one-time code.
//
// Only a name Revealable answers true for is revealed -- one secret by its
// whole name alone -- and a field Withhold names stays withheld. Every other
// name in revealed is ignored, so a declaration cannot carry a password, a
// token or a card number back into a page. Names are read as IsSecretField
// reads them: Code and code are one name.
func (f *Flash) WriteRevealing(w http.ResponseWriter, errs map[string][]string, old url.Values, revealed []string) {
	kept := redactFlash(old, f.withholds, revealed)
	if len(errs) == 0 && len(kept) == 0 {
		return
	}
	f.setFlash(w, f.fit(errs, kept, revealedIn(kept, revealed)))
}

// WriteMessages puts the sentences a request that succeeded has to say about
// it in the browser, for the page it is about to be redirected to: "Profile
// saved." under "status", read once by that page and gone on the next.
//
// It is the other half of answering a form. A rejected one goes back with its
// errors through Write; one that worked redirects to the page that shows the
// result -- post, redirect, get -- and that page cannot know the write just
// happened unless something carries the sentence across the redirect. Drawing
// the result at the address that was posted to is the alternative, and it is
// the one that breaks: a reload posts the form again, and a refused form sent
// "back" to that address lands on a route that only answers POST.
//
// The messages are sentences for a person, by key, and nothing else: no
// value a page would compute from, no record, nothing a handler could pass
// through the controller instead. Each is cut to the length a sentence needs,
// and when they do not fit in MaxFlashBytes the keys at the end of the sorted
// order are dropped first, so which ones survive is the same on every run.
//
// A second call on the same response replaces what the first wrote: the
// cookie carries one set, and the caller passes the whole of it. Write on the
// same response replaces it too, because a response either refuses the
// request or reports what it did. Empty messages are left out, and nothing is
// written when nothing is left.
func (f *Flash) WriteMessages(w http.ResponseWriter, messages map[string]string) {
	kept := make(map[string]string, len(messages))
	for key, message := range messages {
		if key != "" && message != "" {
			kept[key] = message
		}
	}
	if len(kept) == 0 {
		return
	}
	value := f.encode(nil, nil, kept, nil)
	for len(value) > MaxFlashBytes && len(kept) > 1 {
		keys := make([]string, 0, len(kept))
		for key := range kept {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		delete(kept, keys[len(keys)-1])
		value = f.encode(nil, nil, kept, nil)
	}
	f.setFlash(w, value)
}

// setFlash sets the flash cookie on the response, replacing one this response
// already set: a browser given two cookies of one name keeps the last, and a
// header with both is two answers to one question.
func (f *Flash) setFlash(w http.ResponseWriter, value string) {
	header := w.Header()
	if set := header.Values("Set-Cookie"); len(set) > 0 {
		kept := set[:0:0]
		for _, line := range set {
			if !strings.HasPrefix(line, FlashCookieName+"=") {
				kept = append(kept, line)
			}
		}
		header["Set-Cookie"] = kept
	}
	http.SetCookie(w, &http.Cookie{
		Name:     FlashCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(FlashLifetime.Seconds()),
		HttpOnly: true,
		Secure:   f.secure,
		// Lax and not Strict, for the reason the intended address is Lax: the
		// whole point is to be readable on the navigation that follows, and the
		// signature rather than the attribute is what makes the value ours.
		SameSite: http.SameSiteLaxMode,
	})
}

// Take returns the errors and the input Write stored and clears the cookie, so
// they survive exactly one request. It is Read without the messages, for a
// caller that draws only a rejected form: what Read says about when the cookie
// is spent holds here, and a cookie that carried only messages is spent and
// answered with ok false.
func (f *Flash) Take(w http.ResponseWriter, r *http.Request) (map[string][]string, url.Values, bool) {
	got, ok := f.Read(w, r)
	if !ok || (len(got.Errors) == 0 && len(got.Old) == 0) {
		return nil, nil, false
	}
	return got.Errors, got.Old, true
}

// Read returns what Write or WriteMessages stored and clears it, so it
// survives exactly one request.
//
// Cleared on the read rather than counted down like [Store.AgeFlashData],
// which keeps the data for the request that reads it AND ages it afterwards -- a
// second-request window in which a reload shows the message again. A message
// that outlives its redirect appears on a page nobody submitted, and the person
// reading it has no way to tell it is stale.
//
// ok is false, and nothing is cleared, when this request is not a page somebody
// is reading: an asset fetch, a POST, an HTMX fragment or a JSON call would
// spend the flash on a response that draws none of it, and the person would land
// on the form with nothing on it. That is the same failure as not having a flash
// at all, reached through a stylesheet.
//
// ok is false, and the cookie IS cleared, when the value is there and cannot be
// trusted -- forged, minted for another purpose, expired, or unreadable. There
// is nothing to show and no reason to keep it.
func (f *Flash) Read(w http.ResponseWriter, r *http.Request) (Flashed, bool) {
	if !isPageRead(r) {
		return Flashed{}, false
	}
	c, err := r.Cookie(FlashCookieName)
	if err != nil {
		// No cookie, so nothing to clear: writing the clearing header anyway
		// would put a Set-Cookie on every page view in the application.
		return Flashed{}, false
	}

	f.clearFlash(w)

	payload, err := f.signer.Verify(flashPurpose, c.Value)
	if err != nil {
		return Flashed{}, false
	}
	raw, err := url.ParseQuery(payload)
	if err != nil {
		return Flashed{}, false
	}

	got := decodeFlash(raw, f.withholds)
	if got.empty() {
		return Flashed{}, false
	}
	return got, true
}

// isPageRead reports whether this request is a whole page somebody is about to
// read, which is the only kind of request worth spending a one-shot message on.
//
// A GET, because the flash is drawn by the page the redirect landed on and every
// redirect from a rejected form is followed with a GET. And an Accept that asks
// for HTML, which is what separates the navigation from the stylesheet, the
// image and the HTMX fragment that the same page fires on arrival: XHR sends
// */*, and a browser navigating sends text/html.
func isPageRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// clearFlash tells the browser to drop the messages.
//
// One function and not a cookie literal at each call site, because the two
// copies have to agree on Path: a clearing cookie written for a different path
// does not replace the one that is there, and the message quietly survives the
// read that was supposed to spend it.
func (f *Flash) clearFlash(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     FlashCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   f.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// redactFlash returns the input with every secret's value removed, and every
// field withheld reports.
func redactFlash(old url.Values, withheld func(string) bool, revealed []string) url.Values {
	if len(old) == 0 {
		return nil
	}
	kept := make(url.Values, len(old))
	for field, values := range old {
		if withheld(field) || (IsSecretField(field) && !revealedField(field, revealed)) {
			continue
		}
		kept[field] = values
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// revealedField reports whether a request declared this field revealed, and
// the field is one a declaration may reveal.
func revealedField(field string, revealed []string) bool {
	if len(revealed) == 0 || !Revealable(field) {
		return false
	}
	joined := strings.Join(fieldWords(field), "")
	for _, name := range revealed {
		if strings.Join(fieldWords(name), "") == joined {
			return true
		}
	}
	return false
}

// revealedIn is the fields of kept that are there only because they were
// revealed, by their name in kept.
func revealedIn(kept url.Values, revealed []string) []string {
	if len(revealed) == 0 {
		return nil
	}
	var out []string
	for field := range kept {
		if IsSecretField(field) && revealedField(field, revealed) {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}

// redactInput returns the input with every secret's value removed, at any
// depth.
//
// It answers for map[string]any what redactFlash answers for url.Values, and
// both ask IsSecretField -- one list, two shapes. The shapes differ because the
// two carriers do: a cookie is flat, and what a form hands the session is not.
//
// The depth is not caution. A JSON body arrives nested, Only builds a nested
// map out of a dotted key, and GetOldInput reads back by dot -- so a value
// under user.password is written and read exactly like one under password, and
// a rule that only looked at the top level would hand it back.
func redactInput(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for field, value := range input {
		if IsSecretField(field) {
			continue
		}
		out[field] = redactInputValue(value)
	}
	return out
}

// redactInputValue descends into the two shapes that hold further fields, and
// returns everything else as it stands. A list is rebuilt rather than reused so
// that redacting cannot write through into the caller's map.
func redactInputValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactInput(typed)
	case []any:
		list := make([]any, len(typed))
		for i, item := range typed {
			list[i] = redactInputValue(item)
		}
		return list
	}
	return value
}

// IsSecretField reports whether a field's value never goes back in the browser.
// The name is read case-insensitively and with its separators ignored, and is
// secret when it is one of the secret names, contains a secret word or
// fragment, or ends in a secret ending; the variables above say which is which
// and why.
//
// It is exported because the flash cookie is not the only place a rejected
// request's input is dropped, and the alternative is a second list answering
// this same question. Two such lists diverge by being edited once: the one that
// gained token, otp and the qualified names would have been the only one that
// had them.
func IsSecretField(field string) bool {
	words := fieldWords(field)
	joined := strings.Join(words, "")
	return secretNames[joined] || secretByRule(words, joined)
}

// Revealable reports whether a request may declare that a field's value goes
// back after a refusal although IsSecretField keeps it out: whether the field
// is secret only because its whole name is one of the ambiguous ones -- code,
// tokens, secrets, pan -- and no word, fragment or ending of it is a
// credential.
//
// Those names are over-caught on purpose: a form that asks for nothing but a
// code is asking for a one-time one more often than not. A form that knows its
// code is a coupon, a discount or a product code says so for its own request
// -- see http.Revealer -- and only for this kind of name. A password, a token
// or a card number is never revealed by a declaration, whatever the request
// says.
func Revealable(field string) bool {
	words := fieldWords(field)
	joined := strings.Join(words, "")
	return secretNames[joined] && !secretByRule(words, joined)
}

// secretByRule reports whether a word, a fragment or an ending of the name is
// a credential: every rule of IsSecretField but the whole-name one.
func secretByRule(words []string, joined string) bool {
	for _, word := range words {
		if secretWords[word] {
			return true
		}
	}
	for _, fragment := range secretFragments {
		if strings.Contains(joined, fragment) {
			return true
		}
	}
	for _, ending := range secretEndings {
		if strings.HasSuffix(joined, ending) {
			return true
		}
	}
	return false
}

// fieldWords cuts a field name into lowercase words: at every character that
// is not a letter or a digit, and where a lowercase letter or a digit is
// followed by an uppercase one, so newPassword is new and password. A run of
// capitals stays one word until the last of them starts a lowercase one, so
// APIKey is api and key.
func fieldWords(field string) []string {
	runes := []rune(field)
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, strings.ToLower(string(current)))
			current = current[:0]
		}
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if unicode.IsUpper(r) && len(current) > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		current = append(current, r)
	}
	flush()
	return words
}

// fit encodes and signs the flash, giving up what Write documents until the
// cookie is inside MaxFlashBytes.
func (f *Flash) fit(errs map[string][]string, old url.Values, revealed []string) string {
	value := f.encode(errs, old, nil, revealed)
	if len(value) <= MaxFlashBytes {
		return value
	}

	// 1. Old input, longest value first. Messages matter more than redisplay.
	for len(old) > 0 {
		delete(old, longestFlashField(old))
		value = f.encode(errs, old, nil, revealed)
		if len(value) <= MaxFlashBytes {
			return value
		}
	}

	// 2. One message per field. The first is the one that names what to change.
	first := make(map[string][]string, len(errs))
	for field, msgs := range errs {
		if len(msgs) > 0 {
			first[field] = msgs[:1]
		}
	}
	value = f.encode(first, nil, nil, nil)
	if len(value) <= MaxFlashBytes {
		return value
	}

	// 3. Fields, from the end of the sorted order. Sorted rather than map order
	// so that which messages survive is the same on every run -- a flash that
	// keeps a different half each time is one nobody can reproduce a report of.
	for len(first) > 1 {
		names := sortedFlashFields(first)
		delete(first, names[len(names)-1])
		value = f.encode(first, nil, nil, nil)
		if len(value) <= MaxFlashBytes {
			return value
		}
	}
	// One field carrying one message of at most maxFlashMessage bytes, which
	// encode has already capped it to. That always fits.
	return value
}

// encode writes the three parts into one signed payload.
//
// url.Values.Encode sorts its keys, so the same input produces the same bytes
// every time: the cookie is stable across runs, which is what lets the trimming
// above be tested and a report of it be reproduced.
//
// revealed names the fields of old a request declared revealed; a name not in
// old is left out, so trimming old drops its declaration with it.
func (f *Flash) encode(errs map[string][]string, old url.Values, messages map[string]string, revealed []string) string {
	payload := url.Values{}
	for field, msgs := range errs {
		for _, msg := range msgs {
			payload.Add(flashErrorPrefix+field, capFlashMessage(msg))
		}
	}
	for field, values := range old {
		for _, value := range values {
			payload.Add(flashOldPrefix+field, value)
		}
	}
	for key, message := range messages {
		payload.Set(flashMessagePrefix+key, capFlashMessage(message))
	}
	for _, field := range revealed {
		if _, ok := old[field]; ok {
			payload.Add(flashRevealKey, field)
		}
	}
	return f.signer.Sign(flashPurpose, payload.Encode(), FlashLifetime)
}

// capFlashMessage cuts a message to maxFlashMessage bytes, back to the start of
// the rune the cut would split, so a sentence in any language arrives as text
// rather than ending in a broken character.
func capFlashMessage(msg string) string {
	if len(msg) <= maxFlashMessage {
		return msg
	}
	cut := maxFlashMessage
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}

// decodeFlash splits the payload back into its three parts.
//
// A key with none of the prefixes is dropped rather than guessed at. The only
// way one arrives is a payload this version did not write, and inventing a part
// for it would put an unnamed value on a page.
func decodeFlash(raw url.Values, withheld func(string) bool) Flashed {
	var got Flashed

	for key, values := range raw {
		switch {
		case key == flashRevealKey:
			continue
		case strings.HasPrefix(key, flashErrorPrefix):
			if got.Errors == nil {
				got.Errors = map[string][]string{}
			}
			got.Errors[strings.TrimPrefix(key, flashErrorPrefix)] = values
		case strings.HasPrefix(key, flashOldPrefix):
			if got.Old == nil {
				got.Old = url.Values{}
			}
			got.Old[strings.TrimPrefix(key, flashOldPrefix)] = values
		case strings.HasPrefix(key, flashMessagePrefix):
			if len(values) == 0 || values[0] == "" {
				continue
			}
			if got.Messages == nil {
				got.Messages = map[string]string{}
			}
			got.Messages[strings.TrimPrefix(key, flashMessagePrefix)] = values[0]
		}
	}

	// The secrets are dropped again on the way out. They were dropped on the way
	// in, and this side costs nothing: a cookie minted by an older binary, or by
	// a half of the framework that grows a second Write, cannot put a password
	// back into a form through here.
	got.Old = redactFlash(got.Old, withheld, raw[flashRevealKey])
	return got
}

// longestFlashField is the field whose values cost the most bytes, which is what
// step 1 of the trimming gives up first.
//
// Ties are broken by name so the choice is the same on every run.
func longestFlashField(old url.Values) string {
	worst, size := "", -1
	for field, values := range old {
		total := len(field)
		for _, v := range values {
			total += len(v)
		}
		if total > size || (total == size && field < worst) {
			worst, size = field, total
		}
	}
	return worst
}

// sortedFlashFields returns the field names in a stable order.
func sortedFlashFields(errs map[string][]string) []string {
	out := make([]string, 0, len(errs))
	for field := range errs {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}
