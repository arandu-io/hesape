package controllers

// HasMiddleware declares that a controller carries middleware. It is an
// interface the controller implements; the framework introspects it when
// the controller is registered.
type HasMiddleware interface {
	Middleware() []MiddlewareDef
}

// MiddlewareDef pairs a middleware with the controller methods it applies to.
type MiddlewareDef struct {
	// Name is the middleware to apply (e.g. "auth", "can:edit-post").
	Name string
	// Methods limits the middleware to the named controller actions. An
	// empty slice means every action.
	Methods []string
}
