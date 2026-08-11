package middleware

import (
	"context"
	"net/http"

	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/routing"
)

// SubstituteBindings resolves route model bindings defined with
// routing.RouteBindings.Bind.
//
// It mirrors Illuminate\Routing\Middleware\SubstituteBindings.
//
// The middleware reads the path parameters from the request, runs every
// registered binder whose key matches a parameter name, and stores the
// resolved values in the request context. A handler downstream can read
// the resolved model from the context instead of looking it up by the raw
// id.
//
// The binder MUST filter by tenant (REGRA 17): a binding that resolves by
// value alone returns the record of another tenant for the same id.
func SubstituteBindings(bindings *routing.RouteBindings) pipeline.Middleware[http.Handler] {
	resolver := routing.NewImplicitRouteBinding(bindings)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			params := pathParams(r)
			if len(params) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			resolved, err := resolver.Resolve(r.Context(), r, params)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			ctx := r.Context()
			for name, val := range resolved {
				ctx = context.WithValue(ctx, bindingKey(name), val)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bindingKey is the context key type for resolved bindings.
type bindingKey string

// pathParams extracts path parameters from the request using Go 1.22's
// PathValue. It reads every segment and collects values for segments
// that look like path parameters (have non-empty PathValue).
func pathParams(r *http.Request) map[string]string {
	path := r.URL.Path
	params := make(map[string]string)
	segments := splitPath(path)
	for _, seg := range segments {
		if val := r.PathValue(seg); val != "" {
			params[seg] = val
		}
	}
	return params
}

func splitPath(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	p = p[1:] // strip leading /
	return splitOn(p, '/')
}

func splitOn(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
