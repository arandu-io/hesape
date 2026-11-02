//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/view"
)

var menuEdgeDrivers = browser.Registry{"cdp": cdp.Launch}

// menuEdgePage serves a row's menu the way a card draws it on a narrow screen:
// the button sits at the left edge of a size container, the panel is
// `position: fixed` and is right-aligned (data-align="end"), and the panel is
// wider than the room between the button and the screen's left edge. The
// container establishes the containing block of its fixed descendants, so the
// coordinates the behaviour writes are read from the container, not from the
// window, unless the behaviour measures where the panel landed.
func menuEdgePage(t *testing.T, gap int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /m.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `body{margin:0}.card{container-type:inline-size;margin:0 0 0 24px;transform:translateX(0)}`+
			`.panel{position:fixed;width:176px;background:#fff;border:1px solid #ccc;padding:4px;box-sizing:border-box}`+
			`.gap{height:`+fmt.Sprint(gap)+`px}summary{display:inline-block;width:24px;height:24px}[hidden]{display:none!important}`)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>M</title><link rel="stylesheet" href="/m.css">
<script src="%s"></script></head><body><div class="gap"></div><div class="card">
<details id="m" data-menu><summary>Actions</summary><div data-menu-panel data-align="end" role="menu" class="panel"><a role="menuitem" href="/a">One</a><a role="menuitem" href="/b">Two</a><a role="menuitem" href="/c">Three</a></div></details>
</div></body></html>`, view.Asset("ui.js"))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func menuEdgeOpen(t *testing.T, gap int) *browser.Page {
	t.Helper()
	srv := menuEdgePage(t, gap)
	page := browser.Open(t, menuEdgeDrivers, browser.BaseURL(srv.URL), browser.WithViewport(browser.Viewport{Width: 390, Height: 640}))
	page.Visit("/")
	page.Click(browser.CSS("#m > summary", "a summary has no role the locators name"))
	time.Sleep(50 * time.Millisecond)
	return page
}

// A right-aligned menu opened near the left edge of a 390 px screen is shifted
// inside the window instead of starting at a negative x.
func TestARightAlignedMenuNearTheLeftEdgeShiftsInsideTheWindow(t *testing.T) {
	page := menuEdgeOpen(t, 40)
	got := tableEval(t, page, `(function () {
		var r = document.querySelector('#m > [data-menu-panel]').getBoundingClientRect();
		return Math.abs(r.left - 8) < 1 && r.right <= window.innerWidth - 8 + 1;
	})()`)
	if got != "true" {
		t.Fatalf("the panel is not inside the window: %s", tableEval(t, page, `JSON.stringify(document.querySelector('#m > [data-menu-panel]').getBoundingClientRect())`))
	}
}

// A menu with no room below its button turns over and opens upward, inside the
// window.
func TestAMenuWithNoRoomBelowFlipsUpward(t *testing.T) {
	page := menuEdgeOpen(t, 580)
	got := tableEval(t, page, `(function () {
		var b = document.querySelector('#m > summary').getBoundingClientRect();
		var r = document.querySelector('#m > [data-menu-panel]').getBoundingClientRect();
		return r.bottom <= b.top + 1 && r.top >= 0 && r.left >= 7 && r.right <= window.innerWidth - 7;
	})()`)
	if got != "true" {
		t.Fatalf("the panel did not turn over inside the window: %s", tableEval(t, page, `JSON.stringify(document.querySelector('#m > [data-menu-panel]').getBoundingClientRect())`))
	}
}
