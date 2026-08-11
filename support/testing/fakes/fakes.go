package fakes

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// TestingT is the part of *testing.T that an assertion uses.
//
// PHPUnit's assertions are static calls on PHPUnit\Framework\Assert, which
// knows the running test through global state. Go has no such thing, so every
// assertion here takes the test as its first argument. *testing.T, *testing.B
// and *testing.F all satisfy this interface, and a recorder that captures the
// message satisfies it too -- which is how this package tests its own failure
// messages, something testing.TB cannot do because it is sealed.
//
// Errorf rather than Fatalf: PHPUnit aborts the test on the first failed
// assertion, and Go's convention is to report and carry on, so that a test that
// checks three things reports all three failures in one run.
type TestingT interface {
	// Helper marks the caller as a test helper, so a failure points at the
	// line in the test rather than at the line inside this package.
	Helper()
	// Errorf records the failure and marks the test as failed.
	Errorf(format string, args ...any)
}

// Fake marks the fakes in this package, and answers the empty
// Illuminate\Support\Testing\Fakes\Fake interface.
//
// The PHP is an interface with no methods, which Go cannot express as a marker:
// every type satisfies an empty Go interface. It carries one unexported method
// instead, so only the fakes in this package are Fake.
type Fake interface {
	isFake()
}

// class answers PHP's get_class(): the type a fake files a recorded value
// under.
//
// A pointer is filed under the type it points at. PHP has no pointers, and
// get_class(new OrderShipped) is one string however the object was passed
// around, so &OrderShipped{} and OrderShipped{} are the same class here.
func class(v any) reflect.Type {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// className is get_class() as the string an assertion message prints.
func className(v any) string {
	t := class(v)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}

// classToken normalizes what an assertion was handed in place of PHP's
// SomeJob::class: a reflect.Type (from reflect.TypeFor[SomeJob]()), a value of
// the type, or a plain string naming an event.
//
// A string token has no reflect.Type -- string events exist only in EventFake --
// so it returns nil and callers fall back to comparing names.
func classToken(token any) reflect.Type {
	switch tk := token.(type) {
	case nil:
		return nil
	case reflect.Type:
		t := tk
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return t
	case string:
		return nil
	default:
		return class(token)
	}
}

// tokenName is the name an assertion message prints for a class token.
func tokenName(token any) string {
	switch tk := token.(type) {
	case nil:
		return "<nil>"
	case reflect.Type:
		return tk.String()
	case string:
		return tk
	default:
		return className(token)
	}
}

// sameClass answers the array lookup PHP does with get_class($job) as the key:
// QueueFake, EventFake, BusFake and NotificationFake all file records under the
// exact class, so a subclass does not answer for its parent there.
func sameClass(v any, token any) bool {
	t := classToken(token)
	if t == nil {
		if name, ok := token.(string); ok {
			return className(v) == name
		}
		return false
	}
	return class(v) == t
}

// instanceOf answers PHP's `$value instanceof $type`, which is what
// MailFake::mailablesOf filters with -- and the reason a mailable that embeds
// another one is found by an assertion naming the embedded type.
//
// An interface token is satisfied by the value's own type or by a pointer to
// it, because a method set declared on a pointer receiver is the ordinary way
// to write one in Go and PHP draws no such line.
func instanceOf(v any, token any) bool {
	if tk, ok := token.(reflect.Type); ok && tk.Kind() == reflect.Interface {
		t := reflect.TypeOf(v)
		if t == nil {
			return false
		}
		if t.Implements(tk) {
			return true
		}
		return t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(tk)
	}
	return sameClass(v, token)
}

// restore answers PHP's unserialize(serialize($value)), which the fakes use to
// simulate the round trip a job makes through the queue: a payload that only
// exists in memory does not survive it, and a test that never round-trips finds
// that out in production.
//
// Go has no generic serializer, so the round trip is encoding/json, which is
// what a job payload travels as in this ecosystem anyway. A value that cannot
// be marshalled -- one holding a channel or a func -- is recorded unchanged
// rather than dropped, because losing the record would fail the assertion for a
// reason that has nothing to do with what the test is checking.
func restore(v any) any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	t := reflect.TypeOf(v)
	pointer := t.Kind() == reflect.Pointer
	if pointer {
		t = t.Elem()
	}
	out := reflect.New(t)
	if err := json.Unmarshal(data, out.Interface()); err != nil {
		return v
	}
	if pointer {
		return out.Interface()
	}
	return out.Elem().Interface()
}

// plural answers Illuminate\Support\Str::plural for the one word the assertion
// messages need. The full pluralizer lives in the str package; a fake message
// that says "1 times" reads as a bug in the test framework, and this is the
// whole of what it takes to avoid that.
func plural(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}

// callFn calls a truth test that may be nil, which is how every PHP assertion
// here spells "no truth test": `$callback = $callback ?: fn () => true`.
func callFn[T any](callback func(T) bool, value T) bool {
	if callback == nil {
		return true
	}
	return callback(value)
}

// listOf renders the lines an assertion message ends with: what was actually
// recorded, so a failure says what happened instead of only what did not.
func listOf(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "\n- " + strings.Join(lines, "\n- ") + "\n"
}

// countedAs renders "2 mailables" or "1 mailable" for a message header.
func countedAs(count int, noun string) string {
	return fmt.Sprintf("%d %s", count, plural(noun, count))
}
