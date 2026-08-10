package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrTokenMismatch means the token is invalid, expired, or bound to another
// session. It is Laravel's TokenMismatchException, and the 419 a form answers
// with.
var ErrTokenMismatch = errors.New("session: invalid or expired CSRF token")

// CSRF issues double-submit tokens signed with HMAC and bound to the session.
// It keeps no server-side state: the token carries its own expiry, so a
// deployment does not need Redis just to protect forms.
//
// It lives beside the store because the token is Session\Store::token() in
// Laravel: what binds it is the session id, and an issuer that did not know the
// id would be a second, weaker token.
type CSRF struct {
	key []byte
	ttl time.Duration
}

// NewCSRF returns a token issuer keyed by the application key.
func NewCSRF(appKey []byte, ttl time.Duration) *CSRF {
	return &CSRF{key: appKey, ttl: ttl}
}

// Issue generates a token for a session.
//
// An empty session id is accepted, and is not an oversight: the forms that need
// the protection most -- sign in, sign up, password reset -- are submitted by
// somebody who has no session yet.
func (c *CSRF) Issue(sessionID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	exp := strconv.FormatInt(time.Now().Add(c.ttl).Unix(), 10)
	payload := base64.RawURLEncoding.EncodeToString(nonce) + "." + exp
	return payload + "." + c.sign(sessionID, payload), nil
}

// Validate checks signature, expiry and the binding to the session.
func (c *CSRF) Validate(sessionID, token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrTokenMismatch
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(c.sign(sessionID, payload)), []byte(parts[2])) {
		return ErrTokenMismatch
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ErrTokenMismatch
	}
	return nil
}

func (c *CSRF) sign(sessionID, payload string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(sessionID))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
