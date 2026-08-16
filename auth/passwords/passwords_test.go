package passwords_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/passwords"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/database/query/processors"
	"github.com/arandu-io/hesape/hashing"
)

// The tenant every repository in this file is configured with. A reset runs for
// somebody who cannot sign in, so it comes from the wiring and never from the
// address that arrived in the form.
const tenant = "acme"

// cheapHasher is the production hasher these tests hash tokens with: bcrypt
// behind the adapter, at the lowest cost the algorithm accepts.
//
// It is not a double. A token repository hashes the token it mints and compares
// it on the way back, and until hashing.AuthHasher existed there was no hasher
// in the framework that could be passed here at all.
func cheapHasher() auth.Hasher {
	return hashing.ForAuth(hashing.NewBcryptHasher(hashing.Options{Rounds: 4}))
}

// ---------------------------------------------------------------------------
// The fakes
// ---------------------------------------------------------------------------

// resettableUser is an Authenticatable that can also be sent a reset link,
// recording the tokens it was notified with.
type resettableUser struct {
	id       any
	email    string
	password string

	notified []string
	notifyIn error
}

var (
	_ auth.Authenticatable  = (*resettableUser)(nil)
	_ auth.CanResetPassword = (*resettableUser)(nil)
)

func (u *resettableUser) GetAuthIdentifierName() string { return "id" }
func (u *resettableUser) GetAuthIdentifier() any        { return u.id }
func (u *resettableUser) GetAuthPasswordName() string   { return "password" }
func (u *resettableUser) GetAuthPassword() string       { return u.password }
func (u *resettableUser) GetRememberToken() string      { return "" }
func (u *resettableUser) SetRememberToken(string)       {}
func (u *resettableUser) GetRememberTokenName() string  { return "remember_token" }
func (u *resettableUser) GetEmailForPasswordReset() string {
	return u.email
}

func (u *resettableUser) SendPasswordResetNotification(_ context.Context, token string) error {
	if u.notifyIn != nil {
		return u.notifyIn
	}
	u.notified = append(u.notified, token)
	return nil
}

// plainAuthenticatable is a user type that cannot be sent a reset link, which is
// what ErrCannotResetPassword is for.
type plainAuthenticatable struct{ resettableUser }

func (u *plainAuthenticatable) GetEmailForPasswordReset() {}

// fakeUserProvider is the half of auth.UserProvider the broker uses: it answers
// RetrieveByCredentials and nothing else is ever called.
type fakeUserProvider struct {
	user        auth.Authenticatable
	credentials []map[string]any
	err         error
}

func (p *fakeUserProvider) RetrieveByID(context.Context, any) (auth.Authenticatable, error) {
	return nil, nil
}

func (p *fakeUserProvider) RetrieveByToken(context.Context, any, string) (auth.Authenticatable, error) {
	return nil, nil
}

func (p *fakeUserProvider) UpdateRememberToken(context.Context, auth.Authenticatable, string) error {
	return nil
}

func (p *fakeUserProvider) RetrieveByCredentials(_ context.Context, credentials map[string]any) (auth.Authenticatable, error) {
	p.credentials = append(p.credentials, credentials)
	if p.err != nil {
		return nil, p.err
	}
	return p.user, nil
}

func (p *fakeUserProvider) ValidateCredentials(context.Context, auth.Authenticatable, map[string]any) bool {
	return false
}

func (p *fakeUserProvider) RehashPasswordIfRequired(context.Context, auth.Authenticatable, map[string]any, bool) error {
	return nil
}

// fakeTokens is passwords.TokenRepository, recording what it was asked.
type fakeTokens struct {
	token  string
	exists bool
	recent bool

	created int
	deleted int

	createErr error
	existsErr error
}

func (r *fakeTokens) Create(context.Context, auth.CanResetPassword) (string, error) {
	r.created++
	if r.createErr != nil {
		return "", r.createErr
	}
	return r.token, nil
}

func (r *fakeTokens) Exists(_ context.Context, _ auth.CanResetPassword, token string) (bool, error) {
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.exists && token == r.token, nil
}

