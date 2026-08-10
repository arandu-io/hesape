package httpx

import (
	"net/http"

	"github.com/arandu-io/hesape/pipeline"
)

// Middleware is the standard net/http signature, named.
//
// It is an alias and not a defined type, and the = is the whole point:
// hesape/session, hesape/cookie and hesape/exception all produce middleware, and
// none of them may import this package -- exception in particular is what
// answers a failure that never reached a Context. With an alias they return
// pipeline.Middleware[http.Handler] and it IS an httpx.Middleware; with a
// defined type each of them would have to import the layer that calls it, which
// is the cycle this collection is being reorganised to remove.
//
// Being the standard func(http.Handler) http.Handler underneath is what keeps
// every middleware written for the Go ecosystem usable here unchanged. Note the
// one place that is not free: a []func(http.Handler) http.Handler is not
// assignable to a []Middleware, because a slice of an aliased element type
// converts and a slice type does not.
//
// Compose with pipeline.Chain. There is no Chain here: a second one, differing
// only in being non-generic, is a second way to do the same thing (RULE 9).
type Middleware = pipeline.Middleware[http.Handler]
