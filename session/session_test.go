package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
)

// account stands in for whatever an application keeps in a session. The store is
// generic over it and asserts nothing about it, which is the whole reason the
// subject and its roles are not this package's business.
type account struct {
	Roles []string
}

// signedIn builds the record a sign-in handler hands to Start: the payload, plus
// the two strings the store indexes by.
func signedIn(id, tenant string, roles ...string) session.Record[account] {
	return session.Record[account]{
		Payload:   account{Roles: roles},
		SubjectID: id,
		Tenant:    tenant,
	}
}

func newStore(secure bool) *session.RecordStore[account] {
	return session.NewRecordStore(appKey, time.Hour, secure, session.NewArrayHandler[account]())
}

// requestWithCookies replays the cookies a response set, which is how the
// browser round trip is simulated without a browser.
func requestWithCookies(rec *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

func TestSessionStartAndAll(t *testing.T) {
	store := newStore(true)
	want := signedIn("u1", "t1", "admin")
	rec := httptest.NewRecorder()

	id, err := store.Start(context.Background(), rec, want)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("Start returned an empty session id")
	}

	got, err := store.All(context.Background(), requestWithCookies(rec))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if got.SubjectID != want.SubjectID || got.Tenant != want.Tenant {
		t.Fatalf("record = %+v, want %+v", got, want)
	}
	if len(got.Payload.Roles) != 1 || got.Payload.Roles[0] != "admin" {
		t.Fatalf("payload = %+v: what the application put in the session must come back", got.Payload)
	}
}