func (r *fakeTokens) RecentlyCreatedToken(context.Context, auth.CanResetPassword) (bool, error) {
	return r.recent, nil
}

func (r *fakeTokens) Delete(context.Context, auth.CanResetPassword) error {
	r.deleted++
	return nil
}

func (r *fakeTokens) DeleteExpired(context.Context) error { return nil }

// fakeConnection is passwords.Connection over a recording query.Connection, so
// the statements a token repository issues can be read back.
type fakeConnection struct {
	rows       [][]query.Record
	statements []statement
	affected   int64
}

type statement struct {
	kind     string
	sql      string
	bindings []any
}

func (c *fakeConnection) Select(sql string, bindings []any, _ bool) ([]query.Record, error) {
	c.statements = append(c.statements, statement{kind: "select", sql: sql, bindings: bindings})
	if len(c.rows) == 0 {
		return nil, nil
	}
	rows := c.rows[0]
	c.rows = c.rows[1:]
	return rows, nil
}

func (c *fakeConnection) Insert(sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "insert", sql: sql, bindings: bindings})
	return true, nil
}

func (c *fakeConnection) Update(sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "update", sql: sql, bindings: bindings})
	return c.affected, nil
}

func (c *fakeConnection) Delete(sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "delete", sql: sql, bindings: bindings})
	return c.affected, nil
}

func (c *fakeConnection) Statement(sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "statement", sql: sql, bindings: bindings})
	return true, nil
}

func (c *fakeConnection) Table(ctx context.Context, table any, as ...string) *query.Builder {
	return query.NewBuilder(c, grammars.NewSQLiteGrammar(), processors.NewSQLiteProcessor()).From(table, as...)
}

func (c *fakeConnection) queue(rows ...query.Record) *fakeConnection {
	c.rows = append(c.rows, rows)
	return c
}

func (c *fakeConnection) ofKind(t *testing.T, kind string) []statement {
	t.Helper()
	var found []statement
	for _, s := range c.statements {
		if s.kind == kind {
			found = append(found, s)
		}
	}
	return found
}

func (s statement) assertScopedBy(t *testing.T, tenant string) {
	t.Helper()
	if !strings.Contains(s.sql, `"tenant_id"`) {
		t.Fatalf("the statement carries no tenant filter: %s", s.sql)
	}
	for _, binding := range s.bindings {
		if binding == any(tenant) {
			return
		}
	}
	t.Fatalf("the statement does not bind the tenant %q: %s %v", tenant, s.sql, s.bindings)
}

// newBroker wires a broker with a timebox of a microsecond: the wait is what
// makes an address nobody registered take as long as one that is, and a test
// that ran it would spend 200 milliseconds proving nothing here is about.
func newBroker(tokens passwords.TokenRepository, provider auth.UserProvider) *passwords.PasswordBroker {
	return passwords.NewPasswordBroker(tokens, provider, nil, 1)
}

// ---------------------------------------------------------------------------
// PasswordBroker.SendResetLink
// ---------------------------------------------------------------------------

func TestSendResetLinkMintsATokenAndNotifiesTheUser(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token"}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	status, err := broker.SendResetLink(context.Background(), map[string]any{"email": "ana@example.com"}, nil)
	if err != nil {
		t.Fatalf("SendResetLink: %v", err)
	}
	if status != passwords.ResetLinkSent {
		t.Fatalf("SendResetLink answered %q, want %q", status, passwords.ResetLinkSent)
	}
	if tokens.created != 1 {
		t.Fatalf("the broker minted %d tokens, want 1", tokens.created)
	}
	if len(user.notified) != 1 || user.notified[0] != "a-reset-token" {
		t.Fatalf("the user was notified with %v, want the plain token once", user.notified)
	}
}

