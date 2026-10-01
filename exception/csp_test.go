package exception_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/log"
)

// appPolicy is the policy an application's security headers set before the
// handler runs, and the one that refused the pages' inline style.
const appPolicy = "default-src 'self'; style-src 'self'; script-src 'self'"

// styleBlock is the text a browser hashes: everything between <style> and
// </style>.
var styleBlock = regexp.MustCompile(`(?s)<style>(.*?)</style>`)

// behindAppPolicy runs fn behind Recover, behind a middleware that sets the
// application's policy first -- the order a server mounts them in.
func behindAppPolicy(h *exception.Handler, r *http.Request, fn http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	recovered := exception.Recover(h)(fn)
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", appPolicy)
		recovered.ServeHTTP(w, r)
	}).ServeHTTP(rec, r)
	return rec
}

// assertPolicyAllowsItsOwnStyle fails unless the response carries a policy of
// its own whose style-src is the hash of the one style block in its body.
func assertPolicyAllowsItsOwnStyle(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	blocks := styleBlock.FindAllStringSubmatch(rec.Body.String(), -1)
	if len(blocks) != 1 {
		t.Fatalf("the page carries %d style blocks, want exactly one", len(blocks))
	}
	sum := sha256.Sum256([]byte(blocks[0][1]))
	want := "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

	got := rec.Header().Values("Content-Security-Policy")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Content-Security-Policy = %q\nwant %q\nUnder the application's style-src 'self' the page renders unstyled.", got, want)
	}
}

// TestTheStatusPageAllowsItsOwnStyle: in production a failure is the status
// page, and it has to look like one under the application's policy.
func TestTheStatusPageAllowsItsOwnStyle(t *testing.T) {
	rec := behindAppPolicy(exception.NewHandler(exception.Config{}), httptest.NewRequest(http.MethodGet, "/", nil),
		func(http.ResponseWriter, *http.Request) { panic("boom") })

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	assertPolicyAllowsItsOwnStyle(t, rec)
}

// TestTheDebugPageAllowsItsOwnStyle: the page a developer reads a stack trace
// on is the last one that should come out as unstyled text.
func TestTheDebugPageAllowsItsOwnStyle(t *testing.T) {
	rec := behindAppPolicy(devHandler(), httptest.NewRequest(http.MethodGet, "/", nil),
		func(http.ResponseWriter, *http.Request) { panic("boom") })

	if !strings.Contains(rec.Body.String(), "arandu debug") {
		t.Fatal("the development handler did not draw the debug page")
	}
	assertPolicyAllowsItsOwnStyle(t, rec)
}

// TestTheDumpPageAllowsItsOwnStyle: dump-and-die draws the same page, through
// its own path.
func TestTheDumpPageAllowsItsOwnStyle(t *testing.T) {
	r := withCollector(httptest.NewRequest(http.MethodGet, "/", nil), log.NewCollector("req-1"))
	rec := behindAppPolicy(devHandler(), r, func(_ http.ResponseWriter, r *http.Request) {
		log.DumpDie(r.Context(), "checkpoint", 7)
	})

	assertPolicyAllowsItsOwnStyle(t, rec)
}
