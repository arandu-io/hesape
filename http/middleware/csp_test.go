package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/http/middleware"
)

// The text every application has been sending, written out once, so that
// moving the policy into a value is proven not to have changed a byte of it.
const sentBefore = "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data: https://cdn.example.com; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

func TestTheDefaultPolicyIsTheTextSecurityHeadersAlwaysSent(t *testing.T) {
	if got := middleware.DefaultContentSecurityPolicy("https://cdn.example.com").String(); got != sentBefore {
		t.Fatalf("the default policy changed:\n got %s\nwant %s", got, sentBefore)
	}
	rec := httptest.NewRecorder()
	middleware.SecurityHeaders(true, "https://cdn.example.com")(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != sentBefore {
		t.Fatalf("SecurityHeaders sends %s", got)
	}
}

func TestARouteWidensItsOwnPolicyWithoutTouchingTheDefault(t *testing.T) {
	base := middleware.DefaultContentSecurityPolicy()
	page := base.
		Allow("script-src", "https://www.pagador.com.br", "https://*.cardinalcommerce.com").
		Allow("style-src", "'unsafe-inline'").
		Allow("frame-src", "https:").
		Allow("form-action", "https:").
		Allow("report-uri", "/checkout/csp-report").
		Allow("script-src", "https://www.pagador.com.br")
	got := page.String()
	for _, want := range []string{
		"script-src 'self' https://www.pagador.com.br https://*.cardinalcommerce.com;",
		"style-src 'self' 'unsafe-inline';",
		"frame-src https:;",
		"form-action 'self' https:;",
		"report-uri /checkout/csp-report",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the widened policy lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "pagador") != 1 {
		t.Fatal("a source added twice was listed twice")
	}
	if base.String() != middleware.DefaultContentSecurityPolicy().String() {
		t.Fatal("widening a copy changed the policy it was copied from")
	}
	if !strings.Contains(middleware.DefaultContentSecurityPolicy().Allow("object-src", "https://x.example.com").String(), "object-src https://x.example.com;") {
		t.Fatal("a source added to a 'none' directive kept the 'none'")
	}

	rec := httptest.NewRecorder()
	middleware.SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page.Apply(w)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Content-Security-Policy") != got {
		t.Fatal("Apply did not replace the policy SecurityHeaders wrote")
	}
}

func TestASourceThatWouldChangeThePolicyPanics(t *testing.T) {
	for _, tc := range []struct{ directive, source string }{
		{"script-src", "'unsafe-inline'"},
		{"script-src", "'unsafe-eval'"},
		{"style-src", "'unsafe-eval'"},
		{"script-src", "http://cdn.example.com"},
		{"script-src", "https://cdn.example.com; script-src *"},
		{"script-src", "https://*"},
		{"script-src", "https://cdn.*.example.com"},
		{"script-src", "https://cdn.example.com/path.js"},
		{"script-src", "*"},
		{"report-uri", "https://collector.example.com"},
		{"Script-Src", "'self'"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s %q was accepted", tc.directive, tc.source)
				}
			}()
			middleware.DefaultContentSecurityPolicy().Allow(tc.directive, tc.source)
		}()
	}
}

func TestTheReportHandlerKeepsNeitherThePageNorTheBlockedPath(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	body := `{"csp-report":{"document-uri":"https://app.example.com/checkout/SECRET-LINK-TOKEN","violated-directive":"script-src-elem","effective-directive":"script-src-elem","blocked-uri":"https://evil.example.com/skimmer.js?card=4111"}}`
	rec := httptest.NewRecorder()
	middleware.CSPReportHandler(log).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the report was answered %d", rec.Code)
	}
	line := buf.String()
	for _, leak := range []string{"SECRET-LINK-TOKEN", "skimmer.js", "4111", "app.example.com"} {
		if strings.Contains(line, leak) {
			t.Fatalf("the log line kept %q: %s", leak, line)
		}
	}
	if !strings.Contains(line, "directive=script-src-elem") || !strings.Contains(line, "blocked=https://evil.example.com") {
		t.Fatalf("the log line lost the directive or the origin: %s", line)
	}
}