// TestSessionCookieIsHardened checks the three attributes that decide whether a
// stolen cookie is useful: HttpOnly keeps script away from it, Secure keeps it
// off plain HTTP, SameSite blunts cross-site submission.
func TestSessionCookieIsHardened(t *testing.T) {
	store := newStore(true)
	rec := httptest.NewRecorder()

	if _, err := store.Start(context.Background(), rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != session.CookieName {
		t.Fatalf("cookie name = %q, want %q", c.Name, session.CookieName)
	}
	if !c.HttpOnly {
		t.Error("the session cookie must be HttpOnly")
	}
	if !c.Secure {
		t.Error("the session cookie must be Secure outside development")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
}

// TestSessionCookieIsSigned is what stops a forged cookie from ever reaching the
// handler: the id is only accepted with a valid HMAC.
func TestSessionCookieIsSigned(t *testing.T) {
	store := newStore(true)
	rec := httptest.NewRecorder()
	if _, err := store.Start(context.Background(), rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	value := rec.Result().Cookies()[0].Value

	id, _, ok := strings.Cut(value, ".")
	if !ok {
		t.Fatalf("cookie value has no signature: %q", value)
	}

	forged := httptest.NewRequest(http.MethodGet, "/", nil)
	forged.AddCookie(&http.Cookie{Name: session.CookieName, Value: id + ".not-a-signature"})

	if got := store.ID(forged); got != "" {
		t.Fatalf("ID accepted a forged signature: %q", got)
	}
	if _, err := store.All(context.Background(), forged); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("error = %v, want ErrNoSession", err)
	}
}

// TestRegenerateInvalidatesTheOldSession is the anti session fixation guarantee:
// the pre-login id must stop working the moment the user authenticates.
func TestRegenerateInvalidatesTheOldSession(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	first := httptest.NewRecorder()
	oldID, err := store.Start(ctx, first, signedIn("anonymous-visitor", "t1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	oldRequest := requestWithCookies(first)

	second := httptest.NewRecorder()
	newID, err := store.Regenerate(ctx, second, oldID, signedIn("u1", "t1"))
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	if newID == oldID {
		t.Fatal("Regenerate reused the session id: that is session fixation")
	}
	if _, err := store.All(ctx, oldRequest); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("the old session still loads: error = %v, want ErrExpired", err)
	}
	if got, err := store.All(ctx, requestWithCookies(second)); err != nil || got.SubjectID != "u1" {
		t.Fatalf("new session: record = %+v, error = %v", got, err)
	}
}

func TestInvalidateClearsSessionAndCookie(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	rec := httptest.NewRecorder()
	id, err := store.Start(ctx, rec, signedIn("u1", "t1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	request := requestWithCookies(rec)

	out := httptest.NewRecorder()
	if err := store.Invalidate(ctx, out, id); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if _, err := store.All(ctx, request); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("the session survived Invalidate: error = %v", err)
	}
	cleared := out.Result().Cookies()[0]
	if cleared.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want a negative value to delete the cookie", cleared.MaxAge)
	}
}

func TestAllWithoutCookie(t *testing.T) {
	store := newStore(true)

	_, err := store.All(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("error = %v, want ErrNoSession", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	store := session.NewRecordStore(appKey, -time.Second, true, session.NewArrayHandler[account]())
	rec := httptest.NewRecorder()
	if _, err := store.Start(context.Background(), rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := store.All(context.Background(), requestWithCookies(rec))
	if !errors.Is(err, session.ErrExpired) {
		t.Fatalf("error = %v, want ErrExpired", err)
	}
}

// maxAgeOf is the number the browser actually acts on: it decides when the
// cookie is thrown away, whatever the handler believes about the record.
func maxAgeOf(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0].MaxAge
}

// TestARememberedSessionOutlivesAPlainOne is the checkbox doing something. The
// assertion is on the cookie's MaxAge rather than on the stored record, because
// that is the half a browser obeys: a record that lives a month behind a cookie
// that dies in an hour is a promise kept in a place nobody can see.
func TestARememberedSessionOutlivesAPlainOne(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()
	rec := signedIn("u1", "t1")

	plainRec := httptest.NewRecorder()
	if _, err := store.Start(ctx, plainRec, rec); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rememberedRec := httptest.NewRecorder()
	if _, err := store.Start(ctx, rememberedRec, rec, session.Remember(true)); err != nil {
		t.Fatalf("Start remembered: %v", err)
	}

	plain, remembered := maxAgeOf(t, plainRec), maxAgeOf(t, rememberedRec)
	if plain != int(time.Hour.Seconds()) {
		t.Errorf("a plain session's cookie lasts %d seconds, want the store's configured hour", plain)
	}
	if remembered != int(session.RememberLifetime.Seconds()) {
		t.Errorf("ticking remember me got a cookie of %d seconds, want RememberLifetime", remembered)
	}
	if remembered <= plain {
		t.Fatal("remember me did not make the session last longer, so the box does nothing")
	}

	// The record has to agree with the cookie, or the session is signed out on
	// the first request after the ttl the record was written with.
	if _, err := store.All(ctx, requestWithCookies(rememberedRec)); err != nil {
		t.Fatalf("the remembered session does not load: %v", err)
	}
	if got, err := store.All(ctx, requestWithCookies(rememberedRec)); err != nil || !got.Remembered {
		t.Fatalf("the session does not know it was remembered: record = %+v, error = %v", got, err)
	}
}

// TestRememberNeverShortensASession: an application configured with a longer
// session than RememberLifetime keeps its own. The box says "keep me signed in",
// and a person who ticks it must not end up signed out earlier than a person who
// did not.
func TestRememberNeverShortensASession(t *testing.T) {
	long := 90 * 24 * time.Hour
	store := session.NewRecordStore(appKey, long, true, session.NewArrayHandler[account]())

	rec := httptest.NewRecorder()
	if _, err := store.Start(context.Background(), rec, signedIn("u1", "t1"), session.Remember(true)); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := maxAgeOf(t, rec); got != int(long.Seconds()) {
		t.Fatalf("remember me cut the session to %d seconds, and the application had configured %d", got, int(long.Seconds()))
	}
}

// TestRegenerateCarriesRememberMe: the sign-in handler calls Regenerate, never
// Start, so an option Regenerate dropped would leave the checkbox unreadable on
// the one screen that draws it.
func TestRegenerateCarriesRememberMe(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	before := httptest.NewRecorder()
	oldID, err := store.Start(ctx, before, signedIn("visitor", "t1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	after := httptest.NewRecorder()
	if _, err := store.Regenerate(ctx, after, oldID, signedIn("u1", "t1"), session.Remember(true)); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	if got := maxAgeOf(t, after); got != int(session.RememberLifetime.Seconds()) {
		t.Fatalf("signing in with remember me ticked got a cookie of %d seconds, want RememberLifetime", got)
	}
}

// TestASessionWithNoStampIsNotConfirmed is the fail-safe direction: a session
// written before this field existed, or by a handler that does not carry it yet,
// reads as never confirmed. The cost is one password screen; the opposite
// default lets every session that survived a deploy walk past the check.
func TestASessionWithNoStampIsNotConfirmed(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	rec := httptest.NewRecorder()
	if _, err := store.Start(ctx, rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := store.All(ctx, requestWithCookies(rec))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if !got.PasswordConfirmedAt.IsZero() {
		t.Fatalf("a new session came with a confirmation stamp of %v: nobody typed a password on it", got.PasswordConfirmedAt)
	}
	if got.PasswordConfirmedWithin(time.Hour) {
		t.Fatal("a session nobody has ever confirmed a password on was let through the confirmation check")
	}
}

// TestConfirmingThePasswordIsRememberedForAWhile is the point of the stamp:
// asking on every sensitive action is what people route around.
func TestConfirmingThePasswordIsRememberedForAWhile(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	start := httptest.NewRecorder()
	if _, err := store.Start(ctx, start, signedIn("u1", "t1"), session.Remember(true)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	request := requestWithCookies(start)

	confirmed := httptest.NewRecorder()
	if err := store.Confirm(ctx, confirmed, request); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	got, err := store.All(ctx, request)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if !got.PasswordConfirmedWithin(time.Hour) {
		t.Fatal("the password was just typed again and the session still asks for it")
	}

	// Confirm rewrites the record, and a rewrite that forgot the session was
	// remembered would hand back the plain ttl -- somebody who ticked the box and
	// then confirmed a payment signed out that evening.
	if got := maxAgeOf(t, confirmed); got != int(session.RememberLifetime.Seconds()) {
		t.Fatalf("confirming the password left a cookie of %d seconds on a remembered session, want RememberLifetime", got)
	}
}

// TestAConfirmationExpires: the window is the whole reason to record a time
// rather than a boolean.
func TestAConfirmationExpires(t *testing.T) {
	stale := signedIn("u1", "t1")
	stale.PasswordConfirmedAt = time.Now().Add(-2 * time.Hour)
	if stale.PasswordConfirmedWithin(time.Hour) {
		t.Error("a password confirmed two hours ago passed a one hour window: the window is not being read")
	}
	if !stale.PasswordConfirmedWithin(3 * time.Hour) {
		t.Error("a password confirmed two hours ago failed a three hour window, so the person is asked again for nothing")
	}

	// A clock that moved, or a record somebody edited. Neither is proof that a
	// person was there, and a stamp in the future would otherwise pass forever.
	future := signedIn("u1", "t1")
	future.PasswordConfirmedAt = time.Now().Add(time.Hour)
	if future.PasswordConfirmedWithin(time.Hour) {
		t.Error("a confirmation stamped in the future was accepted")
	}

	// "No window configured" must not read as "always confirmed".
	fresh := signedIn("u1", "t1")
	fresh.PasswordConfirmedAt = time.Now()
	if fresh.PasswordConfirmedWithin(0) {
		t.Error("a window of zero accepted a confirmation, so a missing configuration disables the check")
	}
}

// TestSigningOutOneSubjectLeavesTheOthersAlone is what a password reset needs:
// the sessions that end are exactly the ones belonging to that account of that
// tenant. Ending too few leaves whoever forced the reset signed in; ending too
// many signs out a stranger.
func TestSigningOutOneSubjectLeavesTheOthersAlone(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	target := signedIn("u1", "t1")
	laptop := httptest.NewRecorder()
	phone := httptest.NewRecorder()
	if _, err := store.Start(ctx, laptop, target); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := store.Start(ctx, phone, target); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// A different person in the same tenant, and the same subject id in
	// another tenant -- two customers may both call an account "u1", and
	// signing one out must not touch the other.
	colleague := httptest.NewRecorder()
	if _, err := store.Start(ctx, colleague, signedIn("u2", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	namesake := httptest.NewRecorder()
	if _, err := store.Start(ctx, namesake, signedIn("u1", "t2")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := store.DestroyOthers(ctx, target, ""); err != nil {
		t.Fatalf("DestroyOthers: %v", err)
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{"laptop": laptop, "phone": phone} {
		if _, err := store.All(ctx, requestWithCookies(rec)); !errors.Is(err, session.ErrExpired) {
			t.Errorf("the %s of the account whose password was reset is still signed in: error = %v", name, err)
		}
	}
	if _, err := store.All(ctx, requestWithCookies(colleague)); err != nil {
		t.Errorf("a colleague in the same tenant was signed out by somebody else's password reset: %v", err)
	}
	if _, err := store.All(ctx, requestWithCookies(namesake)); err != nil {
		t.Errorf("an account with the same id in another tenant was signed out: %v", err)
	}
}

// TestSigningOutTheOtherSessionsKeepsThisOne is the password change from the
// account screen: the person who is changing it stays where they are.
func TestSigningOutTheOtherSessionsKeepsThisOne(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()
	subject := signedIn("u1", "t1")

	here := httptest.NewRecorder()
	keep, err := store.Start(ctx, here, subject)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	elsewhere := httptest.NewRecorder()
	if _, err := store.Start(ctx, elsewhere, subject); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := store.DestroyOthers(ctx, subject, keep); err != nil {
		t.Fatalf("DestroyOthers: %v", err)
	}

	if _, err := store.All(ctx, requestWithCookies(here)); err != nil {
		t.Errorf("changing the password signed the person out of the browser they changed it in: %v", err)
	}
	if _, err := store.All(ctx, requestWithCookies(elsewhere)); !errors.Is(err, session.ErrExpired) {
		t.Errorf("the other browser survived the password change: error = %v", err)
	}
}

// TestSigningOutWithoutATenantIsRefused: without a tenant there is nothing to
// scope by, and an unscoped "every session of subject 1" reaches every
// customer.
func TestSigningOutWithoutATenantIsRefused(t *testing.T) {
	store := newStore(true)

	if err := store.DestroyOthers(context.Background(), signedIn("u1", ""), ""); err == nil {
		t.Fatal("a record with no tenant was accepted, so the sessions of every customer were in scope")
	}
}

// TestStartingASessionDoesNotTrustTheRecordHandedToIt closes the shortest path
// to a forged long session and a forged confirmation: a handler builds a Record
// from something the request influenced and hands it to Start.
//
// The record the store writes is the only place either fact may come from, and
// Start is the only thing that writes them. Both fields are exported because a
// handler has to serialize them, and exported is reachable.
func TestStartingASessionDoesNotTrustTheRecordHandedToIt(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	rec := httptest.NewRecorder()
	forged := signedIn("u1", "t1")
	forged.Remembered = true
	forged.PasswordConfirmedAt = time.Now()
	if _, err := store.Start(ctx, rec, forged); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := maxAgeOf(t, rec); got != int(time.Hour.Seconds()) {
		t.Errorf("a record that arrived with Remembered set bought a cookie of %d seconds without the option, want the store's hour", got)
	}
	got, err := store.All(ctx, requestWithCookies(rec))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if got.Remembered {
		t.Error("a session was marked remembered by the caller rather than by the checkbox")
	}
	if got.PasswordConfirmedWithin(time.Hour) {
		t.Error("a brand new session arrived already past the password confirmation check")
	}
}

// TestSigningInDoesNotInheritTheOldSessionsConfirmation is the laundering attack
// the fixation rotation would otherwise open: confirm the password on a session,
// read that record, sign in as somebody else with it, and the new session starts
// already confirmed.
func TestSigningInDoesNotInheritTheOldSessionsConfirmation(t *testing.T) {
	store := newStore(true)
	ctx := context.Background()

	first := httptest.NewRecorder()
	oldID, err := store.Start(ctx, first, signedIn("u1", "t1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	request := requestWithCookies(first)
	if err := store.Confirm(ctx, httptest.NewRecorder(), request); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	confirmed, err := store.All(ctx, request)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if !confirmed.PasswordConfirmedWithin(time.Hour) {
		t.Fatal("the confirmation did not take, so this test proves nothing")
	}

	second := httptest.NewRecorder()
	if _, err := store.Regenerate(ctx, second, oldID, confirmed); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	after, err := store.All(ctx, requestWithCookies(second))
	if err != nil {
		t.Fatalf("All after Regenerate: %v", err)
	}
	if after.PasswordConfirmedWithin(time.Hour) {
		t.Fatal("signing in carried the previous session's password confirmation into the new one")
	}
}

// forgetfulHandler stores the record the way a handler with its own wire shape
// does: the fields it was written to know about, and nothing else. It stands in
// for the kv handler, whose stored shape predates PasswordConfirmedAt.
type forgetfulHandler struct{ *session.ArrayHandler[account] }

func (h forgetfulHandler) Write(ctx context.Context, id string, rec session.Record[account], ttl time.Duration) error {
	rec.PasswordConfirmedAt = time.Time{}
	return h.ArrayHandler.Write(ctx, id, rec, ttl)
}

// TestAHandlerThatDropsTheStampIsRefusedRatherThanLooping is the failure that
// cost nothing to produce and could not be diagnosed from the outside: Confirm
// reported success, the stamp was never stored, and the person typed the right
// password onto the same screen forever.
func TestAHandlerThatDropsTheStampIsRefusedRatherThanLooping(t *testing.T) {
	store := session.NewRecordStore[account](appKey, time.Hour, true, forgetfulHandler{session.NewArrayHandler[account]()})
	ctx := context.Background()

	rec := httptest.NewRecorder()
	if _, err := store.Start(ctx, rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := store.Confirm(ctx, httptest.NewRecorder(), requestWithCookies(rec))
	if !errors.Is(err, session.ErrConfirmationNotStored) {
		t.Fatalf("a handler that threw the confirmation away reported %v, and the screen would have asked for the password again forever", err)
	}
}

// TestSigningOutASubjectNobodySignedInIsRefusedByTheHandlerToo: ArrayHandler is
// exported and reachable without the store, and an empty subject id matches every
// session that has no subject on it -- every guest. The kv handler refuses the
// same call, and two handlers that disagree only diverge in production.
func TestSigningOutASubjectNobodySignedInIsRefusedByTheHandlerToo(t *testing.T) {
	handler := session.NewArrayHandler[account]()
	ctx := context.Background()

	guest := session.Record[account]{Tenant: "t1"}
	if err := handler.Write(ctx, "a-reader", guest, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := handler.DestroyIndex(ctx, "t1", "", ""); err == nil {
		t.Error("signing out the subject with no id was accepted, and it names every guest of the tenant")
	}
	if err := handler.DestroyIndex(ctx, "", "u1", ""); err == nil {
		t.Error("signing out a subject with no tenant was accepted, and it names that id in every tenant")
	}
	if _, err := handler.Read(ctx, "a-reader"); err != nil {
		t.Errorf("a reader with no account was signed out by somebody's password reset: %v", err)
	}
}

func TestDevelopmentCookieIsNotSecure(t *testing.T) {
	store := newStore(false)
	rec := httptest.NewRecorder()
	if _, err := store.Start(context.Background(), rec, signedIn("u1", "t1")); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if rec.Result().Cookies()[0].Secure {
		t.Fatal("with secure=false the cookie must work over plain HTTP on localhost")
	}
}
