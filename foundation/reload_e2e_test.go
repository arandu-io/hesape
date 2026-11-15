//go:build e2e

package foundation_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/view"
)

var reloadDrivers = browser.Registry{"cdp": cdp.Launch}

// A development server with the application's own policy. The page that answers
// 403 is the built-in status page, which carries a policy of its own with
// default-src 'none': the browser would refuse the reload script on it and the
// kit would fail the step for a violation the page did nothing to earn.
func reloadServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Cleanup(foundation.SetReloadTagForTest(view.ReloadTag("/_arandu/reload")))

	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /_arandu/reload", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "one-process")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Home</title></head><body><h1>Home</h1></body></html>`)
	})
	mux.HandleFunc("GET /notes/1", func(w http.ResponseWriter, r *http.Request) {
		exception.WriteStatusPage(w, r, http.StatusForbidden, "This note belongs to somebody else.")
	})
	handler := middleware.SecurityHeaders(true)(mux)
	server := httptest.NewServer(foundation.LiveReloadForTest(handler))
	t.Cleanup(server.Close)
	return server
}

// TestAForbiddenPageInDevelopmentIsOpenedWithoutAPolicyViolation: the ordinary
// page follows the restart, and the 403 page, whose policy refuses scripts, is
// left alone and opens clean.
func TestAForbiddenPageInDevelopmentIsOpenedWithoutAPolicyViolation(t *testing.T) {
	server := reloadServer(t)
	page := browser.Open(t, reloadDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 600}))

	page.Visit("/")
	page.ExpectText("Home")
	var tags float64
	page.Evaluate(`document.querySelectorAll('script[src*="arandu-reload.js"]').length`, &tags)
	if tags != 1 {
		t.Fatalf("the ordinary page carries %v reload tags, want 1", tags)
	}

	page.Visit("/notes/1")
	page.ExpectText("This note belongs to somebody else.")
	page.Evaluate(`document.querySelectorAll('script[src*="arandu-reload.js"]').length`, &tags)
	if tags != 0 {
		t.Fatalf("the forbidden page carries %v reload tags, want 0", tags)
	}
}
