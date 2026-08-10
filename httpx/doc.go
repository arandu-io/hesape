// Package httpx is Arandu's Http.
//
// It holds what a request is and what an action answers with: [Context] and its
// readers, [State] for what the framework knows about a request before the
// handler runs, [Reject] and [Back] for a form that failed its rules, [Intended]
// for the address somebody was going to when a guard turned them away, and
// [LocalPath], which is the one answer in the collection to "is this address
// ours".
//
// Routing is not here. hesape/routing matches a request, builds a Context with
// [NewContext] and calls the action; this package registers nothing and knows no
// pattern. The split is Illuminate's own -- Http and Routing are two components
// -- and it is what lets a controller be tested without a router.
//
// # Why httpx and not http
//
// package http collides with net/http in every file of this package:
// [Context.Response] is an http.ResponseWriter and [Middleware] is
// func(http.Handler) http.Handler. The x is what Go writes when the standard
// library already has the word. What a caller types is unchanged: httpx.Context.
//
// # Middleware is an alias
//
//	type Middleware = pipeline.Middleware[http.Handler]
//
// The = is load-bearing. It is what lets hesape/session, hesape/cookie and
// hesape/exception produce middleware without importing this package -- a
// defined type there would force every one of them to import the layer that
// calls them, which is the cycle this collection is being reorganised to remove.
//
// # What is not here yet
//
// Bind, which would fill a struct from a form, and WithStatus, the success
// message that survives a POST-redirect-GET. Both are named in
// docs/31-reorganizacao-hesape.md section 5 as work that does not exist in the
// framework yet, and neither is invented during a move: an API guessed at while
// copying files is an API nobody decided on.
package httpx
