//go:build e2e

package view_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	"github.com/arandu-io/hesape/view"
)

var confirmDrivers = browser.Registry{"cdp": cdp.Launch}

// nativeConfirm replaces the browser's confirm box with one that counts the
// times it was asked and says yes. A box that said no, or hung, would hide the
// defect it exists to catch: the request would not be sent either way.
const nativeConfirm = `window.__native = 0; window.confirm = function () { window.__native++; return true; };`

type confirmPage struct {
	*httptest.Server
	sent atomic.Int32
}

// newConfirmPage serves what the components draw for a question: a form with a
// destructive button, a button that names its own words, an element with no
// question at all, and a link-less menu entry.
func newConfirmPage(t *testing.T, lang string) *confirmPage {
	t.Helper()
	p := &confirmPage{}
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /native.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, nativeConfirm)
	})
	mux.HandleFunc("GET /c.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `dialog[open]{position:fixed;inset:0;margin:auto;padding:12px}`)
	})
	mux.HandleFunc("DELETE /groups/7", func(w http.ResponseWriter, r *http.Request) {
		p.sent.Add(1)
		fmt.Fprint(w, `<p>deleted</p>`)
	})
	mux.HandleFunc("POST /rename", func(w http.ResponseWriter, r *http.Request) {
		p.sent.Add(1)
		fmt.Fprint(w, `<p>renamed</p>`)
	})
	mux.HandleFunc("POST /plain", func(w http.ResponseWriter, r *http.Request) {
		p.sent.Add(1)
		fmt.Fprint(w, `<p>plain</p>`)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="htmx-config" content='{"includeIndicatorStyles":false}'><title>C</title><link rel="stylesheet" href="/c.css">
<script src="/native.js"></script><script src="%s"></script><script src="%s"></script></head><body hx-boost="true">
<form hx-delete="/groups/7" hx-confirm="Delete this group and every membership on it?" hx-target="#out">
<button type="submit" data-variant="destructive">Delete group</button></form>
<button type="button" hx-post="/rename" hx-target="#out" hx-confirm="Rename it now?" data-confirm-title="Rename the group" data-confirm-label="Rename" data-confirm-cancel="Keep it">Open</button>
<button type="button" hx-post="/plain" hx-target="#out" hx-confirm="Send it?">Send</button>
<button type="button" hx-post="/plain" hx-target="#out">No question</button>
<div id="out"></div></body></html>`, lang, view.Asset("htmx.min.js"), view.Asset("ui.js"))
	})
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

func confirmOpen(t *testing.T, p *confirmPage) *browser.Page {
	t.Helper()
	page := browser.Open(t, confirmDrivers, browser.BaseURL(p.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 600}))
	page.Visit("/")
	return page
}

func confirmEval(t *testing.T, page *browser.Page, expression string) string {
	return tableEval(t, page, expression)
}

const (
	askedModal  = `(function(){var d=document.querySelector('dialog[data-kyse-confirm]');return !!d && d.open && d.matches(':modal');})()`
	askedNative = `String(window.__native)`
)

// A destructive question is a modal alert dialog with the question as its
// heading, a destructive button that goes through with it, and the way out
// under the cursor. The browser's own box is never asked.
func TestAQuestionOfADestructiveControlIsAModalAlertDialog(t *testing.T) {
	p := newConfirmPage(t, "en")
	page := confirmOpen(t, p)
	page.ClickRole("button", "Delete group")

	if got := confirmEval(t, page, askedModal); got != "true" {
		t.Fatalf("no modal dialog is open after the click: %s", confirmEval(t, page, `document.body.innerHTML`))
	}
	if got := confirmEval(t, page, askedNative); got != "0" {
		t.Errorf("the browser's own box was asked %s time(s)", got)
	}
	if got := confirmEval(t, page, `(function(){var d=document.querySelector('dialog[data-kyse-confirm]');return [d.getAttribute('role'), d.className, d.querySelector('[data-part=title]').textContent, d.querySelector('[data-part=confirm]').getAttribute('data-variant'), d.querySelector('[data-part=confirm]').textContent, d.querySelector('[data-part=cancel]').textContent, document.activeElement.getAttribute('data-part')].join('|');})()`); got != "alertdialog|alert-dialog|Delete this group and every membership on it?|destructive|Delete group|Cancel|cancel" {
		t.Errorf("the dialog is not the alert the components draw: %s", got)
	}
	if p.sent.Load() != 0 {
		t.Errorf("the request was sent before anybody answered: %d", p.sent.Load())
	}

	page.Click(browser.CSS("dialog[data-kyse-confirm] [data-part=confirm]", "the button of the dialog shares its word with the control that opened it"))
	page.ExpectText("deleted")
	if p.sent.Load() != 1 {
		t.Errorf("a yes sent %d request(s), want 1", p.sent.Load())
	}
	if got := confirmEval(t, page, `String(document.querySelector('dialog[data-kyse-confirm]'))`); got != "null" {
		t.Errorf("the dialog stays on the page after the answer: %s", got)
	}
	if got := confirmEval(t, page, askedNative); got != "0" {
		t.Errorf("the browser's own box was asked %s time(s) after the yes", got)
	}
}

// The way out, and Escape, send nothing and leave no dialog behind.
func TestTheWayOutAndEscapeSendNothing(t *testing.T) {
	p := newConfirmPage(t, "en")
	page := confirmOpen(t, p)

	page.ClickRole("button", "Delete group")
	page.ClickRole("button", "Cancel")
	time.Sleep(150 * time.Millisecond)
	if got := confirmEval(t, page, `String(document.querySelector('dialog[data-kyse-confirm]'))`); got != "null" {
		t.Errorf("the dialog stays after the way out: %s", got)
	}

	page.ClickRole("button", "Delete group")
	page.Press("Escape")
	time.Sleep(150 * time.Millisecond)
	if got := confirmEval(t, page, `String(document.querySelector('dialog[data-kyse-confirm]'))`); got != "null" {
		t.Errorf("the dialog stays after Escape: %s", got)
	}
	if p.sent.Load() != 0 {
		t.Errorf("a no sent %d request(s)", p.sent.Load())
	}
	if got := confirmEval(t, page, askedNative); got != "0" {
		t.Errorf("the browser's own box was asked %s time(s)", got)
	}
}

// The element names its own words: the heading, the button that goes through
// with it and the way out. The question is then the text under the heading, and
// the dialog is the plain one, because nothing said it was destructive.
func TestAnElementNamesTheWordsOfItsOwnQuestion(t *testing.T) {
	p := newConfirmPage(t, "en")
	page := confirmOpen(t, p)
	page.ClickRole("button", "Open")

	if got := confirmEval(t, page, `(function(){var d=document.querySelector('dialog[data-kyse-confirm]');return [d.className, String(d.getAttribute('role')), d.querySelector('[data-part=title]').textContent, d.querySelector('[data-part=message]').textContent, d.querySelector('[data-part=confirm]').textContent, d.querySelector('[data-part=confirm]').hasAttribute('data-variant'), d.querySelector('[data-part=cancel]').textContent].join('|');})()`); got != "dialog|null|Rename the group|Rename it now?|Rename|false|Keep it" {
		t.Errorf("the dialog does not carry the words the element named: %s", got)
	}
	page.ClickRole("button", "Rename")
	page.ExpectText("renamed")
}

// A button that says nothing but its question asks with its own word when it is
// short, and the words nobody wrote come in the page's language.
func TestTheWordsNobodyWroteComeInThePagesLanguage(t *testing.T) {
	p := newConfirmPage(t, "pt-BR")
	page := confirmOpen(t, p)
	page.ClickRole("button", "Send")
	if got := confirmEval(t, page, `(function(){var d=document.querySelector('dialog[data-kyse-confirm]');return [d.querySelector('[data-part=confirm]').textContent, d.querySelector('[data-part=cancel]').textContent].join('|');})()`); got != "Send|Cancelar" {
		t.Errorf("the buttons are not the control's word and the page's language: %s", got)
	}
}

// An element with no question is sent at once, with no dialog.
func TestAnElementWithNoQuestionIsSentAtOnce(t *testing.T) {
	p := newConfirmPage(t, "en")
	page := confirmOpen(t, p)
	page.ClickRole("button", "No question")
	page.ExpectText("plain")
	if got := confirmEval(t, page, `String(document.querySelector('dialog[data-kyse-confirm]'))`); got != "null" {
		t.Errorf("a dialog asked a question nobody wrote: %s", got)
	}
}
