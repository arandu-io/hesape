// Package traits declares nothing, and is kept only so the import path
// resolves.
//
// Shared behaviour lives in the support package, next to the types that use
// it: support.Dump and support.Dd write a value out, support.ForwardCallTo and
// support.ForwardDecoratedCallTo relay a call to another object,
// support.WithLocale runs a callback under a locale, support.Tap hands a value
// to a callback and returns it, and the typed data accessors are embedded in
// support.Fluent, support.UriQueryString and support.ValidatedInput.
package traits
