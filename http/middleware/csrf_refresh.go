package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

// CSRFRefreshPath is where a page asks for a fresh CSRF token: the address ui.js
// fetches while the page stays open, so that a token never expires under a
// person who is still looking at the form it was drawn for.
//
// A token carries its own expiry, and the form that holds it is drawn once. A
// screen left open for longer than the expiry -- an operator's console, a live
// table nobody closes -- then answered 419 to a click that was legitimate in
// every other respect: the session was the same and so was the person. The
// token is not made to live longer, because a token that outlives the page it
// was drawn on is a token that can be replayed; the page asks for a new one
// before the old one lapses.
const CSRFRefreshPath = "/_arandu/csrf"

// answerRefresh answers CSRFRefreshPath: token, issued for the request's own
// binding by CSRFProtect a moment ago, and the seconds it has left.
//
//	{"token": "...", "expires_in": 7199}
//
// It is answered by the middleware and not by a route, so that it exists in
// every application that checks tokens, whichever way the application mounts
// its routes: the token is the middleware's, and so is the address it is asked
// for at.
//
// It adds no authority. Whoever can load any page of the application is handed
// a token on it, bound to the cookie their browser holds, and this hands the
// same to the same browser -- a session that has ended gets a guest's token, as
// a page would, and a request with no cookie gets a fresh guest. What it
// refuses is a request a browser reports as sent from another site, which could
// not read the answer anyway: the response carries no CORS header, and the
// refusal keeps it from being rendered as a page either.
//
// The expiry is given as a count of seconds so that the page plans the renewal
// on its own clock, whatever the difference between that clock and the
// server's. The answer is never cached, and says that it varies with the
// cookie.
func answerRefresh(w http.ResponseWriter, r *http.Request, token string) {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		http.Error(w, "this address answers the application's own pages", http.StatusForbidden)
		return
	}
	left := int64(0)
	if at, ok := session.TokenExpiry(token); ok {
		left = max(0, at.Unix()-support.Now().Unix())
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store, private")
	h.Add("Vary", "Cookie")
	body, _ := json.Marshal(struct {
		Token     string `json:"token"`
		ExpiresIn int64  `json:"expires_in"`
	}{token, left})
	h.Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}
