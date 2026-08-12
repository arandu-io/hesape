package routing

import (
	"net/http"
	"strings"

	"github.com/arandu-io/hesape/routing/matching"
)

// Scheme declares the scheme the route answers on, "http" or "https". PHP
// carries the same fact as a bare string in the action array, put there by the
// group attributes; here it is one key of the same map.
//
// It is not in Illuminate under this name -- PHP writes ['https'] into the
// action and reads it back with in_array. HttpOnly, HttpsOnly and Secure are
// the readers, and this is what writes what they read.
func (rt *Route) Scheme(scheme string) *Route {
	if rt == nil {
		return nil
	}
	rt.action["scheme"] = strings.ToLower(scheme)
	return rt
}

// HttpOnly is Route::httpOnly. It reports whether the route answers only plain
// http, which a route generating a URL for a local development host declares.
func (rt *Route) HttpOnly() bool {
	if rt == nil {
		return false
	}
	scheme, _ := rt.action["scheme"].(string)
	return scheme == "http"
}

// HttpsOnly is Route::httpsOnly.
func (rt *Route) HttpsOnly() bool { return rt.Secure() }

// Secure is Route::secure. It reports whether the route answers only https,
// which is what makes its generated URL https regardless of how the current
// request arrived.
func (rt *Route) Secure() bool {
	if rt == nil {
		return false
	}
	scheme, _ := rt.action["scheme"].(string)
	return scheme == "https"
}

// GetOptionalParameterNames is Route::getOptionalParameterNames. The values
// are nil, as PHP's array_fill_keys leaves them: the map is a set, and the
// question asked of it is only whether a name is in it.
func (rt *Route) GetOptionalParameterNames() map[string]any {
	out := map[string]any{}
	if rt == nil {
		return out
	}
	rest := rt.Pattern
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return out
		}
		end += start
		inner := rest[start+1 : end]
		if strings.HasSuffix(inner, "?") {
			name := strings.TrimSuffix(inner, "?")
			if i := strings.IndexByte(name, ':'); i >= 0 {
				name = name[:i]
			}
			out[name] = nil
		}
		rest = rest[end+1:]
	}
}

// OriginalParameters is Route::originalParameters. It returns every path
// parameter as it arrived, before any binding replaced an id with the record
// it names -- which is what a binder compares against to know it changed
// something.
//
// PHP throws when the route is not bound to a request; here the request is the
// argument, so an unbound call is not expressible.
func (rt *Route) OriginalParameters(req *http.Request) map[string]string {
	out := map[string]string{}
	if rt == nil || req == nil {
		return out
	}
	for _, name := range rt.ParameterNames() {
		if v := req.PathValue(name); v != "" {
			out[name] = v
		}
	}
	return out
}

// SetRouter is Route::setRouter. It attaches the router whose matched
// listeners the route fires and whose binders it resolves through.
func (rt *Route) SetRouter(router *Router) *Route {
	if rt == nil {
		return nil
	}
	if router == nil {
		rt.router = nil
		return rt
	}
	rt.router = router.root
	return rt
}

// Can is Route::can. It records the ability a request must be granted before
// the route answers, and the resources that ability is asked about.
//
// PHP appends the string "can:ability,model" to the route's middleware, and
// the container resolves that string into the Authorize middleware. Middleware
// here is a typed function and there is no container to resolve a name
// through, so the ability is recorded on the route: `aru routes` prints it,
// and the handler asks auth.Authorize for the Grant, which is the only thing
// that opens a repository (RULE 17).
func (rt *Route) Can(ability string, models ...string) *Route {
	if rt == nil {
		return nil
	}
	if len(models) > 0 {
		ability += "," + strings.Join(models, ",")
	}
	existing, _ := rt.action["can"].([]string)
	rt.action["can"] = append(existing, ability)
	return rt
}

// Block is Route::block. It stops two requests of the same session from
// running this route at once -- a double-submitted form, mostly -- by holding
// a lock for lockSeconds and waiting up to waitSeconds to take it.
func (rt *Route) Block(lockSeconds, waitSeconds *int) *Route {
	if rt == nil {
		return nil
	}
	rt.lockSeconds = lockSeconds
	rt.waitSeconds = waitSeconds
	return rt
}

// WithoutBlocking is Route::withoutBlocking.
func (rt *Route) WithoutBlocking() *Route { return rt.Block(nil, nil) }

// LocksFor is Route::locksFor. nil means the route takes no lock.
func (rt *Route) LocksFor() *int {
	if rt == nil {
		return nil
	}
	return rt.lockSeconds
}

// WaitsFor is Route::waitsFor. nil means the route takes no lock.
func (rt *Route) WaitsFor() *int {
	if rt == nil {
		return nil
	}
	return rt.waitSeconds
}

// routeValidators are the four checks, in the order PHP lists them. They hold
// no state, so one set serves every route.
var routeValidators = []matching.ValidatorInterface{
	matching.UriValidator{},
	matching.MethodValidator{},
	matching.SchemeValidator{},
	matching.HostValidator{},
}

// GetValidators is Route::getValidators. It returns the four checks that
// decide whether the route answers a request: path, method, scheme and host.
func (rt *Route) GetValidators() []matching.ValidatorInterface {
	return append([]matching.ValidatorInterface(nil), routeValidators...)
}
