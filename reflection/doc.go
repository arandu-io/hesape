// Package reflection mirrors Illuminate\Reflection.
//
// The files it answers to, in the clone at laravel_illuminate/reflection:
//
//	Reflector.php
//	Traits/ReflectsClosures.php
//	helpers.php
//
// # This component becomes no package
//
// Reflector reads a class it was handed: isCallable asks whether an
// [$object, 'method'] pair resolves, getParameterClassName and its plural read
// the type hints off a parameter, getClassAttributes walks the inheritance
// chain collecting PHP 8 attributes, and isParameterBackedEnumWithStringBackingType
// asks a question no Go type poses. ReflectsClosures does the same to a closure
// so that a call like Event::listen(fn (OrderShipped $e) => ...) can discover
// which event it subscribes to from the parameter's type. helpers.php adds lazy
// and proxy, built on newLazyGhost and newLazyProxy.
//
// Every one of them exists to serve a caller that already exists in Laravel and
// does not exist here: the container resolving a constructor, a facade routing
// a static call, an event dispatcher inferring a subscription from a signature.
// ADR 0001 rejected the container and ADR 0002 the facades, so the callers went
// first and the reader has nobody left to read for. The listener that PHP infers
// from a closure's parameter is a type parameter here, known to the compiler.
//
// Where the same information is genuinely needed -- what fields a struct has,
// what a handler's signature is -- aru reads it from go/ast at build time, in
// aru/internal/typeinfo, and emits Go. That is the framework's thesis in one
// move: the compiler checks the answer, and a wrong answer is a build failure
// rather than a panic in production.
//
// # The reflection that is already here
//
// Refusing to build this package is worth less than knowing where runtime
// reflection got in anyway, so it was counted. As of ADR 0046 the whole of
// hesape imports reflect in four non-test files, at twelve call sites, and the
// full list with what each one is for and whether a generic or an interface
// could replace it is in docs/adr/0046-macro-condicao-e-reflexao.md.
//
// The shape of the list, so that a reader knows what to expect before opening
// it: nine of the twelve sites are the same question asked of a value whose
// static type is any -- is it a map, is it a slice, how long is it, is it nil.
// They are in support/arr and in testing, both of which mirror a PHP surface
// typed mixed, and a generic cannot answer a question about a value whose type
// was already erased at the call. One site, view/runtime.go, is a better panic
// message on a path that only runs when a template is already wrong, and it is
// the one that could move to build time, since kyse knows the type it compiled.
// None of the twelve is on a request path, and none of them decides
// authorization, routing, dispatch or persistence.
//
// The count is the point rather than the verdict. Rerun it before believing it:
//
//	grep -rn '"reflect"' --include='*.go' . | grep -v _test.go
package reflection
