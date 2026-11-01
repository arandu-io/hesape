//go:build e2e

package view_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arandu-io/hesape/arandutest/browser"
	"github.com/arandu-io/hesape/arandutest/browser/cdp"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
)

var renewalDrivers = browser.Registry{"cdp": cdp.Launch}

// renewalTTL is how long a token lives in these pages. The browser runs on the
// real clock, so the lifetime is short and the test waits it out.
const renewalTTL = 6 * time.Second

// renewalWait is longer than the lifetime, with a second to spare for the
// scheduler: by then the token the page was drawn with has lapsed.
const renewalWait = renewalTTL + 1500*time.Millisecond

// renewalPage serves a page with a form the browser sends itself and a form
// htmx sends, both drawn with the page's token, behind the real check. before
// is the script served ahead of ui.js, to hold back what a test needs held
// back; withScript is false for a page that loads no behaviour at all.
func renewalPage(t *testing.T, withScript bool, before string) *httptest.Server {
	t.Helper()
	csrf := session.NewCSRF([]byte("0123456789abcdef0123456789abcdef"), renewalTTL).Secure(false)
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /before.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, before)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		token, _ := hhttp.CSRFTokenFrom(r.Context())
		token = html.EscapeString(token)
		scripts := ""
		if withScript {
			scripts = fmt.Sprintf(`<script src="/before.js"></script><script src="%s"></script><script src="%s"></script>`, view.Asset("htmx.min.js"), view.Asset("ui.js"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "script-src 'self'")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Open page</title>%s</head><body>
<div hx-headers='{"X-CSRF-Token": "%s"}'>
<form method="post" action="/native"><input type="hidden" name="_token" value="%s"><button type="submit">Send native</button></form>
<form hx-post="/htmx" hx-target="#out" hx-swap="innerHTML"><input type="hidden" name="_token" value="%s"><button type="submit">Send htmx</button></form>
<div id="out"></div></div></body></html>`, scripts, token, token, token)
	})
	mux.HandleFunc("POST /native", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Done</title></head><body><p>native accepted</p></body></html>`)
	})
	mux.HandleFunc("POST /htmx", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<p>htmx accepted</p>`)
	})
	server := httptest.NewServer(middleware.CSRFProtect(csrf, func(*http.Request) string { return "" })(mux))
	t.Cleanup(server.Close)
	return server
}

func openRenewal(t *testing.T, server *httptest.Server) *browser.Page {
	t.Helper()
	page := browser.Open(t, renewalDrivers, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 1024, Height: 700}))
	page.Visit("/")
	return page
}

// TestWithoutTheRenewalAPageLeftOpenIsRefused is the control: the same page
// with no behaviour loaded, left open past the life of its token, is answered
// with the status page when the person presses the button. It is what every
// screen did before the page renewed its own token.
func TestWithoutTheRenewalAPageLeftOpenIsRefused(t *testing.T) {
	page := openRenewal(t, renewalPage(t, false, ""))
	time.Sleep(renewalWait)
	page.ClickRole("button", "Send native")
	page.ExpectText("Page Expired")
}

// TestAPageLeftOpenRenewsItsTokenBeforeItLapses: the page is drawn, the token it
// was drawn with lapses, and both forms still go through -- the timer asked for
// a new token while the old one still worked and wrote it into the fields and
// into the header htmx sends.
func TestAPageLeftOpenRenewsItsTokenBeforeItLapses(t *testing.T) {
	page := openRenewal(t, renewalPage(t, true, ""))
	time.Sleep(renewalWait)
	page.ClickRole("button", "Send htmx")
	page.ExpectText("htmx accepted")
	page.Visit("/")
	time.Sleep(renewalWait)
	page.ClickRole("button", "Send native")
	page.ExpectText("native accepted")
}

// held keeps the page from renewing on its own: no timer set for a second or
// more ever fires, so the only thing that can save the click is the check a
// form makes when it is sent. It is what a laptop closed for an afternoon does
// to the timers.
const held = `(function(){var set=window.setTimeout;window.setTimeout=function(f,ms){if(ms>=1000)return 0;return set.apply(window,arguments);};})();`

// TestAFormSentWithADueTokenWaitsForANewOne: with the timers held back, the
// token lapses and nothing has renewed it. The form the browser sends, and the
// request htmx sends, each wait for the new token and are then sent as they
// were.
func TestAFormSentWithADueTokenWaitsForANewOne(t *testing.T) {
	for _, tc := range []struct{ button, want string }{
		{"Send native", "native accepted"},
		{"Send htmx", "htmx accepted"},
	} {
		t.Run(tc.button, func(t *testing.T) {
			page := openRenewal(t, renewalPage(t, true, held))
			time.Sleep(renewalWait)
			page.ClickRole("button", tc.button)
			page.ExpectText(tc.want)
		})
	}
}
