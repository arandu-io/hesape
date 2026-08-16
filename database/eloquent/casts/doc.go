// Package casts holds the custom cast contract, the two encoders every cast
// shares, and the casts this framework ships: collections, encrypted
// collections, binary, stringable and JSON.
//
// # A cast's constructors are methods on the zero value
//
// Two casts spell the same constructor -- AsCollection.Of and AsBinary.Of --
// which a package function cannot hold twice. So each cast is a type, its
// constructors are methods on it, and what would otherwise travel in a cast
// string is a field:
//
//	casts.AsCollection{}.Of(func(item any) (any, error) { … })
//	casts.AsBinary{}.UUID()
//
// The exception is Json, whose constructors have no per-cast meaning at all:
// they are package functions, and the two encoders they replace are package
// variables behind a mutex.
//
// # What is not here
//
// There is no enum cast. A Go enum is a named integer or string, whose column
// already round-trips through the field's own type with no cast at all.
//
// There is no collection subclass to cast into either: there is one generic
// collections.Collection, so naming a subclass would be a second spelling of Of.
//
// An Attribute is found through the registry rather than by reflection: the
// model registers it under its key with concerns.HasAttributes.SetAttributeMutator,
// and every mutator lookup reads that registry.
//
// The encrypter is a field on the cast rather than something it reaches for, and
// the two binary formats are decoded in asbinary.go rather than in a package
// with one caller.
package casts
