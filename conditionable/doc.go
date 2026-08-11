// Package conditionable mirrors Illuminate\Conditionable.
//
// The files it answers to, in the clone at laravel_illuminate/conditionable:
//
//	Traits/Conditionable.php
//	HigherOrderWhenProxy.php
//
// # This component becomes no package, and the two methods still exist
//
// Conditionable is a trait with two methods, when and unless. A trait is not a
// package in Go and cannot become one: when has to be a method on the type it
// returns, and a method's receiver has to be declared in the package that
// declares the type. So there is nothing to import from here, and the doc.go
// exists so that somebody looking finds the reason rather than silence.
//
// What does not follow is that when and unless disappear. The register once
// said Conditionable was rejected outright because Go has if in statement
// position; ADR 0046 replaces that with a narrower rule, because measuring it
// against the fluent builders showed the general claim to be wrong.
//
// # Where they live
//
// The two methods are declared on every hesape type whose API is a chain, and
// on no other type:
//
//	collections.Collection[T]  When, Unless, WhenEmpty, WhenNotEmpty,
//	                           UnlessEmpty, UnlessNotEmpty
//	query.Builder              When, Unless
//
// The rule for a new type is in ADR 0046: a type earns When and Unless when its
// methods return the receiver so that calls chain, and only then. On a type
// whose methods return a result, when is a second spelling of if and the
// framework does not carry it.
//
// # Why the fluent case is not ceremony
//
// if in statement position does replace when, but only by breaking the chain
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
// Both forms compile against the builder as it stands. The Laravel spelling of
// the first line is db.table('posts'), which is Connection::table and is not
// written yet; From is Builder::from, and is the entry point that exists.
//
// The first form is not wrong, and nobody is stopped from writing it. It is
// longer by a variable that exists only to be reassigned, and the reassignment
// is where a builder gets used stale -- q.Where(...) written without the q = in
// front of it compiles, and drops the filter. That failure has one shape and it
// is a filter silently not applied, which is the same class of bug as an empty
// WHERE IN compiled away. When removes the variable, so it removes the
// reassignment that can be forgotten.
//
// # What is skipped
//
// HigherOrderWhenProxy, and with it the zero-argument and one-argument forms of
// when and unless. The proxy exists to capture the next property read or method
// call through __get and __call and apply the condition to it, which is skip
// reason 1 of ADR 0046: PHP language interfaces Go does not have. The
// three-argument form -- condition, callback, default -- is the whole of what
// hesape carries, and it is the form Laravel applications overwhelmingly write.
//
// The closure-valued condition, when(fn ($q) => ...), is not carried either.
// There the argument is a bool computed by the caller before the call, which is
// the same expression one line earlier. Carrying both would be two spellings of
// one thing.
package conditionable
