package controllers

// HasMiddleware mirrors Illuminate\Routing\Controllers\HasMiddleware.
//
// It declares that a controller carries middleware. In Go, this is an
// interface the controller implements; the framework introspects it when
// the controller is registered.
type HasMiddleware interface {
	Middleware() []MiddlewareDef
}

// MiddlewareDef pairs a middleware with the controller methods it applies to.
//
// It mirrors Illuminate\Routing\Controllers\Middleware.
type MiddlewareDef struct {
	// Name is the middleware to apply (e.g. "auth", "can:edit-post").
	Name string
	// Methods limits the middleware to the named controller actions. An
	// empty slice means every action.
	Methods []string
}
