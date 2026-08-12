package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// The fakes below stand in for the four things a guard reads: the user store,
// the session, the cookie jar and the request. They are the smallest thing that
// answers the interfaces in collaborators.go, and they record what was asked of
// them, because most of what a guard does is ask somebody else to do something.

// fakeUser is an Authenticatable with the seven methods and nothing else.
type fakeUser struct {
	id            any
	password      string
	rememberToken string
}

func (u *fakeUser) GetAuthIdentifierName() string { return "id" }
func (u *fakeUser) GetAuthIdentifier() any        { return u.id }
func (u *fakeUser) GetAuthPasswordName() string   { return "password" }
func (u *fakeUser) GetAuthPassword() string       { return u.password }
func (u *fakeUser) GetRememberToken() string      { return u.rememberToken }
func (u *fakeUser) SetRememberToken(token string) { u.rememberToken = token }
func (u *fakeUser) GetRememberTokenName() string  { return "remember_token" }

// rehash records one call to RehashPasswordIfRequired.
type rehash struct {
	user     auth.Authenticatable
	password string
	force    bool
}

// fakeProvider is a user store holding one user, found by id, by remember token
// or by the "email" credential.
type fakeProvider struct {
	user     *fakeUser
	email    string
	password string

	rehashes      []rehash
	tokensWritten []string
	byTokenCalls  int
}

func (p *fakeProvider) RetrieveByID(_ context.Context, identifier any) (auth.Authenticatable, error) {
	if p.user != nil && fmt.Sprint(p.user.id) == fmt.Sprint(identifier) {
		return p.user, nil
	}
	return nil, nil
}

func (p *fakeProvider) RetrieveByToken(_ context.Context, identifier any, token string) (auth.Authenticatable, error) {
	p.byTokenCalls++

	if p.user != nil && fmt.Sprint(p.user.id) == fmt.Sprint(identifier) && token != "" && token == p.user.rememberToken {
		return p.user, nil
	}
	return nil, nil
}

func (p *fakeProvider) UpdateRememberToken(_ context.Context, user auth.Authenticatable, token string) error {
	p.tokensWritten = append(p.tokensWritten, token)
	user.SetRememberToken(token)
	return nil
}

func (p *fakeProvider) RetrieveByCredentials(_ context.Context, credentials map[string]any) (auth.Authenticatable, error) {
	if p.user == nil {
		return nil, nil
	}
	if email, ok := credentials["email"].(string); ok && email == p.email {
		return p.user, nil
	}
	if token, ok := credentials["api_token"].(string); ok && token == p.user.rememberToken {
		return p.user, nil
	}
	return nil, nil
}

func (p *fakeProvider) ValidateCredentials(_ context.Context, user auth.Authenticatable, credentials map[string]any) bool {
	password, _ := credentials["password"].(string)

	return user != nil && password != "" && password == p.password
}

func (p *fakeProvider) RehashPasswordIfRequired(_ context.Context, user auth.Authenticatable, credentials map[string]any, force bool) error {
	password, _ := credentials["password"].(string)

	p.rehashes = append(p.rehashes, rehash{user: user, password: password, force: force})

	if force {
		p.user.password = "hashed:" + password + ":again"
	}
	return nil
}

// fakeSession is the session store, as a map.
type fakeSession struct {
	data        map[string]any
	regenerated int
}

func newFakeSession() *fakeSession { return &fakeSession{data: map[string]any{}} }

func (s *fakeSession) Get(key string, def ...any) any {
	if value, ok := s.data[key]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

func (s *fakeSession) Put(key string, value any) { s.data[key] = value }

func (s *fakeSession) Remove(key string) any {
	value := s.data[key]
	delete(s.data, key)
	return value
}

func (s *fakeSession) Regenerate(_ context.Context, _ bool) error {
	s.regenerated++
	return nil
}

// fakeCookieJar queues cookies by name and remembers what was taken off.
type fakeCookieJar struct {
	queued   map[string]*http.Cookie
	unqueued []string
}

func newFakeCookieJar() *fakeCookieJar {
	return &fakeCookieJar{queued: map[string]*http.Cookie{}}
}

func (j *fakeCookieJar) Make(name, value string, minutes int, _, _ string, _ *bool, httpOnly, _ bool, _ http.SameSite) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, MaxAge: minutes * 60, HttpOnly: httpOnly}
}

