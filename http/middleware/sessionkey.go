package middleware

import (
	"context"
	"net/http"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	rmiddleware "github.com/arandu-io/hesape/routing/middleware"
)

// Sessions is what KeyBySession reads a session through. *hhttp.SessionStore
// satisfies it.
type Sessions interface {
	// IDFromRequest is the session id the request's cookie names, once its
	// signature is verified, or the empty string.
	IDFromRequest(r *http.Request) string
	// Load returns the subject of the request's session, and an error when the
	// store does not hold it.
	Load(ctx context.Context, r *http.Request) (auth.Subject, error)
}

var _ Sessions = (*hhttp.SessionStore)(nil)

// KeyBySession keys on the session id while the store still holds that
// session, and on the address otherwise. Pass the application's
// *hhttp.SessionStore.
//
// The cookie alone is not enough to key on. Its signature proves the id was
// issued here once, and every expired or signed-out id a client kept would be
// a fresh budget of its own; asking the store closes that, so a session that
// is gone falls back to the address like a request with no cookie at all.
//
// The key is byte for byte the one the counter is kept under, and a store that
// cannot answer is a session that does not exist: the request is counted by
// its address rather than let through.
//
// It is routing/middleware.KeyBySession over the two methods the session store
// declares, and the key is that function's. The declared return type is the
// plain func(*http.Request) string, the same function as a KeyFunc.
func KeyBySession(sessions Sessions) func(*http.Request) string {
	return func(r *http.Request) string {
		return rmiddleware.KeyBySession(liveSession{sessions: sessions, r: r})(r)
	}
}

// liveSession answers the two questions the counter asks with the two methods
// the session store declares. It is built per request because Load reads the
// session from the request rather than from an id.
type liveSession struct {
	sessions Sessions
	r        *http.Request
}

func (s liveSession) ID(r *http.Request) string { return s.sessions.IDFromRequest(r) }

func (s liveSession) Exists(ctx context.Context, _ string) bool {
	_, err := s.sessions.Load(ctx, s.r)
	return err == nil
}
