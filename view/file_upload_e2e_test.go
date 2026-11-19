//go:build e2e

package view_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/internal/testkit/browser"
	"github.com/arandu-io/hesape/view"
)

// The markup is what kyse's FileUpload draws, written out by hand because the
// view runtime does not import the component library: the drop area around a
// real input, a status line, a list, and the template of one row.
const uploadField = `<div data-part="root" class="field" data-kyse-behavior="file-upload" data-kyse-props='%s'>
<label data-part="label" for="photos">Photos</label>
<label data-part="dropzone" for="photos" data-dropzone>
<input data-part="input" id="photos" name="photos[]" type="file" multiple%s>
<span data-part="prompt">Drop files here</span>
</label>
<p data-part="summary" role="status" aria-live="polite" aria-atomic="true"></p>
<ul data-part="list" id="photos-files"></ul>
<template data-file-template>
<li data-part="file" class="group"><img data-part="thumbnail" alt="" width="48" height="48" hidden><span data-part="file-name"></span><span data-part="file-size"></span></li>
</template>
</div>`

// uploadScript is the half of the test that a person does with their hands:
// picking files in the system dialog, which a page cannot do, so the input is
// handed a DataTransfer and told that it changed, which is what the browser
// does after the dialog closes. The images are real PNGs drawn on a canvas,
// because a thumbnail is made by decoding one.
const uploadScript = `
window.__pick = async function (specs) {
  var dt = new DataTransfer();
  for (var i = 0; i < specs.length; i++) {
    var s = specs[i], blob;
    if (s.image) {
      var c = document.createElement('canvas');
      c.width = s.w || 64; c.height = s.h || 40;
      var g = c.getContext('2d'); g.fillStyle = '#c33'; g.fillRect(0, 0, c.width, c.height);
      blob = await new Promise(function (done) { c.toBlob(done, 'image/png'); });
    } else {
      blob = new Blob([new Uint8Array(s.size || 10)], { type: s.type || 'text/plain' });
    }
    dt.items.add(new File([blob], s.name, { type: s.image ? 'image/png' : (s.type || 'text/plain') }));
  }
  var input = document.querySelector('input[type=file]');
  input.files = dt.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
};
window.__settled = async function (pictures) {
  for (var n = 0; n < 100; n++) {
    var shown = document.querySelectorAll('[data-part="list"] img:not([hidden])').length;
    if (shown >= pictures) return true;
    await new Promise(function (done) { setTimeout(done, 50); });
  }
  return false;
};
window.__seen = function () {
  var root = document.querySelector('[data-kyse-behavior="file-upload"]');
  var summary = root.querySelector('[data-part="summary"]');
  var input = root.querySelector('input[type=file]');
  return JSON.stringify({
    summary: summary.textContent,
    over: summary.getAttribute('data-over'),
    live: summary.getAttribute('aria-live') + '/' + summary.getAttribute('role'),
    invalid: input.validationMessage,
    ariaInvalid: input.getAttribute('aria-invalid'),
    items: Array.prototype.map.call(root.querySelectorAll('[data-part="list"] > li'), function (li) {
      var img = li.querySelector('img');
      return {
        name: li.querySelector('[data-part="file-name"]').textContent,
        size: li.querySelector('[data-part="file-size"]').textContent,
        over: li.getAttribute('data-over'),
        picture: img.hidden ? '' : img.getAttribute('src').slice(0, 22),
      };
    }),
  });
};`

type uploadSeen struct {
	Summary     string `json:"summary"`
	Over        string `json:"over"`
	Live        string `json:"live"`
	Invalid     string `json:"invalid"`
	AriaInvalid string `json:"ariaInvalid"`
	Items       []struct {
		Name    string `json:"name"`
		Size    string `json:"size"`
		Over    string `json:"over"`
		Picture string `json:"picture"`
	} `json:"items"`
}

// uploadSite serves the field on a page, the same field on a second page that
// is reached by a boosted link, and as a fragment a swap brings in. The policy
// is the one applications send: no inline script, no inline style, and images
// only from the site and from data: URLs.
func uploadSite(t *testing.T, lang, props, attrs string) *httptest.Server {
	t.Helper()
	field := fmt.Sprintf(uploadField, props, attrs)
	head := func(title string) string {
		return fmt.Sprintf(`<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="ypy-config" content='{"includeIndicatorCSS":false}'><title>%s</title>
<script src="%s"></script><script src="%s"></script><script src="/pick.js"></script></head>`, lang, title, view.Asset("ypy.min.js"), view.Asset("ui.js"))
	}
	mux := http.NewServeMux()
	mux.HandleFunc(view.AssetPath, view.Handler)
	mux.HandleFunc("GET /pick.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, uploadScript)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, head("Form")+`<body ypy-boost:inherited="true"><form>`+field+`<button type="reset">Reset</button><button type="submit">Send</button></form></body></html>`)
	})
	mux.HandleFunc("GET /start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, head("Start")+`<body ypy-boost:inherited="true"><a href="/">Open the form</a></body></html>`)
	})
	mux.HandleFunc("GET /swap", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, head("Swap")+`<body><button type="button" ypy-get="/fragment" ypy-target="#slot" ypy-swap="innerHTML">Add the field</button><form id="slot"></form></body></html>`)
	})
	mux.HandleFunc("GET /fragment", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, field)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func uploadOpen(t *testing.T, server *httptest.Server, path string) *browser.Page {
	t.Helper()
	page := browser.Open(t, browser.BaseURL(server.URL), browser.WithViewport(browser.Viewport{Width: 900, Height: 700}))
	page.Visit(path)
	return page
}