func (j *fakeCookieJar) Queue(cookie *http.Cookie) {
	if cookie != nil {
		j.queued[cookie.Name] = cookie
	}
}

func (j *fakeCookieJar) Unqueue(name, _ string) {
	delete(j.queued, name)
	j.unqueued = append(j.unqueued, name)
}

func (j *fakeCookieJar) Forget(name, _, _ string) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", MaxAge: -1}
}

func (j *fakeCookieJar) HasQueued(key, _ string) bool {
	_, ok := j.queued[key]
	return ok
}

// fakeRequest answers the seven things a guard reads off a request.
type fakeRequest struct {
	query   map[string]string
	input   map[string]any
	cookies map[string]string
	bearer  string
	user    string
	passwrd string
	ctx     context.Context
}

func newFakeRequest() *fakeRequest {
	return &fakeRequest{
		query:   map[string]string{},
		input:   map[string]any{},
		cookies: map[string]string{},
		ctx:     context.Background(),
	}
}

func (r *fakeRequest) Query(key string, def ...string) string {
	if value, ok := r.query[key]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

func (r *fakeRequest) Input(key string, def ...any) any {
	if value, ok := r.input[key]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

func (r *fakeRequest) BearerToken() string { return r.bearer }

func (r *fakeRequest) Cookie(name string, def ...string) string {
	if value, ok := r.cookies[name]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

func (r *fakeRequest) GetUser() string          { return r.user }
func (r *fakeRequest) GetPassword() string      { return r.passwrd }
func (r *fakeRequest) Context() context.Context { return r.ctx }

// fakeDispatcher records the events a guard fires and the listeners it
// registers.
type fakeDispatcher struct {
	dispatched []any
	listenedTo []any
}

func (d *fakeDispatcher) Dispatch(event any, _ ...any) []any {
	d.dispatched = append(d.dispatched, event)
	return nil
}

func (d *fakeDispatcher) Listen(events any, _ ...any) {
	d.listenedTo = append(d.listenedTo, events)
}

// firstEvent returns the first event of the given type that was dispatched.
func firstEvent[T any](d *fakeDispatcher) (T, bool) {
	for _, event := range d.dispatched {
		if typed, ok := event.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

// fakeHasher is the Hash facade: a hash is the word "hashed:" and the password.
type fakeHasher struct{}

func (fakeHasher) Make(value string) (string, error) { return "hashed:" + value, nil }
func (fakeHasher) Check(value, hashedValue string) bool {
	return hashedValue == "hashed:"+value || strings.HasPrefix(hashedValue, "hashed:"+value+":")
}
func (fakeHasher) NeedsRehash(string) bool { return false }

// recordingTimebox is a timebox that does not wait, and says whether it was
// asked to return early. Waiting is asserted once, on the real one.
type recordingTimebox struct {
	calls      int
	microsecs  int
	earlyAsked bool
}

func (t *recordingTimebox) Call(callback func(timebox auth.Timebox) (any, error), microseconds int) (any, error) {
	t.calls++
	t.microsecs = microseconds
	return callback(t)
}

func (t *recordingTimebox) ReturnEarly() auth.Timebox     { t.earlyAsked = true; return t }
func (t *recordingTimebox) DontReturnEarly() auth.Timebox { t.earlyAsked = false; return t }

// signedInGuard builds a guard with one registered user, wired to everything.
func signedInGuard(t *testing.T) (*auth.SessionGuard, *fakeProvider, *fakeSession, *fakeCookieJar, *fakeRequest, *fakeDispatcher) {
	t.Helper()

	provider := &fakeProvider{
		user:     &fakeUser{id: 7, password: "hashed:secret"},
		email:    "person@example.com",
		password: "secret",
	}
	session := newFakeSession()
	jar := newFakeCookieJar()
	request := newFakeRequest()
	dispatcher := &fakeDispatcher{}

	guard := auth.NewSessionGuard("web", provider, session, request, &recordingTimebox{}, true, 1, "app-key")
	guard.SetCookieJar(jar)
	guard.SetDispatcher(dispatcher)
	guard.Hasher = fakeHasher{}

	return guard, provider, session, jar, request, dispatcher
}

func TestAttemptSignsInWithTheRightPassword(t *testing.T) {
	guard, provider, session, _, _, dispatcher := signedInGuard(t)

	if !guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, false) {
		t.Fatal("Attempt refused the right password")
	}

	if !guard.Check() || guard.Guest() {
		t.Fatal("the guard does not consider the person signed in")
	}
	if guard.User() != provider.user {
		t.Fatal("the guard resolved somebody else")
	}
	if guard.ID() != 7 {
		t.Fatalf("ID is %v, want 7", guard.ID())
	}
	if session.data[guard.GetName()] != 7 {
		t.Fatalf("the session holds %v under %q", session.data[guard.GetName()], guard.GetName())
	}
	if session.regenerated != 1 {
		t.Fatalf("the session id was regenerated %d times, want 1", session.regenerated)
	}
	if len(provider.rehashes) != 1 || provider.rehashes[0].force {
		t.Fatalf("rehash on login was not asked for, or was forced: %v", provider.rehashes)
	}

	if login, ok := firstEvent[auth.Login](dispatcher); !ok || login.Guard != "web" || login.Remember {
		t.Fatalf("the Login event is wrong or missing: %#v", dispatcher.dispatched)
	}
	if _, ok := firstEvent[auth.Validated](dispatcher); !ok {
		t.Fatal("the Validated event never fired")
	}
	if _, ok := firstEvent[auth.Failed](dispatcher); ok {
		t.Fatal("a Failed event fired on a successful attempt")
	}
}

func TestAttemptRefusesTheWrongPasswordAndSignsNobodyIn(t *testing.T) {
	guard, _, session, _, _, dispatcher := signedInGuard(t)

	if guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "wrong"}, false) {
		t.Fatal("Attempt accepted the wrong password")
	}

	if guard.Check() {
		t.Fatal("a refused attempt left somebody signed in")
	}
	if _, ok := session.data[guard.GetName()]; ok {
		t.Fatal("a refused attempt wrote the session")
	}
	if guard.GetLastAttempted() == nil {
		t.Fatal("GetLastAttempted forgot the account that was tried")
	}

	failed, ok := firstEvent[auth.Failed](dispatcher)
	if !ok || failed.User == nil {
		t.Fatalf("the Failed event is wrong or missing: %#v", dispatcher.dispatched)
	}
	if _, ok := firstEvent[auth.Login](dispatcher); ok {
		t.Fatal("a Login event fired on a refused attempt")
	}
}

func TestAttemptOnAnAddressNobodyRegisteredStillReportsTheAccount(t *testing.T) {
	guard, _, _, _, _, dispatcher := signedInGuard(t)

	if guard.Attempt(context.Background(), map[string]any{"email": "nobody@example.com", "password": "secret"}, false) {
		t.Fatal("Attempt accepted an address nobody registered")
	}

	failed, ok := firstEvent[auth.Failed](dispatcher)
	if !ok {
		t.Fatal("the Failed event never fired")
	}
	if failed.User != nil {
		t.Fatal("the Failed event names a user for an address nobody registered")
	}
}

func TestOnlyASuccessfulAttemptLeavesTheTimeboxEarly(t *testing.T) {
	timebox := &recordingTimebox{}
	provider := &fakeProvider{user: &fakeUser{id: 1, password: "hashed:secret"}, email: "a@b.c", password: "secret"}

	guard := auth.NewSessionGuard("web", provider, newFakeSession(), newFakeRequest(), timebox, true, 0, "key")
	guard.SetCookieJar(newFakeCookieJar())

	if timebox != guard.GetTimebox() {
		t.Fatal("GetTimebox handed back a different timebox")
	}

	guard.Attempt(context.Background(), map[string]any{"email": "a@b.c", "password": "wrong"}, false)
	if timebox.earlyAsked {
		t.Fatal("a refused attempt asked the timebox to return early, which times the answer")
	}
	if timebox.microsecs != 200000 {
		t.Fatalf("the timebox ran for %d microseconds, want the default 200000", timebox.microsecs)
	}

	guard.Attempt(context.Background(), map[string]any{"email": "a@b.c", "password": "secret"}, false)
	if !timebox.earlyAsked {
		t.Fatal("a successful attempt did not return early")
	}
}

func TestARefusedAttemptWaitsOutTheTimebox(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 1, password: "hashed:secret"}, email: "a@b.c", password: "secret"}

	// 20 milliseconds: long enough to measure, short enough for a test suite.
	guard := auth.NewSessionGuard("web", provider, newFakeSession(), newFakeRequest(), nil, true, 20000, "key")
	guard.SetCookieJar(newFakeCookieJar())

	start := time.Now()
	guard.Attempt(context.Background(), map[string]any{"email": "nobody@example.com", "password": "secret"}, false)
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Fatalf("a refused attempt took %s, which is less than the timebox: the clock says whether the account exists", elapsed)
	}
}

