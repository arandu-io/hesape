package users_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// The whole sign-in, end to end: Attempt -> RetrieveByCredentials ->
// ValidateCredentials -> RehashPasswordIfRequired -> Login, with a production
// hasher and a provider over the fake connection.
//
// It is here rather than in auth's own tests because auth cannot import
// auth/users -- the provider imports the guard's package, not the other way
// round -- and because the point of the exercise is that the two real halves
// meet. auth/guards_test.go proves the guard against a fake provider and a fake
// hasher, which is exactly the arrangement that hid the missing hasher.

// fakeSession is the four methods of auth.Session, as a map.
type fakeSession struct {
	values      map[string]any
	regenerated int
}

func newFakeSession() *fakeSession { return &fakeSession{values: map[string]any{}} }

func (s *fakeSession) Get(key string, def ...any) any {
	if value, ok := s.values[key]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

func (s *fakeSession) Put(key string, value any) { s.values[key] = value }

func (s *fakeSession) Remove(key string) any {
	value := s.values[key]
	delete(s.values, key)
	return value
}

func (s *fakeSession) Regenerate(_ context.Context, _ bool) error {
	s.regenerated++
	return nil
}

// fakeRequest is auth.Request with nothing on it: these tests sign in from a
// form, not from a cookie or a Basic header.
type fakeRequest struct {
	cookies map[string]string
}

func (r *fakeRequest) Query(string, ...string) string { return "" }
func (r *fakeRequest) Input(string, ...any) any       { return nil }
func (r *fakeRequest) BearerToken() string            { return "" }
func (r *fakeRequest) GetUser() string                { return "" }
func (r *fakeRequest) GetPassword() string            { return "" }
func (r *fakeRequest) Context() context.Context       { return context.Background() }

func (r *fakeRequest) Cookie(name string, def ...string) string {
	if value, ok := r.cookies[name]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// fakeJar is auth.CookieJar, recording what the guard queued.
type fakeJar struct {
	queued []*http.Cookie
}

func (j *fakeJar) Make(name, value string, minutes int, path, domain string, secure *bool, httpOnly, raw bool, sameSite http.SameSite) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, MaxAge: minutes * 60, HttpOnly: httpOnly}
}

func (j *fakeJar) Queue(cookie *http.Cookie) { j.queued = append(j.queued, cookie) }

func (j *fakeJar) Unqueue(name, path string) {
	kept := j.queued[:0]
	for _, cookie := range j.queued {
		if cookie.Name != name {
			kept = append(kept, cookie)
		}
	}
	j.queued = kept
}

func (j *fakeJar) Forget(name, path, domain string) *http.Cookie {
	return &http.Cookie{Name: name, MaxAge: -1}
}

func (j *fakeJar) HasQueued(key, path string) bool {
	for _, cookie := range j.queued {
		if cookie.Name == key {
			return true
		}
	}
	return false
}

// login is everything a sign-in needs, wired together.
type login struct {
	connection *fakeConnection
	session    *fakeSession
	request    *fakeRequest
	jar        *fakeJar
	guard      *auth.SessionGuard
}

// newLogin wires a guard over a provider over the fake connection, with the
// given hasher on both. The timebox is set to a microsecond: the wait is what
// makes a real sign-in take as long as a failed one, and a test that ran it
// would spend 200 milliseconds proving nothing this file is about.
func newLogin(hasher auth.Hasher, connection *fakeConnection) *login {
	l := &login{
		connection: connection,
		session:    newFakeSession(),
		request:    &fakeRequest{cookies: map[string]string{}},
		jar:        &fakeJar{},
	}
	l.guard = auth.NewSessionGuard(
		"web",
		newEloquentProvider(connection, hasher),
		l.session,
		l.request,
		nil,
		true, // rehashOnLogin
		1,
		"an-application-key",
	)
	l.guard.SetCookieJar(l.jar)
	l.guard.Hasher = hasher
	return l
}

// signedIn reports whether the session names the user.
func (l *login) signedIn(t *testing.T) bool {
	t.Helper()
	return l.session.Get(l.guard.GetName()) != nil
}

