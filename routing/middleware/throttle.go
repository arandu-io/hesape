package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/pipeline"
)

// Refuse answers a request a middleware turned away.
//
// It is a parameter and not a call into a package, because how a refusal is
// written is the request layer's decision -- an HTMX request needs a header
// that a plain one does not, and getting that wrong is how a 429 became a
// button that did nothing. hesape/httpx.Refuse is the function that fits, and
// passing it keeps the routing layer below the layer that owns the answer.
//
// The status and the sentence are this middleware's, and every implementation
// has to keep them: 429 stays 429, so logs and monitoring see what they saw.
type Refuse func(w http.ResponseWriter, r *http.Request, status int, message string)

// KeyFunc chooses what a request is limited against.
//
// Use KeyByIP for public routes and KeyBySession for authenticated ones:
// limiting sign-in attempts by IP alone does not stop distributed credential
// stuffing, because every attempt arrives from a different address.
type KeyFunc func(*http.Request) string

// Throttle limits how many requests a caller may make to the routes under it.
//
//	r.Group(routing.Group{
//		Prefix:     "/api",
//		Middleware: []pipeline.Middleware[http.Handler]{
//			middleware.Throttle(limiter, cache.PerMinute(60), middleware.KeyByIP, httpx.Refuse),
//		},
//	})
//
// The limit argument carries the budget and the window; the key comes from the
// request, so one Limit value serves every caller. Whatever Key the Limit was
// built with is replaced -- a middleware limits per caller by definition, and a
// fixed key would be one budget shared by everybody.
//
// # It fails open
//
// When the store cannot be reached the request is allowed through and the
// failure is logged at ERROR. A rate limiter is a guard against abuse, not a
// dependency of serving a page, and a limiter that refuses everything while its
// store is down converts a cache outage into a total one. Where the opposite is
// right -- a sign-in, where the budget is the security control -- the check
// belongs in the handler, against the same limiter, and it can choose to fail
// closed.
//
// # The headers and the sentence agree
//
// X-RateLimit-Limit and X-RateLimit-Remaining go on every answer, and
// Retry-After goes on the refusal carrying the same number as the sentence,
// computed once. A header and a sentence that disagree about how long to wait
// are worse than one that says nothing.
func Throttle(rl *cache.RateLimiter, limit cache.Limit, key KeyFunc, refuse Refuse) pipeline.Middleware[http.Handler] {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := rl.Attempt(r.Context(), limit.By(key(r)))
			if err != nil {
				log.For(r.Context()).Error("the rate limiter could not be reached; the request was allowed through",
					"path", r.URL.Path, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit.MaxAttempts))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			if result.OK {
				next.ServeHTTP(w, r)
				return
			}

			seconds := strconv.Itoa(int(result.RetryAfter.Seconds()) + 1)
			w.Header().Set("Retry-After", seconds)
			refuse(w, r, http.StatusTooManyRequests,
				"too many requests: wait "+seconds+" seconds and try again")
		})
	}
}

// KeyByIP keys on the peer address: the whole address over IPv4, and the /64 it
// sits in over IPv6.
//
// It reads RemoteAddr and never X-Forwarded-For: a header the client controls
// is a way to reset someone else's counter. Behind a proxy, have the proxy
// rewrite RemoteAddr, or key on something the proxy signs. A proxy that does
// neither gives every request in the world the same key, and then every limit
// keyed this way is a limit on the whole application -- which for the sign-in
// throttle means twenty-five wrong passwords a minute across every customer.
//
// # Why the IPv6 address is masked
//
// Because otherwise it is not a limit. IPv4 addresses are scarce, so keying on
// the whole address costs an attacker money; a /64 is the smallest block any
// end site is given -- a home connection, a VPS, a phone -- and every one of
// them holds eighteen quintillion addresses that all reach this server. Keyed
// on the full address, one machine with a routed /64 had an unlimited number of
// budgets: it could walk a list of accounts forever, and fill the sign-in
// throttle's table on its own, from a single upstream link.
//
// The /64 and not something wider, because it is the one boundary that is
// always a single link. A /48 would be one customer at some providers and a
// whole building at others, and grouping two subscribers under one budget is
// how a limit locks out somebody who did nothing.
func KeyByIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// An unparseable address is used as it stands rather than dropped: it is
	// the only key left, and merging every one of them into a shared bucket
	// would be a way to be limited by somebody else's traffic.
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "ip:" + host
	}
	if addr.Is4() || addr.Is4In6() {
		return "ip:" + addr.Unmap().String()
	}
	// WithZone("") because a link-local scope is the receiving interface's
	// name, not the sender's, and it would key two arrivals of the same address
	// apart.
	block, err := addr.WithZone("").Prefix(64)
	if err != nil {
		return "ip:" + addr.String()
	}
	return "ip:" + block.String()
}

// KeyBySession keys on the session id, falling back to the address for
// anonymous requests. Pass session.Store.ID as the extractor.
func KeyBySession(idFrom func(*http.Request) string) KeyFunc {
	return func(r *http.Request) string {
		if id := idFrom(r); id != "" {
			return "session:" + id
		}
		return KeyByIP(r)
	}
}