func TestLoginWithRememberQueuesTheRecallerAndSetsTheToken(t *testing.T) {
	guard, provider, _, jar, _, _ := signedInGuard(t)

	if !guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, true) {
		t.Fatal("Attempt refused the right password")
	}

	if provider.user.GetRememberToken() == "" {
		t.Fatal("the remember token was never set")
	}
	if len(provider.tokensWritten) != 1 {
		t.Fatalf("the remember token was written %d times, want 1", len(provider.tokensWritten))
	}

	cookie, ok := jar.queued[guard.GetRecallerName()]
	if !ok {
		t.Fatalf("no recaller was queued; the jar holds %v", jar.queued)
	}

	segments := strings.Split(cookie.Value, "|")
	if len(segments) != 3 {
		t.Fatalf("the recaller has %d segments, want 3: %q", len(segments), cookie.Value)
	}
	if segments[0] != "7" || segments[1] != provider.user.GetRememberToken() {
		t.Fatalf("the recaller names the wrong user or token: %q", cookie.Value)
	}
	if segments[2] != guard.HashPasswordForCookie(provider.user.GetAuthPassword()) {
		t.Fatal("the recaller's third segment is not the MAC of the password hash")
	}
	if segments[2] == provider.user.GetAuthPassword() {
		t.Fatal("the recaller carries the password hash itself")
	}
}

