package oauthtwo

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"
)

// ErrStateMismatch answers
// Illuminate\Socialite\OAuthTwo\StateMismatchException.
//
// It is the whole security of the authorization code flow in one value. The
// provider sends the browser back to the application with a code in the query
// string, and nothing in that request proves the application is the one that
// asked for it: an attacker can send a victim a callback URL carrying a code
// issued for the attacker's own account, and an application that accepts it
// signs the victim into the attacker's account, where whatever the victim does
// next is visible to the attacker.
//
// The state is what closes it. A random string is stored before the redirect
// and compared on the way back, so a callback nobody here asked for has nothing
// to match.
var ErrStateMismatch = errors.New("socialite: the state in the callback does not match the one that was stored")

// StateStoreInterface answers
// Illuminate\Socialite\OAuthTwo\StateStoreInterface: where the state waits
// between the redirect out and the callback back.
//
// Illuminate's two methods return string and void. Both return an error here,
// because both do input and output -- a cookie is written to a response and a
// session to a store, and either can fail. A store that swallowed the failure
// would answer with an empty state, and an empty state compares equal to an
// empty query parameter, which is the check passing by accident. [Verify]
// refuses the empty state for exactly that reason.
type StateStoreInterface interface {
	// GetState answers the state that was stored, and forgets it. The single
	// use is deliberate: a state that survives its callback can be replayed.
	GetState() (string, error)
	// SetState stores the state for the callback to find.
	SetState(state string) error
}

// Verify compares the state that came back with the state that was stored, and
// is where [ErrStateMismatch] comes from.
//
// The comparison is constant-time, which is what hash_equals does in the
// current Laravel, and the empty state is refused before the comparison: a
// missing cookie and a missing query parameter are both empty, and equal is the
// wrong answer for two things that are not there.
func Verify(stored, returned string) error {
	if stored == "" || returned == "" {
		return ErrStateMismatch
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(returned)) != 1 {
		return ErrStateMismatch
	}
	return nil
}

// StateCookieName is the cookie [CookieStateStore] keeps the state in.
const StateCookieName = "oauth_state"

// stateLifetime is how long the browser has to come back. Ten minutes is long
// enough for a person to type a password and a second factor, and short enough
// that a state left in a closed tab is gone before anybody finds it.
const stateLifetime = 10 * time.Minute

// CookieStateStore is a [StateStoreInterface] that keeps the state in a
// short-lived cookie of its own.
//
// The clone has no concrete store -- it is an interface and an expectation that
// the framework supplies one -- and the current Laravel puts the state in the
// session. This package cannot: hesape's session.Store holds one typed record
// per session and not a bag of keys, so a state would either bend that record
// into a key-value store or introduce a second session, and a second way to
// keep session state is exactly what RULE 9 exists to refuse.
//
// A cookie is the remaining answer and it is a sound one. The state is not a
// secret and it does not authenticate anybody: it only has to be something the
// attacker cannot both choose and see. HttpOnly keeps it out of scripts,
// SameSite=Lax keeps it off cross-site requests, and the ten-minute lifetime
// keeps it from outliving the sign-in it belongs to.
//
// One store serves one request pair, because it holds the writer it sets the
// cookie on:
//
//	store := oauthtwo.NewCookieStateStore(w, r)
//	provider := oauthtwo.NewGithubProvider(store, id, secret)
type CookieStateStore struct {
	w http.ResponseWriter
	r *http.Request
	// Name is the cookie the state lives in. Empty means [StateCookieName].
	Name string
	// Path is the cookie path. Empty means "/".
	Path string
}

// NewCookieStateStore builds the store for one request.
func NewCookieStateStore(w http.ResponseWriter, r *http.Request) *CookieStateStore {
	return &CookieStateStore{w: w, r: r}
}

func (s *CookieStateStore) name() string {
	if s.Name != "" {
		return s.Name
	}
	return StateCookieName
}

func (s *CookieStateStore) path() string {
	if s.Path != "" {
		return s.Path
	}
	return "/"
}

// SetState answers StateStoreInterface::setState().
func (s *CookieStateStore) SetState(state string) error {
	if s.w == nil {
		return errors.New("socialite: this state store has no response to write the cookie to")
	}
	http.SetCookie(s.w, &http.Cookie{
		Name:     s.name(),
		Value:    state,
		Path:     s.path(),
		MaxAge:   int(stateLifetime.Seconds()),
		HttpOnly: true,
		// Lax and not Strict: the callback arrives as a top-level navigation
		// from the provider's domain, and Strict would withhold the cookie on
		// exactly that request.
		SameSite: http.SameSiteLaxMode,
		Secure:   s.r != nil && s.r.TLS != nil,
	})
	return nil
}

// GetState answers StateStoreInterface::getState(), and clears the cookie as it
// reads it.
//
// Clearing is the part worth stating: a state that stays in the browser can be
// presented again, and the second presentation is a replay of the first
// callback. It is read once and then it is gone, which is what the current
// Laravel's session pull() does.
func (s *CookieStateStore) GetState() (string, error) {
	if s.r == nil {
		return "", errors.New("socialite: this state store has no request to read the cookie from")
	}
	c, err := s.r.Cookie(s.name())
	if err != nil {
		return "", nil // no cookie is no state, and Verify refuses the empty one
	}
	if s.w != nil {
		http.SetCookie(s.w, &http.Cookie{
			Name:     s.name(),
			Value:    "",
			Path:     s.path(),
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   s.r.TLS != nil,
		})
	}
	return c.Value, nil
}
