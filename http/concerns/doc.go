// Package concerns is intentionally empty.
//
// Go has no mechanism for mixing a set of methods into a type from outside
// it: a method's receiver must be declared in the same package as the
// method. Shared behaviour that might otherwise live in a package of its
// own lands directly on the type that needs it, in the package that
// declares that type -- the same reasoning that keeps Macroable and
// Conditionable from being packages either.
package concerns