func TestViaRememberSignsThePersonInFromTheCookie(t *testing.T) {
	guard, provider, session, _, request, dispatcher := signedInGuard(t)

	provider.user.rememberToken = "the-remembered-token"
	request.cookies[guard.GetRecallerName()] = "7|the-remembered-token|" + guard.HashPasswordForCookie(provider.user.GetAuthPassword())

	if guard.ViaRemember() {
		t.Fatal("the guard claims a remembered session before resolving one")
	}

	if guard.User() != provider.user {
		t.Fatal("the recaller cookie did not resolve the user")
	}
	if !guard.ViaRemember() {
		t.Fatal("ViaRemember is false for a session that came from the cookie")
	}
	if session.data[guard.GetName()] != 7 {
		t.Fatal("a remembered sign-in did not write the session")
	}

	login, ok := firstEvent[auth.Login](dispatcher)
	if !ok || !login.Remember {
		t.Fatalf("the Login event should say it came from the cookie: %#v", dispatcher.dispatched)
	}

	// A second call must not go back to the store: the user is cached, and the
	// recaller gets one attempt per request.
	guard.User()
	if provider.byTokenCalls != 1 {
		t.Fatalf("the recaller was looked up %d times, want 1", provider.byTokenCalls)
	}
}

func TestARecallerWithTheWrongTokenSignsNobodyIn(t *testing.T) {
	guard, provider, _, _, request, _ := signedInGuard(t)

	provider.user.rememberToken = "the-remembered-token"
	request.cookies[guard.GetRecallerName()] = "7|not-the-token|whatever"

	if guard.User() != nil {
		t.Fatal("a recaller with the wrong token signed somebody in")
	}
	if guard.ViaRemember() {
		t.Fatal("ViaRemember is true after a recaller that named nobody")
	}
}

