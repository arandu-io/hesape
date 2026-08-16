package fakes

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// TestingT is the part of *testing.T that an assertion uses.
//
// There is no ambient running test to find, so every assertion here takes the
// test as its first argument. *testing.T, *testing.B and *testing.F all
// satisfy this interface, and a recorder that captures the message satisfies
// it too -- which is how this package tests its own failure messages,
// something testing.TB cannot do because it is sealed.
//
// The assertions call Errorf rather than Fatalf, so a test that checks three
// things reports all three failures in one run instead of stopping at the
// first.
type TestingT interface {
	// Helper marks the caller as a test helper, so a failure points at the
	// line in the test rather than at the line inside this package.
	Helper()
	// Errorf records the failure and marks the test as failed.
	Errorf(format string, args ...any)
}

// Fake marks the fakes in this package.
//
// It carries one unexported method rather than none, because every type
// satisfies an empty interface and the marker would then mark nothing.
type Fake interface {
	isFake()
}

// The fakes stand in for the contracts they satisfy, and these lines say so at
// compile time: a method that drifts from the contract is a fake that cannot be
// installed where the real thing was, and the drift is worth finding here
// rather than at the call site.
//
// BusFake is not among them. Its fluent setters -- PipeThrough, Map,
// SerializeAndRestore -- return the concrete type so that a chain reads, and a
// return type does not covary in Go. QueueingDispatcher describes what a
// BusFake forwards to, which is the real dispatcher.
var (
	_ Fake             = (*MailFake)(nil)
	_ Fake             = (*QueueFake)(nil)
	_ Fake             = (*EventFake)(nil)
	_ Fake             = (*BusFake)(nil)
	_ Fake             = (*NotificationFake)(nil)
	_ Fake             = (*ExceptionHandlerFake)(nil)
	_ Queue            = (*QueueFake)(nil)
	_ Dispatcher       = (*EventFake)(nil)
	_ ExceptionHandler = (*ExceptionHandlerFake)(nil)
)

// class returns the type a fake files a recorded value under.
//
// A pointer is filed under the type it points at, so &OrderShipped{} and
// OrderShipped{} are recorded under one class.
func class(v any) reflect.Type {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// className returns the class of a value as the string an assertion message
// prints. A nil value is "<nil>".
func className(v any) string {
	t := class(v)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}

// classToken normalizes the token an assertion names a type with: a
// reflect.Type (from reflect.TypeFor[SomeJob]()), a value of the type, or a
// plain string naming an event.
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

// sameClass reports whether a value was recorded under exactly the token's
// class. QueueFake, EventFake, BusFake and NotificationFake all file records
// under the exact class, so an embedding type does not match the type it
// embeds.
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

// instanceOf reports whether a value satisfies the token, which is the looser
// test MailFake filters with -- and the reason a mailable that embeds another
// one is found by an assertion naming the embedded type.
//
// An interface token is satisfied by the value's own type or by a pointer to
// it. Any other token falls through to [sameClass].
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

// restore simulates the round trip a job makes through the queue: a payload
// that only exists in memory does not survive it, and a test that never
// round-trips finds that out in production.
//
// The round trip is encoding/json, which is what a job payload travels as. A
// value that cannot be marshalled -- one holding a channel or a func -- is
// recorded unchanged rather than dropped, because losing the record would fail
// the assertion for a reason that has nothing to do with what the test is
// checking.
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

// plural adds an s to the word unless the count is one. The full pluralizer
// lives in the string package; a failure message that says "1 times" reads as
// a bug in the test framework, and this is the whole of what it takes to avoid
// that.
func plural(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}

// callFn calls a truth test that may be nil. A nil test accepts everything.
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

// joinNames renders a handful of class names inside one line of a message.
func joinNames(names []string) string {
	return strings.Join(names, ", ")
}

// countedWere renders "2 events were" or "1 event was" for a message header.
//
// The verb agrees with the count because a framework that reports "1 event
// were dispatched" reads as one with a bug in it, and a reader who is already
// looking at a failure does not need the extra doubt.
func countedWere(count int, noun string) string {
	if count == 1 {
		return "1 " + noun + " was"
	}
	return fmt.Sprintf("%d %s were", count, plural(noun, count))
}

// countedAre renders "2 listeners are" or "1 listener is".
func countedAre(count int, noun string) string {
	if count == 1 {
		return "1 " + noun + " is"
	}
	return fmt.Sprintf("%d %s are", count, plural(noun, count))
}

// classNames returns the distinct class of each recorded value, in the order
// each was first recorded.
//
// The order is the order of the first record of that class, not alphabetical,
// because a failure message that lists what happened in the order it happened
// is the one a reader can follow.
func classNames(values []any) []string {
	seen := make(map[string]bool, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := className(value)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// classCounts renders one line per distinct class, with how many of it were
// recorded, as in "OrderShipped dispatched 2 times".
func classCounts(values []any, verb string) []string {
	counts := make(map[string]int, len(values))
	for _, value := range values {
		counts[className(value)]++
	}
	names := classNames(values)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("%s %s %d %s", name, verb, counts[name], plural("time", counts[name])))
	}
	return lines
}

// uuid returns a random version 4 UUID.
//
// The full generator lives in the string package. This is the whole of what
// the fakes ask of it -- a NotificationFake stamps an id on a notification
// that arrived without one -- and reaching across packages for sixteen random
// bytes would be the larger cost.
func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported platform, and an id that
		// no assertion reads is not worth a panic if it ever does.
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

// orderedClock keeps the last millisecond an ordered id was made in, and how
// many were made in it, so that two ids made in the same millisecond still sort
// in the order they were made.
var orderedClock struct {
	sync.Mutex
	millis  int64
	counter uint16
}

// orderedUUID returns a UUID whose bytes sort in the order it was created,
// which is what a batch id is for. It is a version 7: 48 bits of Unix
// milliseconds, then a counter, then random.
//
// The counter is the monotonic form RFC 9562 describes, and it is what makes
// the promise in the name true -- a plain version 7 ties with itself when two
// ids are made inside one millisecond, which is every batch a test dispatches
// in a loop. It is safe to call from several goroutines.
func orderedUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[8:], uint64(time.Now().UnixNano()))
	}

	millis := time.Now().UnixMilli()

	orderedClock.Lock()
	if millis > orderedClock.millis {
		orderedClock.millis, orderedClock.counter = millis, 0
	} else {
		// A clock that went backwards keeps the millisecond it had, so that
		// the ids stay ordered even when the wall clock does not.
		millis = orderedClock.millis
		orderedClock.counter++
	}
	counter := orderedClock.counter
	orderedClock.Unlock()

	b[0] = byte(millis >> 40)
	b[1] = byte(millis >> 32)
	b[2] = byte(millis >> 24)
	b[3] = byte(millis >> 16)
	b[4] = byte(millis >> 8)
	b[5] = byte(millis)
	b[6] = 0x70 | byte(counter>>8&0x0f)
	b[7] = byte(counter)
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

// formatUUID renders the sixteen bytes in the 8-4-4-4-12 form.
func formatUUID(b [16]byte) string {
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}
