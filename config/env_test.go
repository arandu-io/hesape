package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/config"
)

// TestStringTreatsEmptyAsAbsent: a deployment template that rendered to nothing
// is a variable nobody meant to set, and the alternative is an application name
// that appears as "" in the page title and in outgoing mail.
func TestStringTreatsEmptyAsAbsent(t *testing.T) {
	t.Setenv("ARANDU_TEST_STRING", "")

	if got := config.String("ARANDU_TEST_STRING", "fallback"); got != "fallback" {
		t.Errorf("String on an empty value = %q, want the fallback", got)
	}
	t.Setenv("ARANDU_TEST_STRING", "set")
	if got := config.String("ARANDU_TEST_STRING", "fallback"); got != "set" {
		t.Errorf("String = %q, want set", got)
	}
	if got := config.String("ARANDU_TEST_UNSET", "fallback"); got != "fallback" {
		t.Errorf("String on an unset variable = %q, want the fallback", got)
	}
}

// TestMustStringPanicsNamingTheVariable: the name is the whole of what a reader
// needs, and a panic that says "config: required" would send them reading load
// functions.
func TestMustStringPanicsNamingTheVariable(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustString on an unset variable returned instead of panicking")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "ARANDU_TEST_REQUIRED") {
			t.Fatalf("the panic does not name the variable: %v", r)
		}
	}()

	_ = config.MustString("ARANDU_TEST_REQUIRED")
}

func TestMustStringReturnsTheValue(t *testing.T) {
	t.Setenv("ARANDU_TEST_REQUIRED", "present")

	if got := config.MustString("ARANDU_TEST_REQUIRED"); got != "present" {
		t.Errorf("MustString = %q, want present", got)
	}
}

func TestBoolAcceptsEverySpellingPeopleWrite(t *testing.T) {
	for value, want := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "On": true,
		"0": false, "false": false, "FALSE": false, "no": false, "Off": false,
	} {
		t.Setenv("ARANDU_TEST_BOOL", value)
		if got := config.Bool("ARANDU_TEST_BOOL", !want); got != want {
			t.Errorf("Bool(%q) = %v, want %v", value, got, want)
		}
	}
}

// TestBoolFallsBackOnNonsense: refusing to boot over a misspelt boolean costs a
// deploy; Validate is where a combination that matters is refused.
func TestBoolFallsBackOnNonsense(t *testing.T) {
	t.Setenv("ARANDU_TEST_BOOL", "maybe")

	if !config.Bool("ARANDU_TEST_BOOL", true) {
		t.Error("Bool did not fall back on an unrecognised value")
	}
	if config.Bool("ARANDU_TEST_BOOL_UNSET", false) {
		t.Error("Bool on an unset variable did not fall back")
	}
}

func TestInt(t *testing.T) {
	t.Setenv("ARANDU_TEST_INT", "42")
	if got := config.Int("ARANDU_TEST_INT", 7); got != 42 {
		t.Errorf("Int = %d, want 42", got)
	}

	t.Setenv("ARANDU_TEST_INT", "forty-two")
	if got := config.Int("ARANDU_TEST_INT", 7); got != 7 {
		t.Errorf("Int on an unparseable value = %d, want the fallback", got)
	}
	if got := config.Int("ARANDU_TEST_INT_UNSET", 7); got != 7 {
		t.Errorf("Int on an unset variable = %d, want the fallback", got)
	}
}

// TestSecondsReadsSecondsAndNotGoDuration: "3600" travels through a Helm chart
// and "1h" does not, and accepting both would be two formats for one setting.
func TestSecondsReadsSecondsAndNotGoDuration(t *testing.T) {
	t.Setenv("ARANDU_TEST_TTL", "60")
	if got := config.Seconds("ARANDU_TEST_TTL", time.Hour); got != time.Minute {
		t.Errorf("Seconds = %v, want 1m", got)
	}

	t.Setenv("ARANDU_TEST_TTL", "1h")
	if got := config.Seconds("ARANDU_TEST_TTL", time.Hour); got != time.Hour {
		t.Errorf("Seconds on a Go duration string = %v, want the fallback", got)
	}
}

// TestSecondsFallsBackOnZeroAndNegative: a TTL of zero expires everything on
// write, which reads as a cache that does not work rather than as a
// configuration error.
func TestSecondsFallsBackOnZeroAndNegative(t *testing.T) {
	for _, value := range []string{"0", "-60"} {
		t.Setenv("ARANDU_TEST_TTL", value)
		if got := config.Seconds("ARANDU_TEST_TTL", time.Hour); got != time.Hour {
			t.Errorf("Seconds(%q) = %v, want the fallback", value, got)
		}
	}
}
