// Package requestscope remembers, for the life of one HTTP request, what the
// request has already read, so a page that asks the same question at every
// gate and partial pays for the answer once.
//
// A dashboard asks "who is this account, which organization is it in, what may
// it do there" from the layout, from each fragment and from every policy check,
// and each ask is a query. [Remember] answers the second and later asks of the
// same request from memory:
//
//	row, err := requestscope.Remember(ctx, membershipKey{tenant, user}, func() (*Membership, error) {
//		return memberships.Find(ctx, grant, user)
//	})
//
// # A scope belongs to one request
//
// [Middleware] puts a scope on the context of a safe request (GET, HEAD,
// OPTIONS) and on no other. The scope is garbage when the request ends; nothing
// of it is held by the process, so a row changed by another request is read
// afresh by the very next one. A scope that outlived its request would be a
// cache with no expiry, and a permission or membership taken away would keep
// working in it. This package has no way to build one: [Remember] on a context
// that carries no scope runs the load and keeps nothing, which is what a job,
// a listener, a command and every write do.
//
// A request that writes gets no scope for a second reason. A write may change
// what an earlier read returned and then ask again; with no memory it reads the
// database each time, as it would without this package.
//
// # Reads only, never decisions
//
// What is remembered is the row a query read. A decision about that row is never
// remembered: a Policy, auth.Authorize and Subject.Can still run at each call
// site, over the remembered row, so every check is made and none is skipped. Do
// not remember the result of an authorization. A decision is a function of the
// row and of the question, and keeping it separates the two.
//
// # Keys
//
// A key is a comparable value that names the question and every input to it:
// what is read, the tenant the Grant was issued for, and each identifier the
// query filters on. Two questions that differ in any input must not share a
// key, or one request would answer for another. A comparable struct of the
// reading package's own type is the usual shape: two keys are the same only
// when their types are, so keys from different packages never collide.
//
// A key names one value type. Asking for it again with another type is a
// miss: the load runs and its value is returned and not kept.
//
// # What is remembered is shared, not copied
//
// Every caller of the request gets the same value. A row read through a query
// is wired to the connection that read it, and a copy of it refuses to save, so
// copying is not an option and the rule is the other way round: treat what
// [Remember] returns as read-only. Code that means to change a row reads it
// again outside the scope, and a write that changes what the scope holds calls
// [Forget] or [ForgetKey] after it, so the rest of the request reads what the
// write left. A slice is shared too: clone it before sorting or appending.
//
// An error is never kept: the next call loads again. A row that was not found
// is a value, and whether it is kept is the load function's choice -- return
// a nil pointer and no error to remember "not found".
package requestscope
