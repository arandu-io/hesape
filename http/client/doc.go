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
// # Cancellation
//
// Every verb takes a context.Context first, and every attempt is made under a
// context derived from it. Cancelling it ends the request in flight, and ends
// the wait between two retried attempts, rather than holding the caller until
// the destination decides to answer. PendingRequest.Timeout is a ceiling laid
// on top of that context, applied per attempt.
//
// # What a request may reach
//
// A request built here carries a deadline, reads a bounded body, goes out over
// http or https and nothing else, and refuses to connect to an address inside
// the network -- loopback, a private range, or link-local, which is where a
// cloud metadata service answers. The check is made on the address about to be
// connected to, so a redirect is checked again rather than inheriting the
// permission of the hop that sent it.
//
// None of that is a mode to switch on. An application that talks to a service
// of its own names it with Factory.AllowInternalHosts, and one that reads
// larger answers raises Factory.MaxResponseBytes.
//
// # Why not a third-party HTTP client library
//
// Go's net/http is the standard, and the testing surface -- Fake,
// Sequence, Record, AssertSent -- is the value this package adds. A
// wrapper around a third-party HTTP client would be a dependency for a
// dependency.
package client
