package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
)

// seen records the request as the handler under the middleware saw it.
type seen struct {
	remote string
	scheme string
	body   string
	err    error
}

func run(mw func(http.Handler) http.Handler, r *http.Request) (*httptest.ResponseRecorder, *seen) {
	got := &seen{}
	rec := httptest.NewRecorder()

	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.remote, got.scheme = r.RemoteAddr, r.URL.Scheme
		if r.Body != nil {
			var body []byte
			body, got.err = io.ReadAll(r.Body)
			got.body = string(body)
		}
	})).ServeHTTP(rec, r)

	return rec, got
}

func TestTheDefaultHeadersAreOnEveryAnswer(t *testing.T) {
	rec, _ := run(middleware.SecurityHeaders(false), httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Origin-Agent-Cluster":         "?1",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	// The policy has no 'unsafe-inline' in it, and htmx works anyway because it
	// operates through attributes rather than inline script.
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("the policy allows inline script: %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("the policy does not refuse framing: %q", csp)
	}
}

func TestTheCspIsTheFullExpectedPolicy(t *testing.T) {
	rec, _ := run(middleware.SecurityHeaders(false), httptest.NewRequest(http.MethodGet, "/", nil))
	want := "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self'; " +
		"font-src 'self'; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
	if got := rec.Header().Get("Content-Security-Policy"); got != want {
		t.Errorf("Content-Security-Policy = %q, want %q", got, want)
	}
}

// TestTheCspRefusesPluginObjectsExplicitly pins the directive rather than its
// current fallback. Without object-src, an object or embed inherits
// default-src 'self', so a file served by this application remains loadable.
func TestTheCspRefusesPluginObjectsExplicitly(t *testing.T) {
	for _, dev := range []bool{false, true} {
		rec, _ := run(middleware.SecurityHeaders(dev), httptest.NewRequest(http.MethodGet, "/", nil))
		csp := rec.Header().Get("Content-Security-Policy")

		found := false
		for _, directive := range strings.Split(csp, "; ") {
			if directive == "object-src 'none'" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("development=%t Content-Security-Policy has no explicit object-src 'none': %q", dev, csp)
		}
	}
}

func TestHSTSIsProductionOnly(t *testing.T) {
	// Over plain HTTP it pins localhost to https and breaks every developer's
	// machine, and the pin outlives the mistake.
	rec, _ := run(middleware.SecurityHeaders(true), httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("a development server sent %q", got)
	}

	rec, _ = run(middleware.SecurityHeaders(false), httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=") {
		t.Errorf("Strict-Transport-Security = %q", got)
	}
}

var private = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

func TestAForwardedAddressFromSomebodyWhoIsNotTheProxyIsIgnored(t *testing.T) {
	// This is the whole reason the list exists: X-Forwarded-For is a string the
	// client chose, and believing it hands every visitor the ability to be any
	// address they like -- including somebody else's, which is how a throttle
	// becomes a way to lock another account out.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")

	_, got := run(middleware.TrustProxies(private), r)
	if got.remote != "203.0.113.7:44321" {
		t.Errorf("RemoteAddr = %q, and the request did not come from a proxy", got.remote)
	}
}

func TestBehindTheProxyTheVisitorIsTheOneTheProxyNamed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	r.Header.Set("X-Forwarded-Proto", "https")

	_, got := run(middleware.TrustProxies(private), r)
	if got.remote != "198.51.100.4" {
		t.Errorf("RemoteAddr = %q, so every visitor shares the balancer's rate limit", got.remote)
	}
	if got.scheme != "https" {
		t.Errorf("URL.Scheme = %q, so a link built for a mail points at http", got.scheme)
	}
}

func TestTheVisitorIsTheLastHopNobodyTrusts(t *testing.T) {
	// client, spoofed by the client, then the two proxies. Everything to the
	// left of the first untrusted entry was written by whoever that is.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Add("X-Forwarded-For", "203.0.113.9, 198.51.100.4")
	r.Header.Add("X-Forwarded-For", "10.0.0.9")

	_, got := run(middleware.TrustProxies(private), r)
	if got.remote != "198.51.100.4" {
		t.Errorf("RemoteAddr = %q, want the nearest hop that is not ours", got.remote)
	}
}