func pick(t *testing.T, page *browser.Page, specs string, pictures int) uploadSeen {
	t.Helper()
	page.Evaluate(fmt.Sprintf(`window.__pick(%s).then(function () { return window.__settled(%d); })`, specs, pictures), nil)
	var raw string
	page.Evaluate(`window.__seen()`, &raw)
	var seen uploadSeen
	if err := json.Unmarshal([]byte(raw), &seen); err != nil {
		t.Fatalf("the field does not read: %v\n%s", err, raw)
	}
	return seen
}

const (
	twoPhotosAndANote = `[{"name":"front.png","image":true},{"name":"kitchen.png","image":true,"w":120,"h":80},{"name":"lease.txt","size":2048}]`
)

// A pick fills the list: a row per file with its name and its size, a picture
// for each image, and a count in a polite status line. The pictures are data:
// URLs, which is the only kind the policy lets a page show without a stored
// address, so the test fails by itself on a blob: one -- the browser reports
// the violation and the page script turns it into a failure.
func TestAPickedFileIsListedWithItsNameItsSizeAndItsPicture(t *testing.T) {
	page := uploadOpen(t, uploadSite(t, "en", `{}`, ""), "/")
	seen := pick(t, page, twoPhotosAndANote, 2)

	if seen.Summary != "3 files chosen" || seen.Over != "" {
		t.Errorf("summary = %q over %q, want the count and no warning", seen.Summary, seen.Over)
	}
	if seen.Live != "polite/status" {
		t.Errorf("the count is announced as %q, want a polite status", seen.Live)
	}
	if len(seen.Items) != 3 {
		t.Fatalf("%d rows, want 3: %+v", len(seen.Items), seen.Items)
	}
	for i, want := range []struct{ name, picture string }{
		{"front.png", "data:image/jpeg;base64"},
		{"kitchen.png", "data:image/jpeg;base64"},
		{"lease.txt", ""},
	} {
		got := seen.Items[i]
		if got.Name != want.name || got.Picture != want.picture {
			t.Errorf("row %d = %+v, want %s with picture %q", i, got, want.name, want.picture)
		}
		if got.Size == "" || got.Over != "" {
			t.Errorf("row %d size %q over %q", i, got.Size, got.Over)
		}
	}
	if seen.Items[2].Size != "2 kB" {
		t.Errorf("a 2048-byte file reads %q, want 2 kB", seen.Items[2].Size)
	}
	if seen.Invalid != "" || seen.AriaInvalid != "" {
		t.Errorf("a field with no limits is invalid: %q %q", seen.Invalid, seen.AriaInvalid)
	}
}

// One file is "1 file chosen", picking again replaces the list and never adds
// to it, and picking nothing empties it.
func TestPickingAgainReplacesTheListAndNothingEmptiesIt(t *testing.T) {
	page := uploadOpen(t, uploadSite(t, "en", `{}`, ""), "/")
	pick(t, page, twoPhotosAndANote, 2)

	seen := pick(t, page, `[{"name":"only.txt","size":10}]`, 0)
	if seen.Summary != "1 file chosen" || len(seen.Items) != 1 || seen.Items[0].Name != "only.txt" {
		t.Errorf("after a second pick: %q, %+v", seen.Summary, seen.Items)
	}
	seen = pick(t, page, `[]`, 0)
	if seen.Summary != "" || len(seen.Items) != 0 {
		t.Errorf("after an empty pick: %q, %+v", seen.Summary, seen.Items)
	}
}

