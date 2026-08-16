// Package constraints holds the reusable matchers the assertions evaluate.
//
// A constraint answers three questions -- does this value match, what should a
// failure say, and what is this constraint called -- which is the whole of
// [Constraint]. Writing a comparison here rather than inside an assertion is
// what lets more than one assertion share it.
//
// [ArraySubset] matches a value that carries at least what a subset names.
// [SeeInOrder] matches a string that carries several values, each after the
// last.
//
// There is no database constraint here. Asking a connection whether a row is
// there belongs to hesape/arandutest, where the connection already is.
package constraints
