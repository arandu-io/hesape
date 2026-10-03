package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	hhttp "github.com/arandu-io/hesape/http"
)

// SecurityHeaders applies the default headers.
//
// The CSP is restrictive on purpose and still works with HTMX, because HTMX
// operates through attributes rather than inline script. There is no global
// 'unsafe-inline' in this framework.
//
// imageOrigins are the addresses, besides this application's own, that an image
// may load from: the public address of a disk whose files the pages embed, such
// as a bucket served by a CDN. They reach img-src and no other directive, so
// scripts, styles, fonts and connections stay on this origin whatever is passed.
//
// Each is a bare https origin: the scheme and the host, with an optional port
// and nothing after them. An origin that is anything else panics here, while
// the pipeline is wired, rather than going out on every answer as a policy that
// says something other than what was meant: the value is written into a header,
// where a ';' ends the directive and starts another, and a wildcard or a path
// changes which addresses the list admits.
func SecurityHeaders(dev bool, imageOrigins ...string) hhttp.Middleware {
	csp := DefaultContentSecurityPolicy(imageOrigins...).String()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", csp)
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Origin-Agent-Cluster", "?1")
			if !dev {
				// HSTS over plain HTTP would pin localhost to https and break
				// every developer's machine, so it is production only.
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// imageOrigin returns origin as img-src spells it, or panics saying why it
// cannot be one.
//
// A trailing slash is accepted and dropped, because a configured address is
// often written with one; the host is lowercased, because the browser compares
// it that way.
func imageOrigin(origin string) string {
	refuse := func(why string) {
		panic(fmt.Sprintf("http/middleware: SecurityHeaders was given the image origin %q, %s", origin, why))
	}

	u, err := url.Parse(origin)
	if err != nil {
		refuse("which is not an address: " + err.Error())
	}
	if u.Scheme != "https" {
		refuse("which is not https: an image fetched over plain http can be replaced on the way, and a page served over https refuses to draw it anyway")
	}
	if u.Opaque != "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		refuse("which is not a bare origin: write the scheme and the host, and nothing after them")
	}

	name, port, hasPort := strings.Cut(u.Host, ":")
	if !validHostName(name) {
		refuse("whose host is not a name: a wildcard, a quote or a ';' would widen the list or end the directive")
	}
	if hasPort && (port == "" || strings.Trim(port, "0123456789") != "") {
		refuse("whose port is not a number")
	}
	return "https://" + strings.ToLower(u.Host)
}

// validHostName reports whether name is a DNS name made of letters, digits,
// dots and hyphens, which is every character img-src can take from a host
// without that character meaning something to the policy.
func validHostName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}
