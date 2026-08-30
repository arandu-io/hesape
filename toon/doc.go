// Package toon encodes JSON-shaped Go values as Token-Oriented Object
// Notation (TOON) 4.1, pinned to the specification repository's v4.1.1
// release.
//
// The package is output-only. JSON remains the representation for request
// bodies, storage, and general-purpose APIs; TOON is an explicit adapter for
// data sent to language models.
package toon
