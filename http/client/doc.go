// Package client mirrors Illuminate\Http\Client.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Client:
//
//	Batch.php
//	BatchInProgressException.php
//	ConnectionException.php
//	Factory.php
//	HttpClientException.php
//	PendingRequest.php
//	Pool.php
//	Request.php
//	RequestException.php
//	Response.php
//	ResponseSequence.php
//	StrayRequestException.php
//
// The client wraps net/http.Client and adds the testing surface that
// Illuminate provides: stub callbacks (Fake), response sequences, request
// recording, and assertions on what was sent. A PendingRequest is the fluent
// builder — Get, Post, WithToken, WithHeaders, Timeout, Retry — that
// materialises into an *http.Request and sends it.
//
// The Factory holds the global configuration (middleware, options, stubs) and
// issues PendingRequest instances. It is the entry point for both the
// application and the test: a test calls Factory.Fake to intercept requests
// and Factory.AssertSent to verify them.
//
// # Why not a third-party HTTP client library
//
// Go's net/http is the standard, and the testing surface — Fake, Sequence,
// Record, AssertSent — is the value this package adds. A Guzzle wrapper would
// be a dependency for a dependency.
//
// # Not mirrored, and why (ADR 0044)
//
//	Factory::psr7Response        PSR-7 is a PHP interface standard for
//	Request::toPsrRequest        request and response objects, which exists
//	Response::toPsrResponse      because PHP had no common one. Go's are
//	                             *http.Request and *http.Response, and the
//	                             three methods above are Factory.Response,
//	                             Request.HTTPRequest and
//	                             Response.HTTPResponse.
//
//	PendingRequest::buildHandlerStack           assemble Guzzle's
//	PendingRequest::pushHandlers                HandlerStack: its middleware
//	PendingRequest::buildStubHandler            chain, which exists because
//	PendingRequest::buildRecorderHandler        PHP has no interface between
//	PendingRequest::buildBeforeSendingHandler   a client and its transport.
//	                                            Go has http.RoundTripper, and
//	                                            the three behaviours the last
//	                                            three name -- stubbing,
//	                                            recording, before-sending
//	                                            callbacks -- are in
//	                                            PendingRequest.Send, reachable
//	                                            through SetHandler,
//	                                            CreateClient and
//	                                            RunBeforeSendingCallbacks.
//
//	PendingRequest::withNtlmAuth   NTLM is a Microsoft challenge-response
//	                               handshake that Guzzle gets from cURL. Go's
//	                               net/http does not carry it and neither does
//	                               anything in golang.org/x/crypto, so
//	                               offering the method would name an
//	                               authentication that does not happen.
//
//	Response::offsetExists   are PHP's ArrayAccess, the interface behind
//	Response::offsetGet      $response['name']. Go has no operator to
//	Response::offsetSet      overload; Response.JSON with a dotted key is the
//	Response::offsetUnset    read, and the PHP's two write halves throw.
//	Request::offsetExists
//	Request::offsetGet
//	Request::offsetSet
//	Request::offsetUnset
package client
