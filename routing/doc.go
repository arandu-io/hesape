// Package routing decides which handler answers a request, and what the
// address of a handler is called.
//
// It mirrors Illuminate\Routing, and it is a thin shell over http.ServeMux,
// which since Go 1.22 already matches methods and path parameters. What this
// package adds is everything the standard mux has no opinion about: groups
// with an inherited prefix, name and middleware; a route table that survives
// registration so `aru routes` can print it and a view can build a URL from a
// name instead of a string literal; the seven REST routes of a resource
// controller; and the two middlewares that decide whether a request reaches a
// route at all.
//
// # One handler type
//
// A route dispatches to an http.Handler. There is one registration path and
// one handler type on it -- Get, Post, Put, Patch, Delete, Match, Any and
// Fallback all take the same thing -- because the shape this replaced had two:
// Get took an http.HandlerFunc and a controller arrived through a second
// method, Action, whose job was to build a handler out of a controller method
// and translate what it returned. That second path was never a second way to
// route. It was a way to construct a handler, and everything it touched -- the
// request context, the renderer, the flash, a rejected form -- belongs to the
// request layer above this one. Constructing the handler happens there now, and
// a controller action reaches a route the same way any other handler does:
//
//	r.Get("/dashboard", hhttp.Action(dashboard.Index)).Name("dashboard")
//
// Resource takes the adaptation as an argument for the same reason, which is
// what lets this package register controller routes without importing the type
// a controller receives. See Adapter.
//
// # Names, not paths
//
// A hardcoded "/invoices/"+id compiles and keeps compiling after the route
// moves. Routes.Route does not: an unknown name or a wrong number of
// parameters is an error the caller sees, not a 404 the person sees.
//
// The files it answers to, in the clone at
// laravel_illuminate/routing:
//
//	AbstractRouteCollection.php
//	CallableDispatcher.php
//	CompiledRouteCollection.php
//	Controller.php
//	ControllerDispatcher.php
//	ControllerMiddlewareOptions.php
//	CreatesRegularExpressionRouteConstraints.php
//	FiltersControllerMiddleware.php
//	ImplicitRouteBinding.php
//	MiddlewareNameResolver.php
//	PendingResourceRegistration.php
//	PendingSingletonResourceRegistration.php
//	Pipeline.php
//	RedirectController.php
//	Redirector.php
//	ResolvesRouteDependencies.php
//	ResourceRegistrar.php
//	ResponseFactory.php
//	Route.php
//	RouteAction.php
//	RouteBinding.php
//	RouteCollection.php
//	RouteCollectionInterface.php
//	RouteDependencyResolverTrait.php
//	RouteFileRegistrar.php
//	RouteGroup.php
//	RouteParameterBinder.php
//	RouteRegistrar.php
//	RouteSignatureParameters.php
//	RouteUri.php
//	RouteUrlGenerator.php
//	Router.php
//	RoutingServiceProvider.php
//	SortedMiddleware.php
//	UrlGenerator.php
//	ViewController.php
//
// # Method by method, so the list can be checked rather than believed
//
// Twenty-seven public methods of the component have no name here. Each one,
// with the ADR 0044 reason number:
//
//	AbstractRouteCollection::compile, ::dumper, ::toSymfonyRouteCollection,
//	    RouteCollection::toSymfonyRouteCollection, ::toCompiledRouteCollection,
//	    Route::toSymfonyRoute, ::getCompiled and Router::setCompiledRoutes --
//	    reason 3, all eight. They translate the route table into
//	    symfony/routing's own objects and dump it as a compiled PHP matcher,
//	    which is what `route:cache` writes. symfony/routing is not carried here
//	    and there is nothing to compile to: matching is http.ServeMux, which
//	    Go 1.22 gave method and path-parameter matching, and it is built once at
//	    boot in the process that serves.
//	Route::prepareForSerialization, RouteAction::containsSerializedClosure and
//	    Route::flushController -- reason 1: a cached route table is PHP objects
//	    written to disk, so a route whose action is a closure has to be turned
//	    into a SerializableClosure first and the resolved controller instance
//	    dropped. Go has neither closure serialization nor a route cache to write
//	    -- the table is the program.
//	Controller::callAction, ViewController::callAction,
//	    ResolvesRouteDependencies::resolveMethodDependencies,
//	    Route::signatureParameters, RouteSignatureParameters::fromAction,
//	    Route::controllerMiddleware,
//	    FiltersControllerMiddleware::methodExcludedByOptions and
//	    MiddlewareNameResolver::resolve -- reason 2, all eight. They read a
//	    controller method's parameter list by reflection, resolve each type out
//	    of the container, and turn middleware named by string into the objects
//	    that run -- including the only/except lists a controller declares about
//	    its own methods. A handler here is an http.Handler the caller
//	    constructed, and middleware is a pipeline.Middleware value passed to the
//	    route, so there is no name to resolve and no parameter to inject.
//	Route::setContainer, CompiledRouteCollection::setContainer,
//	    Router::setContainer, Router::getCurrentRequest and
//	    Router::prepareResponse -- reason 2: the container again, and the
//	    ambient current request it is used to fetch. Every method here that
//	    needs the request takes it -- [Router.Current], [Router.CurrentRouteName]
//	    and the rest -- because a request read out of ambient state is a request
//	    that can be the wrong one under concurrency. Turning a handler's return
//	    value into a response belongs to hesape/http, which owns what an answer
//	    looks like.
//	AbstractRouteCollection::getIterator -- reason 1: IteratorAggregate is how
//	    PHP writes foreach over the collection. [Routes.GetRoutes] returns the
//	    slice, which range already walks.
//	RouteAction::parse -- reason 1: it normalizes the many shapes a PHP action
//	    can take -- a closure, "Controller@method", [Controller::class, 'method'],
//	    an array with a 'uses' key, an invokable class -- into one array. There
//	    is one shape here, http.Handler, so there is nothing to normalize
//	    (RULE 9). RouteUri::parse is here, as [RouteUri.Parse].
//	Middleware\ValidateSignature::relative, ::absolute,
//	    Middleware\ThrottleRequests::using and ::shouldHashKeys -- reason 2:
//	    each returns the middleware DEFINITION STRING that the kernel's alias
//	    map resolves -- 'signed:relative', 'throttle:api' -- or sets a static
//	    flag that changes how every throttle in the process keys its counter.
//	    Middleware here is a value: [middleware.ValidateSignature] and
//	    [middleware.Throttle] take what they need as arguments, and what a
//	    request is counted against is the [middleware.KeyFunc] passed in, which
//	    is where hashing a key would go if an application wanted one.
//	Middleware\ValidateSignature::handle, Middleware\SubstituteBindings::handle
//	    and Middleware\ThrottleRequests::handle -- reason 1 for the shape:
//	    PHP's handle($request, $next) is a method because a middleware is an
//	    object the pipeline instantiates. Here a middleware is a function, and
//	    the closure [middleware.ValidateSignature] and [middleware.Throttle]
//	    return IS handle -- there is no object left to hang it on.
//	    SubstituteBindings has no middleware at all, and routing/middleware's
//	    own doc says why: the binding takes an auth.Grant, and a middleware has
//	    nowhere to get one that cannot be absent (RULE 17).
//
// Pipeline.php has no counterpart here: composing middleware is
// hesape/pipeline, generic over what it wraps, and this package uses it rather
// than declaring a second one. Redirector.php and ResponseFactory.php answer in
// hesape/http, which owns the request context and therefore owns what an
// answer looks like; the half of the signed-URL story that belongs here is
// SignedRoute and middleware.ValidateSignature, over the Signer in
// hesape/encryption.
package routing
