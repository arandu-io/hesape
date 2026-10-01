package view_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/view"
)

func TestPageCarriesTheErrorsOfTheRequestThatRedirectedHere(t *testing.T) {
	state := hhttp.State{
		Errors: validation.Errors{"password": {"must be at least 12 characters"}},
		Old:    url.Values{"email": {"ada@example.test"}},
	}

	// The state arrives the way the session middleware puts it there, and the
	// handler below is the whole of what a controller writes.
	req := httptest.NewRequest(http.MethodGet, "/signup", nil)
	req = req.WithContext(hhttp.WithState(req.Context(), state))
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	page := view.New(ctx, "Sign up").WithToken("csrf-token")

	// Nothing in the handler mentioned errors, and the errors are on the page.
	if got := page.FieldError("password"); got != "must be at least 12 characters" {
		t.Errorf("FieldError = %q", got)
	}
	if got := page.OldValue("email"); got != "ada@example.test" {
		t.Errorf("OldValue = %q", got)
	}
	if !page.Any() {
		t.Error("Any said no on a page that was redirected here from a rejection")
	}
	// The chrome the constructor is also responsible for.
	if page.Title != "Sign up" || page.Path != "/signup" || page.CSRFToken() != "csrf-token" {
		t.Errorf("chrome = %q %q %q", page.Title, page.Path, page.CSRFToken())
	}
}

func TestAPageNobodyWasRejectedOnDrawsNothing(t *testing.T) {
	// No state on the context at all, which is every request that was not
	// redirected here from a rejection.
	ctx := hhttp.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/signup", nil), nil, nil)

	page := view.New(ctx, "Sign up")

	// A screen asks unconditionally, so all three have to answer without a nil
	// check at the call site.
	if page.Any() {
		t.Error("Any said yes on a fresh page")
	}
	if got := page.FieldError("email"); got != "" {
		t.Errorf("FieldError = %q", got)
	}
	if got := page.OldValue("email"); got != "" {
		t.Errorf("OldValue = %q", got)
	}
	if got := page.All(); got != nil {
		t.Errorf("All = %v, want nil so the banner is not drawn empty", got)
	}
}

func TestOldValueIsEmptyForAPasswordField(t *testing.T) {
	// The value never reaches the page, because it never reaches the flash. The
	// message does, and that is the distinction the whole design turns on: an
	// empty password box that does not say why it was rejected is the failure
	// being fixed.
	page := view.Page{
		Errors: validation.Errors{"password": {"must be at least 12 characters"}},
		Old:    url.Values{"email": {"ada@example.test"}},
	}

	if got := page.OldValue("password"); got != "" {
		t.Errorf("OldValue(password) = %q", got)
	}
	if got := page.FieldError("password"); got == "" {
		t.Error("the password box would come back empty and silent")
	}
}

func TestOldOrKeepsTheRejectedEditRatherThanTheStoredValue(t *testing.T) {
	page := view.Page{Old: url.Values{"title": {"A better title"}, "subtitle": {""}}}

	if got := page.OldOr("title", "The stored title"); got != "A better title" {
		t.Errorf("OldOr = %q: an edit form that reverts undoes what somebody is in the middle of writing", got)
	}
	// Deliberately cleared, and it must stay cleared. Falling back here would
	// refill a box somebody emptied on purpose.
	if got := page.OldOr("subtitle", "The stored subtitle"); got != "" {
		t.Errorf("OldOr on a cleared field = %q, want empty", got)
	}
	// Nothing was flashed for it at all.
	if got := page.OldOr("body", "The stored body"); got != "The stored body" {
		t.Errorf("OldOr fallback = %q", got)
	}
}

