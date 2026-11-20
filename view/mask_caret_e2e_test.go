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

// newMaskPage serves a CPF box drawn the way the masked component draws it,
// and a second one that arrives with a stored value and is never focused.
func newMaskPage(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="ypy-config" content='{"includeIndicatorCSS":false}'><title>M</title>
<script src="%s"></script><script src="%s"></script></head><body>
<form>
<div data-kyse-behavior="mask" data-kyse-props='{"pattern":"000.000.000-00"}'>
<label for="cpf">CPF</label>
<input id="cpf" name="cpf" type="text" inputmode="numeric" autocomplete="off">
<input type="hidden" data-part="raw" name="cpf" disabled>
</div>
<div data-kyse-behavior="mask" data-kyse-props='{"pattern":"000.000.000-00"}'>
<label for="stored">Stored</label>
<input id="stored" name="stored" type="text" inputmode="numeric" value="52998224725">
<input type="hidden" data-part="raw" name="stored" disabled>
</div>
</form></body></html>`, view.Asset("ypy.min.js"), view.Asset("ui.js"))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The selections the page set since the spy was installed, and the state of
// the CPF box: what it shows, what it submits and where its caret is.
const (
	maskSpy   = `(function(){window.__sel=0;var o=HTMLInputElement.prototype.setSelectionRange;HTMLInputElement.prototype.setSelectionRange=function(){window.__sel++;return o.apply(this,arguments);};return 'ok';})()`
	maskState = `(function(){var i=document.getElementById('cpf');var r=i.parentNode.querySelector('[data-part=raw]');return i.value+'|'+r.value+'|'+i.selectionStart+'|'+window.__sel;})()`
)

// Typing at the end of the box never places the caret by hand: writing the
// value already leaves it there, and Safari on the iPhone dropped a digit at
// the separators when the mask placed it on every keystroke anyway. A box that
// arrives with a value is formatted without being given a selection.
func TestTheMaskLeavesTheCaretAloneWhileTypingAtTheEnd(t *testing.T) {
	srv := newMaskPage(t)
	page := browser.Open(t, browser.BaseURL(srv.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 600}))
	page.Visit("/")

	if got := tableEval(t, page, `document.getElementById('stored').value+'|'+(document.activeElement&&document.activeElement.id)`); got != "529.982.247-25|" {
		t.Errorf("the stored box reads %q, want it formatted and not focused", got)
	}

	tableEval(t, page, maskSpy)
	page.Click(browser.Role("textbox", "CPF"))
	for _, key := range "52998224725" {
		page.Press(string(key))
	}
	if got, want := tableEval(t, page, maskState), "529.982.247-25|52998224725|14|0"; got != want {
		t.Errorf("after typing at the end the box reads %q, want %q", got, want)
	}

	// Correcting a digit in the middle still keeps the caret beside it: the
	// deletion and the digit typed are each placed once.
	tableEval(t, page, `(function(){var i=document.getElementById('cpf');i.setSelectionRange(3,3);window.__sel=0;return 'ok';})()`)
	page.Press("Backspace")
	page.Press("1")
	if got, want := tableEval(t, page, maskState), "521.982.247-25|52198224725|3|2"; got != want {
		t.Errorf("after correcting the third digit the box reads %q, want %q", got, want)
	}
}
