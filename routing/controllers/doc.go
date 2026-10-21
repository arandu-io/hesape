// Package controllers is how a controller reaches a route: the Router an
// application registers its controllers on, the seven resource actions and the
// single action a controller implements, and the contract a controller uses to
// declare its own middleware.
//
// # The Router, and the router underneath it
//
// hesape/routing.Router matches a request and dispatches it to an
// http.Handler; it holds no request state, and a controller action reaches it
// only once something has turned the action into a handler. That something
// needs the view renderer, the route table and the flash, which belong to the
// request layer, so it is hesape/http.NewAdapter, and routing takes it as a
// parameter.
//
// Router is the one place the two are put together. It carries the renderer
// and the flash the kernel wires at boot, and every method that takes a
// controller action -- Action, Resource, Singleton, ResourceAction, Invokable
// -- goes through the adapter built from them, so an action is answered the
// same way whichever method registered it. The verb methods register a plain
// handler as it stands. Nothing is stored here: every registration goes
// straight through to the hesape/routing router, whose table every sub-router
// shares.
//
// # The resource interfaces
//
// Indexer through Destroyer and Invoker are the generic interfaces of
// hesape/routing instantiated with hesape/http.Context, which is the context
// every action receives. A controller declaring Index(*hhttp.Context) error
// satisfies Indexer literally, so the type assertion Resource makes finds it.
package controllers
