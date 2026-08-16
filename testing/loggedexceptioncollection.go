package testing

import "sort"

// LoggedExceptionCollection is every error the application logged while a
// request was being handled, in the order they were logged.
//
// It is a slice because the type exists to be named in a signature, not to add
// behaviour.
type LoggedExceptionCollection []error

// First returns the first error logged, or nil when nothing was.
func (c LoggedExceptionCollection) First() error {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// Last returns the last error logged, or nil when nothing was.
//
// It is the one a failure message uses: when a handler catches an error, logs
// it and answers 500, the last logged error is the cause, and printing it is
// the difference between "expected 200, got 500" and a sentence naming the bug.
func (c LoggedExceptionCollection) Last() error {
	if len(c) == 0 {
		return nil
	}
	return c[len(c)-1]
}

// IsEmpty reports whether nothing was logged.
func (c LoggedExceptionCollection) IsEmpty() bool { return len(c) == 0 }

// Count returns how many errors were logged.
func (c LoggedExceptionCollection) Count() int { return len(c) }

// Push appends errors and returns the extended collection, the way append
// does.
func (c LoggedExceptionCollection) Push(errs ...error) LoggedExceptionCollection {
	return append(c, errs...)
}

// sortStrings sorts names in place.
//
// It exists so the assertion helpers that walk a map produce their messages in
// a fixed order: a map range is randomised in Go, and a failure message whose
// field order changes between runs cannot be compared against a previous run.
func sortStrings(values []string) { sort.Strings(values) }
