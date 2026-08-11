// Package macroable mirrors Illuminate\Macroable.
//
// The file it answers to, in the clone at laravel_illuminate/macroable:
//
//	Traits/Macroable.php
//
// # This component becomes no package
//
// Macroable exists so that code outside a class can add a method to it. macro
// registers a closure under a name, mixin registers every public and protected
// method of an object at once, hasMacro asks whether a name is taken,
// flushMacros empties the register, and __call and __callStatic route a call
// the class never declared into that register, binding the closure to the
// instance on the way.
//
// All five end at the same place: a call the type did not declare. In Go the
// receiver of a method has to be declared in the package that declares the
// type, and there is no hook for a call that resolves to nothing -- the
// compiler rejects the program before it runs. That is skip reason 1 of ADR
// 0046, a language interface Go does not have, and it is the whole of the
// component.
//
// The useful question is not whether macro can exist. It is what somebody
// writes when they would have reached for it. There are three answers below,
// and which one applies is decided by what the added behaviour needs from the
// value it operates on.
//
// # 1. A free function, when the behaviour only reads its argument
//
// This is the answer for Str, Arr, Number and every other package whose surface
// is functions over values rather than methods on state. Str::macro is the most
// common macro in a Laravel application and it is also the one that costs the
// least to give up, because a macro over Str was already a function pretending
// to be a method.
//
//	// Illuminate
//	Str::macro('initials', fn ($value) => ...);
//	Str::initials('Paulo Lima');   // "PL"
//
//	// hesape, in the package that wanted the macro
//	package report
//
//	import (
//		"strings"
//
//		"github.com/arandu-io/hesape/str"
//	)
//
//	// Initials is what Str::macro('initials') would have registered.
//	func Initials(value string) string {
//		var out strings.Builder
//		for _, word := range strings.Fields(str.Squish(value)) {
//			out.WriteString(strings.ToUpper(word[:1]))
//		}
//		return out.String()
//	}
//
// The call site reads report.Initials(name) rather than str.Initials(name), and
// that difference is the point rather than a cost: the package that owns the
// behaviour is named at the call.
//
// # 2. A type of your own embedding the framework's, when the behaviour chains
//
// This is the answer for Collection, and for anything else whose methods return
// the receiver so calls can be strung together. Embedding promotes every method
// of the embedded type, so the new method sits beside Filter, Map and Sort
// rather than in a second vocabulary.
//
//	// Illuminate
//	Collection::macro('published', function () {
//		return $this->filter(fn ($post) => $post->published);
//	});
//	$posts->published()->sortByDesc('created_at');
//
//	// hesape
//	package blog
//
//	import "github.com/arandu-io/hesape/collections"
//
//	// Posts is a Collection of Post that carries the methods this package
//	// would have registered with Collection::macro.
//	type Posts struct {
//		collections.Collection[Post]
//	}
//
//	// Published is what Collection::macro('published') would have registered.
//	func (p Posts) Published() Posts {
//		return Posts{p.Filter(func(post Post, _ int) bool { return post.Published })}
//	}
//
// The cost is one conversion at each boundary -- Posts{c} on the way in,
// p.Collection on the way out -- and in exchange a reader who sees Published
// can find it, because it is declared in the package the type is declared in.
//
// # 3. A small interface, when the behaviour crosses a package boundary
//
// mixin is the Macroable member that has no counterpart above: it takes a whole
// object and grafts its methods onto a class, so that a function written
// against the class silently gains a capability. In Go the same shape is an
// interface, declared in the package that consumes it and satisfied by whoever
// wants to be passed in. Nothing is grafted; the requirement is written down.
//
//	// Illuminate
//	Response::mixin(new CachingMacros);   // every response gains cacheFor()
//
//	// hesape
//	package feed
//
//	import "time"
//
//	// Cacheable is what this package requires of a body it is asked to write:
//	// that it can state its own freshness. Any type in any package satisfies it
//	// by declaring the method.
//	type Cacheable interface {
//		MaxAge() time.Duration
//	}
//
// Use the free function when the behaviour is a transformation of a value the
// caller already holds. Use the embedded type when the behaviour belongs to a
// type the caller owns and the calls chain. Use the interface when more than
// one type answers the same need and the need is stated by the consumer.
//
// # What is lost
//
// A third-party package can no longer add a method to everybody's Collection.
// In Laravel a package like spatie/laravel-collection-macros registers its
// methods once and every Collection in the process has them, including
// collections built by code that never heard of the package. None of the three
// forms above does that: report.Initials has to be imported by name, Posts has
// to be constructed, and a function typed against collections.Collection[Post]
// will not see Published no matter what any other package did.
//
// This is a real loss and ADR 0046 records it as one. It removes a category of
// ecosystem package that worked well in PHP, and it puts an explicit conversion
// at every boundary where a wrapped type meets a framework signature. What it
// buys is that every method call names the package that declared it, that no
// import can change the meaning of code it does not appear in, and that
// flushMacros -- which exists only because macros are global mutable state that
// leaks between tests -- has nothing to flush.
package macroable
