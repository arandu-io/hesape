package config_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/encryption"
)

// StrictBool reads exactly what Bool reads, in either direction.
func TestStrictBoolAcceptsWhatBoolAccepts(t *testing.T) {
	for value, want := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "On": true,
		"0": false, "false": false, "FALSE": false, "no": false, "Off": false,
	} {
		t.Setenv("ARANDU_TEST_BOOL", value)
		got, err := config.StrictBool("ARANDU_TEST_BOOL", !want)
		if err != nil {
			t.Errorf("StrictBool(%q): %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("StrictBool(%q) = %v, want %v", value, got, want)
		}
		if lenient := config.Bool("ARANDU_TEST_BOOL", !want); lenient != got {
			t.Errorf("%q: StrictBool says %v and Bool says %v", value, got, lenient)
		}
	}
}

// A word the reader does not know is refused, where Bool would fall back on it
// in silence. "t" and "f" are the spellings strconv.ParseBool takes and Bool
// does not; a value padded with spaces is refused rather than trimmed, because
// Bool would not trim it.
func TestStrictBoolRefusesAWordItCannotRead(t *testing.T) {
	for _, value := range []string{"sometimes", "yes-please", "2", "t", "F", "y", "n", " true", "true ", "true\n"} {
		t.Setenv("ARANDU_TEST_BOOL", value)
		got, err := config.StrictBool("ARANDU_TEST_BOOL", true)
		if err == nil {
			t.Errorf("ARANDU_TEST_BOOL=%q was read as %v; it reads as neither true nor false, and the default would have been used in silence", value, got)
		}
	}
}

// The refusal names the variable, shows the value quoted so a stray space is
// visible, lists what is accepted, and says how to ask for the default.
func TestStrictBoolSaysWhatItRefusedAndWhatItAccepts(t *testing.T) {
	t.Setenv("SESSION_ENCRYPT", "yes-please")

	_, err := config.StrictBool("SESSION_ENCRYPT", false)
	if err == nil {
		t.Fatal("SESSION_ENCRYPT=yes-please was accepted")
	}
	want := `SESSION_ENCRYPT is "yes-please", and it is read as a boolean.

    SESSION_ENCRYPT=true

The accepted spellings are true, false, 1, 0, yes, no, on and off, in any case. Leave it unset to keep the default.`
	if err.Error() != want {
		t.Errorf("the refusal reads:\n%s\nwant:\n%s", err, want)
	}
}

// Unset, empty and blank are the default, in both directions.
func TestStrictBoolKeepsTheDefaultWhenNothingWasWritten(t *testing.T) {
	for _, fallback := range []bool{true, false} {
		got, err := config.StrictBool("ARANDU_TEST_BOOL_UNSET", fallback)
		if err != nil || got != fallback {
			t.Errorf("unset: StrictBool = %v, %v; want %v, nil", got, err, fallback)
		}
		for _, blank := range []string{"", "   ", "\t"} {
			t.Setenv("ARANDU_TEST_BOOL", blank)
			got, err := config.StrictBool("ARANDU_TEST_BOOL", fallback)
			if err != nil || got != fallback {
				t.Errorf("ARANDU_TEST_BOOL=%q: StrictBool = %v, %v; want %v, nil", blank, got, err, fallback)
			}
		}
	}
}

// Load refuses an APP_DEBUG it cannot read, before it falls back on the
// environment's default.
//
// It used to read the variable leniently, so APP_DEBUG=sometimes was the debug
// page in dev and no debug page anywhere else, and the .env that said
// "sometimes" was never questioned.
func TestLoadRefusesAnAppDebugItCannotRead(t *testing.T) {
	for _, env := range []string{"dev", "staging", "prod"} {
		t.Run(env, func(t *testing.T) {
			emptyProject(t)
			t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))
			t.Setenv("APP_ENV", env)
			t.Setenv("APP_DEBUG", "sometimes")

			app, err := config.Load()
			if err == nil {
				t.Fatalf("APP_DEBUG=sometimes was accepted as %v in %s", app.Debug, env)
			}
			if !strings.HasPrefix(err.Error(), `APP_DEBUG is "sometimes", and it is read as a boolean.`) {
				t.Errorf("the refusal does not name the variable and its value: %v", err)
			}
		})
	}
}

// What Load accepted before it still accepts: a spelling it knows, and a blank
// value, which is the environment's default.
func TestLoadStillReadsAnAppDebugItKnows(t *testing.T) {
	for value, want := range map[string]bool{"off": false, "0": false, "No": false, "true": true, "   ": true} {
		emptyProject(t)
		t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))
		t.Setenv("APP_ENV", "dev")
		t.Setenv("APP_DEBUG", value)

		app, err := config.Load()
		if err != nil {
			t.Errorf("APP_DEBUG=%q: %v", value, err)
			continue
		}
		if app.Debug != want {
			t.Errorf("APP_DEBUG=%q: Debug = %v, want %v", value, app.Debug, want)
		}
	}
}
