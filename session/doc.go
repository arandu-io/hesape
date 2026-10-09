// Package session issues sessions, carries the flash across the redirect that
// follows a rejected form, and mints the CSRF token bound to the session.
//
// # The session is RecordStore
//
// [RecordStore] is the one session path. It mints an id, signs the cookie the
// id travels in, and keeps one typed [Record] per session in a [Handler]. The
// signature is checked before the handler is touched, so a forged cookie never
// reaches the store. The record carries the tenant and the subject, which is
// what lets [RecordStore.DestroyOthers] end every other session of an account,
// and the password confirmation stamp [RecordStore.Confirm] writes.
//
// What is on that path:
//
//   - [RecordStore] and [NewRecordStore], with [Remember] to ask for a session
//     that survives closing the browser.
//   - [Record], and [Handler], where it lives between requests: [ArrayHandler]
//     in memory, the RESP handler in hesape/redis for more than one instance,
//     and [Decode] over a [Keeper] that stores raw records.
//   - [Flash], the one-shot signed cookie that carries the messages and the
//     typed input of a rejected request even when there is no session at all.
//     Cleared on the read, so a message cannot appear on a page nobody
//     submitted.
//   - [CSRF], the double-submit token bound to the session id, or to a guest
//     id for a visitor who has none.
//
// [ErrTokenMismatch] is a sentinel error: no fields, no methods, checked
// with errors.Is.
//
// # The second path, and why it is deprecated
//
// [Store], built by [SessionManager] and loaded by the StartSession middleware
// in hesape/session/middleware, is a second session: a bag of keys per
// request, with its own flash, old input and CSRF token, behind six
// [SessionHandler] implementations and an [EncryptedStore]. It shares nothing
// with [RecordStore] but this package -- not the handler and not the record --
// and the id its cookie carries is not signed.
//
// Nothing outside that path builds it, and two ways to keep a session are two
// ways for one of them to be the one nobody tested. Every name on it is
// deprecated in favour of [RecordStore] and goes in a later minor release, with
// the middleware, the sessions table, and what exists only on top of the
// Store in hesape/auth, hesape/http and hesape/testing.
package session
