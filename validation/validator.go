// Package validation checks a submitted form against a set of rules.
//
// The surface is Laravel's, string rules included, so that somebody arriving
// from it recognises the whole thing without reading anything:
//
//	var Register = validation.MustCompile(validation.Rules{
//		"name":     "required|max:255",
//		"email":    "required|email",
//		"password": "required|min:12|confirmed",
//	})
//
//	in, err := ctx.Validate(requests.Register)
//	if err != nil {
//		return err
//	}
//
// A rule set is compiled ONCE, in a package-level variable, and every rule
// string is parsed and checked there: an unknown rule, a missing or unparseable
// argument, a pattern that does not compile, a cross-field reference naming a
// field that does not exist -- each of those fails at boot, naming the field,
// the rule and the file, and all of them are reported together. A rule set that
// boots is a rule set whose names are all real.
//
// Sixty-one of Laravel's rules ship. Every one of the others is named in
// refused.go with the reason it is not here and where the answer lives instead,
// so a rule copied from a Laravel application gets an argument rather than
// "unknown rule". Two of those reasons are worth knowing before writing
// anything: `unique` and `exists` reach a repository and a rule set carries no
// security.Grant (RULE 17), and `date_format` takes a Go layout
// (date_format:2006-01-02), never a PHP one.
//
// There is no reflection and there are no struct tags. The rule set is data,
// the request struct is written by hand or generated, and the values that
// passed are read out of Input one at a time -- so a field nobody declared a
// rule for cannot reach a repository through here.
//
// This is the package Illuminate\Validation answers to. The rule table in
// rules.go is Concerns\ValidatesAttributes and Rules\*, the boot check in
// compile.go is ValidationRuleParser, and the sentence a failure carries is
// Concerns\FormatsMessages. Illuminate\Validation\Concerns and
// Illuminate\Validation\Rules do not become packages of their own: a rule here
// is one entry in one table, and splitting the arity, the check and the message
// across three files is how a rule ends up accepting an argument nobody
// validates. The pieces of the component that are not here are decisions rather
// than gaps -- Factory, ValidationServiceProvider and the DatabasePresence
// verifiers all assume a container and a repository reachable without a Grant,
// which RULES 2 and 17 refuse, and NestedRules and ConditionalRules assume a
// nested input, which a submitted form is not.
package validation

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Errors maps a field to its messages. It serializes straight into the HTMX
// partial that re-renders the form with inline errors.
type Errors map[string][]string

// Add appends a message to a field. It is a no-op on a nil map, so a caller
// that forgot to initialize the map does not panic in the middle of a request.
func (e Errors) Add(field, msg string) {
	if e == nil {
		return
	}
	e[field] = append(e[field], msg)
}

// Any reports whether validation failed.
func (e Errors) Any() bool { return len(e) > 0 }

