package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
)

func withFlash(t *testing.T) (*session.Flash, *httptest.ResponseRecorder, *hhttp.Context) {
	t.Helper()
	f := session.NewFlash([]byte(strings.Repeat("k", 32)), false)
	req := httptest.NewRequest(stdhttp.MethodPost, "/profile", nil)
	req = req.WithContext(hhttp.WithFlash(req.Context(), f))
	rec := httptest.NewRecorder()
	return f, rec, hhttp.NewContext(rec, req, nil, nil)
}

func flashCookie(rec *httptest.ResponseRecorder) *stdhttp.Cookie {
	for _, c := range (&stdhttp.Response{Header: rec.Header()}).Cookies() {
		if c.Name == session.FlashCookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestWithCarriesTheStatusAcrossTheRedirect(t *testing.T) {
	f, rec, ctx := withFlash(t)

	if err := ctx.With(hhttp.StatusKey, "Perfil salvo.").Redirect("/profile"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != stdhttp.StatusSeeOther {
		t.Fatalf("answered %d, want %d", rec.Code, stdhttp.StatusSeeOther)
	}
	cookie := flashCookie(rec)
	if cookie == nil {
		t.Fatal("the redirect carries no flash")
	}

	page := httptest.NewRequest(stdhttp.MethodGet, "/profile", nil)
	page.Header.Set("Accept", "text/html")
	page.AddCookie(cookie)
	got, ok := f.Read(httptest.NewRecorder(), page)
	if !ok || got.Messages[hhttp.StatusKey] != "Perfil salvo." {
		t.Errorf("the page read %v, %v", got.Messages, ok)
	}
}

func TestASecondWithAddsToTheFirst(t *testing.T) {
	f, rec, ctx := withFlash(t)
	ctx.With("status", "Saved.").With("notice", "Check your e-mail.")

	page := httptest.NewRequest(stdhttp.MethodGet, "/profile", nil)
	page.Header.Set("Accept", "text/html")
	page.AddCookie(flashCookie(rec))
	got, _ := f.Read(httptest.NewRecorder(), page)
	if got.Messages["status"] != "Saved." || got.Messages["notice"] != "Check your e-mail." {
		t.Errorf("Messages = %v", got.Messages)
	}
}

func TestWithWithoutAFlashOnTheRequestWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := hhttp.NewContext(rec, httptest.NewRequest(stdhttp.MethodPost, "/profile", nil), nil, nil)
	ctx.With(hhttp.StatusKey, "Saved.")
	if len(rec.Header().Values("Set-Cookie")) != 0 {
		t.Error("a request with no flash set a cookie")
	}
}