// An address nobody registered is a status and not an error, and the form is
// expected to render it exactly like a success: this endpoint is anonymous, and
// telling the two apart is a way to read the customer list.
func TestSendResetLinkToAnAddressNobodyRegistered(t *testing.T) {
	tokens := &fakeTokens{token: "a-reset-token"}
	broker := newBroker(tokens, &fakeUserProvider{})

	status, err := broker.SendResetLink(context.Background(), map[string]any{"email": "nobody@example.com"}, nil)
	if err != nil {
		t.Fatalf("SendResetLink: %v", err)
	}
	if status != passwords.InvalidUser {
		t.Fatalf("SendResetLink answered %q, want %q", status, passwords.InvalidUser)
	}
	if tokens.created != 0 {
		t.Fatal("the broker minted a token for an address nobody registered")
	}
}

// The throttle is what makes the form safe to leave open: without it, every
// submit mails somebody else's inbox from an anonymous endpoint.
func TestSendResetLinkIsThrottled(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token", recent: true}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	status, err := broker.SendResetLink(context.Background(), map[string]any{"email": "ana@example.com"}, nil)
	if err != nil {
		t.Fatalf("SendResetLink: %v", err)
	}
	if status != passwords.ResetThrottled {
		t.Fatalf("SendResetLink answered %q, want %q", status, passwords.ResetThrottled)
	}
	if tokens.created != 0 {
		t.Fatal("a throttled request minted a token anyway")
	}
	if len(user.notified) != 0 {
		t.Fatal("a throttled request sent a notification")
	}
}

// The callback takes delivery over, and an empty status from it means the link
// was sent.
func TestSendResetLinkHandsTheTokenToTheCallback(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token"}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	var delivered string
	status, err := broker.SendResetLink(context.Background(), map[string]any{"email": "ana@example.com"},
		func(_ auth.CanResetPassword, token string) (string, error) {
			delivered = token
			return "", nil
		})
	if err != nil {
		t.Fatalf("SendResetLink: %v", err)
	}
	if delivered != "a-reset-token" {
		t.Fatalf("the callback was handed %q", delivered)
	}
	if status != passwords.ResetLinkSent {
		t.Fatalf("an empty status from the callback became %q", status)
	}
	if len(user.notified) != 0 {
		t.Fatal("the user was notified as well as the callback being called")
	}
}

// ---------------------------------------------------------------------------
// PasswordBroker.Reset
// ---------------------------------------------------------------------------

func TestResetStoresThePasswordAndSpendsTheToken(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token", exists: true}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	var stored string
	status, err := broker.Reset(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"token":    "a-reset-token",
		"password": "correct horse battery staple",
	}, func(_ auth.CanResetPassword, password string) error {
		stored = password
		return nil
	})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if status != passwords.PasswordReset {
		t.Fatalf("Reset answered %q, want %q", status, passwords.PasswordReset)
	}
	if stored != "correct horse battery staple" {
		t.Fatalf("the callback was handed %q", stored)
	}
	if tokens.deleted != 1 {
		t.Fatalf("the token was deleted %d times, want once: a link that survives its own use is a permanent key to the account", tokens.deleted)
	}
}

