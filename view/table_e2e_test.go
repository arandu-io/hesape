//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/view"
)

var tableDrivers = browser.Registry{"cdp": cdp.Launch}

// tablePage serves a region shaped as the component library draws a data table
// -- the attributes the behaviour reads, and nothing the stylesheet is needed
// for -- so the behaviour is proved on its own: three pages of five rows, a
// search, a menu, a selection and a bar of bulk actions.
type tablePage struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	posted   []string
}

func (p *tablePage) region(page int, q string) string {
	var rows strings.Builder
	for i := 1; i <= 5; i++ {
		key := (page-1)*5 + i
		fmt.Fprintf(&rows, `<tr><td><input type="checkbox" data-part="select" name="ids" form="t-bulk" value="%d" aria-label="Select row %d"></td><td>row %d</td></tr>`, key, key, key)
	}
	return fmt.Sprintf(`<div id="t" data-part="root" role="region" aria-label="T" data-kyse-behavior="table" data-total="15" data-url="/?page=%d" data-kyse-props='{"selectName":"ids","selected":"{n} of {total} row(s) selected.","selectedCount":"{n} selected"}'>
<form id="t-query" action="/" method="get" hx-get="/" hx-trigger="submit, change" hx-target="#t" hx-swap="outerHTML" hx-select="#t" hx-push-url="true" hx-sync="closest [data-part=root]:replace">
<label for="t-search">Search T</label><input id="t-search" data-part="search" type="text" name="q" value="%s"></form>
<details id="t-columns" data-menu><summary>Columns</summary><div data-menu-panel data-align="end" role="menu" class="panel"><a role="menuitemcheckbox" aria-checked="true" href="/?page=2&amp;noise=1" hx-get="/?page=2&amp;noise=1" hx-target="#t" hx-swap="outerHTML" hx-select="#t" hx-push-url="true">Name</a></div></details>
<form id="t-bulk" data-part="bulk" method="post" action="/bulk" hidden><button type="submit" name="do" value="archive">Archive</button></form>
<table><tbody>%s</tbody></table>
<div data-part="count" role="status" data-selected-template="{n} of {total} row(s) selected.">0 of 15 row(s) selected.</div>
<a data-step="next" href="/?page=%d&amp;noise=1" hx-get="/?page=%d&amp;noise=1" hx-target="#t" hx-swap="outerHTML" hx-select="#t" hx-push-url="true">Next</a>
<a data-step="previous" href="/?page=%d&amp;noise=1" hx-get="/?page=%d&amp;noise=1" hx-target="#t" hx-swap="outerHTML" hx-select="#t" hx-push-url="true">Previous</a>
</div>`, page, q, rows.String(), min(page+1, 3), min(page+1, 3), max(page-1, 1), max(page-1, 1))
}