func TestLogoutClearsTheSessionAndTheRecaller(t *testing.T) {
	guard, provider, session, jar, request, dispatcher := signedInGuard(t)

	provider.user.rememberToken = "the-remembered-token"
	request.cookies[guard.GetRecallerName()] = "7|the-remembered-token|" + guard.HashPasswordForCookie(provider.user.GetAuthPassword())

	if guard.User() == nil {
		t.Fatal("the recaller cookie did not resolve the user")
	}

	guard.Logout(context.Background())

	if guard.Check() || guard.User() != nil {
		t.Fatal("somebody is still signed in after Logout")
	}
	if _, ok := session.data[guard.GetName()]; ok {
		t.Fatal("Logout left the user id in the session")
	}
	if len(jar.unqueued) == 0 || jar.unqueued[0] != guard.GetRecallerName() {
		t.Fatalf("Logout did not unqueue the recaller: %v", jar.unqueued)
	}

	forget, ok := jar.queued[guard.GetRecallerName()]
	if !ok || forget.MaxAge >= 0 || forget.Value != "" {
		t.Fatalf("Logout did not queue a cookie that deletes the recaller: %#v", forget)
	}
	if provider.user.GetRememberToken() == "the-remembered-token" {
		t.Fatal("Logout left the old remember token working in the other browser")
	}
	if len(provider.tokensWritten) != 1 {
		t.Fatalf("the new remember token was written %d times, want 1", len(provider.tokensWritten))
	}
	if _, ok := firstEvent[auth.Logout](dispatcher); !ok {
		t.Fatal("the Logout event never fired")
	}
	if guard.ID() != nil {
		t.Fatal("ID answers after a logout")
	}
}

func TestLogoutCurrentDeviceLeavesTheRememberTokenAlone(t *testing.T) {
	guard, provider, _, _, _, dispatcher := signedInGuard(t)

	guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, true)
	token := provider.user.GetRememberToken()

	guard.LogoutCurrentDevice()

	if guard.Check() {
		t.Fatal("somebody is still signed in on this device")
	}
	if provider.user.GetRememberToken() != token {
		t.Fatal("LogoutCurrentDevice cycled the remember token, which signs the other browser out too")
	}
	if _, ok := firstEvent[auth.CurrentDeviceLogout](dispatcher); !ok {
		t.Fatal("the CurrentDeviceLogout event never fired")
	}
}

func TestLogoutOtherDevicesRehashesThePasswordAndReissuesTheRecaller(t *testing.T) {
	guard, provider, _, jar, _, dispatcher := signedInGuard(t)

	guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, true)

	before := jar.queued[guard.GetRecallerName()].Value

	if _, err := guard.LogoutOtherDevices(context.Background(), "secret"); err != nil {
		t.Fatalf("LogoutOtherDevices refused the current password: %v", err)
	}

	forced := false
	for _, call := range provider.rehashes {
		if call.force && call.password == "secret" {
			forced = true
		}
	}
	if !forced {
		t.Fatalf("the password was not rehashed with force: %v", provider.rehashes)
	}

	after := jar.queued[guard.GetRecallerName()].Value
	if after == before {
		t.Fatal("the recaller was not reissued, so this browser signs itself out too")
	}
	if strings.Split(after, "|")[2] == strings.Split(before, "|")[2] {
		t.Fatal("the recaller's MAC did not change with the password hash")
	}
	if _, ok := firstEvent[auth.OtherDeviceLogout](dispatcher); !ok {
		t.Fatal("the OtherDeviceLogout event never fired")
	}
}

func TestLogoutOtherDevicesRefusesTheWrongPassword(t *testing.T) {
	guard, provider, _, _, _, _ := signedInGuard(t)

	guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, true)
	rehashesBefore := len(provider.rehashes)

	if _, err := guard.LogoutOtherDevices(context.Background(), "not-the-password"); err != auth.ErrPasswordMismatch {
		t.Fatalf("LogoutOtherDevices answered %v, want ErrPasswordMismatch", err)
	}
	if len(provider.rehashes) != rehashesBefore {
		t.Fatal("the wrong password still rehashed the stored one")
	}
}

func TestLogoutOtherDevicesNeedsAHasher(t *testing.T) {
	guard, _, _, _, _, _ := signedInGuard(t)
	guard.Hasher = nil

	guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, false)

	if _, err := guard.LogoutOtherDevices(context.Background(), "secret"); err != auth.ErrHasherNotSet {
		t.Fatalf("LogoutOtherDevices answered %v, want ErrHasherNotSet", err)
	}
}

func TestOnceSignsInForThisRequestOnly(t *testing.T) {
	guard, provider, session, _, _, _ := signedInGuard(t)

	if !guard.Once(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}) {
		t.Fatal("Once refused the right password")
	}
	if guard.User() != provider.user {
		t.Fatal("Once did not set the user")
	}
	if len(session.data) != 0 {
		t.Fatalf("Once wrote the session: %v", session.data)
	}
	if session.regenerated != 0 {
		t.Fatal("Once regenerated the session id")
	}
}