// TestAttemptSignsInWithARealHasher is the path the first defect blocked: a
// production hasher wired into a guard and a provider, from the form to the
// session.
func TestAttemptSignsInWithARealHasher(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "correct horse battery staple"),
	})
	l := newLogin(hasher, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}, false) {
		t.Fatal("Attempt refused the password the stored hash was made from")
	}

	if !l.signedIn(t) {
		t.Fatal("the session does not name the user")
	}
	if l.session.Get(l.guard.GetName()) != any(int64(7)) {
		t.Fatalf("the session holds %#v, want the user's id", l.session.Get(l.guard.GetName()))
	}
	if l.session.regenerated != 1 {
		t.Fatalf("the session id was regenerated %d times, want 1: a session handed out before the sign-in must not be one after it", l.session.regenerated)
	}
	if user := l.guard.User(); user == nil || user.GetAuthIdentifier() != any(int64(7)) {
		t.Fatalf("the guard resolved %#v", user)
	}
	if !l.guard.Check() || l.guard.Guest() {
		t.Fatal("the guard reports nobody signed in")
	}

	// One select and nothing else: the hash already meets the parameters, so
	// rehashPasswordIfRequired writes nothing on an ordinary sign-in.
	statement := connection.only(t)
	statement.assertScopedBy(t, tenant)
	statement.assertNeverBinds(t, "correct horse battery staple")
}

// The end to end of the second defect. The same login body that could never
// sign in -- the password sent as a JSON number -- goes through the whole path.
func TestAttemptSignsInWithAPasswordThatIsNotAString(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "12345"),
	})
	l := newLogin(hasher, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": float64(12345),
	}, false) {
		t.Fatal("Attempt refused a password that was sent unquoted")
	}
	if !l.signedIn(t) {
		t.Fatal("the session does not name the user")
	}
}

func TestAttemptRefusesAWrongPassword(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "correct horse battery staple"),
	})
	l := newLogin(hasher, connection)

	if l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery stapler",
	}, false) {
		t.Fatal("a wrong password signed in")
	}
	if l.signedIn(t) {
		t.Fatal("the session names a user after a refused attempt")
	}
	// The account was found, which is what a "somebody tried to sign in" notice
	// listens for, and it is not the same thing as having signed in.
	if l.guard.GetLastAttempted() == nil {
		t.Fatal("the guard did not record the account the attempt named")
	}
}

func TestAttemptRefusesAnAddressNobodyRegistered(t *testing.T) {
	l := newLogin(cheapHasher(), &fakeConnection{})

	if l.guard.Attempt(context.Background(), map[string]any{
		"email":    "nobody@example.com",
		"password": "correct horse battery staple",
	}, false) {
		t.Fatal("an address nobody registered signed in")
	}
	if l.signedIn(t) {
		t.Fatal("the session names a user")
	}
}

// A sign-in that ticked the box mints a remember token, writes it, and queues
// the recaller.
func TestAttemptWithRememberWritesTheTokenAndQueuesTheCookie(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{affected: 1}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "correct horse battery staple"),
	})
	l := newLogin(hasher, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}, true) {
		t.Fatal("Attempt refused the right password")
	}

	written := l.connection.last(t)
	if written.kind != "update" {
		t.Fatalf("the last statement was a %s, want the remember token update", written.kind)
	}
	written.assertScopedBy(t, tenant)
	if !strings.Contains(written.sql, `"remember_token"`) {
		t.Fatalf("the update does not write the remember token: %s", written.sql)
	}

	if len(l.jar.queued) != 1 {
		t.Fatalf("the guard queued %d cookies, want the recaller", len(l.jar.queued))
	}
	recaller := l.jar.queued[0]
	if recaller.Name != l.guard.GetRecallerName() {
		t.Fatalf("the queued cookie is %q, want %q", recaller.Name, l.guard.GetRecallerName())
	}
	// The recaller carries the id, the remember token and a MAC of the password
	// hash -- never the hash itself.
	parts := strings.Split(recaller.Value, "|")
	if len(parts) != 3 {
		t.Fatalf("the recaller has %d segments, want 3: %q", len(parts), recaller.Value)
	}
	if strings.Contains(recaller.Value, "$2") {
		t.Fatalf("the recaller carries the password hash: %q", recaller.Value)
	}
	if parts[2] != l.guard.HashPasswordForCookie(l.guard.User().GetAuthPassword()) {
		t.Fatal("the recaller's third segment is not the MAC of the password hash")
	}
}

// A sign-in that proves the plain password is the only moment a weaker hash can
// be upgraded, and this is that moment: the row is rewritten and the user stays
// signed in.
func TestAttemptRehashesAHashMadeWithWeakerParameters(t *testing.T) {
	stored := mustHash(t, hasherAt(4), "correct horse battery staple")

	stronger := hasherAt(6)
	connection := (&fakeConnection{affected: 1}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": stored,
	})
	l := newLogin(stronger, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}, false) {
		t.Fatal("Attempt refused the right password")
	}

	written := l.connection.last(t)
	if written.kind != "update" {
		t.Fatalf("the last statement was a %s, want the rehash", written.kind)
	}
	written.assertScopedBy(t, tenant)
	written.assertNeverBinds(t, "correct horse battery staple")

	rewritten := l.guard.User().GetAuthPassword()
	if rewritten == stored {
		t.Fatal("the weaker hash was not rewritten")
	}
	if !stronger.Check("correct horse battery staple", rewritten) {
		t.Fatal("the rewritten hash does not verify the password that was just typed")
	}
}

