package exceptions

import (
	"fmt"
	"runtime"
	"strings"
)

// InvalidArgumentException answers to
// Illuminate\Testing\Exceptions\InvalidArgumentException: an argument of the
// wrong type handed to an assertion.
//
// It is an error rather than a thrown exception, which is the (T, error)
// mechanical change: PHP throws, Go returns. It is the one shape a caller
// needs, because the only producer is Create.
type InvalidArgumentException struct {
	// Argument is the one-based position of the argument that was wrong, as
	// PHPUnit counts them.
	Argument int

	// Type is what the argument had to be, written the way the message says
	// it: "array or ArrayAccess".
	Type string

	// Function is the function that was called with it.
	Function string
}

// Error implements error.
func (e *InvalidArgumentException) Error() string {
	return fmt.Sprintf("Argument #%d of %s() must be %s %s",
		e.Argument, e.Function, article(e.Type), e.Type)
}

// Create answers to InvalidArgumentException::create.
//
// The PHP reads the call stack to name the function that was passed the bad
// argument, and so does this: runtime.Caller stands in for debug_backtrace.
// The name is the caller of Create, which is the assertion that was misused --
// naming Create itself would point at this file instead of at the mistake.
func Create(argument int, typ string) error {
	return &InvalidArgumentException{
		Argument: argument,
		Type:     typ,
		Function: callerName(2),
	}
}

// callerName answers to the debug_backtrace() read at the top of create().
//
// skip is counted the way runtime.Caller counts it: 0 is callerName, 1 is
// Create, 2 is whoever called Create.
func callerName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}

	// runtime writes the fully qualified name -- import path, package, type and
	// method. The PHP prints Class::method, so the import path is dropped and
	// what is left is the last two segments.
	name := fn.Name()
	if at := strings.LastIndexByte(name, '/'); at >= 0 {
		name = name[at+1:]
	}
	return name
}

// article answers to the in_array(lcfirst($type)[0], ['a','e','i','o','u'])
// in create(): "an array", "a string".
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
