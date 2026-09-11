package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// routes is the URLGenerator hesape/routing implements, with the two answers
// this package cares about: a path, and a name nobody registered.
type routes map[string]string

func (t routes) Route(name string, params ...string) (string, error) {
	pattern, known := t[name]
	if !known {
		return "", fmt.Errorf("no route named %q", name)
	}
	for _, p := range params {
		pattern = strings.Replace(pattern, "{}", p, 1)
	}
	return pattern, nil
}

// renderer records what a Context asked to be drawn.
type renderer struct {
	status int
	name   string
	data   any
}

func (r *renderer) Render(_ context.Context, w stdhttp.ResponseWriter, status int, name string, data any) error {
	r.status, r.name, r.data = status, name, data
	w.WriteHeader(status)
	_, err := w.Write([]byte(name))
	return err
}

func request(method, target string) *stdhttp.Request {
	return httptest.NewRequest(method, target, nil)
}

func TestAViewIsDrawnWithTwoHundredAndAFragmentWithWhateverTheHandlerSaid(t *testing.T) {
	view := &renderer{}
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, request(stdhttp.MethodGet, "/invoices"), view, nil)

	if err := ctx.View("invoices/index", "rows"); err != nil {
		t.Fatalf("View: %v", err)
	}
	if view.status != stdhttp.StatusOK || view.name != "invoices/index" {
		t.Errorf("drew %q at %d", view.name, view.status)
	}

	// A form that failed its rules answers 422 with the form, so the browser and
	// the logs agree with each other. 200 would make both believe it worked.
	rec = httptest.NewRecorder()
	ctx = hhttp.NewContext(rec, request(stdhttp.MethodGet, "/invoices"), view, nil)
	if err := ctx.Fragment(stdhttp.StatusUnprocessableEntity, "invoices/form", nil); err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	if rec.Code != stdhttp.StatusUnprocessableEntity {
		t.Errorf("the fragment answered %d", rec.Code)
	}
}

func TestRenderingWithNoViewLayerWiredSaysWhichLineIsMissing(t *testing.T) {
	// The alternative is a nil dereference in a stack trace that points at the
	// framework rather than at the line nobody wrote.
	ctx := hhttp.NewContext(httptest.NewRecorder(), request(stdhttp.MethodGet, "/"), nil, nil)

	err := ctx.View("home", nil)
	if err == nil {
		t.Fatal("rendering with no renderer answered 200 with an empty body")
	}
	if !strings.Contains(err.Error(), "bootstrap/app.go") {
		t.Errorf("the failure does not name the fix: %v", err)
	}
}

func TestAURLComesFromTheRouteNameAndAnUnknownNameIsEmptyRatherThanFatal(t *testing.T) {
	table := routes{"invoices.show": "/invoices/{}"}
	ctx := hhttp.NewContext(httptest.NewRecorder(), request(stdhttp.MethodGet, "/"), nil, table)

	if got := ctx.URL("invoices.show", "42"); got != "/invoices/42" {
		t.Errorf("URL = %q, want /invoices/42", got)
	}
	// A page with a missing button is recoverable; a renderer that panics takes
	// the whole page down to report something a missing link would have said.
	if got := ctx.URL("invoices.destroy", "42"); got != "" {
		t.Errorf("an unknown route name produced %q", got)
	}
	// And with no table wired at all, which is every test that drives an action
	// directly.
	bare := hhttp.NewContext(httptest.NewRecorder(), request(stdhttp.MethodGet, "/"), nil, nil)
	if got := bare.URL("invoices.show", "42"); got != "" {
		t.Errorf("URL with no table = %q", got)
	}
}

func TestARedirectToANamedRouteFallsBackToTheFrontPageRatherThanNowhere(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, request(stdhttp.MethodPost, "/invoices"), nil, routes{})

	if err := ctx.RedirectRoute("invoices.index"); err != nil {
		t.Fatalf("RedirectRoute: %v", err)
	}
	// An empty Location is a browser that stays where it is with no explanation.
	if to := rec.Header().Get("Location"); to != "/" {
		t.Errorf("Location = %q, want /", to)
	}
}