func TestOnceUsingIDAndLoginUsingIDAnswerNilForAStranger(t *testing.T) {
	guard, provider, _, _, _, _ := signedInGuard(t)

	if guard.OnceUsingID(context.Background(), 7) != provider.user {
		t.Fatal("OnceUsingID did not find the user")
	}
	if guard.OnceUsingID(context.Background(), 404) != nil {
		t.Fatal("OnceUsingID invented a user")
	}
	if guard.LoginUsingID(context.Background(), 404, false) != nil {
		t.Fatal("LoginUsingID invented a user")
	}
	if guard.LoginUsingID(context.Background(), 7, false) != provider.user {
		t.Fatal("LoginUsingID did not sign the user in")
	}
}

func TestAttemptWhenLetsACallbackRefuseAValidPassword(t *testing.T) {
	guard, _, session, _, _, dispatcher := signedInGuard(t)

	suspended := func(user auth.Authenticatable, g *auth.SessionGuard) bool { return false }

	credentials := map[string]any{"email": "person@example.com", "password": "secret"}

	if guard.AttemptWhen(context.Background(), credentials, []func(auth.Authenticatable, *auth.SessionGuard) bool{suspended}, false) {
		t.Fatal("AttemptWhen signed in a user its callback refused")
	}
	if guard.Check() || len(session.data) != 0 {
		t.Fatal("a refused callback still signed the person in")
	}
	if _, ok := firstEvent[auth.Failed](dispatcher); !ok {
		t.Fatal("a callback refusal did not fire the Failed event")
	}

	allowed := func(user auth.Authenticatable, g *auth.SessionGuard) bool { return user != nil && g != nil }

	if !guard.AttemptWhen(context.Background(), credentials, []func(auth.Authenticatable, *auth.SessionGuard) bool{allowed}, false) {
		t.Fatal("AttemptWhen refused a user its callback allowed")
	}
}

func TestValidateChecksTheCredentialsWithoutSigningAnybodyIn(t *testing.T) {
	guard, _, session, _, _, _ := signedInGuard(t)

	if !guard.Validate(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}) {
		t.Fatal("Validate refused the right password")
	}
	if guard.Check() || len(session.data) != 0 {
		t.Fatal("Validate signed somebody in")
	}
	if guard.Validate(context.Background(), map[string]any{"email": "person@example.com", "password": "wrong"}) {
		t.Fatal("Validate accepted the wrong password")
	}
}

func TestBasicSignsInFromTheAuthorizationHeader(t *testing.T) {
	guard, _, _, _, request, _ := signedInGuard(t)

	request.user = "person@example.com"
	request.passwrd = "secret"

	if err := guard.Basic(context.Background(), "email", nil); err != nil {
		t.Fatalf("Basic refused the right credentials: %v", err)
	}
	if !guard.Check() {
		t.Fatal("Basic did not sign the person in")
	}

	// Already signed in: the PHP returns without touching anything.
	if err := guard.Basic(context.Background(), "email", nil); err != nil {
		t.Fatalf("Basic complained about somebody already signed in: %v", err)
	}
}

func TestBasicAndOnceBasicRefuseTheWrongCredentials(t *testing.T) {
	guard, _, _, _, request, _ := signedInGuard(t)

	request.user = "person@example.com"
	request.passwrd = "wrong"

	if err := guard.Basic(context.Background(), "email", nil); err != auth.ErrInvalidBasicCredentials {
		t.Fatalf("Basic answered %v, want ErrInvalidBasicCredentials", err)
	}
	if err := guard.OnceBasic(context.Background(), "email", nil); err != auth.ErrInvalidBasicCredentials {
		t.Fatalf("OnceBasic answered %v, want ErrInvalidBasicCredentials", err)
	}
	if guard.Check() {
		t.Fatal("a refused Basic attempt signed somebody in")
	}
}

func TestTheGuardWithoutACookieJarSaysSo(t *testing.T) {
	guard := auth.NewSessionGuard("web", &fakeProvider{}, newFakeSession(), newFakeRequest(), &recordingTimebox{}, true, 1, "")

	jar, err := guard.GetCookieJar()
	if err != auth.ErrCookieJarNotSet || jar != nil {
		t.Fatalf("GetCookieJar answered (%v, %v), want ErrCookieJarNotSet", jar, err)
	}
}

