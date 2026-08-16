// Package reflection exports nothing.
//
// A function value carries its parameter types in its own type, a struct's
// fields are known to the compiler, and a type assertion asks at the call site
// what a runtime lookup would only ask later. Nothing in this module needs a
// package for reading types back at run time, so none is provided.
//
// Where the same information is needed in order to emit code, it is read from
// go/ast at build time, which makes a wrong answer a build failure rather than
// a panic in production.
package reflection
