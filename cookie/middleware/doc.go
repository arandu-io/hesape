// Package middleware provides two HTTP middlewares for cookies:
// [EncryptCookies], which encrypts and decrypts cookie values, and
// [AddQueuedCookiesToResponse], which flushes a cookie jar's queue onto the
// response.
//
// Each is a struct with a Handle method. Handle takes the next handler and
// returns one, rather than taking the request and a closure, because the
// response here is written as the handler runs rather than held as a value
// that can still be changed; both middlewares wrap the http.ResponseWriter
// and do their work at the last moment before the header goes out.
//
// [EncryptCookies] goes outside [AddQueuedCookiesToResponse], so that a
// queued cookie is encrypted too:
//
//	r.Get("/", h, encrypt.Handle, queue.Handle)
package middleware