// More files than one submit takes and a file over the size are both said
// before the submit, in the status line, on the row that is over and in the
// browser's own validity, which is what holds the form back. The files stay in
// the input: choosing is never refused, only sending.
func TestALimitIsWarnedBeforeTheSubmit(t *testing.T) {
	page := uploadOpen(t, uploadSite(t, "en", `{"maxFiles":2,"maxBytes":1048576}`, ""), "/")
	seen := pick(t, page, `[{"name":"a.png","image":true},{"name":"b.txt","size":10},{"name":"huge.txt","size":1572864}]`, 1)

	for _, want := range []string{"3 files chosen", "Send up to 2 files at a time", "One file is over 1 MB"} {
		if !strings.Contains(seen.Summary, want) {
			t.Errorf("the summary %q does not say %q", seen.Summary, want)
		}
	}
	if seen.Over != "true" {
		t.Errorf("the summary is not marked over: %q", seen.Over)
	}
	if seen.Invalid == "" || seen.AriaInvalid != "true" {
		t.Errorf("the input is not held: message %q, aria-invalid %q", seen.Invalid, seen.AriaInvalid)
	}
	overs := ""
	for _, item := range seen.Items {
		if item.Over == "true" {
			overs += item.Name + " "
		}
	}
	if overs != "huge.txt " {
		t.Errorf("rows marked over: %q, want only huge.txt", overs)
	}
	if seen.Items[2].Size != "1.5 MB" {
		t.Errorf("a 1.5 MiB file reads %q", seen.Items[2].Size)
	}
	var held string
	page.Evaluate(`String(document.querySelector('form').checkValidity())`, &held)
	if held != "false" {
		t.Errorf("the form is valid with a file over the limit")
	}

	seen = pick(t, page, `[{"name":"fine.txt","size":10}]`, 0)
	if seen.Over != "" || seen.Invalid != "" || seen.AriaInvalid != "" || strings.Contains(seen.Summary, "Send up to") {
		t.Errorf("a fixed pick is still warned: %+v", seen)
	}
	page.Evaluate(`String(document.querySelector('form').checkValidity())`, &held)
	if held != "true" {
		t.Errorf("the form is still held after the pick is fixed")
	}
}

// Every sentence is the server's, and the size is written in the page's own
// language: a Brazilian page reads 1,5 MB and the sentences it was given.
func TestTheSentencesAndTheNumbersAreThePagesOwn(t *testing.T) {
	props := `{"maxFiles":1,"maxBytes":1048576,"chosenOne":"1 arquivo escolhido","chosenMany":"{n} arquivos escolhidos","tooMany":"Envie até {max} por vez.","tooBigOne":"Um passa de {size}.","tooBigMany":"{n} passam de {size}."}`
	page := uploadOpen(t, uploadSite(t, "pt-BR", props, ""), "/")
	seen := pick(t, page, `[{"name":"a.txt","size":10},{"name":"huge.txt","size":1572864},{"name":"huger.txt","size":2097152}]`, 0)

	want := "3 arquivos escolhidos. Envie até 1 por vez. 2 passam de 1 MB."
	if seen.Summary != want {
		t.Errorf("summary = %q, want %q", seen.Summary, want)
	}
	if seen.Items[1].Size != "1,5 MB" {
		t.Errorf("size = %q, want 1,5 MB", seen.Items[1].Size)
	}
}

// The behaviour is mounted where the markup arrives: on a page reached by a
// boosted link, and in a fragment a swap inserted. Nothing is registered by
// the application.
func TestTheListWorksAfterABoostedNavigationAndAfterASwap(t *testing.T) {
	site := uploadSite(t, "en", `{}`, "")

	page := uploadOpen(t, site, "/start")
	page.ClickRole("link", "Open the form")
	page.ExpectURL("/")
	seen := pick(t, page, twoPhotosAndANote, 2)
	if seen.Summary != "3 files chosen" || len(seen.Items) != 3 {
		t.Errorf("after a boosted navigation: %q, %d rows", seen.Summary, len(seen.Items))
	}

	page = uploadOpen(t, site, "/swap")
	page.ClickRole("button", "Add the field")
	page.ExpectText("Drop files here")
	seen = pick(t, page, twoPhotosAndANote, 2)
	if seen.Summary != "3 files chosen" || len(seen.Items) != 3 {
		t.Errorf("after a swap: %q, %d rows", seen.Summary, len(seen.Items))
	}
}

// A reset of the form empties the input, and the list goes with it.
func TestResettingTheFormEmptiesTheList(t *testing.T) {
	page := uploadOpen(t, uploadSite(t, "en", `{"maxFiles":1}`, ""), "/")
	pick(t, page, `[{"name":"a.txt","size":10},{"name":"b.txt","size":10}]`, 0)
	page.ClickRole("button", "Reset")

	var raw string
	page.Evaluate(`new Promise(function (done) { setTimeout(function () { done(window.__seen()); }, 100); })`, &raw)
	var seen uploadSeen
	if err := json.Unmarshal([]byte(raw), &seen); err != nil {
		t.Fatal(err)
	}
	if seen.Summary != "" || len(seen.Items) != 0 || seen.Invalid != "" {
		t.Errorf("after a reset: %+v", seen)
	}
}

// A field that posts each file as it is chosen gets its list from the server,
// so the behaviour draws nothing there.
func TestAFieldThatPostsOnChangeIsLeftToTheServer(t *testing.T) {
	page := uploadOpen(t, uploadSite(t, "en", `{}`, ` ypy-post="/upload" ypy-trigger="change" ypy-target="#photos-files" ypy-swap="beforeend"`), "/")
	page.Evaluate(`window.__pick([{"name":"a.txt","size":10}]).then(function () { return true; })`, nil)

	var raw string
	page.Evaluate(`window.__seen()`, &raw)
	var seen uploadSeen
	if err := json.Unmarshal([]byte(raw), &seen); err != nil {
		t.Fatal(err)
	}
	if seen.Summary != "" {
		t.Errorf("the behaviour wrote %q over a field the server answers for", seen.Summary)
	}
}
