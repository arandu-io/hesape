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
// reflection got in anyway, so it was counted. ADR 0046 audits eight non-test
// files line by line, at twenty-one call sites, giving for each one what it is
// for and whether a generic or an interface could replace it. Read the audit in
// docs/adr/0046-macro-condicao-e-reflexao.md.
//
// The repository total is larger than the audited set and moves faster: thirteen
// non-test files at the last measurement, with collections/arr alone answering
// for nearly half. Trust the command below over any number written here.
//
// The shape of the list, so that a reader knows what to expect before opening
// it. Ten of the twenty-one are in packages that exist only to serve tests,
// testing and support/testing/fakes: not _test.go files, so the count includes
// them on purpose, but nothing serving a request reaches them. Nine of the
// eleven that remain are the same question asked of a value whose static type is
// any -- is it a map, is it a slice, how long is it, is it nil, are these two
// equal. They are in support/arr, support/optional and log/context, all three
// mirroring a PHP surface typed mixed, and a generic cannot answer a question
// about a value whose type was already erased at the call. Of the last two, one
// is a better panic message on a path that only runs when a template is already
// wrong (view/runtime.go), and it is the one that moves to build time once kyse
// type-checks the template; the other is BackedEnum::tryFrom
// (support/interactswithdata.go), where an interface would work and was turned
// down on price, because it would put a method on every enum in every
// application.
//
// None of it decides authorization, routing, dispatch or persistence: auth,
// routing, validation, queue, events, database, session, encryption, hashing,
// http and cache import reflect in zero non-test files. That is the sentence
// worth keeping, and it was re-measured after the last additions rather than
// carried over from the first count.
//
// The count is the point rather than the verdict, and it moves -- four files,
// then eight, then thirteen, in the hours it took to write ADR 0046, as other
// packages landed. Rerun it before believing any number above:
//
//	grep -rln '"reflect"' --include='*.go' . | grep -v _test.go | grep -v reflection/doc.go
//
// # The four names the measurement still asks about
//
// Reflector::getClassAttribute and getParameterClassNames read PHP attributes
// and parameter type hints off a ReflectionClass. They are how the container
// decides what to inject and how #[ObservedBy] and #[ScopedBy] find their
// classes -- both refused by ADR 0001, and both answered here by the type
// system rather than by a lookup at run time.
//
// Reflector::isParameterSubclassOf asks whether a parameter's declared type is
// a subclass of a name given as a string. In Go the question is a type
// assertion, and the compiler asks it at the call.
//
// helpers.php's typeFromParameter, on the anonymous class that uses
// ReflectsClosures, reads the type of a closure's first parameter so that
// `once(fn (Foo $f) => ...)` can find out what Foo is. A Go function value
// carries its parameter types in its own type, so the caller already has what
// this recovers -- and where the value is needed generically, it is a type
// parameter, which is the same fact stated where the compiler can check it.
package reflection
