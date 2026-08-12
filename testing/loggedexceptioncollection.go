package testing

import "sort"

// LoggedExceptionCollection answers to
// Illuminate\Testing\LoggedExceptionCollection: every error the application
// logged while a request was being handled.
//
// In PHP it is an empty subclass of Collection, and the whole of it is the
// name -- what it carries is errors, so a failing assertion can say which one
// the handler swallowed instead of only that the status was 500. Here it is a
// slice for the same reason: the type exists to be named in a signature, not to
// add behaviour.
//
// The collection methods PHP inherits from Collection are not repeated on it;
// the ones a test actually reaches for are [LoggedExceptionCollection.First],
// [LoggedExceptionCollection.Last] and [LoggedExceptionCollection.IsEmpty], and
// anything else is a range over the slice.
type LoggedExceptionCollection []error

// First answers to Collection::first. It returns nil when nothing was logged,
// which is the same answer PHP gives.
func (c LoggedExceptionCollection) First() error {
	if len(c) == 0 {
		return nil
	}
	return c[0]
}

// Last answers to Collection::last.
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

// IsEmpty answers to Collection::isEmpty.
func (c LoggedExceptionCollection) IsEmpty() bool { return len(c) == 0 }

// Count answers to Collection::count.
func (c LoggedExceptionCollection) Count() int { return len(c) }

// Push answers to Collection::push.
func (c LoggedExceptionCollection) Push(errs ...error) LoggedExceptionCollection {
	return append(c, errs...)
}

// sortStrings sorts names in place.
//
// It exists so the assertion helpers that walk a map produce their messages in
// a fixed order: a map range is randomised in Go, and a failure message whose
// field order changes between runs cannot be compared against a previous run.
func sortStrings(values []string) { sort.Strings(values) }
