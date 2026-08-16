// Package conditionable exports nothing.
//
// When and Unless cannot live in a package of their own: a method's receiver
// has to be declared in the package that declares the type, so every type that
// offers them declares them itself.
//
// # Where they are
//
// The two methods are declared on every type in this module whose API is a
// chain, and on no other type:
//
//	collections.Collection[T]  When, Unless, WhenEmpty, WhenNotEmpty,
//	                           UnlessEmpty, UnlessNotEmpty
//	query.Builder              When, Unless
//
// A type earns When and Unless when its methods return the receiver so that
// calls chain, and only then. On a type whose methods return a result rather
// than the receiver, When is a second spelling of if.
//
// # Why the fluent case is not ceremony
//
// An if in statement position does replace When, but only by breaking the chain
// and naming an intermediate:
//
//	q = q.From("posts")
//	if published {
//		q = q.Where("published", true)
//	}
//	q = q.OrderBy("id")
//
// against
//
//	q.From("posts").
//		When(published, func(q *query.Builder) { q.Where("published", true) }, nil).
//		OrderBy("id")
//
// The first form is not wrong, and nobody is stopped from writing it. It is
// longer by a variable that exists only to be reassigned, and the reassignment
// is where a builder gets used stale: q.Where(...) written without the q = in
// front of it compiles, and drops the filter. That failure has one shape, and
// it is a filter silently not applied. When removes the variable, so it removes
// the reassignment that can be forgotten.
//
// The condition is a bool the caller computes before the call, and the negation
// is written in the expression -- Unless(cond, ...) or When(!cond, ...) --
// where a reader can see it.
package conditionable