func TestResetRefusesAWrongToken(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token", exists: true}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	called := false
	status, err := broker.Reset(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"token":    "another-token",
		"password": "correct horse battery staple",
	}, func(auth.CanResetPassword, string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if status != passwords.InvalidToken {
		t.Fatalf("Reset answered %q, want %q", status, passwords.InvalidToken)
	}
	if called {
		t.Fatal("the callback ran for a token that does not exist")
	}
	if tokens.deleted != 0 {
		t.Fatal("a wrong token deleted the live one")
	}
}

func TestResetForAnAddressNobodyRegistered(t *testing.T) {
	tokens := &fakeTokens{token: "a-reset-token", exists: true}
	broker := newBroker(tokens, &fakeUserProvider{})

	status, err := broker.Reset(context.Background(), map[string]any{
		"email":    "nobody@example.com",
		"token":    "a-reset-token",
		"password": "correct horse battery staple",
	}, func(auth.CanResetPassword, string) error { return nil })
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if status != passwords.InvalidUser {
		t.Fatalf("Reset answered %q, want %q", status, passwords.InvalidUser)
	}
}

// The order of the last two steps is the whole point: the token is deleted after
// the callback, so a store that could not write the new password leaves the
// person holding a link that still works.
func TestResetLeavesTheTokenAliveWhenTheCallbackFails(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	tokens := &fakeTokens{token: "a-reset-token", exists: true}
	broker := newBroker(tokens, &fakeUserProvider{user: user})

	failed := errors.New("the user store did not answer")
	if _, err := broker.Reset(context.Background(), map[string]any{
		"email":    "ana@example.com",
		"token":    "a-reset-token",
		"password": "correct horse battery staple",
	}, func(auth.CanResetPassword, string) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("Reset answered %v, want the callback's error", err)
	}
	if tokens.deleted != 0 {
		t.Fatal("the token was spent on a reset that did not happen")
	}
}

// ---------------------------------------------------------------------------
// PasswordBroker.GetUser
// ---------------------------------------------------------------------------

// The token is not a column. It is taken out before the credentials reach the
// user provider, where it would become a where clause that matches nobody.
func TestGetUserDoesNotLookTheUserUpByTheToken(t *testing.T) {
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	provider := &fakeUserProvider{user: user}
	broker := newBroker(&fakeTokens{}, provider)

	if _, err := broker.GetUser(context.Background(), map[string]any{
		"email": "ana@example.com",
		"token": "a-reset-token",
	}); err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	if len(provider.credentials) != 1 {
		t.Fatalf("the provider was asked %d times", len(provider.credentials))
	}
	if _, ok := provider.credentials[0]["token"]; ok {
		t.Fatalf("the token reached the user lookup: %v", provider.credentials[0])
	}
	if provider.credentials[0]["email"] != "ana@example.com" {
		t.Fatalf("the lookup was %v", provider.credentials[0])
	}
}

// A user type that cannot be sent a reset link is a wiring mistake, so it is an
// error and not a status: a status is shown to whoever typed the address, and
// the person who needs to see this is the one who wired it.
func TestGetUserRefusesAUserThatCannotBeSentALink(t *testing.T) {
	broker := newBroker(&fakeTokens{}, &fakeUserProvider{user: &plainAuthenticatable{}})

	if _, err := broker.GetUser(context.Background(), map[string]any{"email": "ana@example.com"}); !errors.Is(err, passwords.ErrCannotResetPassword) {
		t.Fatalf("GetUser answered %v, want ErrCannotResetPassword", err)
	}
}

// ---------------------------------------------------------------------------
// DatabaseTokenRepository
// ---------------------------------------------------------------------------

func newDatabaseTokens(connection passwords.Connection, throttle time.Duration) *passwords.DatabaseTokenRepository {
	return passwords.NewDatabaseTokenRepository(
		connection, cheapHasher(), "password_reset_tokens", "an-application-key",
		time.Hour, throttle, tenant,
	)
}

// What is stored is a hash of the token and never the token, so a copy of the
// table is a list of addresses that asked and not a set of working reset links.
func TestDatabaseTokenRepositoryStoresAHashAndNotTheToken(t *testing.T) {
	connection := &fakeConnection{affected: 1}
	repository := newDatabaseTokens(connection, 0)
	user := &resettableUser{id: int64(7), email: "ana@example.com"}

	token, err := repository.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("Create answered with no token")
	}

	inserts := connection.ofKind(t, "insert")
	if len(inserts) != 1 {
		t.Fatalf("Create issued %d inserts, want 1", len(inserts))
	}
	inserts[0].assertScopedBy(t, tenant)

	var stored string
	for _, binding := range inserts[0].bindings {
		value, ok := binding.(string)
		if !ok {
			continue
		}
		if value == token {
			t.Fatal("the plain token was written to the table")
		}
		if strings.HasPrefix(value, "$2") {
			stored = value
		}
	}
	if stored == "" {
		t.Fatalf("no hashed token was written: %v", inserts[0].bindings)
	}
	if !cheapHasher().Check(token, stored) {
		t.Fatal("the stored hash does not verify the token that was returned")
	}

	// The record the address had before is removed first, so one address has at
	// most one live token.
	if len(connection.ofKind(t, "delete")) != 1 {
		t.Fatal("Create did not delete the address's existing record first")
	}
}

