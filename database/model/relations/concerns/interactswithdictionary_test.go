package concerns

import (
	"fmt"
	"strings"
	"testing"
)

// stringerKey is a value that answers String(), which is the PHP's __toString
// and an enum's value in one.
type stringerKey struct{ value string }

func (k stringerKey) String() string { return k.value }

// TestGetDictionaryKeyRendersEveryScalarToAString.
//
// A PHP array key coerces: $dictionary[1] and $dictionary["1"] are one bucket. A
// Go map keyed by any does not, so a parent whose key the driver returned as
// int64(1) would miss a child whose foreign key came back as "1" -- and the
// relation would come out empty with every value on screen looking right.
//
// Rendering everything to a string is what keeps the dictionary matching the way
// the PHP one matches, so the table below is the coercion itself.
func TestGetDictionaryKeyRendersEveryScalarToAString(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"admin", "admin"},
		{[]byte("admin"), "admin"},
		{stringerKey{"admin"}, "admin"},

		{true, "true"},
		{false, "false"},

		{int(1), "1"},
		{int8(1), "1"},
		{int16(1), "1"},
		{int32(1), "1"},
		{int64(1), "1"},
		{int64(-1), "-1"},

		{uint(1), "1"},
		{uint8(1), "1"},
		{uint16(1), "1"},
		{uint32(1), "1"},
		{uint64(1), "1"},

		{float32(1), "1"},
		{float64(1), "1"},
		{float64(1.5), "1.5"},
	} {
		got, err := GetDictionaryKey(c.in)
		if err != nil {
			t.Errorf("GetDictionaryKey(%#v): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("GetDictionaryKey(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEveryIntegerTypeKeysToTheSameBucket is the property the table above only
// implies, asserted directly: one row's key, read back as any of the shapes a
// driver or a caller can hand over, has to land in one bucket.
func TestEveryIntegerTypeKeysToTheSameBucket(t *testing.T) {
	shapes := []any{
		int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		float32(1), float64(1),
		"1", []byte("1"), stringerKey{"1"},
	}

	buckets := map[string][]any{}
	for _, shape := range shapes {
		key, err := GetDictionaryKey(shape)
		if err != nil {
			t.Fatalf("GetDictionaryKey(%#v): %v", shape, err)
		}
		buckets[key] = append(buckets[key], shape)
	}

	if len(buckets) != 1 {
		t.Fatalf("one row's key landed in %d buckets: %#v", len(buckets), buckets)
	}
}

// TestAStringerWinsOverTheTypeUnderIt.
//
// The case order matters: a named string type that answers String() has to be
// asked, not read as the string it is built on, because that is what an enum
// whose value differs from its name is.
func TestAStringerWinsOverTheTypeUnderIt(t *testing.T) {
	got, err := GetDictionaryKey(stringerKey{"published"})
	if err != nil {
		t.Fatalf("GetDictionaryKey: %v", err)
	}
	if got != "published" {
		t.Fatalf("GetDictionaryKey = %q, want the value String() answered", got)
	}
}

// TestGetDictionaryKeyRefusesWhatCannotKeyAnything.
//
// The PHP throws InvalidArgumentException for the same case. Answering the empty
// string instead would put every unkeyable value in one bucket, which is a
// relation matching rows that have nothing to do with each other.
func TestGetDictionaryKeyRefusesWhatCannotKeyAnything(t *testing.T) {
	for _, unkeyable := range []any{
		struct{ A int }{A: 1},
		[]int{1, 2},
		map[string]int{"a": 1},
		func() {},
	} {
		got, err := GetDictionaryKey(unkeyable)
		if err == nil {
			t.Errorf("GetDictionaryKey(%T) = %q with no error, and it cannot key anything", unkeyable, got)
			continue
		}
		if got != "" {
			t.Errorf("GetDictionaryKey(%T) answered %q beside an error", unkeyable, got)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%T", unkeyable)) {
			t.Errorf("GetDictionaryKey(%T): %v, and the error has to name the type", unkeyable, err)
		}
	}
}

// TestNilKeysToTheEmptyStringWithoutAnError.
//
// A nullable foreign key is a row that belongs to nothing, and it is not a
// programming error. It keys to the empty bucket, which nothing with a real key
// matches.
func TestNilKeysToTheEmptyStringWithoutAnError(t *testing.T) {
	got, err := GetDictionaryKey(nil)
	if err != nil {
		t.Fatalf("GetDictionaryKey(nil): %v", err)
	}
	if got != "" {
		t.Fatalf("GetDictionaryKey(nil) = %q", got)
	}

	real, err := GetDictionaryKey(int64(1))
	if err != nil {
		t.Fatalf("GetDictionaryKey: %v", err)
	}
	if got == real {
		t.Fatal("a null foreign key landed in the same bucket as a real one")
	}
}

// TestDictionaryKeyAnswersTheEmptyStringForWhatItCannotRender.
//
// The unexported wrapper is what Sync and Toggle compare through, and it drops
// the error. That is the right trade there -- an unkeyable id is one that
// matches nothing rather than a call that fails -- but it means two unkeyable
// values compare equal, so it is pinned rather than assumed.
func TestDictionaryKeyAnswersTheEmptyStringForWhatItCannotRender(t *testing.T) {
	if got := dictionaryKey(int64(7)); got != "7" {
		t.Fatalf("dictionaryKey = %q for a real key", got)
	}
	if got := dictionaryKey(struct{ A int }{A: 1}); got != "" {
		t.Fatalf("dictionaryKey = %q for a value it cannot render", got)
	}
	if dictionaryKey(struct{ A int }{A: 1}) != dictionaryKey(nil) {
		t.Fatal("an unkeyable value and a null key landed in different buckets, and this pins that they do not")
	}
}