func TestTheSessionKeyAndTheRecallerNameAreOfThisGuard(t *testing.T) {
	web := auth.NewSessionGuard("web", &fakeProvider{}, newFakeSession(), nil, nil, true, 1, "")
	admin := auth.NewSessionGuard("admin", &fakeProvider{}, newFakeSession(), nil, nil, true, 1, "")

	if !strings.HasPrefix(web.GetName(), "login_web_") {
		t.Fatalf("the session key is %q", web.GetName())
	}
	if !strings.HasPrefix(web.GetRecallerName(), "remember_web_") {
		t.Fatalf("the recaller name is %q", web.GetRecallerName())
	}
	if web.GetName() == admin.GetName() || web.GetRecallerName() == admin.GetRecallerName() {
		t.Fatal("two guards share a session key, so one signs the other in")
	}
	if web.GetName() == "login_web_" || len(web.GetName()) != len("login_web_")+40 {
		t.Fatalf("the class hash is missing from %q", web.GetName())
	}
}

func TestAttemptingRegistersAListenerForTheAttemptingEvent(t *testing.T) {
	guard, _, _, _, _, dispatcher := signedInGuard(t)

	guard.Attempting(func(auth.Attempting) {})

	if len(dispatcher.listenedTo) != 1 {
		t.Fatalf("Attempting registered %d listeners, want 1", len(dispatcher.listenedTo))
	}
	if _, ok := dispatcher.listenedTo[0].(auth.Attempting); !ok {
		t.Fatalf("Attempting listened for %T, want auth.Attempting", dispatcher.listenedTo[0])
	}
}

func TestGuardHelpersAnswerForAGuestAndForAUser(t *testing.T) {
	guard, provider, _, _, _, _ := signedInGuard(t)

	if !guard.Guest() || guard.Check() || guard.HasUser() {
		t.Fatal("a guard with nobody signed in says somebody is")
	}
	if guard.ID() != nil {
		t.Fatal("ID answers for a guest")
	}

	user, err := guard.Authenticate()
	if user != nil {
		t.Fatal("Authenticate handed back a user for a guest")
	}
	authError, ok := err.(*auth.AuthenticationError)
	if !ok {
		t.Fatalf("Authenticate answered %T, want *auth.AuthenticationError", err)
	}
	if authError.Error() != "Unauthenticated." {
		t.Fatalf("the message is %q, want Unauthenticated.", authError.Error())
	}

	guard.SetUser(provider.user)

	if !guard.HasUser() || !guard.Check() || guard.Guest() {
		t.Fatal("SetUser did not sign the person in")
	}
	if user, err := guard.Authenticate(); user != provider.user || err != nil {
		t.Fatalf("Authenticate answered (%v, %v)", user, err)
	}

	guard.ForgetUser()

	if guard.HasUser() {
		t.Fatal("ForgetUser kept the user")
	}
	if guard.GetProvider() != provider {
		t.Fatal("GetProvider handed back a different provider")
	}
}

func TestSetUserUndoesALogout(t *testing.T) {
	guard, provider, _, _, _, _ := signedInGuard(t)

	guard.Attempt(context.Background(), map[string]any{"email": "person@example.com", "password": "secret"}, false)
	guard.Logout(context.Background())

	guard.SetUser(provider.user)

	if !guard.Check() || guard.User() != provider.user {
		t.Fatal("SetUser after a logout left the guard logged out")
	}
}

func TestTheTokenGuardReadsTheTokenFromEveryPlaceInOrder(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 3, rememberToken: "the-token"}, email: "a@b.c"}

	request := newFakeRequest()
	guard := auth.NewTokenGuard(provider, request, "", "", false)

	if guard.GetTokenForRequest() != "" {
		t.Fatal("a request with no token has one")
	}

	request.passwrd = "from-basic"
	if guard.GetTokenForRequest() != "from-basic" {
		t.Fatalf("the Basic password was not read: %q", guard.GetTokenForRequest())
	}

	request.bearer = "from-bearer"
	if guard.GetTokenForRequest() != "from-bearer" {
		t.Fatalf("the bearer token does not win over the Basic password: %q", guard.GetTokenForRequest())
	}

	request.input["api_token"] = "from-input"
	if guard.GetTokenForRequest() != "from-input" {
		t.Fatalf("the input does not win over the bearer token: %q", guard.GetTokenForRequest())
	}

	request.query["api_token"] = "from-query"
	if guard.GetTokenForRequest() != "from-query" {
		t.Fatalf("the query string does not win: %q", guard.GetTokenForRequest())
	}
}