func TestARedirectIsThreeHundredAndThreeAndUnderHTMXIsAHeader(t *testing.T) {
	// 303 after a POST is what tells the browser to GET the next address
	// instead of posting the body to it again.
	rec := httptest.NewRecorder()
	hhttp.Redirect(rec, request(stdhttp.MethodPost, "/invoices"), "/invoices/42")
	if rec.Code != stdhttp.StatusSeeOther {
		t.Errorf("answered %d, want 303", rec.Code)
	}

	// An HTMX request that gets a 302 follows it inside the fragment, so the
	// whole page ends up nested in a div.
	req := request(stdhttp.MethodPost, "/invoices")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	hhttp.Redirect(rec, req, "/invoices/42")

	if to := rec.Header().Get("HX-Redirect"); to != "/invoices/42" {
		t.Errorf("HX-Redirect = %q", to)
	}
	// A body alongside HX-Redirect is swapped in before the browser navigates.
	if rec.Code != stdhttp.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("answered %d with %d bytes, want 204 and nothing", rec.Code, rec.Body.Len())
	}
}

func TestARefusalKeepsItsStatusAndUnderHTMXAsksForAReload(t *testing.T) {
	// The status and the sentence are the caller's: logs, monitoring and every
	// non-HTMX client see exactly what they saw before.
	rec := httptest.NewRecorder()
	hhttp.Refuse(rec, request(stdhttp.MethodGet, "/admin"), stdhttp.StatusForbidden, "this account may not open that")

	if rec.Code != stdhttp.StatusForbidden {
		t.Errorf("answered %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "may not open") {
		t.Errorf("the sentence was lost: %q", rec.Body.String())
	}
	if rec.Header().Get("HX-Refresh") != "" {
		t.Error("a plain request was asked to reload, which it would do twice")
	}

	// htmx does not swap a 4xx, so without the reload the person clicks and
	// nothing at all happens.
	req := request(stdhttp.MethodGet, "/admin")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	hhttp.Refuse(rec, req, stdhttp.StatusForbidden, "this account may not open that")

	if rec.Header().Get("HX-Refresh") != "true" {
		t.Error("the refusal was invisible: no swap, and no reload asked for")
	}
	if rec.Code != stdhttp.StatusForbidden {
		t.Errorf("answered %d, want the caller's 403 unchanged", rec.Code)
	}
}

// invoice is the entity, with a field that must never leave.
type invoice struct {
	ID              int
	InternalMargin  int
	OwningAccountID int
}

// invoiceResource is the list of fields the invoice may answer with, and the
// reason Context.JSON takes one: the entity above grew two fields after the
// handler was written, and neither of them is here.
type invoiceResource struct{ invoice invoice }

func (r invoiceResource) ToArray() map[string]any {
	return map[string]any{"id": r.invoice.ID, "note": missing{}}
}

func (r invoiceResource) With() map[string]any { return map[string]any{"version": 1} }

// missing is a value that reports itself absent, which is what a conditional
// field resolves to when the condition does not hold.
type missing struct{}

func (missing) IsMissing() bool { return true }

type invalidResource struct{}

func (invalidResource) ToArray() map[string]any {
	return map[string]any{"stream": make(chan int)}
}

func (invalidResource) With() map[string]any { return nil }

type untouchedWriter struct {
	header stdhttp.Header
	status int
	body   strings.Builder
}

func (w *untouchedWriter) Header() stdhttp.Header { return w.header }

func (w *untouchedWriter) WriteHeader(status int) { w.status = status }

func (w *untouchedWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = stdhttp.StatusOK
	}
	return w.body.Write(body)
}

