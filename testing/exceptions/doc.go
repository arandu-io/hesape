// Package exceptions holds the errors an assertion returns when it was called
// wrongly.
//
// [InvalidArgumentException] is the whole of it: an argument of the wrong type
// handed to an assertion, naming the position, the type it had to be, and the
// assertion that was misused. [Create] builds one and reads that last part off
// the call stack.
//
// It is about the test being wrong, not the code under test. An assertion that
// fails reports through testing.T; an assertion that could not run at all
// returns one of these.
package exceptions
