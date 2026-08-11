package validation

import (
	"fmt"
	"sort"
	"strings"
)

// Errors maps a field to its messages. It answers to
// Illuminate\Support\MessageBag, which is what Laravel's $errors is in a view,
// and it serializes straight into the HTMX partial that re-renders the form
// with inline errors.
type Errors map[string][]string

// Add answers to MessageBag::add. It is a no-op on a nil map, so a caller that
// forgot to initialize the map does not panic in the middle of a request.
func (e Errors) Add(field, msg string) {
	if e == nil {
		return
	}
	e[field] = append(e[field], msg)
}

// Any answers to MessageBag::any: validation failed.
func (e Errors) Any() bool { return len(e) > 0 }

// Error renders the errors with fields in a stable order, so that logs and
// golden files do not change between runs.
func (e Errors) Error() string {
	fields := e.Keys()

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

// First answers to MessageBag::first: the first message for a key, or the first
// message of any key when none is given.
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

// Get answers to MessageBag::get: every message for a key.
//
// An absent key is an empty slice, as the PHP returns [], not null. The
// wildcard form the PHP supports is not read here: this bag keys by the field
// name the validator wrote, and no rule writes a star into one.
func (e Errors) Get(key string) []string { return e[key] }

// All answers to MessageBag::all: every message in the bag, with the fields in
// a stable order so two renders of the same failure agree.
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

// Keys answers to MessageBag::keys, sorted for the reason All is.
func (e Errors) Keys() []string {
	fields := make([]string, 0, len(e))
	for field := range e {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// Has answers to MessageBag::has: every key given has at least one message. No
// key at all is Any, as the PHP's null is.
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

// HasAny answers to MessageBag::hasAny: at least one of the keys has a message.
func (e Errors) HasAny(keys ...string) bool {
	for _, key := range keys {
		if len(e[key]) > 0 {
			return true
		}
	}
	return false
}

// Missing answers to MessageBag::missing: not HasAny.
func (e Errors) Missing(keys ...string) bool { return !e.HasAny(keys...) }

// Messages answers to MessageBag::messages: the raw map.
func (e Errors) Messages() map[string][]string { return e }

// GetMessages answers to MessageBag::getMessages, which the PHP defines as an
// alias of messages.
func (e Errors) GetMessages() map[string][]string { return e.Messages() }

// Count answers to MessageBag::count: the number of messages, not the number of
// fields.
func (e Errors) Count() int {
	n := 0
	for _, msgs := range e {
		n += len(msgs)
	}
	return n
}

// IsEmpty answers to MessageBag::isEmpty.
func (e Errors) IsEmpty() bool { return !e.Any() }

// IsNotEmpty answers to MessageBag::isNotEmpty.
func (e Errors) IsNotEmpty() bool { return e.Any() }

// Unique answers to MessageBag::unique: All with the repeats dropped, keeping
// the first of each.
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

// Forget answers to MessageBag::forget: drop every message for a key.
func (e Errors) Forget(key string) Errors {
	delete(e, key)
	return e
}

// AddIf answers to MessageBag::addIf.
func (e Errors) AddIf(condition bool, field, msg string) Errors {
	if condition {
		e.Add(field, msg)
	}
	return e
}

// Merge answers to MessageBag::merge: the messages of other appended to the
// ones already here.
func (e Errors) Merge(other Errors) Errors {
	if e == nil {
		return e
	}
	for field, msgs := range other {
		e[field] = append(e[field], msgs...)
	}
	return e
}
