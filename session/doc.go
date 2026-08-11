// Package session issues sessions, carries the flash across the redirect that
// follows a rejected form, and mints the CSRF token bound to the session.
//
// It mirrors Illuminate\Session. The files it answers to, in the clone at
// laravel_illuminate/Session:
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
//   - [Store], Illuminate's Store: one session, loaded for one request, holding
//     a bag of keys. The flash, the old input, the CSRF token and the previous
//     URL live in it, which is to say everything that makes old() and $errors
//     work on the page a rejected form is sent back to.
//   - [SessionHandler] and the six handlers that implement it:
//     [ArraySessionHandler], [NullSessionHandler], [FileSessionHandler],
//     [CookieSessionHandler], [CacheBasedSessionHandler] and
//     [DatabaseSessionHandler].
//   - [EncryptedStore], the same session sealed before it reaches the handler.
//   - [SessionManager], which builds one from configuration.
//   - [RecordStore], the other kind of session store -- see below.
//   - [Flash], the one-shot signed cookie that carries the messages and the
//     typed input of a rejected request even when there is no session at all.
//   - [CSRF], the double-submit token bound to the session id.
//
// The middleware is github.com/arandu-io/hesape/session/middleware:
// StartSession and AuthenticateSession. The generator is
// github.com/arandu-io/hesape/session/console: SessionTableCommand.
//
// TokenMismatchException is [ErrTokenMismatch]. An exception class with no
// fields and no methods is a sentinel error in Go, and errors.Is is how a caller
// asks the question `catch (TokenMismatchException)` asks.
//
// # Two stores, and which one to reach for
//
// [Store] is the Laravel one: string keys, dot notation, `mixed` values, flash
// and old input. Reach for it for anything a page draws.
//
// [RecordStore] is the other half of what Illuminate spreads across Store,
// SessionManager and StartSession -- signing the cookie, minting ids, reading
// one typed [Record] back, ending every session of a subject at once. It is
// generic over the payload, so what auth keeps about who is signed in is
// checked by the compiler rather than asserted out of a map. They share
// [Handler] and [Record]; nothing else is duplicated.
//
// # Two flashes, and which one to reach for
//
// [Store.Flash] is the session one: it needs a session, and it survives exactly
// one request because [Store.Save] ages it.
//
// [Flash] is a signed cookie with no session behind it, and it exists for the
// three screens that need the messages most -- sign in, sign up, password reset
// -- which are submitted by somebody who has no session yet. Cleared on the read
// rather than aged, so a message cannot appear on a page nobody submitted.
//
// # SymfonySessionDecorator is not here
//
// It exists to satisfy Symfony's HttpFoundation SessionInterface, and its
// methods -- set(), clear(), getBag(), registerBag(), getMetadataBag() -- are
// either aliases of [Store]'s own or bag plumbing from a framework Go has no
// equivalent of. SessionServiceProvider is not here either: this collection has
// no container (ADR 0001), and wiring is the application's, in one place a
// person can read.
package session