func TestJSONAndStatusAnswerWithoutAViewLayer(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, request(stdhttp.MethodGet, "/api/invoices/42"), nil, nil)

	resource := invoiceResource{invoice{ID: 42, InternalMargin: 31, OwningAccountID: 7}}
	if err := ctx.JSON(stdhttp.StatusCreated, resource); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if rec.Code != stdhttp.StatusCreated {
		t.Errorf("answered %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := rec.Body.String(), `{"data":{"id":42},"version":1}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	rec = httptest.NewRecorder()
	ctx = hhttp.NewContext(rec, request(stdhttp.MethodDelete, "/invoices/42"), nil, nil)
	if err := ctx.Status(stdhttp.StatusNoContent); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if rec.Code != stdhttp.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("answered %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestTOONAnswersThroughTheSameResourceAllowlistAsJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, request(stdhttp.MethodGet, "/ai/invoices/42"), nil, nil)

	resource := invoiceResource{invoice{ID: 42, InternalMargin: 31, OwningAccountID: 7}}
	if err := ctx.TOON(stdhttp.StatusCreated, resource); err != nil {
		t.Fatalf("TOON: %v", err)
	}
	if rec.Code != stdhttp.StatusCreated {
		t.Errorf("answered %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/toon; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/toon; charset=utf-8", got)
	}
	want := "data:\n  id: 42\nversion: 1"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestTOONEncodingFailureLeavesTheResponseUntouched(t *testing.T) {
	w := &untouchedWriter{header: make(stdhttp.Header)}
	ctx := hhttp.NewContext(w, request(stdhttp.MethodGet, "/ai/invoices/42"), nil, nil)

	if err := ctx.TOON(stdhttp.StatusCreated, invalidResource{}); err == nil {
		t.Fatal("TOON accepted a channel that cannot belong to the JSON data model")
	}
	if w.status != 0 || w.body.Len() != 0 || len(w.header) != 0 {
		t.Errorf("failed encoding wrote status=%d headers=%v body=%q", w.status, w.header, w.body.String())
	}
}

func TestTheRequestIsReadThroughTheNamesTheVocabularyAlreadyUses(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodPost, "/invoices/42?tab=lines",
		strings.NewReader("amount=1200"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "42")

	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if got := ctx.Param("id"); got != "42" {
		t.Errorf("Param = %q", got)
	}
	if got := ctx.Query("tab"); got != "lines" {
		t.Errorf("Query = %q", got)
	}
	if got := ctx.Input("amount"); got != "1200" {
		t.Errorf("Input = %q", got)
	}
	if ctx.Ctx() != req.Context() {
		t.Error("Ctx is not the request context, so the Collector and the request id are lost")
	}
}

// errNoRenderer is a value and not a formatted string, so it stays comparable.
func TestTheMissingRendererFailureIsOneValue(t *testing.T) {
	ctx := hhttp.NewContext(httptest.NewRecorder(), request(stdhttp.MethodGet, "/"), nil, nil)

	first := ctx.View("home", nil)
	second := ctx.Fragment(stdhttp.StatusOK, "home", nil)
	if !errors.Is(second, first) {
		t.Errorf("two calls produced two failures: %v and %v", first, second)
	}
}

// invoices is the typed struct a controller hands a view. It is declared here
// rather than reused from a fixture so that the assertion below is about the
// shape crossing the wire and not about somebody else's struct tags.
type invoices struct {
	Open  int      `json:"open"`
	Names []string `json:"names"`
}

// TestAClientThatDrawsForItselfGetsTheValuesAndNotTheDrawing is the seam that
// lets one handler serve a browser and a client with its own controls.
//
// The alternative is a second handler for the second client, and a handler
// that exists twice is a second path: the two drift, and the one nobody is
// looking at is the one that stops checking something.
func TestAClientThatDrawsForItselfGetsTheValuesAndNotTheDrawing(t *testing.T) {
	view := &renderer{}
	rec := httptest.NewRecorder()

	r := request(stdhttp.MethodGet, "/invoices")
	r.Header.Set("Accept", hhttp.ViewDataMediaType)
	ctx := hhttp.NewContext(rec, r, view, nil)

	if err := ctx.View("invoices/index", invoices{Open: 2, Names: []string{"ada", "grace"}}); err != nil {
		t.Fatalf("View: %v", err)
	}

	if view.name != "" {
		t.Errorf("the renderer was asked to draw %q; a client that draws for itself gets no markup", view.name)
	}

	var answered hhttp.ViewData
	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("decoding the answer: %v\n%s", err, rec.Body)
	}
	if answered.View != "invoices/index" {
		t.Errorf("the answer names %q, want the view the handler rendered", answered.View)
	}

	values, ok := answered.Data.(map[string]any)
	if !ok {
		t.Fatalf("the values came through as %T", answered.Data)
	}
	if values["open"] != float64(2) {
		t.Errorf("open came through as %v", values["open"])
	}
}

// TestABrowserStillGetsTheDrawing is the other half, and the one that would
// fail silently: a negotiation that matched too widely would answer every page
// in the application with values, and every browser would show them as text.
func TestABrowserStillGetsTheDrawing(t *testing.T) {
	for _, accept := range []string{
		"",
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"application/json",
		"application/vnd.arandu.view+xml",
	} {
		t.Run(accept, func(t *testing.T) {
			view := &renderer{}
			r := request(stdhttp.MethodGet, "/invoices")
			if accept != "" {
				r.Header.Set("Accept", accept)
			}
			ctx := hhttp.NewContext(httptest.NewRecorder(), r, view, nil)

			if err := ctx.View("invoices/index", invoices{}); err != nil {
				t.Fatalf("View: %v", err)
			}
			if view.name != "invoices/index" {
				t.Error("the renderer was not asked to draw")
			}
		})
	}
}

// TestTheMediaTypeIsFoundBesideOthers keeps the match honest: a client may
// send several, with parameters, in any order.
func TestTheMediaTypeIsFoundBesideOthers(t *testing.T) {
	for _, accept := range []string{
		hhttp.ViewDataMediaType,
		"text/html, " + hhttp.ViewDataMediaType,
		hhttp.ViewDataMediaType + "; charset=utf-8",
		"*/*, " + hhttp.ViewDataMediaType + ";q=1.0",
	} {
		t.Run(accept, func(t *testing.T) {
			view := &renderer{}
			r := request(stdhttp.MethodGet, "/invoices")
			r.Header.Set("Accept", accept)
			ctx := hhttp.NewContext(httptest.NewRecorder(), r, view, nil)

			if err := ctx.View("invoices/index", invoices{}); err != nil {
				t.Fatalf("View: %v", err)
			}
			if view.name != "" {
				t.Error("the renderer drew markup for a client that asked for the values")
			}
		})
	}
}

// TestTheAnswerSaysItVariesByAccept is one header and it is the one that makes
// this safe behind a proxy.
//
// The same address now has two representations chosen by a request header. A
// cache that did not know would serve whichever it stored first: markup to a
// client that cannot draw it, or values to a browser that shows them as text.
func TestTheAnswerSaysItVariesByAccept(t *testing.T) {
	rec := httptest.NewRecorder()
	r := request(stdhttp.MethodGet, "/invoices")
	r.Header.Set("Accept", hhttp.ViewDataMediaType)
	ctx := hhttp.NewContext(rec, r, &renderer{}, nil)

	if err := ctx.View("invoices/index", invoices{}); err != nil {
		t.Fatalf("View: %v", err)
	}

	if got := rec.Header().Get("Vary"); got != "Accept" {
		t.Errorf("Vary is %q, want Accept", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, hhttp.ViewDataMediaType) {
		t.Errorf("Content-Type is %q", got)
	}
}

// TestAFragmentCarriesItsStatusIntoTheValues keeps the status meaning the same
// thing in both representations. A rejected form answers 422 with the form, and
// a client that drew that form has to see the same 422 -- otherwise the two
// clients disagree about whether the write happened.
func TestAFragmentCarriesItsStatusIntoTheValues(t *testing.T) {
	rec := httptest.NewRecorder()
	r := request(stdhttp.MethodPost, "/invoices")
	r.Header.Set("Accept", hhttp.ViewDataMediaType)
	ctx := hhttp.NewContext(rec, r, &renderer{}, nil)

	if err := ctx.Fragment(stdhttp.StatusUnprocessableEntity, "invoices/form", invoices{}); err != nil {
		t.Fatalf("Fragment: %v", err)
	}

	if rec.Code != stdhttp.StatusUnprocessableEntity {
		t.Errorf("answered %d, want 422", rec.Code)
	}
}

// screenWithToken is what a real page is: its own values, and the chrome that
// carries the session's token.
type screenWithToken struct {
	Title string `json:"title"`
	token string
}

func (s screenWithToken) CSRFToken() string { return s.token }

// screenWithoutToken is a page with no form on it.
type screenWithoutToken struct {
	Title string `json:"title"`
}

// viewDataOf renders a page as values and answers the envelope.
func viewDataOf(t *testing.T, data any) hhttp.ViewData {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set("Accept", hhttp.ViewDataMediaType)

	ctx := &hhttp.Context{Response: recorder, Request: request}
	if err := ctx.View("home", data); err != nil {
		t.Fatalf("the page was not rendered as values: %v", err)
	}

	var envelope hhttp.ViewData
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("what came back is not an envelope: %v -- %s", err, recorder.Body.String())
	}
	return envelope
}

// TestTheEnvelopeCarriesTheTokenThroughTheOneContract is the whole point of the
// field.
//
// A client that draws for itself has neither the hidden field a form writes nor
// the header attribute an enhanced request sends, so it has to be given the
// value and send it back. Where it reads it from has to be declared, and the
// contract it is read through here is the one the markup path already uses:
// a page provides the token by having the method, and both representations ask
// for it the same way.
func TestTheEnvelopeCarriesTheTokenThroughTheOneContract(t *testing.T) {
	envelope := viewDataOf(t, screenWithToken{Title: "Invoices", token: "the-token"})

	if envelope.Token != "the-token" {
		t.Errorf("the envelope carries the token %q", envelope.Token)
	}
	if envelope.View != "home" {
		t.Errorf("the envelope names the view %q", envelope.View)
	}
}

// TestAPageWithNoTokenSendsNone keeps the field from appearing empty on every
// page that has no form.
//
// The markup of such a page carries no hidden field and no header attribute.
// The envelope of it carries no token, which is the same answer written the
// other way.
func TestAPageWithNoTokenSendsNone(t *testing.T) {
	envelope := viewDataOf(t, screenWithoutToken{Title: "About"})

	if envelope.Token != "" {
		t.Errorf("a page with no token sent %q", envelope.Token)
	}
}

// TestTheTokenIsOmittedRatherThanSentEmpty keeps a field nobody filled out of
// every response.
func TestTheTokenIsOmittedRatherThanSentEmpty(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set("Accept", hhttp.ViewDataMediaType)

	ctx := &hhttp.Context{Response: recorder, Request: request}
	if err := ctx.View("home", screenWithoutToken{Title: "About"}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(recorder.Body.String(), "token") {
		t.Errorf("a page with no token still named the field: %s", recorder.Body.String())
	}
}

// TestTheTokenIsInTheEnvelopeAndNotOnlyInTheValues fixes what a client was
// depending on before the field existed.
//
// It read the token out of the page's own values, under whatever name the
// field happened to marshal as. Nothing declared that name, so a tag added to
// that field, a rename, or a change of embedding would have moved it with
// nothing failing anywhere.
func TestTheTokenIsInTheEnvelopeAndNotOnlyInTheValues(t *testing.T) {
	envelope := viewDataOf(t, screenWithToken{Title: "Invoices", token: "the-token"})

	if envelope.Token == "" {
		t.Fatal("the envelope carries no token, so a client has to go looking in the values")
	}

	// And the values of this page carry it nowhere: the method is how it is
	// provided, and an unexported field marshals to nothing.
	values, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(values), "the-token") {
		t.Errorf("the values carry the token as well, which is two places to read it from: %s", values)
	}
}