// Once is the sign-in with no session and no cookie: it authenticates for this
// request and writes nothing.
func TestOnceAuthenticatesWithoutASession(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "correct horse battery staple"),
	})
	l := newLogin(hasher, connection)

	if !l.guard.Once(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}) {
		t.Fatal("Once refused the right password")
	}
	if l.guard.User() == nil {
		t.Fatal("Once did not resolve the user for this request")
	}
	if l.signedIn(t) {
		t.Fatal("Once wrote the user into the session")
	}
	if len(l.jar.queued) != 0 {
		t.Fatalf("Once queued %d cookies", len(l.jar.queued))
	}
}

// LogoutOtherDevices is the second consumer of auth.Hasher on the guard itself,
// and the one that reads it directly rather than through the provider. It was
// unreachable in production for the same reason: the field could hold nothing
// but a test double.
func TestLogoutOtherDevicesChecksThePasswordWithTheRealHasher(t *testing.T) {
	hasher := cheapHasher()
	stored := mustHash(t, hasher, "correct horse battery staple")

	connection := (&fakeConnection{affected: 1}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": stored,
	})
	l := newLogin(hasher, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}, false) {
		t.Fatal("Attempt refused the right password")
	}

	if _, err := l.guard.LogoutOtherDevices(context.Background(), "not the password"); err != auth.ErrPasswordMismatch {
		t.Fatalf("LogoutOtherDevices answered %v for a wrong password, want ErrPasswordMismatch", err)
	}

	if _, err := l.guard.LogoutOtherDevices(context.Background(), "correct horse battery staple"); err != nil {
		t.Fatalf("LogoutOtherDevices: %v", err)
	}

	// The password is rehashed with force, which changes the MAC every other
	// session's recaller carries.
	written := l.connection.last(t)
	if written.kind != "update" {
		t.Fatalf("the last statement was a %s, want the forced rehash", written.kind)
	}
	if l.guard.User().GetAuthPassword() == stored {
		t.Fatal("the password hash was not rewritten, so the other sessions still verify")
	}
}

// Logout cycles the remember token, so the recaller sitting in another browser
// stops naming anybody.
func TestLogoutCyclesTheRememberToken(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{affected: 1}).queue(query.Record{
		"id":             int64(7),
		"email":          "ana@example.com",
		"password":       mustHash(t, hasher, "correct horse battery staple"),
		"remember_token": "the-old-token",
	})
	l := newLogin(hasher, connection)

	if !l.guard.Attempt(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"password": "correct horse battery staple",
	}, false) {
		t.Fatal("Attempt refused the right password")
	}
	user := l.guard.User()

	l.guard.Logout(context.Background())

	if l.signedIn(t) {
		t.Fatal("the session still names the user")
	}
	if l.guard.Check() {
		t.Fatal("the guard still reports somebody signed in")
	}
	if user.GetRememberToken() == "the-old-token" {
		t.Fatal("the remember token was not cycled")
	}
	written := l.connection.last(t)
	if written.kind != "update" || !strings.Contains(written.sql, `"remember_token"`) {
		t.Fatalf("the last statement was %v, want the remember token update", written)
	}
}

// The recaller path: a browser that comes back with the cookie and no session is
// signed in from it, through the provider's constant-time token comparison.
func TestAUserComesBackFromTheRecallerCookie(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":             int64(7),
		"email":          "ana@example.com",
		"password":       mustHash(t, hasher, "correct horse battery staple"),
		"remember_token": "the-remember-token",
	})
	l := newLogin(hasher, connection)
	l.request.cookies[l.guard.GetRecallerName()] = "7|the-remember-token|a-mac"

	user := l.guard.User()
	if user == nil {
		t.Fatal("the recaller cookie signed nobody in")
	}
	if !l.guard.ViaRemember() {
		t.Fatal("the guard does not report that this session came from the cookie")
	}
	if !l.signedIn(t) {
		t.Fatal("the session was not written from the recaller")
	}
	connection.last(t).assertScopedBy(t, tenant)
}

// A recaller carrying the wrong token names nobody, however right its id is.
func TestARecallerWithTheWrongTokenSignsNobodyIn(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":             int64(7),
		"email":          "ana@example.com",
		"password":       mustHash(t, hasher, "correct horse battery staple"),
		"remember_token": "the-remember-token",
	})
	l := newLogin(hasher, connection)
	l.request.cookies[l.guard.GetRecallerName()] = "7|another-token|a-mac"

	if user := l.guard.User(); user != nil {
		t.Fatalf("a recaller with the wrong token resolved %#v", user)
	}
	if l.signedIn(t) {
		t.Fatal("the session was written from a recaller that did not match")
	}
}
