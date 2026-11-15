//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/internal/testkit/browser"
	"github.com/arandu-io/hesape/view"
)

// lifecyclePage serves a behaviour that writes down every hook it is told about,
// in a region a button swaps with the default swap and with an explicit one.
func lifecyclePage(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /probe.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `window.__hooks = [];
arandu.ui.define('probe', {
  mounted: function (ctx) { window.__hooks.push('mounted ' + ctx.element.id); },
  updated: function (ctx) { window.__hooks.push('updated ' + ctx.element.id); },
  destroyed: function (ctx) { window.__hooks.push('destroyed ' + ctx.element.id); },
});`)
	})
	mux.HandleFunc("GET /next", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<section><div id="new" data-kyse-behavior="probe">new</div></section>`)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "script-src 'self'")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="ypy-config" content='{"includeIndicatorCSS":false}'><title>L</title>
<script src="%s"></script><script src="%s"></script><script src="/probe.js"></script></head><body>
<button ypy-get="/next" ypy-target="#out" ypy-swap="innerHTML">Swap</button>
<div id="out"><section><div id="old" data-kyse-behavior="probe">old</div></section></div>
<div id="kept" data-kyse-behavior="probe">kept</div></body></html>`, view.Asset("ypy.min.js"), view.Asset("ui.js"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestABehaviourIsMountedWhereItArrivesAndDestroyedWhereItLeaves
// is the order YPY settles a swap in, seen from a behaviour: the element that
// leaves is told, the element that arrives is mounted and not updated by the
// swap that brought it, and the element outside the swap is left alone.
func TestABehaviourIsMountedWhereItArrivesAndDestroyedWhereItLeaves(t *testing.T) {
	server := lifecyclePage(t)
	page := browser.Open(t, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 600}))
	page.Visit("/")
	page.ClickRole("button", "Swap")
	page.ExpectText("new")

	var hooks []any
	page.Evaluate(`window.__hooks`, &hooks)
	got := fmt.Sprint(hooks)
	want := map[string]bool{}
	for _, h := range hooks {
		want[fmt.Sprint(h)] = true
	}
	for _, expected := range []string{"mounted old", "mounted kept", "destroyed old", "mounted new"} {
		if !want[expected] {
			t.Errorf("no %q among the hooks: %s", expected, got)
		}
	}
	for _, refused := range []string{"updated new", "updated old", "updated kept", "destroyed kept", "destroyed new"} {
		if want[refused] {
			t.Errorf("%q among the hooks: %s", refused, got)
		}
	}
}