// A token that matches, has not expired and belongs to the address is the only
// one that exists.
func TestDatabaseTokenRepositoryExists(t *testing.T) {
	repository := newDatabaseTokens(&fakeConnection{affected: 1}, 0)
	user := &resettableUser{id: int64(7), email: "ana@example.com"}

	token, err := repository.Create(context.Background(), &resettableUser{email: user.email})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	hashed, err := cheapHasher().Make(token)
	if err != nil {
		t.Fatalf("hashing the token: %v", err)
	}

	for name, test := range map[string]struct {
		record query.Record
		token  string
		want   bool
	}{
		"the token it was minted for": {
			query.Record{"email": user.email, "token": hashed, "created_at": time.Now()}, token, true,
		},
		"another token": {
			query.Record{"email": user.email, "token": hashed, "created_at": time.Now()}, "another-token", false,
		},
		"a record that expired": {
			query.Record{"email": user.email, "token": hashed, "created_at": time.Now().Add(-2 * time.Hour)}, token, false,
		},
		"a created_at nobody can read": {
			query.Record{"email": user.email, "token": hashed, "created_at": "not a time"}, token, false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			connection := (&fakeConnection{affected: 1}).queue(test.record)
			repository := newDatabaseTokens(connection, 0)

			exists, err := repository.Exists(context.Background(), user, test.token)
			if err != nil {
				t.Fatalf("Exists: %v", err)
			}
			if exists != test.want {
				t.Fatalf("Exists answered %v, want %v", exists, test.want)
			}
			connection.ofKind(t, "select")[0].assertScopedBy(t, tenant)
		})
	}
}

// No record at all is "no token", not a failure.
func TestDatabaseTokenRepositoryExistsWithNoRecord(t *testing.T) {
	repository := newDatabaseTokens(&fakeConnection{}, 0)

	exists, err := repository.Exists(context.Background(), &resettableUser{email: "ana@example.com"}, "a-reset-token")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("a table with no record answered that the token exists")
	}
}

func TestDatabaseTokenRepositoryThrottle(t *testing.T) {
	user := &resettableUser{email: "ana@example.com"}

	for name, test := range map[string]struct {
		createdAt time.Time
		throttle  time.Duration
		want      bool
	}{
		"minted a moment ago":    {time.Now(), time.Minute, true},
		"minted long enough ago": {time.Now().Add(-2 * time.Minute), time.Minute, false},
		"throttling turned off":  {time.Now(), 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			connection := (&fakeConnection{}).queue(query.Record{
				"email": user.email, "token": "a-hash", "created_at": test.createdAt,
			})
			repository := newDatabaseTokens(connection, test.throttle)

			recent, err := repository.RecentlyCreatedToken(context.Background(), user)
			if err != nil {
				t.Fatalf("RecentlyCreatedToken: %v", err)
			}
			if recent != test.want {
				t.Fatalf("RecentlyCreatedToken answered %v, want %v", recent, test.want)
			}
		})
	}
}

// A repository wired with no tenant reaches nothing, because auth.SystemGrant
// answers the zero Grant for an empty tenant and the zero Grant passes no Check.
func TestADatabaseTokenRepositoryWithNoTenantCannotReadAnything(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"email": "ana@example.com"})
	repository := passwords.NewDatabaseTokenRepository(
		connection, cheapHasher(), "password_reset_tokens", "an-application-key",
		time.Hour, 0, "",
	)
	user := &resettableUser{email: "ana@example.com"}

	if _, err := repository.Exists(context.Background(), user, "a-reset-token"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Exists answered %v, want auth.ErrForbidden", err)
	}
	if _, err := repository.Create(context.Background(), user); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Create answered %v, want auth.ErrForbidden", err)
	}
	if len(connection.statements) != 0 {
		t.Fatalf("an unscoped repository reached the connection: %v", connection.statements)
	}
}

// ---------------------------------------------------------------------------
// CacheTokenRepository
// ---------------------------------------------------------------------------

