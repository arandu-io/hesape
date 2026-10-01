package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrTokenMismatch means the token is invalid, expired, or bound to another
// session: the 419 a form answers with.
var ErrTokenMismatch = errors.New("session: invalid or expired CSRF token")

// ErrUnboundToken is what Issue answers for an empty binding: a token bound to
// nothing would be accepted from every visitor alike.
var ErrUnboundToken = errors.New("session: a CSRF token needs a session id or a guest binding: pass what CSRF.Binding returns")

// GuestCookieName is the cookie a visitor with no session carries, so that the
// CSRF tokens issued to them are bound to them and to nobody else.
const GuestCookieName = "arandu_csrf_guest"

// guestPrefix marks a binding as a guest's, so that it can never equal a
// session id.
const guestPrefix = "guest:"

// CSRF issues double-submit tokens signed with HMAC and bound to the session.
// It keeps no server-side state: the token carries its own expiry, so a
// deployment does not need Redis just to protect forms.
//
// It lives beside the store because what binds it is the session id, and an
// issuer that did not know the id would be a second, weaker token.
type CSRF struct {
	key      []byte
	ttl      time.Duration
	insecure bool
}

// NewCSRF returns a token issuer over appKey and ttl. [Store.RegenerateToken]
// mints a simpler random string that lives in the session; this exists for
// a token that carries its own expiry instead.
func NewCSRF(appKey []byte, ttl time.Duration) *CSRF {
	return &CSRF{key: appKey, ttl: ttl}
}

// Secure sets whether the guest cookie carries the Secure attribute. It does
// by default; pass false only in development, over plain HTTP, where a browser
// would not send a Secure cookie back. It returns c, so it chains onto NewCSRF.
func (c *CSRF) Secure(on bool) *CSRF {
	c.insecure = !on
	return c
}

// Binding is what a token for this request is bound to: the session id when
// there is one, and otherwise the visitor's guest id.
//
// The forms that need the protection most -- sign in, sign up, password reset --
// are submitted by somebody who has no session yet. A token bound to the empty
// id was one token for all of them: any visitor's token passed on any other
// visitor's form for as long as it lived. A guest is instead bound to a random
// id carried in GuestCookieName -- signed, HttpOnly, SameSite=Lax, and Secure
// unless Secure(false) was called -- so a token works only in the browser it
// was issued to.
//
// When issuing, pass the ResponseWriter: a request without a valid guest cookie
// is given one, set on w, and the new id is returned. When validating, pass a
// nil w: nothing is minted, and a request with neither a session nor a guest
// cookie returns the empty string, which Validate refuses.
func (c *CSRF) Binding(w http.ResponseWriter, r *http.Request, sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	if cookie, err := r.Cookie(GuestCookieName); err == nil {
		if id, sig, ok := strings.Cut(cookie.Value, "."); ok && id != "" && hmac.Equal([]byte(c.signGuest(id)), []byte(sig)) {
			return guestPrefix + id
		}
	}
	if w == nil {
		return ""
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	cookie := &http.Cookie{
		Name:     GuestCookieName,
		Value:    id + "." + c.signGuest(id),
		Path:     "/",
		HttpOnly: true,
		Secure:   !c.insecure,
		SameSite: http.SameSiteLaxMode,
	}
	if c.ttl > 0 {
		cookie.MaxAge = int(c.ttl.Seconds())
	}
	http.SetCookie(w, cookie)
	return guestPrefix + id
}

// Issue generates a token bound to binding: a signed token that carries its
// own expiry, so there is nothing to store.
//
// binding is a session id, or for a visitor with no session what Binding
// returns. An empty binding is refused with ErrUnboundToken rather than signed,
// because a token bound to nothing is valid for everybody.
func (c *CSRF) Issue(binding string) (string, error) {
	if binding == "" {
		return "", ErrUnboundToken
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	exp := strconv.FormatInt(time.Now().Add(c.ttl).Unix(), 10)
	payload := base64.RawURLEncoding.EncodeToString(nonce) + "." + exp
	return payload + "." + c.sign(binding, payload), nil
}

// Validate checks signature, expiry and the binding to the session, or to the
// guest Binding names. There is no session copy of the token to compare
// against, so the three facts are checked against the signature instead. An
// empty binding validates nothing.
func (c *CSRF) Validate(binding, token string) error {
	if binding == "" {
		return ErrTokenMismatch
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrTokenMismatch
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(c.sign(binding, payload)), []byte(parts[2])) {
		return ErrTokenMismatch
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ErrTokenMismatch
	}
	return nil
}

func (c *CSRF) sign(binding, payload string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(binding))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// signGuest signs a guest id under a purpose of its own, so that no token
// signature can stand in for a cookie's and no cookie's for a token's.
func (c *CSRF) signGuest(id string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("csrf guest cookie"))
	m.Write([]byte{0})
	m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
