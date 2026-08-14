// Package pipeline mirrors Illuminate\Pipeline.
//
// The files it answers to, in the clone at
// laravel_illuminate/pipeline (Laravel 13, illuminate/pipeline ^13.0):
//
//	Hub.php                      -> Hub
//	Pipeline.php                 -> Pipeline, Pipe, Destination
//	PipelineServiceProvider.php  -> nothing (ADR 0001, ADR 0002)
//
// [Chain], [Middleware], [Handler] and [Identity] answer to no PHP method: they
// are the curried shape of the same onion, and "Two shapes, one onion" below
// says why one class there is two types here.
//
// [Pipeline] is the fluent one: Send, Through, Pipe, Then, ThenReturn and
// Finally, in that vocabulary and that order. [Hub] holds pipelines under
// names. [Chain] over [Middleware] is the same onion for a stack that wraps a
// handler instead of receiving the passable.
//
// # Two shapes, one onion
//
// Illuminate\Pipeline is one class because PHP is untyped: then() answers
// mixed, so the same object serves a middleware stack that turns a Request into
// a Response and a filter chain that answers with the object it was given. In
// Go those are two types and cannot be one:
//
//   - [Pipeline] is the fluent mirror, passable in and passable out. It is the
//     shape the bus, the queue and every filter chain use --
//     Pipeline::send($job)->through($middleware)->thenReturn().
//   - [Chain] over [Middleware] is the shape where the passable is curried into
//     the handler. Middleware[http.Handler] is the standard
//     func(http.Handler) http.Handler, so a stage wraps the handler rather than
//     being handed a request, and the stack answers with a Response, which no
//     Pipeline[T] can say.
//
// hesape/http names the second one -- hhttp.Middleware is an alias of
// pipeline.Middleware[http.Handler] -- and hesape/routing composes it with
// [Chain]. Neither of them has a composer of its own, and neither should get
// one: there is one Chain and one Pipeline, and the choice between them is the
// shape of the stage, not a preference (RULE 9).
//
// # No container
//
// setContainer(), getContainer() and the string pipe it exists for
// ('Class:arg1,arg2', resolved by name) are not here. ADR 0002 rejected the
// container. What replaces it is the closure: a [Pipe] is a func, so whatever
// it needs -- a logger, a repository, a clock -- is captured by the function
// that builds it, and a pipeline is read by following values rather than by
// guessing what a name resolves to.
//
// # The methods with no answer here
//
// Every one of them, with the numbered reason from ADR 0044: (1) a PHP language
// feature Go does not have, (2) a method that only serves the container, a
// facade or a service provider, (3) a driver this ecosystem does not carry.
//
//   - Pipeline::via -- reason 1. It chooses, by string, which method name
//     carry() invokes on a class pipe: `$pipe->{$this->method}(...)`. That is
//     the variable-method family. What replaces it is the method value:
//     `Through(auditor.Handle)` is via('handle')->through([$auditor]), resolved
//     by the compiler instead of at the line that runs it, and a pipe whose
//     method was renamed fails the build rather than the request.
//   - Pipeline::withinTransaction -- reason 2. It is
//     `getContainer()->make('db')->connection(...)->transaction(...)` and
//     nothing else. What replaces it is a pipe: a stage that opens the
//     transaction, calls next and commits or rolls back is where the onion
//     already puts before-and-after, and it is the only form that says which
//     connection it used.
//   - Pipeline::getContainer, Pipeline::setContainer, Hub::getContainer and
//     Hub::setContainer -- reason 2, the container itself. What replaces them is
//     the closure: a [Pipe] is a func, so the logger, the repository or the
//     clock it needs is captured by the function that builds it --
//     `func auditPipe(log *slog.Logger) Pipe[Order]` -- and passed as a value
//     rather than resolved from a name at the moment it is called.
//   - PipelineServiceProvider::register and ::provides -- reason 2. They bind
//     the Hub into the container, which ADR 0001 and ADR 0002 rejected.
//   - Pipeline::prepareDestination, ::carry, ::parsePipeString, ::pipes,
//     ::handleCarry and ::handleException are protected and are the body of
//     [Pipeline.Then]. parsePipeString is also reason 1 on its own: it splits
//     'Class:arg1,arg2' and resolves the class by name.
//
// The Macroable trait is reason 1: methods added to a class at runtime through
// __call. Conditionable is not skipped -- [Pipeline.When] and [Pipeline.Unless]
// are here, because a Pipeline is a chain and ADR 0046 says a chain earns them.
//
// # A name that moved
//
// Pipe was the alias for Middleware[Handler[T]] in this package. A pipe in
// Illuminate is `function ($passable, $next)`, so the name went to the stage of
// a [Pipeline] (ADR 0044). The curried form lost nothing but the shorthand: it
// is written Middleware[Handler[T]], which is what the alias stood for.
//
// # Sources
//
// The clone is the source. One behaviour comes from the second one: Hub.Pipe
// failing on a name that was never defined follows
// reference_laravel/framework/src/Illuminate/Pipeline/Hub.php, which throws
// InvalidArgumentException where the clone reads a missing array key.
package pipeline
