package database

import (
	"testing"
	"time"
)

// TestOrderedIDCarriesTheMillisecondInTheFirstSixBytes: the layout is what
// makes the id sortable, and what any reader of a version 7 UUID reads the time
// back from.
func TestOrderedIDCarriesTheMillisecondInTheFirstSixBytes(t *testing.T) {
	at := time.UnixMilli(0x0123456789ab)
	id, err := orderedID(at)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := id[:13], "01234567-89ab"; got != want {
		t.Fatalf("id = %q, want it to open with %q", id, want)
	}
	if id[14] != '7' {
		t.Fatalf("id = %q, want version 7 in the third group", id)
	}
	if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Fatalf("id = %q, want variant 10 in the fourth group", id)
	}
}

// TestOrderedIDsSortInTheOrderTheyWereMade: later ids compare greater as text,
// down to the sub-millisecond part of the clock, which is what keeps an index on
// the key growing at its right edge.
func TestOrderedIDsSortInTheOrderTheyWereMade(t *testing.T) {
	base := time.UnixMilli(1_790_000_000_000)
	instants := []time.Time{
		base,
		base.Add(300 * time.Microsecond),
		base.Add(700 * time.Microsecond),
		base.Add(time.Millisecond),
		base.Add(time.Hour),
	}
	previous := ""
	for _, at := range instants {
		id, err := orderedID(at)
		if err != nil {
			t.Fatal(err)
		}
		if id <= previous {
			t.Fatalf("id %q made at %v does not sort after %q", id, at, previous)
		}
		previous = id
	}
}

// TestOrderedIDsAreStrictlyIncreasingInOneProcess: ids made faster than the
// clock's 1/4096 of a millisecond, or under a clock that steps back, still sort
// in the order they were made.
func TestOrderedIDsAreStrictlyIncreasingInOneProcess(t *testing.T) {
	at := time.UnixMilli(1_790_000_000_000)
	first := nextOrderedStamp(at)
	same := nextOrderedStamp(at)
	back := nextOrderedStamp(at.Add(-time.Second))
	if !(first < same && same < back) {
		t.Fatalf("stamps %d, %d, %d are not strictly increasing", first, same, back)
	}

	previous := ""
	for range 10_000 {
		id, err := NewOrderedID()
		if err != nil {
			t.Fatal(err)
		}
		if id <= previous {
			t.Fatalf("id %q does not sort after %q", id, previous)
		}
		previous = id
	}
}
