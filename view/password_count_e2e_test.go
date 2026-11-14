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

var passwordDrivers = browser.Registry{"cdp": cdp.Launch}

// newPasswordPage serves a box drawn the way the component draws it for a
// policy of 12 to 128 characters: the minimum line, and the maximum line that
// stays out of sight until the box is over the limit.
func newPasswordPage(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="htmx-config" content='{"includeIndicatorStyles":false}'><title>P</title>
<script src="%s"></script><script src="%s"></script></head><body>
<div data-part="root" class="field password" data-kyse-behavior="password" data-kyse-props='{"min":12,"max":128}'>
<label for="password">Password</label>
<input data-part="input" id="password" name="password" type="password">
<section data-part="panel" hidden>
<div data-part="meter" aria-valuenow="0" aria-valuemax="1"><span data-part="fill"></span></div>
<p data-part="strength" data-password-summary="{met} of {total} requirements met">0 of 1 requirements met</p>
<ul data-part="requirements">
<li data-part="requirement" data-requirement="min" data-met="false"><span class="sr-only" data-password-status>Not met:</span>At least 12 characters</li>
<li data-part="requirement" data-requirement="max" data-met="false" hidden><span class="sr-only" data-password-status>Not met:</span>At most 128 characters</li>
</ul>
<button type="button" data-part="done">Done</button>
</section></div></body></html>`, view.Asset("htmx.min.js"), view.Asset("ui.js"))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const passwordState = `(function(){var m=document.querySelector('[data-requirement=max]');return document.querySelector('[data-part=strength]').textContent+'|'+(m.hidden?'max hidden':'max shown')+'|'+document.querySelector('[data-part=meter]').getAttribute('aria-valuemax');})()`

// The summary counts the lines on the screen. The maximum is out of the count
// while it is out of sight, and counts, unmet, from the moment the box is over
// the limit: the summary never says "1 of 1" over a list that shows two lines
// with one of them unmet.
func TestThePasswordSummaryCountsTheMaximumOnlyWhileItIsShown(t *testing.T) {
	srv := newPasswordPage(t)
	page := browser.Open(t, passwordDrivers, browser.BaseURL(srv.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 600}))
	page.Visit("/")

	for _, step := range []struct {
		typed int
		want  string
	}{
		{0, "0 of 1 requirements met|max hidden|1"},
		{5, "0 of 1 requirements met|max hidden|1"},
		{12, "1 of 1 requirements met|max hidden|1"},
		{128, "1 of 1 requirements met|max hidden|1"},
		{129, "1 of 2 requirements met|max shown|2"},
		{12, "1 of 1 requirements met|max hidden|1"},
		{0, "0 of 1 requirements met|max hidden|1"},
	} {
		page.Fill("Password", strings.Repeat("a", step.typed))
		if got := tableEval(t, page, passwordState); got != step.want {
			t.Errorf("with %d characters the box reads %q, want %q", step.typed, got, step.want)
		}
	}
}
