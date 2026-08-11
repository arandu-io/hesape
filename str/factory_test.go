package str_test

import (
	"testing"
	"time"

	"github.com/arandu-io/hesape/str"
)

func TestCreateUUIDsUsing(t *testing.T) {
	t.Cleanup(str.ResetFactoryState)

	str.CreateUUIDsUsing(func() string { return "pinned" })
	for _, got := range []string{str.UUID(), str.UUID7(), str.OrderedUUID()} {
		if got != "pinned" {
			t.Errorf("a pinned factory produced %q, want %q", got, "pinned")
		}
	}

	str.CreateUUIDsNormally()
	if got := str.UUID(); got == "pinned" || !str.IsUUID(got) {
		t.Errorf("UUID() after CreateUUIDsNormally = %q", got)
	}
}

func TestCreateUUIDsUsingSequence(t *testing.T) {
	t.Cleanup(str.ResetFactoryState)

	str.CreateUUIDsUsingSequence([]string{"first", "second"}, nil)
	if got := str.UUID(); got != "first" {
		t.Errorf("the first value of the sequence was %q, want %q", got, "first")
	}
	if got := str.UUID(); got != "second" {
		t.Errorf("the second value of the sequence was %q, want %q", got, "second")
	}
	if got := str.UUID(); !str.IsUUID(got) {
		t.Errorf("past the end of the sequence the value was %q, want a generated UUID", got)
	}

	str.CreateUUIDsUsingSequence([]string{"only"}, func() string { return "missing" })
	if got := str.UUID(); got != "only" {
		t.Errorf("the sequence produced %q, want %q", got, "only")
	}
	if got := str.UUID(); got != "missing" {
		t.Errorf("whenMissing produced %q, want %q", got, "missing")
	}
}

func TestFreezeUUIDs(t *testing.T) {
	t.Cleanup(str.ResetFactoryState)

	frozen := str.FreezeUUIDs()
	if str.UUID() != frozen || str.UUID() != frozen {
		t.Error("a frozen UUID changed between calls")
	}
	str.CreateUUIDsNormally()

	var seen string
	inside := str.FreezeUUIDs(func(uuid string) { seen = str.UUID() })
	if seen != inside {
		t.Errorf("inside the callback the UUID was %q, want %q", seen, inside)
	}
	if got := str.UUID(); got == inside {
		t.Error("the freeze outlived its callback")
	}
}

func TestCreateULIDsUsing(t *testing.T) {
	t.Cleanup(str.ResetFactoryState)

	str.CreateULIDsUsing(func() string { return "pinned" })
	if got := str.ULID(); got != "pinned" {
		t.Errorf("a pinned factory produced %q, want %q", got, "pinned")
	}

	str.CreateULIDsUsingSequence([]string{"one"}, nil)
	if got := str.ULID(); got != "one" {
		t.Errorf("the sequence produced %q, want %q", got, "one")
	}
	if got := str.ULID(); !str.IsULID(got) {
		t.Errorf("past the end of the sequence the value was %q, want a generated ULID", got)
	}

	frozen := str.FreezeULIDs()
	if str.ULID() != frozen {
		t.Error("a frozen ULID changed between calls")
	}
	str.CreateULIDsNormally()
	if got := str.ULID(); got == frozen {
		t.Error("CreateULIDsNormally left the freeze in place")
	}
}

func TestCreateRandomStringsUsing(t *testing.T) {
	t.Cleanup(str.ResetFactoryState)

	str.CreateRandomStringsUsing(func(length int) string { return "pinned" })
	if got := str.Random(16); got != "pinned" {
		t.Errorf("a pinned factory produced %q, want %q", got, "pinned")
	}

	str.CreateRandomStringsUsingSequence([]string{"a", "b"}, nil)
	if got := str.Random(4); got != "a" {
		t.Errorf("the sequence produced %q, want %q", got, "a")
	}
	if got := str.Random(4); got != "b" {
		t.Errorf("the sequence produced %q, want %q", got, "b")
	}
	if got := str.Random(4); len(got) != 4 {
		t.Errorf("past the end of the sequence the value was %q, want four characters", got)
	}

	str.CreateRandomStringsNormally()
	if got := str.Random(8); len(got) != 8 {
		t.Errorf("Random(8) after CreateRandomStringsNormally = %q", got)
	}
}

// TestOrderedUUIDIsVersionFourAndSortsByTime pins what makes it worth having:
// the version nibble still reads 4, and two of them come back in the order they
// were made.
func TestOrderedUUIDIsVersionFourAndSortsByTime(t *testing.T) {
	first := str.OrderedUUID()
	time.Sleep(2 * time.Millisecond)
	second := str.OrderedUUID()

	if !str.IsUUID(first) {
		t.Fatalf("OrderedUUID() = %q, which is not a UUID", first)
	}
	if first[14] != '4' {
		t.Errorf("OrderedUUID() has version %q, want 4", string(first[14]))
	}
	// Only the leading timestamp is ordered; the rest is random, so two values
	// made in the same millisecond may fall either way.
	if first[:13] >= second[:13] {
		t.Errorf("OrderedUUID() went backwards: %q then %q", first, second)
	}
}
