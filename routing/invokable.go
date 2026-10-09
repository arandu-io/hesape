package routing

import "strings"

// Invoker is a controller with one action. It answers one route, so the action
// needs no name beyond Invoke.
//
// C is the request context the action receives, a type parameter for the
// reason Adapter gives: the type lives in the layer above this package.
type Invoker[C any] interface {
	Invoke(*C) error
}

// Invokable registers a single-action controller under one method and one
// pattern.
//
//	routing.Invokable(r, http.MethodPost, "/exports", ExportController{}, adapt).Name("exports.store")
//
// The controller is an Invoker rather than an any, so a controller without
// Invoke fails to compile at this line instead of registering nothing. The
// route is not named; name it with Name, as any single route. It is
// registered on the router it is given, so it carries that group's prefix,
// name and middleware, and it goes through the same adapter as every other
// controller action.
//
// The method is written in any case. An empty one panics at registration, as
// Match does: a route that answers every method is Any, and it is chosen by
// name rather than by leaving the method out.
func Invokable[C any](r *Router, method, pattern string, controller Invoker[C], adapt Adapter[C]) *Route {
	method = strings.ToUpper(method)
	if method == "" {
		panic("routing: Invokable was given no method. Name one, as in Invokable(r, \"POST\", pattern, controller, adapt)")
	}
	return r.handle(method, pattern, adapt(controller.Invoke))
}
