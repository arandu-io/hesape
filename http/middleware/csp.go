package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// ContentSecurityPolicy is one Content-Security-Policy, held as an ordered list
// of directives rather than as a string.
//
// SecurityHeaders sends DefaultContentSecurityPolicy on every answer. A route
// that has to load something else -- a payment page that runs an acquirer's
// card script, a 3-D Secure challenge drawn by the card issuer -- widens that
// same policy for itself with Allow and writes it with Apply, instead of
// relaxing the policy of the whole application. The value is immutable: Allow
// returns a copy, so one route widening it never reaches another.
//
// Every source is checked when it is added, and a source that could change what
// the header says panics there, while the route is being written, for the
// reason imageOrigin does: the value goes into a header where a ';' ends the
// directive and starts another.
type ContentSecurityPolicy struct {
	directives []cspDirective
}

type cspDirective struct {
	name    string
	sources []string
}

// DefaultContentSecurityPolicy is the policy SecurityHeaders sends: everything
// from this origin, images also from imageOrigins, plugins none, framing none.
// See SecurityHeaders for what imageOrigins accepts.
func DefaultContentSecurityPolicy(imageOrigins ...string) ContentSecurityPolicy {
	images := []string{"'self'", "data:"}
	for _, origin := range imageOrigins {
		images = append(images, imageOrigin(origin))
	}
	return ContentSecurityPolicy{directives: []cspDirective{
		{name: "default-src", sources: []string{"'self'"}},
		{name: "script-src", sources: []string{"'self'"}},
		{name: "style-src", sources: []string{"'self'"}},
		// Explicit, though default-src already covers it. A font is vendored and
		// served from this origin like everything else (view.RegisterAsset), and
		// spelling it out is what makes the line say so.
		{name: "font-src", sources: []string{"'self'"}},
		{name: "img-src", sources: images},
		{name: "connect-src", sources: []string{"'self'"}},
		// Plugin objects can execute active content, so the same-origin fallback
		// from default-src is not restrictive enough for them.
		{name: "object-src", sources: []string{"'none'"}},
		{name: "frame-ancestors", sources: []string{"'none'"}},
		{name: "base-uri", sources: []string{"'self'"}},
		{name: "form-action", sources: []string{"'self'"}},
	}}
}

// Allow returns a copy of the policy with sources added to directive, which is
// appended after the existing ones when the policy does not have it yet. A
// source already listed is not listed twice, and adding to a directive that
// says 'none' replaces the 'none'.
//
// A source is one of:
//
//   - a keyword: 'self', 'none', 'strict-dynamic', 'report-sample', or
//     'unsafe-inline' in style-src and nowhere else -- an inline style cannot
//     run code, an inline script is the attack the policy exists for, and
//     'unsafe-eval' is not accepted anywhere;
//   - a scheme: https: or data:;
//   - an https origin, whose host may start with one "*." label to admit the
//     subdomains of one name, as a third-party script's own hosts need;
//   - for report-uri only, a path on this origin.
//
// Anything else panics, naming the source and why.
func (p ContentSecurityPolicy) Allow(directive string, sources ...string) ContentSecurityPolicy {
	if !validDirectiveName(directive) {
		panic(fmt.Sprintf("http/middleware: %q is not a Content-Security-Policy directive name", directive))
	}
	out := ContentSecurityPolicy{directives: make([]cspDirective, len(p.directives))}
	for i, d := range p.directives {
		out.directives[i] = cspDirective{name: d.name, sources: append([]string(nil), d.sources...)}
	}
	at := -1
	for i, d := range out.directives {
		if d.name == directive {
			at = i
		}
	}
	if at < 0 {
		out.directives = append(out.directives, cspDirective{name: directive})
		at = len(out.directives) - 1
	}
	for _, source := range sources {
		source = cspSource(directive, source)
		current := out.directives[at].sources
		if len(current) == 1 && current[0] == "'none'" {
			current = nil
		}
		listed := false
		for _, existing := range current {
			listed = listed || existing == source
		}
		if !listed {
			current = append(current, source)
		}
		out.directives[at].sources = current
	}
	return out
}

// String is the header value, directives in order, separated by "; ".
func (p ContentSecurityPolicy) String() string {
	parts := make([]string, 0, len(p.directives))
	for _, d := range p.directives {
		parts = append(parts, d.name+" "+strings.Join(d.sources, " "))
	}
	return strings.Join(parts, "; ")
}

// Apply writes the policy on w, replacing the one SecurityHeaders wrote. Call
// it before the first byte of the body: a header set after that is not sent.
func (p ContentSecurityPolicy) Apply(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", p.String())
}

func validDirectiveName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c == '-') {
			return false
		}
	}
	return true
}

func cspSource(directive, source string) string {
	refuse := func(why string) {
		panic(fmt.Sprintf("http/middleware: the source %q cannot be added to %s, %s", source, directive, why))
	}
	switch source {
	case "'self'", "'none'", "'strict-dynamic'", "'report-sample'", "https:", "data:":
		return source
	case "'unsafe-inline'":
		if directive != "style-src" && directive != "style-src-elem" && directive != "style-src-attr" {
			refuse("because inline code is what the policy exists to stop; only styles may be inline")
		}
		return source
	case "'unsafe-eval'", "'wasm-unsafe-eval'":
		refuse("because evaluating strings as code is never admitted")
	}
	if directive == "report-uri" {
		if !strings.HasPrefix(source, "/") || strings.HasPrefix(source, "//") || strings.ContainsAny(source, " ;,'\"") {
			refuse("because a report address is a path on this origin")
		}
		return source
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		refuse("because it is not a bare https origin")
	}
	name, port, hasPort := strings.Cut(u.Host, ":")
	host := strings.TrimPrefix(name, "*.")
	if !validHostName(host) || strings.Contains(host, "*") || !strings.Contains(host, ".") {
		refuse("because its host is not a name, or carries a wildcard anywhere but the first label")
	}
	if hasPort && (port == "" || strings.Trim(port, "0123456789") != "") {
		refuse("because its port is not a number")
	}
	return "https://" + strings.ToLower(u.Host)
}

// CSPReportHandler receives the violation reports a policy with report-uri
// sends, and records each as one log line: the directive that was violated and
// the origin of what was blocked. It answers 204 whatever it was sent.
//
// Nothing else of a report is kept. A report carries the address of the page
// it came from, and a page that needs its own policy is often one whose address
// is a capability -- a payment link, a signed invitation -- so the document URI
// never reaches the log, and a blocked URI is cut down to its origin.
func CSPReportHandler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Report struct {
				Violated  string `json:"violated-directive"`
				Effective string `json:"effective-directive"`
				Blocked   string `json:"blocked-uri"`
			} `json:"csp-report"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err == nil && log != nil {
			directive := body.Report.Effective
			if directive == "" {
				directive = body.Report.Violated
			}
			blocked := body.Report.Blocked
			if u, err := url.Parse(blocked); err == nil && u.Host != "" {
				blocked = u.Scheme + "://" + u.Host
			} else if blocked != "inline" && blocked != "eval" {
				blocked = "other"
			}
			log.WarnContext(r.Context(), "content security policy violation", "directive", directive, "blocked", blocked)
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