func TestAChainThatIsOursAllTheWayDownLeavesTheFirstEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Set("X-Forwarded-For", "10.0.0.7, 10.0.0.9")

	_, got := run(middleware.TrustProxies(private), r)
	if got.remote != "10.0.0.7" {
		t.Errorf("RemoteAddr = %q, want the leftmost entry", got.remote)
	}
}

func TestNothingIsTakenFromAChainThatCannotBeRead(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Set("X-Forwarded-For", "not-an-address")
	r.Header.Set("X-Forwarded-Proto", "javascript")

	_, got := run(middleware.TrustProxies(private), r)
	if got.remote != "10.0.0.3:8080" {
		t.Errorf("RemoteAddr = %q, want the peer left alone", got.remote)
	}
	if got.scheme != "" {
		t.Errorf("URL.Scheme = %q, and it ends up in a link", got.scheme)
	}
}

func TestWithNoTrustedProxiesNothingIsBelieved(t *testing.T) {
	// The right behaviour for a process listening on the internet directly, and
	// the right behaviour for a project that has not configured this yet.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")

	_, got := run(middleware.TrustProxies(nil), r)
	if got.remote != "10.0.0.3:8080" {
		t.Errorf("RemoteAddr = %q with an empty trust list", got.remote)
	}
}

func TestTheHostIsNeverTakenFromAHeader(t *testing.T) {
	// Rewriting it is how a cache is poisoned and how a password-reset link is
	// sent pointing at somebody else's server.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Host = "app.example.test"
	r.Header.Set("X-Forwarded-Host", "evil.example")

	middleware.TrustProxies(private)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Host != "app.example.test" {
			t.Errorf("Host = %q", r.Host)
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}

func TestABodyOverTheLimitIsRefusedRatherThanRead(t *testing.T) {
	// One POST is enough to take a process down, and it needs no credentials and
	// no route that does anything.
	r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(strings.Repeat("a", 4096)))

	_, got := run(middleware.LimitBodySize(64), r)
	if got.err == nil {
		t.Fatalf("the whole body was read: %d bytes", len(got.body))
	}
	if len(got.body) > 64 {
		t.Errorf("%d bytes were read past a limit of 64", len(got.body))
	}
}

func TestABodyUnderTheLimitArrivesWhole(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader("title=a+draft"))

	_, got := run(middleware.LimitBodySize(1<<20), r)
	if got.err != nil {
		t.Fatalf("reading: %v", got.err)
	}
	if got.body != "title=a+draft" {
		t.Errorf("body = %q", got.body)
	}
}

