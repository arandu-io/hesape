package exceptions

import (
	"fmt"
	"runtime"
	"strings"
)

// InvalidArgumentException is an argument of the wrong type handed to an
// assertion.
//
// It is returned, not panicked: an assertion that could not run at all is a
// bug in the test, and the caller decides how to report it. [Create] is the
// only producer.
type InvalidArgumentException struct {
	// Argument is the one-based position of the argument that was wrong.
	Argument int

	// Type is what the argument had to be, written the way the message reads
	// it: "an array", "a string".
	Type string

	// Function is the function that was called with it.
	Function string
}

// Error implements error.
func (e *InvalidArgumentException) Error() string {
	return fmt.Sprintf("Argument #%d of %s() must be %s %s",
		e.Argument, e.Function, article(e.Type), e.Type)
}

// Create returns an [InvalidArgumentException] for the argument at the given
// one-based position, which had to be of the given type.
//
// The function it names is the caller of Create, which is the assertion that
// was misused -- naming Create itself would point at this file instead of at
// the mistake.
func Create(argument int, typ string) error {
	return &InvalidArgumentException{
		Argument: argument,
		Type:     typ,
		Function: callerName(2),
	}
}

// callerName reads the name of a function off the call stack, trimmed of its
// import path.
//
// skip is counted the way runtime.Caller counts it: 0 is callerName, 1 is
// [Create], 2 is whoever called Create. It answers "unknown" when the stack
// cannot be read.
func callerName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}

	// The runtime writes the fully qualified name -- import path, package, type
	// and method -- and only the tail after the last slash is worth printing.
	name := fn.Name()
	if at := strings.LastIndexByte(name, '/'); at >= 0 {
		name = name[at+1:]
	}
	return name
}

// article picks the indefinite article for a type name: "an array", "a
// string".
func article(typ string) string {
	if typ == "" {
		return "a"
	}
	switch strings.ToLower(typ)[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}
