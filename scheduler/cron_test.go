package scheduler_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/scheduler"
)

func at(spec string) time.Time {
	t, err := time.Parse(time.RFC3339, spec)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseAcceptsWhatPeopleWrite(t *testing.T) {
	for _, c := range []struct {
		spec    string
		matches string
		misses  string
	}{
		{"* * * * *", "2026-08-03T13:47:00Z", ""},
		{"0 3 * * *", "2026-08-03T03:00:00Z", "2026-08-03T04:00:00Z"},
		{"30 * * * *", "2026-08-03T13:30:00Z", "2026-08-03T13:31:00Z"},
		{"*/15 * * * *", "2026-08-03T13:45:00Z", "2026-08-03T13:44:00Z"},
		{"0 9-17 * * *", "2026-08-03T09:00:00Z", "2026-08-03T18:00:00Z"},
		{"0 0 1 * *", "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z"},
		{"0 0 * * 1", "2026-08-03T00:00:00Z", "2026-08-04T00:00:00Z"}, // a Monday
		{"0,30 * * * *", "2026-08-03T13:30:00Z", "2026-08-03T13:15:00Z"},
		{"@daily", "2026-08-03T00:00:00Z", "2026-08-03T01:00:00Z"},
		{"@hourly", "2026-08-03T13:00:00Z", "2026-08-03T13:01:00Z"},
	} {
		s, err := scheduler.Parse(c.spec)
		if err != nil {
			t.Errorf("%q: %v", c.spec, err)
			continue
		}
		if !s.Matches(at(c.matches)) {
			t.Errorf("%q does not match %s", c.spec, c.matches)
		}
		if c.misses != "" && s.Matches(at(c.misses)) {
			t.Errorf("%q matches %s and should not", c.spec, c.misses)
		}
	}
}

// TestParseRefusesWhatWouldNeverFire: an unparseable spec caught at boot beats
// a task that silently never runs, which is the failure mode of every scheduler
// that validates lazily.
func TestParseRefusesWhatWouldNeverFire(t *testing.T) {
	for _, spec := range []string{
		"",            // nothing
		"* * * *",     // four fields
		"* * * * * *", // six: seconds are deliberately not supported
		"60 * * * *",  // minute out of range
		"* 24 * * *",  // hour out of range
		"* * * * 7",   // weekday out of range
		"abc * * * *", // not a number
		"5-1 * * * *", // counts backwards
		"*/0 * * * *", // a step of zero is an infinite set
		"@every 30s",  // the shorthand that would be a busy loop
	} {
		if _, err := scheduler.Parse(spec); err == nil {
			t.Errorf("%q was accepted", spec)
		}
	}
}

// TestTheErrorNamesTheField: "invalid cron" sends people to the wrong field of
// five.
func TestTheErrorNamesTheField(t *testing.T) {
	_, err := scheduler.Parse("* 99 * * *")
	if err == nil {
		t.Fatal("an hour of 99 was accepted")
	}
	if !strings.Contains(err.Error(), "hour") {
		t.Errorf("the error does not name the field: %v", err)
	}
}

func TestNextFindsTheFollowingRun(t *testing.T) {
	s := scheduler.MustParse("0 3 * * *")

	next := s.Next(at("2026-08-03T13:00:00Z"))
	if want := at("2026-08-04T03:00:00Z"); !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

// TestNextGivesUpOnASpecThatNeverFires: February 30th matches nothing, and
// returning zero is what lets `aru schedule:list` say so instead of hanging.
func TestNextGivesUpOnASpecThatNeverFires(t *testing.T) {
	s := scheduler.MustParse("0 0 30 2 *")

	if next := s.Next(at("2026-08-03T13:00:00Z")); !next.IsZero() {
		t.Fatalf("next = %s, want the zero time", next)
	}
}

// TestDayAndWeekdayAreOr matches Vixie cron: "0 0 1 * 1" is the first of the
// month AND every Monday, not their intersection. It surprises people, and
// matching the surprise beats being the one implementation that differs.
func TestDayAndWeekdayAreOr(t *testing.T) {
	s := scheduler.MustParse("0 0 1 * 1")

	if !s.Matches(at("2026-08-01T00:00:00Z")) { // the first, a Saturday
		t.Error("the first of the month did not match")
	}
	if !s.Matches(at("2026-08-03T00:00:00Z")) { // a Monday, not the first
		t.Error("a Monday did not match")
	}
	if s.Matches(at("2026-08-04T00:00:00Z")) { // a Tuesday, not the first
		t.Error("a Tuesday matched")
	}
}
