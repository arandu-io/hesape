package str_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/str"
)

func TestUUIDIsVersion4AndPassesItsOwnCheck(t *testing.T) {
	id := str.UUID()
	if !str.IsUUID(id) {
		t.Fatalf("UUID() = %q, which IsUUID refuses", id)
	}
	if id[14] != '4' {
		t.Errorf("UUID() = %q, want version 4 in the third group", id)
	}
	if !strings.ContainsRune("89ab", rune(id[19])) {
		t.Errorf("UUID() = %q, want the RFC 4122 variant in the fourth group", id)
	}
}

func TestUUID7IsVersion7AndSortsByTime(t *testing.T) {
	id := str.UUID7()
	if !str.IsUUID(id) {
		t.Fatalf("UUID7() = %q, which IsUUID refuses", id)
	}
	if id[14] != '7' {
		t.Errorf("UUID7() = %q, want version 7 in the third group", id)
	}

	// The point of version 7 is that a later one sorts after an earlier one, and
	// that is what a cursor and a clustered primary key both rest on.
	first := str.UUID7()
	time.Sleep(2 * time.Millisecond)
	second := str.UUID7()
	if first >= second {
		t.Errorf("UUID7 %q was generated before %q but does not sort before it", first, second)
	}
}

func TestULIDIsSortableAndInTheAlphabet(t *testing.T) {
	id := str.ULID()
	if len(id) != 26 {
		t.Fatalf("ULID() = %q, want 26 characters", id)
	}
	if !str.IsULID(id) {
		t.Fatalf("ULID() = %q, which IsULID refuses", id)
	}
	for _, r := range id {
		if strings.ContainsRune("ILOU", r) {
			t.Errorf("ULID() = %q, which holds a rune Crockford base32 excludes", id)
		}
	}

	first := str.ULID()
	time.Sleep(2 * time.Millisecond)
	second := str.ULID()
	if first >= second {
		t.Errorf("ULID %q was generated before %q but does not sort before it", first, second)
	}
}

func TestIsUUIDAndIsULIDRefuseWhatTheValidatorRefused(t *testing.T) {
	for _, c := range []struct {
		value string
		uuid  bool
		ulid  bool
	}{
		{"0f3a5f5e-4d2b-4e3a-9c1d-1f2e3a4b5c6d", true, false},
		{"0f3a5f5e4d2b4e3a9c1d1f2e3a4b5c6d", false, false},
		{"not-a-uuid", false, false},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", false, true},
		{"01ARZ3NDEKTSV4RRFFQ69G5FA", false, false},  // one character short
		{"81ARZ3NDEKTSV4RRFFQ69G5FAV", false, false}, // timestamp overflows
		{"01ARZ3NDEKTSV4RRFFQ69G5FAU", false, false}, // U is not in the alphabet
		{"", false, false},
	} {
		if got := str.IsUUID(c.value); got != c.uuid {
			t.Errorf("IsUUID(%q) = %v, want %v", c.value, got, c.uuid)
		}
		if got := str.IsULID(c.value); got != c.ulid {
			t.Errorf("IsULID(%q) = %v, want %v", c.value, got, c.ulid)
		}
	}
}

func TestRandomIsTheLengthAskedForAndAlphanumeric(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 16, 40, 257} {
		got := str.Random(n)
		want := max(n, 0)
		if len(got) != want {
			t.Fatalf("Random(%d) = %q, want %d characters", n, got, want)
		}
		for _, r := range got {
			alphanumeric := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
			if !alphanumeric {
				t.Fatalf("Random(%d) = %q, which holds %q", n, got, r)
			}
		}
	}
}

// TestRandomDoesNotRepeatItself is the cheapest check that the generator is
// wired to the system source at all: a constant or an unseeded counter fails it.
func TestRandomDoesNotRepeatItself(t *testing.T) {
	seen := make([]string, 0, 64)
	for range 64 {
		seen = append(seen, str.Random(16))
	}
	slices.Sort(seen)
	if len(slices.Compact(seen)) != 64 {
		t.Error("Random(16) returned the same value twice in sixty-four draws")
	}
}
