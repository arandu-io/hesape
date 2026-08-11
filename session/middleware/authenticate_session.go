package middleware

import (
	"crypto/subtle"
	"net/http"
)

// Guard is the part of an authentication guard [AuthenticateSession] uses.
//
// It is declared here, minimally, rather than imported, because this package
// sits below the one that authenticates and importing it would be the cycle.
// hesape/auth is what an application wires behind it.
type Guard interface {
	// GetDefaultDriver names the guard. It is what the session key is suffixed
	// with, so two guards on one application do not invalidate each other's
	// sessions.
	GetDefaultDriver() string

	// GetAuthPassword returns the signed-in subject's stored password hash, and
	// false when nobody is signed in.
	//
	// The HASH and not the password, and not a "has it changed" answer: the
	// comparison has to happen here, against what the session recorded when it
	// started, and a guard that answered the question itself would be trusting
	// the thing being checked.
	GetAuthPassword(r *http.Request) (string, bool)

	// LogoutCurrentDevice ends the session on this device and no other.
	LogoutCurrentDevice(r *http.Request) error
}

// AuthenticateSession ends a session whose password has changed underneath it.
//
// It is Illuminate\Session\Middleware\AuthenticateSession, and it is the reason
// "change my password" signs out the other browsers. The mechanism is small: the
// session records the password hash it started with, and every request compares
// it with the one in the database. A password change rewrites the hash, so every
// session that recorded the old one stops on its next request -- including the
// one belonging to whoever forced the change, which is the whole point.
//
// It runs after [StartSession] and after whatever authenticates the request.
type AuthenticateSession struct {
	auth Guard
	// redirectTo is what Illuminate keeps in a static. It is an instance field
	// here: a package-level one is process-global mutable state, which under
	// -race is a data race the moment two tests set it, and which nothing can
	// unset afterwards.
	redirectTo func(r *http.Request) string
}

// NewAuthenticateSession returns the middleware.
func NewAuthenticateSession(auth Guard) *AuthenticateSession {
	return &AuthenticateSession{auth: auth}
}

// RedirectUsing sets what the middleware sends somebody to when their session
// is ended. Returning "" answers 401 instead of redirecting.
//
// Illuminate's is a static method. See the field it sets for why this one is
// not.
func (m *AuthenticateSession) RedirectUsing(callback func(r *http.Request) string) *AuthenticateSession {
	m.redirectTo = callback
	return m
}

// passwordHashKey is where the hash is recorded. Suffixed with the guard, which
// is Illuminate's "password_hash_"+driver.
func passwordHashKey(driver string) string { return "password_hash_" + driver }

// Handle is the middleware.
func (m *AuthenticateSession) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store, ok := Session(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		hash, signedIn := m.auth.GetAuthPassword(r)
		if !signedIn || hash == "" {
			// Nobody is signed in, or the account has no password -- a subject
			// authenticated some other way. There is nothing to compare.
			next.ServeHTTP(w, r)
			return
		}

		key := passwordHashKey(m.auth.GetDefaultDriver())
		if !store.Has(key) {
			store.Put(key, hash)
			next.ServeHTTP(w, r)
			m.rememberHash(r, store, key)
			return
		}

		recorded, _ := store.Get(key, "").(string)
		// Constant time, because this compares two hashes and a length-or-prefix
		// comparison leaks how much of one matches. It costs nothing here.
		if subtle.ConstantTimeCompare([]byte(recorded), []byte(hash)) != 1 {
			m.logout(w, r, store)
			return
		}

		next.ServeHTTP(w, r)
		m.rememberHash(r, store, key)
	})
}

// rememberHash re-records the hash after the request, so a password changed BY
// this request does not sign it out on the next one.
func (m *AuthenticateSession) rememberHash(r *http.Request, store sessionBag, key string) {
	if hash, signedIn := m.auth.GetAuthPassword(r); signedIn && hash != "" {
		store.Put(key, hash)
	}
}

// logout ends the session and answers.
func (m *AuthenticateSession) logout(w http.ResponseWriter, r *http.Request, store sessionBag) {
	_ = m.auth.LogoutCurrentDevice(r)
	store.Flush()

	if m.redirectTo != nil {
		if to := m.redirectTo(r); to != "" {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
	}
	http.Error(w, "Unauthenticated.", http.StatusUnauthorized)
}

// sessionBag is the part of a session these two helpers touch. It is here so
// they say what they need rather than taking the whole store.
type sessionBag interface {
	Put(key string, value any)
	Flush()
}
