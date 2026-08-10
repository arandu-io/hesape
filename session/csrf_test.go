package session_test

import (
	"errors"
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
// need the protection most.
func TestCSRFIssuesForASessionlessForm(t *testing.T) {
	c := session.NewCSRF(appKey, time.Hour)

	token, err := c.Issue("")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := c.Validate("", token); err != nil {
		t.Fatalf("a token issued before there was a session did not validate: %v", err)
	}
	if err := c.Validate("session-1", token); !errors.Is(err, session.ErrTokenMismatch) {
		t.Fatal("a token minted with no session was accepted on one, so the binding is not being checked")
	}
}
