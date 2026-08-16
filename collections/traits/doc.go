// Package traits is empty, and stays empty.
//
// A method's receiver must be declared in the same package as the method, so
// there is no way to mix a set of methods into a type from outside the package
// that declares it. Behaviour shared between types therefore lands directly on
// each type that has it, in the package that declares that type.
package traits