func TestAtNamesHostsThatTrustHostsWasNotBuiltWith(t *testing.T) {
	t.Cleanup(middleware.FlushState)
	middleware.At([]string{"trusted.test"}, true)

	if got := middleware.Hosts(); len(got) != 1 || got[0] != "trusted.test" {
		t.Fatalf("Hosts() = %v, want [trusted.test]", got)
	}
	if !middleware.TrustsSubdomains() {
		t.Fatal("At asked for subdomains and TrustsSubdomains says otherwise")
	}

	mw := middleware.TrustHosts(nil, false)

	for _, host := range []string{"trusted.test", "shop.trusted.test"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		if rec, _ := run(mw, r); rec.Code != http.StatusOK {
			t.Fatalf("host %q was refused with %d", host, rec.Code)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "evil.test"
	if rec, _ := run(mw, r); rec.Code != http.StatusBadRequest {
		t.Fatalf("an untrusted host answered %d, want 400", rec.Code)
	}
}

func TestFlushStateForgetsWhatAtWasTold(t *testing.T) {
	middleware.At([]string{"trusted.test"}, true)
	middleware.FlushState()

	if got := middleware.Hosts(); got != nil {
		t.Fatalf("Hosts() = %v after FlushState, want nil", got)
	}
	if middleware.TrustsSubdomains() {
		t.Fatal("TrustsSubdomains survived FlushState")
	}
}

func TestSkipWhenTakesTheRequestOutOfTheCorsHandling(t *testing.T) {
	t.Cleanup(middleware.FlushState)
	middleware.SkipWhen(func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/internal/")
	})

	mw := middleware.HandleCors([]string{"*"}, []string{http.MethodGet}, nil, 0, false)

	skipped := httptest.NewRequest(http.MethodGet, "/internal/health", nil)
	skipped.Header.Set("Origin", "https://example.test")
	if rec, _ := run(mw, skipped); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("a skipped request should carry no CORS headers")
	}

	handled := httptest.NewRequest(http.MethodGet, "/public/health", nil)
	handled.Header.Set("Origin", "https://example.test")
	// "*" answers "*", and not the caller's own origin echoed back.
	//
	// This assertion used to expect the echo, which is what the middleware did.
	// Without credentials the two behave the same for a browser, so the test
	// passed -- and the moment credentials were turned on, the echo became the
	// way every site on the internet reads a signed-in user's responses. The
	// wildcard and a named origin are kept apart so that CorsConfig.Valid can
	// refuse the one combination that leaks.
	if rec, _ := run(mw, handled); rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("a request that is not skipped should carry the CORS headers")
	}
}

// TestTheWholeForwardedChainReachesTheRequest pins the other half of the
// IPs regression: the middleware knew the chain and threw it away, keeping
// only the one address it wrote onto RemoteAddr, so hhttp.Request.IPs had
// nothing to answer with but a list of one.
//
// The chain it leaves is the forwarded entries plus the peer, with our own
// proxies removed, nearest hop first. Only the first is verified --
// everything to the left of it was written by whoever that is -- which is
// why IP is the first and not the last.
func TestTheWholeForwardedChainReachesTheRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")

	var chain []string
	var remote string
	middleware.TrustProxies(private)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		chain = hhttp.NewRequest(r).IPs()
		remote = r.RemoteAddr
	})).ServeHTTP(httptest.NewRecorder(), r)

	if remote != "2.2.2.2" {
		t.Fatalf("RemoteAddr = %q, want the nearest hop that is not ours", remote)
	}
	if len(chain) != 2 || chain[0] != "2.2.2.2" || chain[1] != "1.1.1.1" {
		t.Fatalf("IPs = %v, want [2.2.2.2 1.1.1.1]", chain)
	}
}

// TestOurOwnProxiesAreNotInTheChain pins the filtering: a hop inside the
// trusted prefixes is infrastructure, not a visitor, and it is dropped.
func TestOurOwnProxiesAreNotInTheChain(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.3:8080"
	r.Header.Add("X-Forwarded-For", "203.0.113.9, 198.51.100.4")
	r.Header.Add("X-Forwarded-For", "10.0.0.9")

	var chain []string
	middleware.TrustProxies(private)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		chain = hhttp.NewRequest(r).IPs()
	})).ServeHTTP(httptest.NewRecorder(), r)

	if len(chain) != 2 || chain[0] != "198.51.100.4" || chain[1] != "203.0.113.9" {
		t.Fatalf("IPs = %v, want [198.51.100.4 203.0.113.9]", chain)
	}
}

// TestAnUntrustedPeerLeavesNoChain pins that nothing is attached when the
// request did not come from a proxy we trust: the list of one the raw
// RemoteAddr gives is the whole truth there.
func TestAnUntrustedPeerLeavesNoChain(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")

	var chain []string
	middleware.TrustProxies(private)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		chain = hhttp.NewRequest(r).IPs()
	})).ServeHTTP(httptest.NewRecorder(), r)

	if len(chain) != 1 || chain[0] != "203.0.113.7" {
		t.Fatalf("IPs = %v, want [203.0.113.7]", chain)
	}
}