// Error renders the errors with fields in a stable order, so that logs and
// golden files do not change between runs.
func (e Errors) Error() string {
	fields := make([]string, 0, len(e))
	for f := range e {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	var b strings.Builder
	b.WriteString("validation failed: ")
	for i, f := range fields {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s (%s)", f, strings.Join(e[f], ", "))
	}
	return b.String()
}

// Validatable is implemented by every request type.
type Validatable interface {
	Validate() Errors
}

// The helper list below is short on purpose: domain rules do not belong to the
// framework.

// Required rejects an empty or blank value.
func Required(e Errors, field, value string) {
	if strings.TrimSpace(value) == "" {
		e.Add(field, "is required")
	}
}

// MinLen counts runes, not bytes: a limit measured in bytes rejects valid input
// in any language that needs more than one byte per character.
func MinLen(e Errors, field, value string, n int) {
	if len([]rune(value)) < n {
		e.Add(field, fmt.Sprintf("must be at least %d characters", n))
	}
}

// MaxLen counts runes, not bytes.
func MaxLen(e Errors, field, value string, n int) {
	if len([]rune(value)) > n {
		e.Add(field, fmt.Sprintf("must be at most %d characters", n))
	}
}

// Email checks the shape only. Deliverability is proven by sending mail, never
// by a regular expression.
//
// Whitespace is rejected rather than trimmed: an address with a space in it is
// almost always a paste accident, and silently trimming input hides the mistake
// from the person who made it.
// The shape itself is emailShape, which the `email` rule also calls: two
// implementations of "is this an address" would drift, and the one that drifted
// would be whichever a screen did not exercise.
func Email(e Errors, field, value string) {
	if !emailShape(value) {
		e.Add(field, "is not a valid email address")
	}
}

// NotZero reports a value that was never filled in.
//
// It is Required for everything that is not text. Required takes a string and
// the generator used to hand it a literal "" for an int, a date or an amount --
// so every required field of those types failed validation with "is required"
// no matter what was sent, and the generated create endpoint could not be used
// at all. Found by audit.
//
// A time.Time is asked rather than compared: a parsed "0001-01-01T00:00:00Z"
// carries a location the zero value does not, so == says they differ when they
// do not.
//
// Bool has no meaningful zero to reject -- false is an answer, not an absence --
// and the specification refuses `required` on a bool for that reason.
func NotZero[T comparable](e Errors, field string, value T) {
	if t, ok := any(value).(time.Time); ok {
		if t.IsZero() {
			e.Add(field, "is required")
		}
		return
	}
	var zero T
	if value == zero {
		e.Add(field, "is required")
	}
}

// Confirmed rejects a value its confirmation field does not repeat.
//
// It is what a "confirm your password" box is for, and the message goes on the
// confirmation rather than on the field itself: a form that reports "password
// does not match" next to the first box tells the person to change the one they
// meant, and they change it, and the form fails again.
//
// An empty confirmation is reported here rather than by Required, so the field
// gets one message instead of two saying the same thing.
func Confirmed(e Errors, field, value, confirmation string) {
	if value == "" {
		// Nothing to confirm yet. Whatever rule rejected the value itself has
		// already said so, and a second message about the copy is noise.
		return
	}
	if confirmation == "" {
		e.Add(field, "is required")
		return
	}
	if value != confirmation {
		e.Add(field, "does not match")
	}
}

// First answers to Illuminate\Support\MessageBag::first: the first message for
// a key, or the first message of any key when none is given.
//
// The PHP's $key defaults to null; the variadic is how Go spells that, and only
// the first key is read. A key with no messages is the empty string, which is
// what the empty-string default of Arr::first returns there.
//
// With no key the field order is not stable -- a map has none -- so that form
// answers "what is wrong" and not "which field". When which field failed
// matters, pass the key.
func (e Errors) First(key ...string) string {
	if len(key) > 0 {
		msgs := e[key[0]]
		if len(msgs) == 0 {
			return ""
		}
		return msgs[0]
	}
	for _, msgs := range e {
		if len(msgs) > 0 {
			return msgs[0]
		}
	}
	return ""
}

// Get answers to Illuminate\Support\MessageBag::get: every message for a key.
//
// An absent key is an empty slice, as the PHP returns [], not null. The
// wildcard form the PHP supports is not read here: this bag keys by the field
// name the validator wrote, and no rule writes a star into one.
func (e Errors) Get(key string) []string { return e[key] }

// All answers to Illuminate\Support\MessageBag::all: every message in the bag,
// with the fields in a stable order so two renders of the same failure agree.
//
// The PHP has no order to keep -- a PHP array remembers insertion -- and a Go
// map has none to remember, so the fields are sorted.
func (e Errors) All() []string {
	out := make([]string, 0, len(e))
	for _, field := range e.Keys() {
		out = append(out, e[field]...)
	}
	return out
}

// Keys answers to Illuminate\Support\MessageBag::keys, sorted for the reason
// All is.
func (e Errors) Keys() []string {
	fields := make([]string, 0, len(e))
	for field := range e {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// Has answers to Illuminate\Support\MessageBag::has: every key given has at
// least one message. No key at all is Any, as the PHP's null is.
func (e Errors) Has(keys ...string) bool {
	if len(keys) == 0 {
		return e.Any()
	}
	for _, key := range keys {
		if len(e[key]) == 0 {
			return false
		}
	}
	return true
}

// HasAny answers to Illuminate\Support\MessageBag::hasAny: at least one of the
// keys given has a message.
func (e Errors) HasAny(keys ...string) bool {
	for _, key := range keys {
		if len(e[key]) > 0 {
			return true
		}
	}
	return false
}

// Missing answers to Illuminate\Support\MessageBag::missing: not HasAny.
func (e Errors) Missing(keys ...string) bool { return !e.HasAny(keys...) }

// Messages answers to Illuminate\Support\MessageBag::messages: the raw map.
func (e Errors) Messages() map[string][]string { return e }

// GetMessages answers to Illuminate\Support\MessageBag::getMessages, which the
// PHP defines as an alias of messages.
func (e Errors) GetMessages() map[string][]string { return e.Messages() }

// Count answers to Illuminate\Support\MessageBag::count: the number of
// messages, not the number of fields.
func (e Errors) Count() int {
	n := 0
	for _, msgs := range e {
		n += len(msgs)
	}
	return n
}

// IsEmpty answers to Illuminate\Support\MessageBag::isEmpty.
func (e Errors) IsEmpty() bool { return !e.Any() }

// IsNotEmpty answers to Illuminate\Support\MessageBag::isNotEmpty.
func (e Errors) IsNotEmpty() bool { return e.Any() }

// Unique answers to Illuminate\Support\MessageBag::unique: All with the
// repeats dropped, keeping the first of each.
func (e Errors) Unique() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(e))
	for _, msg := range e.All() {
		if _, ok := seen[msg]; ok {
			continue
		}
		seen[msg] = struct{}{}
		out = append(out, msg)
	}
	return out
}

// Forget answers to Illuminate\Support\MessageBag::forget: drop every message
// for a key.
func (e Errors) Forget(key string) Errors {
	delete(e, key)
	return e
}

// AddIf answers to Illuminate\Support\MessageBag::addIf.
func (e Errors) AddIf(condition bool, field, msg string) Errors {
	if condition {
		e.Add(field, msg)
	}
	return e
}

// Merge answers to Illuminate\Support\MessageBag::merge: the messages of other
// appended to the ones already here.
func (e Errors) Merge(other Errors) Errors {
	if e == nil {
		return e
	}
	for field, msgs := range other {
		e[field] = append(e[field], msgs...)
	}
	return e
}