func newCacheTokens(throttle time.Duration) *passwords.CacheTokenRepository {
	return passwords.NewCacheTokenRepository(
		cache.New(cache.NewArrayStore()), cheapHasher(), "an-application-key",
		time.Hour, throttle, tenant,
	)
}

// The same contract over a cache: mint, verify, spend.
func TestCacheTokenRepositoryRoundTrip(t *testing.T) {
	repository := newCacheTokens(0)
	user := &resettableUser{id: int64(7), email: "ana@example.com"}
	ctx := context.Background()

	token, err := repository.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	exists, err := repository.Exists(ctx, user, token)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("the token it just minted does not exist")
	}

	wrong, err := repository.Exists(ctx, user, "another-token")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if wrong {
		t.Fatal("another token exists")
	}

	if err := repository.Delete(ctx, user); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	spent, err := repository.Exists(ctx, user, token)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if spent {
		t.Fatal("the token survived being deleted")
	}
}

// The key is the address hashed, because a cache key travels further than a row
// does and an address is personal data.
func TestCacheTokenRepositoryKeyIsNotTheAddress(t *testing.T) {
	repository := newCacheTokens(0)
	key := repository.CacheKey(&resettableUser{email: "ana@example.com"})

	if strings.Contains(key, "ana@example.com") || strings.Contains(key, "ana") {
		t.Fatalf("the cache key carries the address: %q", key)
	}
	if len(key) != 64 {
		t.Fatalf("the cache key is %d characters, want a sha256 in hex", len(key))
	}
}

// A second Create replaces the first, so the link from the first request stops
// working -- the same rule the table repository enforces by deleting the row.
func TestCacheTokenRepositoryKeepsOneLiveTokenPerAddress(t *testing.T) {
	repository := newCacheTokens(0)
	user := &resettableUser{email: "ana@example.com"}
	ctx := context.Background()

	first, err := repository.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := repository.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first == second {
		t.Fatal("the second request minted the same token")
	}

	stale, err := repository.Exists(ctx, user, first)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if stale {
		t.Fatal("the first token still works after a second was minted")
	}
}

func TestCacheTokenRepositoryThrottle(t *testing.T) {
	user := &resettableUser{email: "ana@example.com"}
	ctx := context.Background()

	throttled := newCacheTokens(time.Minute)
	if _, err := throttled.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	recent, err := throttled.RecentlyCreatedToken(ctx, user)
	if err != nil {
		t.Fatalf("RecentlyCreatedToken: %v", err)
	}
	if !recent {
		t.Fatal("a token minted a moment ago is not recent")
	}

	off := newCacheTokens(0)
	if _, err := off.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if recent, err = off.RecentlyCreatedToken(ctx, user); err != nil {
		t.Fatalf("RecentlyCreatedToken: %v", err)
	}
	if recent {
		t.Fatal("throttling is on with a throttle of zero")
	}
}

// An address with no entry is "no token", not a failure: neither caller treats
// a cache miss as one.
func TestCacheTokenRepositoryOnAMiss(t *testing.T) {
	repository := newCacheTokens(time.Minute)
	user := &resettableUser{email: "nobody@example.com"}

	exists, err := repository.Exists(context.Background(), user, "a-reset-token")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("a miss answered that the token exists")
	}

	recent, err := repository.RecentlyCreatedToken(context.Background(), user)
	if err != nil {
		t.Fatalf("RecentlyCreatedToken: %v", err)
	}
	if recent {
		t.Fatal("a miss answered that a token was recently created")
	}
}

// Every statement and every cache call is taken under one of the two named
// actions, which is what `aru doctor` audits system grants by.
func TestTheTokenActionsAreTheTwoConstants(t *testing.T) {
	if passwords.ReadToken != auth.Action("password.read") {
		t.Fatalf("ReadToken is %q", passwords.ReadToken)
	}
	if passwords.WriteToken != auth.Action("password.write") {
		t.Fatalf("WriteToken is %q", passwords.WriteToken)
	}
}
