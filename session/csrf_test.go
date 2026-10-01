package session_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
)

var appKey = []byte("0123456789abcdef0123456789abcdef")

func TestCSRFIssueAndValidate(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	token, err := c.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := c.Validate("session-1", token); err != nil {
		t.Fatalf("Validate on a fresh token: %v", err)
	}
}

// TestCSRFIsBoundToTheSession is the property that makes double-submit safe: a
// token stolen from one session is useless in another.
func TestCSRFIsBoundToTheSession(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	token, err := c.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := c.Validate("session-2", token); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("error = %v, want ErrTokenMismatch", err)
	}
}

func TestCSRFRejectsAnotherKey(t *testing.T) {
	issuer := session.NewCSRF(appKey, time.Hour)
	other := session.NewCSRF([]byte("ffffffffffffffffffffffffffffffff"), time.Hour)

	token, err := issuer.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := other.Validate("session-1", token); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("error = %v, want ErrTokenMismatch", err)
	}
}

func TestCSRFRejectsExpiredToken(t *testing.T) {
	c := session.NewCSRF(appKey, -time.Second)

	token, err := c.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := c.Validate("session-1", token); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("error = %v, want ErrTokenMismatch", err)
	}
}

// TestCSRFRejectsTamperedExpiry proves the expiry is signed, not just carried:
// otherwise anyone could extend their own token.
func TestCSRFRejectsTamperedExpiry(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	token, err := c.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	tampered := parts[0] + "." + "9999999999" + "." + parts[2]

	if err := c.Validate("session-1", tampered); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("error = %v, want ErrTokenMismatch", err)
	}
}

func TestCSRFRejectsMalformedToken(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	for _, token := range []string{"", "a", "a.b", "a.b.c.d"} {
		if err := c.Validate("session-1", token); !errors.Is(err, session.ErrTokenMismatch) {
			t.Fatalf("token %q: error = %v, want ErrTokenMismatch", token, err)
		}
	}
}

// TestCSRFIssuesForASessionlessForm: sign in, sign up and password reset are
// submitted by somebody who has no session at all, and they are the forms that
// need the protection most. The token is bound to the guest cookie Binding sets.
func TestCSRFIssuesForASessionlessForm(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	issued := httptest.NewRecorder()
	token, err := c.Issue(c.Binding(issued, httptest.NewRequest(http.MethodGet, "/login", nil), ""))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	post := httptest.NewRequest(http.MethodPost, "/login", nil)
	for _, cookie := range issued.Result().Cookies() {
		post.AddCookie(cookie)
	}
	if err := c.Validate(c.Binding(nil, post, ""), token); err != nil {
		t.Fatalf("a token issued before there was a session did not validate: %v", err)
	}
	if err := c.Validate("session-1", token); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatal("a token minted with no session was accepted on one, so the binding is not being checked")
	}
}

// TestAGuestTokenWorksOnlyForTheGuestItWasIssuedTo: a guest's token was bound to
// the empty session id, so the token one visitor fetched from the sign-in page
// passed on every other visitor's form for as long as it lived -- a page
// rendered once for an attacker was a token for every victim.
func TestAGuestTokenWorksOnlyForTheGuestItWasIssuedTo(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	visit := func() (string, []*http.Cookie) {
		rec := httptest.NewRecorder()
		token, err := c.Issue(c.Binding(rec, httptest.NewRequest(http.MethodGet, "/login", nil), ""))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		return token, rec.Result().Cookies()
	}
	attackerToken, _ := visit()
	_, victimCookies := visit()

	victimPost := httptest.NewRequest(http.MethodPost, "/login", nil)
	for _, cookie := range victimCookies {
		victimPost.AddCookie(cookie)
	}
	if err := c.Validate(c.Binding(nil, victimPost, ""), attackerToken); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("another visitor's token was accepted: %v", err)
	}
	if err := c.Validate(c.Binding(nil, httptest.NewRequest(http.MethodPost, "/login", nil), ""), attackerToken); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatalf("a token was accepted from a browser that carries no guest cookie: %v", err)
	}
}

// TestAnEmptyBindingIsNeverSigned: Issue("") minted the universal token, and
// Validate("") accepted it.
func TestAnEmptyBindingIsNeverSigned(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)
	if _, err := c.Issue(""); !errors.Is(err, session.ErrUnboundToken) {
		t.Fatalf("Issue(\"\") error = %v, want ErrUnboundToken", err)
	}
}

// TestTheGuestCookieIsHardened: the cookie is the guest's credential for the
// token, so it is out of reach of script and of other sites' posts, and sent
// only over TLS outside development.
func TestTheGuestCookieIsHardened(t *testing.T) {
	rec := httptest.NewRecorder()
	session.NewCSRF(appKey, time.Hour).Binding(rec, httptest.NewRequest(http.MethodGet, "/", nil), "")
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want the guest cookie", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != session.GuestCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("guest cookie = %+v, want HttpOnly, Secure and SameSite=Lax", cookie)
	}

	dev := httptest.NewRecorder()
	session.NewCSRF(appKey, time.Hour).Secure(false).Binding(dev, httptest.NewRequest(http.MethodGet, "/", nil), "")
	if dev.Result().Cookies()[0].Secure {
		t.Fatal("Secure(false) still set the Secure attribute")
	}
}

// TestATamperedGuestCookieIsNotABinding: the cookie is signed, so a visitor
// cannot claim another visitor's guest id by writing it.
func TestATamperedGuestCookieIsNotABinding(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.AddCookie(&http.Cookie{Name: session.GuestCookieName, Value: "chosen.forged"})
	if got := c.Binding(nil, req, ""); got != "" {
		t.Fatalf("Binding = %q for a forged cookie, want empty", got)
	}
}