func newTablePage(t *testing.T) *tablePage {
	t.Helper()
	p := &tablePage{}
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /t.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.panel{position:fixed;background:#fff;border:1px solid #ccc;padding:4px}[hidden]{display:none!important}`)
	})
	mux.HandleFunc("POST /bulk", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.mu.Lock()
		p.posted = append(p.posted, strings.Join(r.PostForm["ids"], ",")+"|"+r.PostForm.Get("do"))
		p.mu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, r.URL.RawQuery)
		p.mu.Unlock()
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
			fmt.Fprint(w, p.region(page, r.URL.Query().Get("q")))
			return
		}
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="htmx-config" content='{"includeIndicatorStyles":false}'><title>T</title><link rel="stylesheet" href="/t.css">
<script src="%s"></script><script src="%s"></script></head><body hx-boost="true">%s</body></html>`,
			view.Asset("htmx.min.js"), view.Asset("ui.js"), p.region(page, r.URL.Query().Get("q")))
	})
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *tablePage) seen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func tableOpen(t *testing.T, p *tablePage) *browser.Page {
	t.Helper()
	page := browser.Open(t, tableDrivers, browser.BaseURL(p.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	return page
}

func tableEval(t *testing.T, page *browser.Page, expression string) string {
	t.Helper()
	var out any
	page.Evaluate(expression, &out)
	return fmt.Sprint(out)
}

// The keys ticked outlive the rows they were ticked on: a page turned is a
// region swapped, and what was ticked on the page left is still ticked, still
// counted and still posted.
func TestTheSelectionOutlivesTheSwapAndPostsEveryKey(t *testing.T) {
	p := newTablePage(t)
	page := tableOpen(t, p)
	bar := browser.CSS("#t-bulk", "the bar is a form")
	page.ExpectHidden(bar)

	page.Find(browser.Role("checkbox", "Select row 2")).Check()
	page.ExpectVisible(bar)
	page.ExpectText("1 of 15 row(s) selected.")
	page.ClickRole("link", "Next")
	page.ExpectURL("/?page=2")
	page.Find(browser.Role("checkbox", "Select row 7")).Check()
	page.ExpectText("2 of 15 row(s) selected.")
	if got := tableEval(t, page, `Array.from(document.querySelectorAll('#t-bulk input[data-bulk-key]')).map(function (i) { return i.value; }).join(',')`); got != "2" {
		t.Fatalf("the keys of other pages ride in the bar as %q, want 2", got)
	}
	page.ClickRole("link", "Previous")
	page.ExpectURL("/?page=1")
	if got := tableEval(t, page, `document.querySelector('input[value="2"]').checked`); got != "true" {
		t.Fatalf("the box ticked before the swap is %s after it", got)
	}
	page.ClickRole("button", "Archive")
	page.ExpectURL("/")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.posted) != 1 || p.posted[0] != "2,7|archive" && p.posted[0] != "7,2|archive" {
		t.Fatalf("the bar posted %v, want both keys and the action", p.posted)
	}
}

// An open menu is still open when the region comes back from a choice made in
// it, and closes for a click anywhere else; the address bar
// holds the address the server says is on screen, not the one that was asked.
func TestAMenuStaysOpenAcrossTheSwapAndTheAddressIsTheServers(t *testing.T) {
	p := newTablePage(t)
	page := tableOpen(t, p)
	page.Click(browser.CSS("#t-columns > summary", "a summary has no role the locators name"))
	if got := tableEval(t, page, `document.getElementById('t-columns').open`); got != "true" {
		t.Fatal("the menu did not open")
	}
	page.ClickRole("menuitemcheckbox", "Name")
	page.ExpectURL("/?page=2")
	if got := tableEval(t, page, `document.getElementById('t-columns').open`); got != "true" {
		t.Fatal("the menu closed when the region was swapped")
	}
	if got := tableEval(t, page, `location.search`); got != "?page=2" {
		t.Fatalf("the address is %q, want the server's ?page=2 and not the asked ?page=2&noise=1", got)
	}
}

// One word typed and paused on is one request, one word typed and submitted is
// one, and the box keeps the focus through the swap.
func TestTheSearchIsOneRequestAndKeepsTheFocus(t *testing.T) {
	p := newTablePage(t)
	page := tableOpen(t, p)
	typed := func(text string) {
		page.Evaluate(`(function () {
			var box = document.getElementById('t-search');
			var text = `+fmt.Sprintf("%q", text)+`;
			for (var i = 1; i <= text.length; i++) {
				box.value = text.slice(0, i);
				box.dispatchEvent(new Event('input', { bubbles: true }));
			}
			return true;
		})()`, nil)
	}
	pause := func() { page.Evaluate(`new Promise(function (r) { setTimeout(r, 700); })`, nil) }

	page.Click(browser.Role("textbox", "Search T"))
	before := p.seen()
	typed("abc")
	page.ExpectURL("/?page=1")
	pause()
	if got := p.seen() - before; got != 1 {
		t.Fatalf("a word paused on made %d requests, want 1", got)
	}
	if got := tableEval(t, page, `document.activeElement.id`); got != "t-search" {
		t.Fatalf("the box lost the focus: %q", got)
	}

	before = p.seen()
	typed("abcd")
	page.Press("Enter")
	pause()
	if got := p.seen() - before; got != 1 {
		t.Fatalf("a word submitted made %d requests, want 1", got)
	}
}

// A menu is placed beside its button, under it, and inside the window.
func TestAMenuIsPlacedBesideItsButton(t *testing.T) {
	p := newTablePage(t)
	page := tableOpen(t, p)
	page.Click(browser.CSS("#t-columns > summary", "a summary has no role the locators name"))
	time.Sleep(50 * time.Millisecond)
	got := tableEval(t, page, `(function () {
		var b = document.querySelector('#t-columns > summary').getBoundingClientRect();
		var r = document.querySelector('#t-columns > [data-menu-panel]').getBoundingClientRect();
		return r.top >= b.bottom - 1 && r.right <= window.innerWidth && r.left >= 0 && Math.abs(r.right - b.right) < 2;
	})()`)
	if got != "true" {
		t.Fatalf("the panel is not under its button, aligned to its end, inside the window")
	}
}
