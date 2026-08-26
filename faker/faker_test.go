package faker_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/faker"
)

// The one property worth testing here is determinism, and it is the reason this
// package exists rather than a dependency.
//
// A factory that fails prints its seed. The run that reproduces the failure
// takes the seed back and has to get the same rows -- otherwise the seed is
// decoration and the failure is not reproducible at all.

// TestTheSameSeedYieldsTheSameSequence is the contract.
func TestTheSameSeedYieldsTheSameSequence(t *testing.T) {
	first, second := faker.New(42), faker.New(42)

	for i := range 200 {
		a := first.Name() + first.Email() + first.Word() + first.UUID()
		b := second.Name() + second.Email() + second.Word() + second.UUID()
		if a != b {
			t.Fatalf("draw %d diverged:\n  %s\n  %s", i, a, b)
		}
	}
}

// TestTwoSeedsDiverge is the other half: a generator that answered the same
// thing for every seed would pass the test above and be useless.
func TestTwoSeedsDiverge(t *testing.T) {
	a, b := faker.New(1), faker.New(2)

	same := 0
	for range 50 {
		if a.Name() == b.Name() {
			same++
		}
	}
	if same > 25 {
		t.Fatalf("two seeds agreed on %d of 50 names; they are not independent", same)
	}
}

// TestTheSeedIsPinned is the golden test.
//
// It is deliberately brittle. Determinism across runs is what the test above
// checks; this one checks determinism across releases of this package and of
// the toolchain, which is the property a recorded seed in an old bug report
// depends on. If it fails, the sequence changed -- and that is a decision to
// take on purpose, not a line to update because it went red.
func TestTheSeedIsPinned(t *testing.T) {
	f := faker.New(2026)
	got := []string{f.Name(), f.Email(), f.Word(), f.UUID()}

	want := []string{
		"Guido Johnson",
		"shafi.hamilton110@example.invalid",
		"quartz",
		"3498d107-59cf-4d8f-aab0-09e51860dc7e",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("draw %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestUniqueDoesNotRepeatItself covers the wrapper, and covers the one place it
// deliberately stops: a bounded range runs out, and giving up is better than
// hanging.
func TestUniqueDoesNotRepeatItself(t *testing.T) {
	f := faker.New(7).Unique()

	seen := map[string]bool{}
	for range 100 {
		email := f.Email()
		if seen[email] {
			t.Fatalf("Unique repeated %q", email)
		}
		seen[email] = true
	}

	// Bool is not made unique, and the doc says so. Asking twice has to answer
	// rather than loop forever looking for a third boolean.
	_ = f.Bool()
	_ = f.Bool()
}

// TestTheGeneratedValuesLookLikeTheirNames is a shape check, not a corpus test.
func TestTheGeneratedValuesLookLikeTheirNames(t *testing.T) {
	f := faker.New(99)

	if !strings.Contains(f.Email(), "@") {
		t.Error("Email has no at sign")
	}
	// example.test and example.invalid are reserved by RFC 6761. A seeded
	// database that mails a real address is a seeded database that has mailed a
	// stranger.
	email := f.Email()
	if !strings.HasSuffix(email, "example.test") && !strings.HasSuffix(email, "example.invalid") {
		t.Errorf("Email = %q, want a reserved domain", email)
	}
	if s := f.Sentence(5); !strings.HasSuffix(s, ".") || s[0] < 'A' || s[0] > 'Z' {
		t.Errorf("Sentence = %q, want it capitalised and stopped", s)
	}
	if n := f.Int(3, 3); n != 3 {
		t.Errorf("Int(3, 3) = %d, want 3", n)
	}
	if v := f.Float(1, 2, 2); v < 1 || v > 2 {
		t.Errorf("Float(1, 2, 2) = %v, out of range", v)
	}

	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	at := f.Time(from, to)
	if at.Before(from) || at.After(to) {
		t.Errorf("Time = %v, out of range", at)
	}
	if !at.Equal(at.Truncate(time.Second)) {
		t.Errorf("Time = %v, want it truncated to the second", at)
	}
}