func TestTheTokenGuardResolvesTheUserAndValidates(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 3, rememberToken: "the-token"}, email: "a@b.c"}

	request := newFakeRequest()
	request.query["api_token"] = "the-token"

	guard := auth.NewTokenGuard(provider, request, "api_token", "api_token", false)

	if guard.User() != provider.user {
		t.Fatal("the token did not resolve the user")
	}
	if !guard.Check() || guard.ID() != 3 {
		t.Fatal("the guard does not consider the request signed in")
	}
	if !guard.Validate(context.Background(), map[string]any{"api_token": "the-token"}) {
		t.Fatal("Validate refused a good token")
	}
	if guard.Validate(context.Background(), map[string]any{"api_token": "another-token"}) {
		t.Fatal("Validate accepted a token nobody holds")
	}
	if guard.Validate(context.Background(), map[string]any{}) {
		t.Fatal("Validate accepted a request with no token at all")
	}
}

func TestTheTokenGuardHashesTheTokenWhenItIsStoredHashed(t *testing.T) {
	sum := sha256.Sum256([]byte("the-token"))
	stored := hex.EncodeToString(sum[:])

	provider := &fakeProvider{user: &fakeUser{id: 3, rememberToken: stored}, email: "a@b.c"}

	request := newFakeRequest()
	request.query["api_token"] = "the-token"

	guard := auth.NewTokenGuard(provider, request, "api_token", "api_token", true)

	if guard.User() != provider.user {
		t.Fatal("the hashed token did not resolve the user")
	}
}

func TestTheTokenGuardWithNoTokenResolvesNobodyAndSetRequestReplacesIt(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 3, rememberToken: "the-token"}, email: "a@b.c"}

	guard := auth.NewTokenGuard(provider, newFakeRequest(), "api_token", "api_token", false)

	if guard.User() != nil || guard.Check() {
		t.Fatal("a request with no token resolved a user")
	}

	replacement := newFakeRequest()
	replacement.query["api_token"] = "the-token"

	if guard.SetRequest(replacement) != guard {
		t.Fatal("SetRequest handed back a different guard")
	}

	// The PHP caches the user for the request, and a nil is not cached: the
	// field is still empty, so the new request is read.
	if guard.User() != provider.user {
		t.Fatal("the replaced request was not read")
	}
}

func TestTheRequestGuardIsItsCallback(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 9}}

	calls := 0
	callback := func(request auth.Request, p auth.UserProvider) auth.Authenticatable {
		calls++
		if request == nil || p == nil {
			return nil
		}
		if request.(*fakeRequest).query["who"] != "9" {
			return nil
		}
		return provider.user
	}

	request := newFakeRequest()
	request.query["who"] = "9"

	guard := auth.NewRequestGuard(callback, request, provider)

	if guard.User() != provider.user {
		t.Fatal("the callback's user did not come back")
	}
	if guard.User() != provider.user || calls != 1 {
		t.Fatalf("the callback ran %d times, want 1: the user is resolved once per request", calls)
	}
	if !guard.Check() || guard.ID() != 9 {
		t.Fatal("the guard does not consider the request signed in")
	}
}

func TestTheRequestGuardValidatesWithTheRequestInTheCredentials(t *testing.T) {
	provider := &fakeProvider{user: &fakeUser{id: 9}}

	callback := func(request auth.Request, _ auth.UserProvider) auth.Authenticatable {
		if request == nil || request.(*fakeRequest).query["who"] != "9" {
			return nil
		}
		return provider.user
	}

	guard := auth.NewRequestGuard(callback, newFakeRequest(), provider)

	good := newFakeRequest()
	good.query["who"] = "9"

	if !guard.Validate(context.Background(), map[string]any{"request": good}) {
		t.Fatal("Validate refused a request the callback accepts")
	}
	if guard.Validate(context.Background(), map[string]any{"request": newFakeRequest()}) {
		t.Fatal("Validate accepted a request the callback refuses")
	}
	if guard.HasUser() {
		t.Fatal("Validate resolved this guard's own user, which it must not")
	}

	if guard.SetRequest(good) != guard {
		t.Fatal("SetRequest handed back a different guard")
	}
	if guard.User() != provider.user {
		t.Fatal("the replaced request was not read")
	}
}
