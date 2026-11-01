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
	"github.com/arandu-io/hesape/view"
)

// searchServer serves a data table whose server takes its time to answer, so
// the window between a request leaving and its answer arriving is as wide as a
// slow network makes it: the rows are the search's matches, the box echoes what
// was sent, and every query the server received is kept.
type searchServer struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
	delay   time.Duration
}

func (s *searchServer) region(q string) string {
	return fmt.Sprintf(`<div id="s" data-part="root" role="region" aria-label="S" data-kyse-behavior="table" data-total="1" data-url="/?q=%s" data-kyse-props='{}'>
<form id="s-query" action="/" method="get" hx-get="/" hx-trigger="submit, change" hx-target="#s" hx-swap="outerHTML" hx-select="#s" hx-push-url="true" hx-sync="closest [data-part=root]:replace">
<label for="s-search">Search S</label><input id="s-search" data-part="search" type="text" name="q" value="%s">
<div data-part="control" data-control="text"><label for="s-code">Code</label><input id="s-code" type="search" name="code" value=""></div>
</form><table><tbody><tr><td>row for %s</td></tr></tbody></table></div>`, q, q, q)
}

func newSearchServer(t *testing.T, delay time.Duration) *searchServer {
	t.Helper()
	s := &searchServer{delay: delay}
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.queries = append(s.queries, r.URL.Query().Get("q"))
		s.mu.Unlock()
		if r.Header.Get("HX-Request") == "true" {
			time.Sleep(s.delay)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
			fmt.Fprint(w, s.region(r.URL.Query().Get("q")))
			return
		}
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="htmx-config" content='{"includeIndicatorStyles":false}'><title>S</title>
<script src="%s"></script><script src="%s"></script></head><body hx-boost="true">%s</body></html>`,
			view.Asset("htmx.min.js"), view.Asset("ui.js"), s.region(r.URL.Query().Get("q")))
	})
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *searchServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

func searchOpen(t *testing.T, s *searchServer) *browser.Page {
	t.Helper()
	page := browser.Open(t, tableDrivers, browser.BaseURL(s.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	return page
}

func wait(page *browser.Page, ms int) {
	page.Evaluate(fmt.Sprintf(`new Promise(function (r) { setTimeout(r, %d); })`, ms), nil)
}

// Letters typed after a request left and before its answer arrives are kept:
// the answer replaces the region and the box comes back holding what was sent,
// and what was typed since is put back and asked for in its turn.
func TestWhatIsTypedWhileASearchIsInFlightIsKept(t *testing.T) {
	s := newSearchServer(t, 900*time.Millisecond)
	page := searchOpen(t, s)
	box := browser.Role("textbox", "Search S")

	// Keys are typed through the page itself: Page.Press waits for the network
	// to be idle first, and the point is to type while it is not.
	typeKey := func(char string) {
		page.Evaluate(`(function () {
			var box = document.getElementById('s-search');
			box.focus();
			box.setRangeText(`+fmt.Sprintf("%q", char)+`, box.value.length, box.value.length, 'end');
			box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: `+fmt.Sprintf("%q", char)+` }));
			return true;
		})()`, nil)
	}

	page.Click(box)
	typeKey("a")
	// The request for "a" leaves 300 ms after the key and is answered 900 ms
	// later, at 1200. "bc" is typed at 1000, so its own wait would end at 1300:
	// the answer comes first, and the box it replaces is the box "bc" was typed in.
	wait(page, 1000)
	typeKey("b")
	typeKey("c")
	wait(page, 2500)

	if got := tableEval(t, page, `document.getElementById('s-search').value`); got != "abc" {
		t.Fatalf("the box holds %q after the answer, want abc", got)
	}
	sent := s.sent()
	if last := sent[len(sent)-1]; last != "abc" {
		t.Fatalf("the last request asked for %q, want abc (all: %v)", last, sent)
	}
	if got := tableEval(t, page, `document.activeElement.id`); got != "s-search" {
		t.Fatalf("the box lost the focus: %q", got)
	}
	if got := tableEval(t, page, `document.getElementById('s-search').selectionStart`); got != "3" {
		t.Fatalf("the caret is at %s, want 3", got)
	}
}

// The same holds for a text control beside the search, and what the server
// echoes is not mistaken for something typed: a request answered with the text
// it sent is not asked again.
func TestAnAnswerThatEchoesWhatWasSentIsNotAskedAgain(t *testing.T) {
	s := newSearchServer(t, 100*time.Millisecond)
	page := searchOpen(t, s)
	page.Click(browser.Role("textbox", "Search S"))
	page.Evaluate(`(function () {
		var box = document.getElementById('s-search');
		box.value = 'a';
		box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: 'a' }));
		return true;
	})()`, nil)
	wait(page, 1200)
	if got := len(s.sent()); got != 2 { // the page, then "a"
		t.Fatalf("the server saw %d requests, want 2: %v", got, s.sent())
	}
}

// A letter composed in two strokes is not asked for in the middle of the
// composition. The request goes when it ends.
func TestAComposedLetterIsAskedForWhenTheCompositionEnds(t *testing.T) {
	s := newSearchServer(t, 0)
	page := searchOpen(t, s)
	page.Click(browser.Role("textbox", "Search S"))
	before := len(s.sent())

	page.Evaluate(`(function () {
		var box = document.getElementById('s-search');
		box.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
		box.value = 'p~';
		box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertCompositionText', isComposing: true }));
		return true;
	})()`, nil)
	wait(page, 800)
	if got := len(s.sent()) - before; got != 0 {
		t.Fatalf("a composition in progress made %d requests, want none", got)
	}

	page.Evaluate(`(function () {
		var box = document.getElementById('s-search');
		box.value = 'pã';
		box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertCompositionText', isComposing: true }));
		box.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: 'ã' }));
		return true;
	})()`, nil)
	wait(page, 900)
	sent := s.sent()
	if got := len(sent) - before; got != 1 || sent[len(sent)-1] != "pã" {
		t.Fatalf("the composed letter made %d requests, last %q; want one, for pã (%v)", got, sent[len(sent)-1], sent)
	}
	if got := tableEval(t, page, `document.getElementById('s-search').value`); got != "pã" {
		t.Fatalf("the box holds %q, want pã", got)
	}
}

// completeRows is a list the browser works whole: three rows, the second with
// a dialog drawn in its cell and a span the markup marks as not searchable, a
// page-size menu, and the controls of the toolbar.
const completeRows = `<div id="c" data-part="root" role="region" aria-label="C" data-kyse-behavior="table" data-complete="true" data-total="3" data-kyse-props='{"complete":true,"pageSize":10,"rows":"{total} rows.","rowsOne":"{total} row."}'>
<form id="c-query" data-part="query" action="/" method="get" hx-boost="false">
<label for="c-search">Search C</label><input id="c-search" data-part="search" type="text" name="q">
<div data-part="control" data-control="select"><label for="c-kind">Kind</label><select id="c-kind" name="kind"><option value="">All</option><option value="open">Open</option><option value="paid">Paid</option></select></div>
<div data-part="control" data-control="text"><label for="c-code">Code</label><input id="c-code" type="search" name="code"></div>
<div data-part="control" data-control="period"><input type="hidden" data-part="from" name="from" value=""><input type="hidden" data-part="to" name="to" value=""></div>
<a data-part="reset" href="/">Reset</a>
</form>
<table data-part="table"><thead><tr><th>Name</th><th>Actions</th></tr></thead><tbody>
<tr data-facet-kind="open" data-facet-code="AB-1" data-facet-from="2026-10-05T10:00:00"><td>Câmera Garagem</td><td><a id="go" class="btn" data-size="icon" href="#went">go</a></td></tr>
<tr data-facet-kind="paid" data-facet-code="CD-2" data-facet-from="2026-09-20"><td>café com leite</td><td><dialog id="d"><h2>Cancelar pedido</h2><button>Excluir tudo</button></dialog><span data-search="false">secreto</span><form id="f" data-size="sm" method="get" action="#sent"><button id="send" type="submit">send</button></form></td></tr>
<tr data-facet-kind="open" data-facet-code="EF-3" data-facet-from="2026-10-20"><td>ÂNCORA</td><td></td></tr>
<tr data-empty-row hidden><td>none</td></tr>
</tbody></table>
<div data-part="count"></div>
<details data-part="sizes" data-menu><summary>10</summary><div data-menu-panel><a data-size="2" href="/?per_page=2" aria-checked="false">2</a><a data-size="10" href="/?per_page=10" aria-checked="true">10</a></div></details>
</div>`

func completeOpen(t *testing.T) *browser.Page {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="htmx-config" content='{"includeIndicatorStyles":false}'><title>C</title><script src="%s"></script><script src="%s"></script></head><body>%s</body></html>`,
			view.Asset("htmx.min.js"), view.Asset("ui.js"), completeRows)
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	page := browser.Open(t, tableDrivers, browser.BaseURL(srv.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	return page
}

func visibleRows(t *testing.T, page *browser.Page) string {
	t.Helper()
	return tableEval(t, page, `Array.from(document.querySelectorAll('#c tbody tr:not([data-empty-row])')).filter(function (r) { return !r.hidden; }).map(function (r) { return r.cells[0].firstChild.textContent.trim(); }).join('|')`)
}

func search(t *testing.T, page *browser.Page, text string) {
	t.Helper()
	page.Evaluate(`(function () {
		var box = document.getElementById('c-search');
		box.value = `+fmt.Sprintf("%q", text)+`;
		box.dispatchEvent(new Event('input', { bubbles: true }));
		return true;
	})()`, nil)
	wait(page, 300)
}

// A complete list is searched without regard to case or to the marks on a
// letter, and what a cell holds that is not the row's own words -- a dialog,
// the text of an action, anything marked data-search="false" -- is not searched.
func TestACompleteListIsSearchedWithoutCaseOrAccentsAndNotByItsDialogs(t *testing.T) {
	page := completeOpen(t)
	for text, want := range map[string]string{
		"camera":        "Câmera Garagem",
		"CÂM":           "Câmera Garagem",
		"cafe":          "café com leite",
		"ANCORA":        "ÂNCORA",
		"âncora":        "ÂNCORA",
		"cancelar":      "", // the dialog's title
		"excluir":       "", // the dialog's button
		"secreto":       "", // data-search="false"
		"":              "Câmera Garagem|café com leite|ÂNCORA",
		"garagem camer": "",
	} {
		search(t, page, text)
		if got := visibleRows(t, page); got != want {
			t.Errorf("searching %q shows %q, want %q", text, got, want)
		}
	}
}

// An element carrying data-size that is not an entry of the page-size menu is
// its own: a link goes where it points, a form submits, and the size of the page
// does not move.
func TestAControlWithADataSizeInACellIsNotAPageSize(t *testing.T) {
	page := completeOpen(t)
	page.Click(browser.CSS("#go", "a link drawn with data-size=icon, as a Kyse icon button is"))
	wait(page, 200)
	if got := tableEval(t, page, `location.hash`); got != "#went" {
		t.Fatalf("the link did not go where it points: %q", got)
	}
	if got := tableEval(t, page, `document.querySelector('#c').getAttribute('data-total') + '|' + document.querySelector('[data-part=sizes] summary').textContent`); got != "3|10" {
		t.Fatalf("the table moved: %s", got)
	}

	page.Evaluate(`document.getElementById('d').removeAttribute('open'); true`, nil)
	page.Click(browser.CSS("#send", "a button of a form that carries data-size=sm"))
	wait(page, 400)
	if got := tableEval(t, page, `location.hash`); got != "#sent" {
		t.Fatalf("the form did not submit: %q", got)
	}

	// The menu still sets the size.
	page.Visit("/")
	page.Click(browser.CSS(`[data-part=sizes] summary`, "the page-size menu"))
	page.Click(browser.CSS(`[data-part=sizes] [data-size="2"]`, "the entry of two rows"))
	if got := tableEval(t, page, `Array.from(document.querySelectorAll('#c tbody tr:not([data-empty-row])')).filter(function (r) { return !r.hidden; }).length`); got != "2" {
		t.Fatalf("a size of two shows %s rows", got)
	}
}

// The controls of the toolbar narrow a complete list in the browser: a choice by
// equality, a text by what it finds, and a period by the day the row states.
func TestTheControlsOfTheToolbarNarrowACompleteList(t *testing.T) {
	page := completeOpen(t)
	set := func(expression string) {
		page.Evaluate(`(function () { `+expression+` return true; })()`, nil)
		wait(page, 300)
	}
	set(`var from = document.querySelector('#c [data-part=from]'); from.value = '2026-10-01'; document.querySelector('#c [data-part=to]').value = '2026-10-31'; from.dispatchEvent(new Event('change', { bubbles: true }));`)
	if got := visibleRows(t, page); got != "Câmera Garagem|ÂNCORA" {
		t.Fatalf("the period of October shows %q, want the rows of October", got)
	}
	set(`var kind = document.getElementById('c-kind'); kind.value = 'open'; kind.dispatchEvent(new Event('change', { bubbles: true }));`)
	if got := visibleRows(t, page); got != "Câmera Garagem|ÂNCORA" {
		t.Fatalf("kind=open shows %q", got)
	}
	set(`var code = document.getElementById('c-code'); code.value = 'ef'; code.dispatchEvent(new Event('input', { bubbles: true }));`)
	if got := visibleRows(t, page); got != "ÂNCORA" {
		t.Fatalf("code=ef shows %q", got)
	}
	set(`document.querySelector('[data-part=reset]').click();`)
	if got := tableEval(t, page, `document.querySelector('#c [data-part=from]').value + document.getElementById('c-kind').value + document.getElementById('c-code').value`); got != "" {
		t.Fatalf("reset left %q in the controls", got)
	}
	if got := visibleRows(t, page); got != "Câmera Garagem|café com leite|ÂNCORA" {
		t.Fatalf("reset shows %q", got)
	}
}

var _ = strings.TrimSpace

// The footer of a list the browser narrows says "1 row." for exactly one match
// and the plural for every other count, zero included.
func TestTheFooterOfACompleteListSaysOneRowInTheSingular(t *testing.T) {
	page := completeOpen(t)
	count := func() string {
		return tableEval(t, page, `document.querySelector('#c [data-part=count]').textContent`)
	}
	for text, want := range map[string]string{"camera": "1 row.", "": "3 rows.", "zzz": "0 rows.", "a": "3 rows."} {
		search(t, page, text)
		if got := count(); got != want {
			t.Errorf("searching %q says %q, want %q", text, got, want)
		}
	}
}
