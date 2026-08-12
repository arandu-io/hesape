package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

func flash() *session.Flash {
	return session.NewFlash([]byte(strings.Repeat("k", 32)), false)
}

func failed() validation.Errors {
	errs := validation.Errors{}
	errs.Add("title", "is required")
	return errs
}

// submitted is the request a form makes: a body, and the address of the page it
// was on in the Referer.
func submitted(target, referer string) *stdhttp.Request {
	r := httptest.NewRequest(stdhttp.MethodPost, target, strings.NewReader("title=&body=a+draft"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	return r
}

func TestARejectedFormGoesBackToItselfWithTheMessagesAndWhatWasTyped(t *testing.T) {
	rec := httptest.NewRecorder()
	hhttp.Reject(rec, submitted("http://example.test/posts", "http://example.test/posts/new"), flash(), failed())

	if rec.Code != stdhttp.StatusSeeOther {
		t.Fatalf("answered %d, want %d", rec.Code, stdhttp.StatusSeeOther)
	}
	if to := rec.Header().Get("Location"); to != "/posts/new" {
		t.Errorf("sent to %q, want the form it came from", to)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("no flash was left behind, so the form comes back with nothing on it")
	}
	// One person's messages and one person's typed input must not be kept by a
	// cache shared between people.
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestARejectedFormUnderHTMXGetsHXRedirect(t *testing.T) {
	req := submitted("http://example.test/posts", "http://example.test/posts/new")
	req.Header.Set("HX-Request", "true")

	rec := httptest.NewRecorder()
	hhttp.Reject(rec, req, flash(), failed())

	// htmx does not swap a 4xx, so a body would be fetched and thrown away --
	// which is the failure this whole path exists to remove.
	if to := rec.Header().Get("HX-Redirect"); to != "/posts/new" {
		t.Errorf("HX-Redirect = %q, want %q", to, "/posts/new")
	}
	if rec.Code != stdhttp.StatusNoContent {
		t.Errorf("answered %d, want %d", rec.Code, stdhttp.StatusNoContent)
	}
}

func TestRejectRefusesAForeignRefererAndFallsBackToRoot(t *testing.T) {
	// The address ends up in a Location header and comes off a header the
	// sender chose. Every one of these is somebody else's site.
	for _, referer := range []string{
		"https://evil.example/login",
		"http://evil.example/posts/new",
		"//evil.example/x",
		"javascript:alert(1)",
		"",
	} {
		t.Run(referer, func(t *testing.T) {
			rec := httptest.NewRecorder()
			hhttp.Reject(rec, submitted("http://example.test/posts", referer), flash(), failed())

			if to := rec.Header().Get("Location"); to != "/" {
				t.Errorf("Referer %q sent the browser to %q", referer, to)
			}
		})
	}
}

func TestWhatWasTypedComesBackAndAPasswordNeverDoes(t *testing.T) {
	// The flash is what carries it, and Reject is what fills the flash from the
	// body -- including when the handler rejected before reading the body.
	req := httptest.NewRequest(stdhttp.MethodPost, "http://example.test/signup",
		strings.NewReader("email=ada%40example.test&password=hunter2"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.test/signup")

	rec := httptest.NewRecorder()
	f := flash()
	hhttp.Reject(rec, req, f, failed())

	next := httptest.NewRequest(stdhttp.MethodGet, "/signup", nil)
	// The browser navigating to the page the redirect pointed at. The Accept is
	// what tells session.Flash this is a page somebody is about to read rather
	// than a stylesheet or a fragment, and only a page spends the message.
	next.Header.Set("Accept", "text/html")
	for _, c := range rec.Result().Cookies() {
		next.AddCookie(c)
	}
	errs, old, ok := f.Take(httptest.NewRecorder(), next)
	if !ok {
		t.Fatal("the page the person was sent to found no flash, so the form is blank")
	}
	if len(errs["title"]) == 0 {
		t.Errorf("the messages did not survive the redirect: %v", errs)
	}
	if got := old.Get("email"); got != "ada@example.test" {
		t.Errorf("the address they typed came back as %q", got)
	}
	if got := old.Get("password"); got != "" {
		t.Errorf("the password went back to the browser as %q", got)
	}
}

func TestBackKeepsTheQueryOfThePageTheFormWasOn(t *testing.T) {
	// A form reached at /posts/new?from=drafts must come back to the same list,
	// or the person lands on a page that has lost where they were.
	req := httptest.NewRequest(stdhttp.MethodPost, "http://example.test/posts", nil)
	req.Header.Set("Referer", "http://example.test/posts/new?from=drafts")

	if got := hhttp.Back(req); got != "/posts/new?from=drafts" {
		t.Errorf("Back = %q, want the address with its query", got)
	}
}

func TestBackAcceptsARefererThatIsAlreadyAPath(t *testing.T) {
	req := httptest.NewRequest(stdhttp.MethodPost, "http://example.test/posts", nil)
	req.Header.Set("Referer", "/posts/new")

	if got := hhttp.Back(req); got != "/posts/new" {
		t.Errorf("Back = %q, want /posts/new", got)
	}
}