func TestErrorSummaryNamesTheFieldAndTheBareMessageDoesNot(t *testing.T) {
	page := view.Page{Errors: validation.Errors{
		"password_confirmation": {"does not match"},
		"email":                 {"is not a valid email address"},
	}}

	// Sorted, so the banner does not reshuffle between two renders of the same
	// failure.
	want := []string{
		"Email is not a valid email address",
		"Password confirmation does not match",
	}
	if got := page.All(); !reflect.DeepEqual(got, want) {
		t.Errorf("All =\n%q\nwant\n%q", got, want)
	}

	// The same message drawn under its own labelled box says only what to
	// change. The name is prepended for the banner and baked into nothing
	// else.
	if got := page.FieldError("email"); got != "is not a valid email address" {
		t.Errorf("FieldError = %q, want the bare sentence", got)
	}
}

func TestPageStillSatisfiesLayout(t *testing.T) {
	// The compile-time assertion in page.go is the real check; this names it so
	// that a build failure there points at a test with a reason in it. Adding
	// Any and All to Layout is what makes the banner the layout's
	// -- and every screen in every project gets them by embedding Page, which is
	// what keeps that addition from being a change each of them has to make.
	var _ view.Layout = view.Page{}

	page := view.Page{Errors: validation.Errors{"email": {"is required"}}}
	var layout view.Layout = page
	if !layout.Any() || len(layout.All()) != 1 {
		t.Error("a layout cannot see that this page was rejected")
	}
}

// TestNewTakesTheCSRFTokenFromTheRequest: the middleware that protects forms
// issues the token and stores it on the request; the controller writes
// view.New(ctx, title) and nothing else, and the form carries the token.
func TestNewTakesTheCSRFTokenFromTheRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/posts/new", nil)
	req = req.WithContext(hhttp.WithCSRFToken(req.Context(), "issued-token"))
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if got := view.New(ctx, "New post").CSRFToken(); got != "issued-token" {
		t.Fatalf("CSRFToken = %q, want the token issued for the request", got)
	}
}

// TestWithTokenReplacesTheIssuedToken: a page rendered outside the protecting
// middleware still has a way to carry one, and that way wins.
func TestWithTokenReplacesTheIssuedToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/posts/new", nil)
	req = req.WithContext(hhttp.WithCSRFToken(req.Context(), "issued-token"))
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)

	if got := view.New(ctx, "New post").WithToken("explicit").CSRFToken(); got != "explicit" {
		t.Fatalf("CSRFToken = %q, want explicit", got)
	}
}

// TestNewWithoutAnIssuedTokenCarriesNone: no middleware, no token -- and no
// panic, because a page drawn in a test or behind an API route still renders.
func TestNewWithoutAnIssuedTokenCarriesNone(t *testing.T) {
	ctx := hhttp.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), nil, nil)
	if got := view.New(ctx, "Home").CSRFToken(); got != "" {
		t.Fatalf("CSRFToken = %q, want empty", got)
	}
}

// inputPage is the interface the form inputs ask of the page they are handed:
// the message for their own name, and what was typed into it.
type inputPage interface {
	FieldError(name string) string
	OldOr(name, fallback string) string
}

// TestAPageThatEmbedsPageIsWhatAnInputAsksFor: every generated form declared
// FieldError over First by hand, once per view, because Page did not have it.
// Embedding Page is now the whole of it.
func TestAPageThatEmbedsPageIsWhatAnInputAsksFor(t *testing.T) {
	type createData struct {
		view.Page
		StoreURL string
	}
	var _ inputPage = createData{}

	state := hhttp.State{Errors: validation.Errors{"title": {"is required", "is too short"}}}
	req := httptest.NewRequest(http.MethodGet, "/posts/new", nil)
	req = req.WithContext(hhttp.WithState(req.Context(), state))
	data := createData{Page: view.New(hhttp.NewContext(httptest.NewRecorder(), req, nil, nil), "New post")}

	var page inputPage = data
	if got := page.FieldError("title"); got != "is required" {
		t.Errorf("FieldError = %q, want the first message", got)
	}
	if got := page.FieldError("body"); got != "" {
		t.Errorf("FieldError for an accepted field = %q, want empty", got)
	}
	if got := data.First("title"); got != page.FieldError("title") {
		t.Errorf("First = %q, and FieldError answers %q: the two must not disagree", got, page.FieldError("title"))
	}
}
