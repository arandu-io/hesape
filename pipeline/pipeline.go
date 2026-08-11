package pipeline

import (
	"errors"
	"fmt"
)

// Destination is the closure a pipeline ends with, and the closure every pipe
// is handed as the rest of the pipeline.
//
// It answers two things that are one thing in PHP: the $destination of
// Pipeline::then() and the $stack that Pipeline::carry() passes to each pipe.
// Both are a Closure taking the passable, so both are this type here. The
// (T, error) pair is where the PHP throws.
type Destination[T any] func(passable T) (T, error)

// Pipe is one stage of a [Pipeline]: it is given the passable and the rest of
// the pipeline, and answers with the result.
//
// It is Illuminate's pipe written as a func -- `function ($passable, $next)`,
// the shape a Laravel middleware, a job middleware and a filter already have. A
// pipe that returns without calling next stops the pipeline, which is what an
// authorization check or a rate limiter does.
//
// A pipe that is nil fails the call when the pipeline reaches it. PHP fails
// there too, on `is_callable(null)` falling through to a container lookup.
type Pipe[T any] func(passable T, next Destination[T]) (T, error)

// Pipeline mirrors Illuminate\Pipeline\Pipeline: an object is sent through a
// list of pipes, each free to inspect it, change it, hand it on or refuse it,
// and a destination closes the onion.
//
// The passable and the result are the same type parameter. In PHP both are
// mixed, so a pipeline may end with a destination answering something else
// entirely -- the router ends with a Response. Here a pipe returns what the
// next one takes, so the onion is closed over one type; a stack whose answer is
// not the value it was given is [Chain] over [Middleware], which is the form
// the HTTP stack uses. That is the whole of the `mixed` narrowing, and the
// package comment says which shape belongs where.
//
// The zero value is a pipeline with no pipes over the zero passable, and it is
// usable. A Pipeline is not safe for concurrent use: it is a builder, built and
// run in one place, exactly as `new Pipeline` is in PHP.
type Pipeline[T any] struct {
	passable T
	pipes    []Pipe[T]
	finally  func(passable T)
}

// New starts a pipeline over T.
//
// It answers `new Pipeline($container)` without the container: ADR 0002
// rejected it, so a pipe that needs a dependency is built by a closure that
// already holds it. Nothing is resolved by name here, which is also why a pipe
// is a func rather than a class name in a string.
func New[T any]() *Pipeline[T] {
	return &Pipeline[T]{}
}

// Send sets the object being sent through the pipeline. It answers send().
func (p *Pipeline[T]) Send(passable T) *Pipeline[T] {
	p.passable = passable
	return p
}

// Through sets the pipes. It answers through().
//
// through() assigns rather than appends, so calling Through twice keeps only
// the second list, and calling it with no pipes empties the list -- that is
// through([]). [Pipeline.Pipe] is the one that appends. The list is copied, so
// a caller reusing its own slice cannot change a pipeline it already built.
func (p *Pipeline[T]) Through(pipes ...Pipe[T]) *Pipeline[T] {
	p.pipes = append([]Pipe[T](nil), pipes...)
	return p
}

// Pipe pushes additional pipes onto the pipeline. It answers pipe(), the one
// that appends where [Pipeline.Through] replaces.
func (p *Pipeline[T]) Pipe(pipes ...Pipe[T]) *Pipeline[T] {
	p.pipes = append(p.pipes, pipes...)
	return p
}

// Finally sets a callback to run after the pipeline ends, whatever the outcome.
// It answers finally().
//
// The callback is given the passable as [Pipeline.Send] left it, not what the
// pipes handed each other: PHP passes $this->passable, the field, which no pipe
// can reach. It runs on the way out of a failure and of a panic as well, which
// is what the `finally` block around then() does. Calling Finally twice keeps
// the second callback, as assigning the property twice does.
func (p *Pipeline[T]) Finally(callback func(passable T)) *Pipeline[T] {
	p.finally = callback
	return p
}

// Then runs the pipeline with a final destination. It answers then().
//
// The first pipe given to [Pipeline.Through] is the outermost, so the order of
// the list is the order of execution: PHP reverses the list and folds it, which
// is the same onion. The error returned is the first one a pipe or the
// destination reports, and it is returned rather than thrown because that is
// what Go does with what handleException() rethrows.
//
// A pipe that is nil fails the call, and only if the pipeline reaches it: a
// pipe that stops the pipeline before it shields it, exactly as PHP does not
// evaluate a stage nobody calls. A destination that is nil fails before
// anything runs, including the callback set with [Pipeline.Finally] -- in PHP
// the destination is a typed argument, so it fails before then() reaches its
// try block.
func (p *Pipeline[T]) Then(destination Destination[T]) (T, error) {
	if destination == nil {
		var zero T
		return zero, errors.New("pipeline: Then needs a destination, and nil is not one")
	}

	if p.finally != nil {
		defer p.finally(p.passable)
	}

	next := destination
	for i := len(p.pipes) - 1; i >= 0; i-- {
		pipe, stack, at := p.pipes[i], next, i
		next = func(passable T) (T, error) {
			if pipe == nil {
				var zero T
				return zero, fmt.Errorf("pipeline: pipe %d is nil, and a pipeline cannot be sent through nothing", at)
			}
			return pipe(passable, stack)
		}
	}
	return next(p.passable)
}

// ThenReturn runs the pipeline and returns the result. It answers thenReturn(),
// which is then(fn ($passable) => $passable): what comes back is the passable
// as the last pipe handed it on, not what [Pipeline.Send] was given.
func (p *Pipeline[T]) ThenReturn() (T, error) {
	return p.Then(func(passable T) (T, error) { return passable, nil })
}
