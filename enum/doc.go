// Package enum is the contract a closed set of values satisfies, and the
// operations the layers around it derive from that contract instead of
// repeating the set.
//
// Go has no enum keyword, so a closed set is a named type over a string or an
// integer plus the methods that refuse everything outside it. The generator
// writes those methods; this package names them, so validation, serialization,
// persistence and a form can recognise the type at runtime and read the cases
// off it.
//
// The defect it exists to remove is the second copy of the set. A list of cases
// written a second time -- in a validation rule, in a <select>, in a switch --
// is a list that can disagree with the type, and nothing compares them. Every
// function here takes the type, or a value of it, and answers from that.
//
// # Two spellings, and which one a function reads
//
// A value has a shown spelling, which is String, and a stored spelling, which
// is what Value hands the column. For a text-backed set they are the same
// string. For an integer-backed one they are not: the column holds 2 and the
// form shows "normal". Every function here says which of the two it reads, and
// the ones that cannot answer for an integer-backed set say so rather than
// guessing.
package enum
