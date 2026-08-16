// Package client is an HTTP client wrapping net/http.Client, with a
// testing surface built in: stub callbacks (Fake), response sequences,
// request recording, and assertions on what was sent. A PendingRequest is
// the fluent builder -- Get, Post, WithToken, WithHeaders, Timeout, Retry
// -- that materialises into an *http.Request and sends it.
//
// The Factory holds the global configuration (middleware, options, stubs)
// and issues PendingRequest instances. It is the entry point for both the
// application and the test: a test calls Factory.Fake to intercept
// requests and Factory.AssertSent to verify them.
//
// # Why not a third-party HTTP client library
//
// Go's net/http is the standard, and the testing surface -- Fake,
// Sequence, Record, AssertSent -- is the value this package adds. A
// wrapper around a third-party HTTP client would be a dependency for a
// dependency.
package client
