package routing

import (
	"context"
	"fmt"
	"net/http"
)

// RouteBindings holds the custom parameter binders for a router.
//
// It is the Go equivalent of the bindings registered with
// Route::bind() and Route::model().
type RouteBindings struct {
	m map[string]BindingFunc
}

// NewRouteBindings returns an empty set of bindings.
func NewRouteBindings() *RouteBindings {
	return &RouteBindings{m: make(map[string]BindingFunc)}
}

// BindingFunc is a function that resolves a string into a value.
//
// It is the shape of a custom binder the caller registers with Router.Bind,
// and it receives the raw parameter value read from the path and the request.
// It may return an error; a nil error with a nil value means "this parameter
// is not meant to be resolved by this binder".
type BindingFunc func(ctx context.Context, value string, req *http.Request) (any, error)

// Bind registers a custom binder for a key on this bindings set.
//
// When a route carries a parameter named {key}, the binder is called to
// resolve the value, and the route handler receives the resolved value in
// the request context, not the raw string.
//
// The binder MUST use the Grant from the context to filter by tenant
// (REGRA 17). A binder that looks up by value alone returns the record of
// another tenant for the same id, which is the most direct form of the
// cross-tenant leak.
func (b *RouteBindings) Bind(key string, fn BindingFunc) {
	b.m[key] = fn
}

// ImplicitRouteBinding resolves route parameters using the registered binders.
//
// It mirrors Illuminate\Routing\ImplicitRouteBinding.
type ImplicitRouteBinding struct {
	bindings *RouteBindings
}

// NewImplicitRouteBinding creates a resolver backed by the bindings set.
func NewImplicitRouteBinding(b *RouteBindings) *ImplicitRouteBinding {
	return &ImplicitRouteBinding{bindings: b}
}

// Resolve runs the binders for every route parameter whose key has one
// registered, and returns the resolved values keyed by parameter name.
//
// Parameters without a registered binder are returned as their raw string
// value. A binder that returns (nil, nil) also leaves the value as-is.
func (b *ImplicitRouteBinding) Resolve(ctx context.Context, req *http.Request, params map[string]string) (map[string]any, error) {
	if b.bindings == nil || b.bindings.m == nil {
		return stringMapToAny(params), nil
	}

	out := make(map[string]any, len(params))
	for name, value := range params {
		binder, ok := b.bindings.m[name]
		if !ok {
			out[name] = value
			continue
		}
		resolved, err := binder(ctx, value, req)
		if err != nil {
			return nil, fmt.Errorf("routing: binding %q: %w", name, err)
		}
		if resolved == nil {
			out[name] = value
		} else {
			out[name] = resolved
		}
	}
	return out, nil
}

func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
