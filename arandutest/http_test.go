package arandutest_test

import (
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/arandutest"
	"github.com/arandu-io/hesape/auth"
)

// echoCookie is a handler that reports what the client sent and sets whatever
// the query string asks it to set. It is the smallest thing that can tell a jar
// that keeps a cookie from a jar that merely accumulates them.
func echoCookie(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("set") {
		case "":
		case "clear":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
		default:
			http.SetCookie(w, &http.Cookie{Name: "session", Value: r.URL.Query().Get("set"), Path: "/"})
		}

		if c, err := r.Cookie("session"); err == nil {
			_, _ = w.Write([]byte("carrying " + c.Value))
			return
		}
		_, _ = w.Write([]byte("carrying nothing"))
	})
}

// The property a feature test rests on: after the server clears a cookie, the
// next request does not carry it. A jar that only appends fails this while
// looking like it works, because http.Request.Cookie returns the first value of
// a name and the cleared one is filed behind the live one.
func TestSigningOutIsVisibleToTheNextRequest(t *testing.T) {
	client := arandutest.NewClient(t, echoCookie(t))

	client.Get("/?set=first").See("carrying nothing")
	client.Get("/").See("carrying first")

	client.Get("/?set=clear")
	client.Get("/").See("carrying nothing")
}

// Signing in a second time must replace the session, not hide behind it. When
// it hid, the client went on sending the first id -- which the server had
// deleted when it issued the second -- so every assertion after the second
// sign-in was made by somebody anonymous, and the test read on regardless.
func TestASecondSessionReplacesTheFirstInsteadOfQueueingBehindIt(t *testing.T) {
	client := arandutest.NewClient(t, echoCookie(t))

	client.Get("/?set=first")
	client.Get("/?set=second")

	client.Get("/").See("carrying second").DontSee("carrying first")
}

// whoami answers with what a policy would see: the subject the request carries,
// read the way every policy reads it.
func whoami(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := auth.SubjectFrom(r.Context())
		if !ok {
			_, _ = w.Write([]byte("nobody loaded a session"))
			return
		}
		if s.IsGuest() {
			_, _ = w.Write([]byte("guest of " + s.Tenant))
			return
		}
		_, _ = w.Write([]byte("subject " + s.ID + " of " + s.Tenant))
	})
}

// The one this package exists for. ActingAs has to put the subject where the
// framework reads it -- auth.SubjectFrom, the same call every policy makes --
// and not in a key of its own, which is what it used to do: a context value
// nothing read, so the handler stayed anonymous and the test said it was
// somebody.
func TestActingAsIsVisibleWhereAPolicyLooks(t *testing.T) {
	client := arandutest.NewClient(t, whoami(t))

	client.Get("/").See("nobody loaded a session")

	client.ActingAs(auth.Subject{ID: "u1", Tenant: "acme"})
	client.Get("/").See("subject u1 of acme")
}

// It holds for every request afterwards, not just the next one. A client that
// forgot after the first call is the sign-in that has to be repeated before
// each assertion, and the test that stops doing it is anonymous without saying
// so.
func TestActingAsHoldsForEveryLaterRequest(t *testing.T) {
	client := arandutest.NewClient(t, whoami(t)).ActingAs(auth.Subject{ID: "u1", Tenant: "acme"})

	client.Get("/").See("subject u1 of acme")
	client.Get("/again").See("subject u1 of acme")
}

// A declared anonymous reader is a subject too, and it is not the same fact as
// a request that never loaded a session: Authorize refuses the second and lets
// a policy decide about the first.
func TestActingAsAcceptsADeclaredGuest(t *testing.T) {
	client := arandutest.NewClient(t, whoami(t)).ActingAs(auth.Guest("acme"))

	client.Get("/").See("guest of acme")
}
