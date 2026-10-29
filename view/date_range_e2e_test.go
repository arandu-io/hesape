//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/view"
)

var rangeDrivers = browser.Registry{"cdp": cdp.Launch}

// rangePage serves the markup the Kyse DateRangePicker draws -- only the
// attributes the behaviour reads and the little CSS it needs to be visible --
// so the behaviour is proved on its own: two months, three presets, a clear.
// September 2026 starts on a Tuesday and the week starts on Sunday.
func rangePage(t *testing.T, attrs string) *httptest.Server {
	t.Helper()
	var months strings.Builder
	for index, title := range []string{"setembro 2026", "outubro 2026"} {
		months.WriteString(`<section data-part="month"><h2 data-part="title">` + title + `</h2><table role="grid"><tbody>`)
		for week := 0; week < 6; week++ {
			months.WriteString(`<tr>`)
			for day := 0; day < 7; day++ {
				months.WriteString(`<td role="gridcell" data-date="2026-01-01" tabindex="-1"></td>`)
			}
			months.WriteString(`</tr>`)
		}
		months.WriteString(`</tbody></table></section>`)
		_ = index
	}
	body := fmt.Sprintf(`<div id="r" data-part="root" data-month="2026-09" data-kyse-behavior="date-range" data-kyse-props='{"months":["janeiro","fevereiro","março","abril","maio","junho","julho","agosto","setembro","outubro","novembro","dezembro"],"firstDay":0,"format":"02/01/2006","placeholder":"Escolher período","pending":"Escolha a data final"%s}'>
<form id="f"><button type="button" data-part="trigger" id="r-trigger" aria-expanded="false" aria-controls="r-panel"><span data-part="value" data-empty="true">Escolher período</span></button>
<input type="hidden" data-part="from" name="de" value=""><input type="hidden" data-part="to" name="ate" value="">
<div data-part="backdrop" hidden></div>
<div data-part="panel" id="r-panel" class="panel" role="dialog" hidden>
<button type="button" data-range-days="7">7 dias</button><button type="button" data-range-days="1">Hoje</button>
<button type="button" data-part="month-previous" data-range-step="-1">prev</button><button type="button" data-part="month-next" data-range-step="1">next</button>
%s
<p data-part="status" role="status"></p><button type="button" data-range-clear>Limpar</button>
</div></form></div>`, attrs, months.String())

	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /r.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.panel{position:fixed;background:#fff;border:1px solid #ccc}[hidden]{display:none!important}body{margin:0}.gap{height:40px}td{width:30px;height:30px}`)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>R</title><link rel="stylesheet" href="/r.css">
<script src="%s"></script></head><body><div class="gap"></div>%s</body></html>`, view.Asset("ui.js"), body)
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rangeOpen(t *testing.T, attrs string) *browser.Page {
	// The panel opens on the months the server drew when no period is held,
	// and on the current month by default; these tests draw September 2026.
	t.Helper()
	srv := rangePage(t, attrs)
	page := browser.Open(t, rangeDrivers, browser.BaseURL(srv.URL), browser.WithViewport(browser.Viewport{Width: 1280, Height: 800}))
	page.Visit("/")
	// Count the changes the form receives: a period is one request.
	page.Evaluate(`(function () { window.changes = 0; document.getElementById('f').addEventListener('change', function () { window.changes++; }); return true; })()`, nil)
	return page
}

func rangeEval(t *testing.T, page *browser.Page, expression string) string {
	t.Helper()
	var out any
	page.Evaluate(expression, &out)
	return fmt.Sprint(out)
}

func dayCell(date string) browser.Locator {
	return browser.CSS(`#r-panel [role=gridcell][data-date="`+date+`"]:not([data-outside=true])`, "a day of the grid")
}

func values(t *testing.T, page *browser.Page) string {
	return rangeEval(t, page, `document.querySelector('[name=de]').value + '|' + document.querySelector('[name=ate]').value + '|' + document.querySelector('[data-part=value]').textContent + '|' + window.changes`)
}

// The first day pressed is the start and the second is the end: nothing is
// written between them, the period is one change after the second, the panel
// closes and the field says it.
func TestAPeriodIsTwoPressesAndOneChange(t *testing.T) {
	page := rangeOpen(t, `,"min":"2026-08-01","max":"2026-12-31"`)
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.ExpectVisible(browser.CSS("#r-panel", "the panel"))
	if got := rangeEval(t, page, `document.getElementById('r-trigger').getAttribute('aria-expanded')`); got != "true" {
		t.Fatalf("aria-expanded is %s", got)
	}
	if got := rangeEval(t, page, `document.querySelectorAll('#r-panel [role=gridcell][data-date="2026-09-15"]:not([data-outside=true])').length`); got != "1" {
		t.Fatalf("the 15th of September is drawn %s times", got)
	}

	page.Click(dayCell("2026-09-15"))
	if got := values(t, page); got != "||Escolher período|0" {
		t.Fatalf("after the first press %q: nothing is written yet", got)
	}
	if got := rangeEval(t, page, `document.querySelector('[data-part=status]').textContent`); got != "Escolha a data final" {
		t.Fatalf("the status says %q", got)
	}
	page.Evaluate(`document.querySelector('#r-panel [data-date="2026-10-07"]:not([data-outside=true])').dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))`, nil)
	if got := rangeEval(t, page, `document.querySelector('#r-panel [data-date="2026-09-20"]:not([data-outside=true])').getAttribute('data-range')`); got != "middle" {
		t.Fatalf("hovering the end previews the period: a day between is %q", got)
	}
	page.Click(dayCell("2026-10-07"))
	if got := values(t, page); got != "2026-09-15|2026-10-07|15/09/2026 – 07/10/2026|1" {
		t.Fatalf("after the second press %q", got)
	}
	page.ExpectHidden(browser.CSS("#r-panel", "the panel"))
	if got := rangeEval(t, page, `document.activeElement.id`); got != "r-trigger" {
		t.Fatalf("the focus is on %q, want the field", got)
	}

	// Open again: the period is drawn, start and end marked.
	page.Click(browser.CSS("#r-trigger", "the field"))
	for date, want := range map[string]string{"2026-09-15": "start", "2026-09-30": "middle", "2026-10-07": "end"} {
		got := rangeEval(t, page, `document.querySelector('#r-panel [data-date="`+date+`"]:not([data-outside=true])').getAttribute('data-range')`)
		if got != want {
			t.Errorf("%s is %q, want %q", date, got, want)
		}
	}
}

// The end pressed before the start swaps them, and a day outside the bounds is
// not chosen.
func TestAnEarlierSecondPressSwapsAndABlockedDayIsRefused(t *testing.T) {
	page := rangeOpen(t, `,"min":"2026-09-10","max":"2026-10-20"`)
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Evaluate(`document.querySelector('#r-panel [data-date="2026-09-05"]:not([data-outside=true])').click()`, nil)
	if got := rangeEval(t, page, `document.querySelector('[data-part=status]').textContent`); got != "" {
		t.Fatalf("a day before Min started a period: %q", got)
	}
	page.Click(dayCell("2026-10-07"))
	page.Click(dayCell("2026-09-15"))
	if got := values(t, page); got != "2026-09-15|2026-10-07|15/09/2026 – 07/10/2026|1" {
		t.Fatalf("got %q", got)
	}
}

// The keyboard picks a period without the mouse: the arrows walk the days, Enter
// chooses, Escape gives the panel back without writing anything.
func TestAPeriodIsPickedFromTheKeyboard(t *testing.T) {
	page := rangeOpen(t, ``)
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Evaluate(`document.querySelector('#r-panel [data-date="2026-09-10"]:not([data-outside=true])').focus()`, nil)
	page.Press("Enter")
	for i := 0; i < 7; i++ {
		page.Press("ArrowRight")
	}
	page.Press("ArrowDown")
	page.Press("Enter")
	if got := values(t, page); got != "2026-09-10|2026-09-24|10/09/2026 – 24/09/2026|1" {
		t.Fatalf("got %q", got)
	}

	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Press("Escape")
	page.ExpectHidden(browser.CSS("#r-panel", "the panel"))
	if got := values(t, page); got != "2026-09-10|2026-09-24|10/09/2026 – 24/09/2026|1" {
		t.Fatalf("Escape wrote something: %q", got)
	}
	if got := rangeEval(t, page, `document.activeElement.id`); got != "r-trigger" {
		t.Fatalf("the focus is on %q", got)
	}
}

// A shortcut sets both days at once and Clear empties them, each one change.
func TestAShortcutAndClearAreOneChangeEach(t *testing.T) {
	page := rangeOpen(t, ``)
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Click(browser.CSS("[data-range-days='1']", "today"))
	today := rangeEval(t, page, `(function () { var n = new Date(); var p = function (x) { return (x < 10 ? '0' : '') + x; }; return n.getFullYear() + '-' + p(n.getMonth() + 1) + '-' + p(n.getDate()); })()`)
	got := rangeEval(t, page, `document.querySelector('[name=de]').value + '|' + document.querySelector('[name=ate]').value + '|' + window.changes`)
	if got != today+"|"+today+"|1" {
		t.Fatalf("Today wrote %q, want %s twice and one change", got, today)
	}
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Click(browser.CSS("[data-range-clear]", "clear"))
	if got := values(t, page); got != "||Escolher período|2" {
		t.Fatalf("Clear wrote %q", got)
	}
}

// A press outside gives the panel back and writes nothing, even half way through
// a period.
func TestAPressOutsideClosesWithoutWriting(t *testing.T) {
	page := rangeOpen(t, ``)
	page.Click(browser.CSS("#r-trigger", "the field"))
	page.Click(dayCell("2026-09-15"))
	page.Click(browser.CSS("body", "the page"))
	page.ExpectHidden(browser.CSS("#r-panel", "the panel"))
	if got := values(t, page); got != "||Escolher período|0" {
		t.Fatalf("got %q", got)
	}
}

// The panel is placed from the field and kept inside the window.
func TestThePanelStaysInsideTheWindow(t *testing.T) {
	page := rangeOpen(t, ``)
	page.Click(browser.CSS("#r-trigger", "the field"))
	got := rangeEval(t, page, `(function () { var r = document.getElementById('r-panel').getBoundingClientRect(); return r.left >= 0 && r.right <= document.documentElement.clientWidth && r.top >= 0 && r.bottom <= innerHeight; })()`)
	if got != "true" {
		t.Fatal("the panel is outside the window")
	}
}
