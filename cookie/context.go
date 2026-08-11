package cookie

import "context"

// jarKey is the context key the request's jar travels under. It is an
// unexported empty struct type, so nothing outside this package can put a jar
// where the middleware will look for one.
type jarKey struct{}

// WithCookieJar returns a copy of ctx carrying the jar for this request.
//
// It answers to nothing in the PHP, and it exists because of what PHP does not
// have to do. There, the container hands the same CookieJar singleton to every
// controller, and the queue on it is a per-request queue because the process
// serves one request and dies. Here one process serves many at once, so the
// AddQueuedCookiesToResponse middleware clones the application's jar per
// request and calls this; a handler that queued on the shared jar instead would
// be writing a cookie into somebody else's response.
func WithCookieJar(ctx context.Context, j *CookieJar) context.Context {
	return context.WithValue(ctx, jarKey{}, j)
}

// CookieJarFrom returns the jar for this request, which is what a handler calls
// to queue a cookie:
//
//	cookie.CookieJarFrom(r.Context()).Queue(c)
//
// It is nil when no jar was installed, which means the
// AddQueuedCookiesToResponse middleware is not on this route. Nothing would
// write the queue in that case, so the nil is deliberate and not softened into
// an empty jar that swallows cookies.
func CookieJarFrom(ctx context.Context) *CookieJar {
	j, _ := ctx.Value(jarKey{}).(*CookieJar)
	return j
}
