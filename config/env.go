package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The readers below are the only place anything reads a setting out of the
// process environment. They are exported because the configuration of an
// application is split across packages -- the connection is parsed by database,
// the TTLs by session, the level by log -- and every one of them needs the same
// four conversions. A second copy of "read an int, fall back on nonsense" is
// how two settings end up disagreeing about what an empty value means.
//
// All of them read the environment [Load] has already populated from.env, so
// none of them are correct before Load has run.

// String returns the value of key, or fallback when it is unset or empty.
//
// Empty counts as absent here, unlike in [LoadDotenv]: a variable a deployment
// template rendered to nothing is a variable nobody meant to set, and the
// alternative is a name that renders as "" on the login page.
func String(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// MustString returns the value of a required variable and panics when it is
// unset or empty. The message names the variable.
//
// It panics rather than returning an error because of where it is called: a
// load function, once, at process start, before anything is listening. Giving
// it an error return would mean threading one through every loader, and the
// value of that thread is a message printed a few frames higher. A required
// setting that is missing is not a condition to handle -- it is a process that
// must not start.
//
// Never call it on a request path. There is no APP_ setting worth a 500 that
// String with a documented fallback could not have covered.
func MustString(key string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	panic("config: " + key + " is required and is not set")
}

// Bool returns the value of key read as a boolean, or fallback when it is
// unset, empty or not one of the accepted spellings.
//
// The accepted spellings are 1/true/yes/on and 0/false/no/off, in any case,
// because all six appear in.env files people have already written. Anything
// else falls back instead of failing, which is right for a setting whose
// fallback is a working answer. Where a dropped word would change what the
// process does -- whether the debug page shows, whether a cookie is Secure --
// read it with [StrictBool], which refuses the word instead.
func Bool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	if value, known := parseBool(v); known {
		return value
	}
	return fallback
}

// boolSpellings is what parseBool reads, in the words a refusal shows.
const boolSpellings = "true, false, 1, 0, yes, no, on and off, in any case"

// StrictBool returns the value of key read as a boolean, or fallback when it is
// unset or blank, and an error when it is written and is not one of the
// spellings [Bool] accepts.
//
// It is Bool for a setting read once at boot, where falling back in silence is
// the defect: APP_DEBUG=sometimes would be the environment's default with the
// .env saying otherwise and no line anywhere reporting the word was dropped.
// The spellings are Bool's own, read by the same parser, so the two never
// disagree about what "yes" means.
//
// Unset, empty and blank -- only spaces -- are the default, because a variable a
// template rendered to nothing is a variable nobody meant to set. A value
// padded with spaces is refused rather than trimmed, because Bool would not
// trim it: the error shows it quoted, so the space is visible. The error names
// the variable and the accepted spellings, and ends with how to ask for the
// default.
func StrictBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	value, known := parseBool(v)
	if !known {
		return false, fmt.Errorf(`%s is %q, and it is read as a boolean.

    %s=true

The accepted spellings are %s. Leave it unset to keep the default.`, key, v, key, boolSpellings)
	}
	return value, nil
}

// parseBool reads v as one of the accepted spellings, and reports whether it
// was one. It is the one parser behind [Bool] and [StrictBool].
func parseBool(v string) (value, known bool) {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// Int returns the value of key read as an integer, or fallback when it is unset
// or does not parse.
func Int(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Seconds returns the value of key read as a count of seconds, or fallback when
// it is unset, does not parse, or is not positive.
//
// Seconds, not Go's duration syntax: these values are written by deployment
// tooling as often as by people, and "3600" travels through a Helm chart better
// than "1h". Zero and negative fall back because a TTL of zero expires
// everything on write, which reads as a cache that does not work rather than as
// a configuration error.
func Seconds(key string, fallback time.Duration) time.Duration {
	if n := Int(key, -1); n > 0 {
		return time.Duration(n) * time.Second
	}
	return fallback
}
