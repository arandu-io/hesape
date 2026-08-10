// Package session issues sessions, carries the one-shot flash across the
// redirect that follows a rejected form, and mints the CSRF token bound to the
// session.
//
// It mirrors Illuminate\Session. The files it answers to, in the clone at
// laravel_illuminate/session:
//
//	ArraySessionHandler.php
//	CacheBasedSessionHandler.php
//	CookieSessionHandler.php
//	DatabaseSessionHandler.php
//	EncryptedStore.php
//	ExistenceAwareInterface.php
//	FileSessionHandler.php
//	NullSessionHandler.php
//	SessionManager.php
//	SessionServiceProvider.php
//	Store.php
//	SymfonySessionDecorator.php
//	TokenMismatchException.php
//
// # What is here
//
//   - Store, the thing that starts, reads, regenerates and ends a session. The
//     cookie carries the id and an HMAC of it, so a forged cookie is refused
//     before a handler is touched.
//   - Handler, where the records live between requests, and ArrayHandler, the
//     one this package ships. A distributed store is an adapter -- see the kv
//     repository.
//   - Flash, the messages and the typed input of a rejected request, carried to
//     the page the redirect lands on.
//   - CSRF, the double-submit token bound to the session id.
//
// # What is deliberately not here
//
// The store is generic over the payload the application keeps in a session, so
// this package knows nothing about who is signed in: the subject, the roles and
// the tenant belong to the auth package, which builds the Record it hands to
// Start. Two things this package DOES keep beside the payload are the
// remember-me answer and the password confirmation stamp, because both decide
// how long the record and its cookie live, and a lifetime computed from
// something the caller may rewrite is not a lifetime.
//
// A session is not a bag of arbitrary keys either. There is no Get and no Put by
// name: the payload is one typed value, written whole. Laravel's Session::get
// exists because PHP has nowhere else to put a struct.
package session
