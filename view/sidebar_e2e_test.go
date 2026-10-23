//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/view"
)

var sidebarDrivers = browser.Registry{"cdp": cdp.Launch}

// sidebarPage serves a page with a sidebar drawn in mode, the button that
// names it, and a link that swaps a second sidebar in through htmx, under the
// same policy an application serves.
func sidebarPage(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "script-src 'self'")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sidebar</title>
<script src="%s"></script><script src="%s"></script><script src="%s"></script></head><body>
<button type="button" data-sidebar-toggle="nav">Menu</button>
<aside class="sidebar" id="nav" data-sidebar-mode="%s"><nav aria-label="Sidebar"><a href="/">Home</a></nav></aside>
<main><button type="button" hx-get="/late" hx-target="#slot">Load</button><div id="slot"></div></main>
</body></html>`, view.Asset("htmx.min.js"), view.Asset("basecoat.bundle.js"), view.Asset("ui.js"), mode)
	})
	mux.HandleFunc("GET /late", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<aside class="sidebar" id="late" data-sidebar-mode="full"><nav aria-label="Late"><a href="/">Home</a></nav></aside>`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func evalString(t *testing.T, page *browser.Page, expr string) string {
	t.Helper()
	var got string
	page.Evaluate(expr, &got)
	return got
}

const (
	navMode     = `document.getElementById('nav').getAttribute('data-sidebar-mode')`
	navExpanded = `String(document.querySelector('[data-sidebar-toggle="nav"]').getAttribute('aria-expanded'))`
	navSaved    = `String(localStorage.getItem('arandu.sidebar.nav'))`
)

// TestTheSidebarFoldsToTheStripAndComesBackThatWay drives the toggle on a wide
// screen: a press switches the mode, the button says which, the choice is
// remembered, the next page comes back folded, and a value that is not one of
// the two modes is ignored.
func TestTheSidebarFoldsToTheStripAndComesBackThatWay(t *testing.T) {
	server := sidebarPage(t, "full")
	page := browser.Open(t, sidebarDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	page.Evaluate(`localStorage.clear()`, nil)
	page.Visit("/")

	if got := evalString(t, page, navMode); got != "full" {
		t.Fatalf("first paint mode = %q, want the server's full", got)
	}
	if got := evalString(t, page, navExpanded); got != "true" {
		t.Fatalf("aria-expanded on the full column = %q, want true", got)
	}

	page.ClickRole("button", "Menu")
	if got := evalString(t, page, navMode); got != "slim" {
		t.Fatalf("after one press mode = %q, want slim", got)
	}
	if got := evalString(t, page, navExpanded); got != "false" {
		t.Fatalf("aria-expanded on the strip = %q, want false", got)
	}
	if got := evalString(t, page, navSaved); got != "slim" {
		t.Fatalf("remembered = %q, want slim", got)
	}

	page.Visit("/")
	if got := evalString(t, page, navMode); got != "slim" {
		t.Fatalf("the next page came back %q, want the remembered slim", got)
	}
	if got := evalString(t, page, navExpanded); got != "false" {
		t.Fatalf("aria-expanded after the restore = %q, want false", got)
	}

	page.ClickRole("button", "Menu")
	if got := evalString(t, page, navMode); got != "full" {
		t.Fatalf("after the second press mode = %q, want full", got)
	}
	if got := evalString(t, page, navSaved); got != "full" {
		t.Fatalf("remembered = %q, want full", got)
	}

	for _, junk := range []string{"constructor", "hidden", "__proto__", ""} {
		page.Evaluate(fmt.Sprintf(`localStorage.setItem('arandu.sidebar.nav', %q)`, junk), nil)
		page.Visit("/")
		if got := evalString(t, page, navMode); got != "full" {
			t.Fatalf("with %q remembered the mode = %q, want the server's full", junk, got)
		}
	}
}

// TestASidebarSwappedInComesBackInTheRememberedMode holds the htmx half: a
// sidebar that arrives after the page loaded is restored as it lands.
func TestASidebarSwappedInComesBackInTheRememberedMode(t *testing.T) {
	server := sidebarPage(t, "full")
	page := browser.Open(t, sidebarDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	page.Evaluate(`localStorage.setItem('arandu.sidebar.late', 'slim')`, nil)
	page.ClickRole("button", "Load")
	page.Find(browser.CSS("#late", "the swapped sidebar is located by id")).ExpectVisible()
	if got := evalString(t, page, `document.getElementById('late').getAttribute('data-sidebar-mode')`); got != "slim" {
		t.Fatalf("swapped sidebar mode = %q, want the remembered slim", got)
	}
}

// TestTheSidebarSwitchesWhenStorageThrows: a browser that refuses storage
// still switches the page it is on, and says nothing in the console.
func TestTheSidebarSwitchesWhenStorageThrows(t *testing.T) {
	server := sidebarPage(t, "slim")
	page := browser.Open(t, sidebarDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	page.Evaluate(`Object.defineProperty(window, 'localStorage', { get: function () { throw new DOMException('denied', 'SecurityError'); } }), true`, nil)
	page.ClickRole("button", "Menu")
	if got := evalString(t, page, navMode); got != "full" {
		t.Fatalf("with storage refused the mode = %q, want full", got)
	}
}

// TestOnANarrowScreenTheButtonOpensTheOverlay: below the breakpoint the mode
// is left alone and nothing is remembered; the button opens and closes the
// overlay the vendored script drives.
func TestOnANarrowScreenTheButtonOpensTheOverlay(t *testing.T) {
	server := sidebarPage(t, "full")
	page := browser.Open(t, sidebarDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 480, Height: 800}))
	page.Visit("/")
	page.Evaluate(`localStorage.clear()`, nil)
	page.Visit("/")

	hidden := `String(document.getElementById('nav').getAttribute('aria-hidden'))`
	if got := evalString(t, page, hidden); got != "true" {
		t.Fatalf("the overlay starts aria-hidden = %q, want true", got)
	}
	page.ClickRole("button", "Menu")
	if got := evalString(t, page, hidden); got != "false" {
		t.Fatalf("after a press the overlay is aria-hidden = %q, want false", got)
	}
	if got := evalString(t, page, navExpanded); got != "true" {
		t.Fatalf("aria-expanded on the open overlay = %q, want true", got)
	}
	if got := evalString(t, page, navMode); got != "full" {
		t.Fatalf("the narrow press changed the mode to %q", got)
	}
	if got := evalString(t, page, navSaved); got != "null" {
		t.Fatalf("the narrow press remembered %q", got)
	}
}
