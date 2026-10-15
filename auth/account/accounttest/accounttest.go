package accounttest

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/hashing"
	"github.com/arandu-io/hesape/otp"
)

// Fixture is one fresh database and the application's half of the account
// rules over it.
type Fixture[U account.Account] struct {
	// DB holds the users table, the outbox and the second factor's tables,
	// migrated and empty.
	DB *database.DB
	// Query opens a query on the users table: models.Users(db).Base().
	Query func() *model.Builder
	// Policy is the application's user policy.
	Policy auth.Policy[U]
}

// Run checks the account package against the application's users table, user
// model and user policy. open is called once per subtest, so no subtest is
// affected by what another left behind.
func Run[U account.Account](t *testing.T, open func(t *testing.T) Fixture[U]) {
	t.Helper()
	for _, c := range cases[U]() {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, &env[U]{t: t, f: open(t), ctx: context.Background()})
		})
	}
}

// testCase is one subtest of the suite.
type testCase[U account.Account] struct {
	name string
	run  func(t *testing.T, e *env[U])
}

func cases[U account.Account]() []testCase[U] {
	var all []testCase[U]
	all = append(all, userCases[U]()...)
	all = append(all, hookCases[U]()...)
	all = append(all, factorCases[U]()...)
	all = append(all, challengeCases[U]()...)
	all = append(all, compatibilityCases[U]()...)
	return all
}

// The values every subtest shares.
const (
	tenantA  = "tenant-a"
	tenantB  = "tenant-b"
	password = "a-long-enough-password"
	issuer   = "Arandu"
)

// appKey is the application key the second factor's secrets are sealed under.
var appKey = []byte("0123456789abcdef0123456789abcdef")

// env is one subtest's database and the helpers that read it.
type env[U account.Account] struct {
	t   *testing.T
	f   Fixture[U]
	ctx context.Context
}

// users builds the account rules with hooks; a nil throttle is the default.
func (e *env[U]) users(hooks account.Hooks[U], throttle auth.SignInThrottle) *account.Users[U] {
	e.t.Helper()
	users, err := account.NewUsers(account.Config[U]{
		DB: e.f.DB, Query: e.f.Query, Policy: e.f.Policy, Throttle: throttle, Hooks: hooks,
	})
	if err != nil {
		e.t.Fatalf("building the account rules: %v", err)
	}
	return users
}

// factors builds the second factor over users, counting challenge attempts in
// store.
func (e *env[U]) factors(users account.FactorUsers, store cache.Store) *account.Factors {
	e.t.Helper()
	factors, err := account.NewFactors(e.f.DB, appKey, store, nil, users)
	if err != nil {
		e.t.Fatalf("building the second factor: %v", err)
	}
	return factors
}

// register creates an account through Register, as a person at the form.
func (e *env[U]) register(users *account.Users[U], tenant, name, email string) U {
	e.t.Helper()
	user, err := users.Register(e.ctx, tenant, account.Registration{Name: name, Email: email, Password: password})
	if err != nil {
		e.t.Fatalf("registering %s in %s: %v", email, tenant, err)
	}
	return user
}

// subject is the session subject of an account.
func subject(user account.Account) auth.Subject {
	return auth.Subject{ID: user.AccountID(), Tenant: user.AccountTenant()}
}

// value reads one column of one row, as the driver hands it back.
func (e *env[U]) value(query string, args ...any) any {
	e.t.Helper()
	var out any
	if err := e.f.DB.QueryRowContext(e.ctx, query, args...).Scan(&out); err != nil {
		e.t.Fatalf("reading %q: %v", query, err)
	}
	return out
}

// count reads a count(*).
func (e *env[U]) count(query string, args ...any) int64 {
	e.t.Helper()
	var out int64
	if err := e.f.DB.QueryRowContext(e.ctx, query, args...).Scan(&out); err != nil {
		e.t.Fatalf("counting %q: %v", query, err)
	}
	return out
}

// text reads one text column of one row.
func (e *env[U]) text(query string, args ...any) string {
	e.t.Helper()
	var out sql.NullString
	if err := e.f.DB.QueryRowContext(e.ctx, query, args...).Scan(&out); err != nil {
		e.t.Fatalf("reading %q: %v", query, err)
	}
	return out.String
}

// events reads the stored events of one account, oldest first.
func (e *env[U]) events(userID string) []events.Stored {
	e.t.Helper()
	stored, err := events.NewOutbox(outboxHandle{e.f.DB}).PendingAll(e.ctx, 500)
	if err != nil {
		e.t.Fatalf("reading the outbox: %v", err)
	}
	var out []events.Stored
	for _, event := range stored {
		if event.AggregateID == userID {
			out = append(out, event)
		}
	}
	return out
}

// eventCount counts the stored events of one name for one account.
func (e *env[U]) eventCount(userID, name string) int {
	e.t.Helper()
	n := 0
	for _, event := range e.events(userID) {
		if event.Name == name {
			n++
		}
	}
	return n
}

// outboxHandle is the fixture's database as the outbox reads it.
type outboxHandle struct{ *database.DB }

func (h outboxHandle) InTransaction(ctx context.Context) bool {
	return database.InTransaction(ctx, h.DB)
}

// sealSecret encrypts a TOTP secret the way an enrolment stores it.
func sealSecret(t *testing.T, secret []byte) string {
	t.Helper()
	encrypter, err := encryption.NewEncrypter(appKey, encryption.AES256GCM)
	if err != nil {
		t.Fatalf("creating the encrypter: %v", err)
	}
	payload, err := encrypter.EncryptString(otp.EncodeSecret(secret))
	if err != nil {
		t.Fatalf("encrypting the authenticator secret: %v", err)
	}
	return payload
}

// seedFactor stores an enrolment for an account that exists, with its time
// step unspent.
func (e *env[U]) seedFactor(tenant, userID, secret string, confirmed bool) {
	e.t.Helper()
	var confirmedAt any
	if confirmed {
		confirmedAt = time.Now().UTC()
	}
	if _, err := e.f.DB.ExecContext(e.ctx, `
		INSERT INTO user_two_factor (user_id, tenant_id, secret, confirmed_at, last_used_step, created_at)
		VALUES (?, ?, ?, ?, 0, ?)`, userID, tenant, secret, confirmedAt, time.Now().UTC()); err != nil {
		e.t.Fatalf("seeding the second factor: %v", err)
	}
}

// seedRecoveryCode stores one unspent recovery code, hashed the way every
// recovery code ever issued was hashed.
func (e *env[U]) seedRecoveryCode(tenant, userID, id, code string) {
	e.t.Helper()
	hash, err := hashing.Make("arandu:two-factor-recovery:" + twofactor.NormalizeCode(code))
	if err != nil {
		e.t.Fatalf("hashing the recovery code: %v", err)
	}
	e.seedRecoveryHash(tenant, userID, id, hash)
}

func (e *env[U]) seedRecoveryHash(tenant, userID, id, hash string) {
	e.t.Helper()
	if _, err := e.f.DB.ExecContext(e.ctx, `
		INSERT INTO user_recovery_codes (id, tenant_id, user_id, code_hash, used_at, created_at)
		VALUES (?, ?, ?, ?, NULL, ?)`, id, tenant, userID, hash, time.Now().UTC()); err != nil {
		e.t.Fatalf("seeding the recovery code: %v", err)
	}
}

// codes returns the code an authenticator holding secret shows now, and one
// that is not it.
func codes(t *testing.T, secret []byte) (right, wrong string) {
	t.Helper()
	right, err := otp.Default().Generate(secret, time.Now())
	if err != nil {
		t.Fatalf("generating an authenticator code: %v", err)
	}
	digits := []byte(right)
	digits[0] = '0' + (digits[0]-'0'+1)%10
	return right, string(digits)
}

// race starts count calls at once and returns what each answered.
func race(count int, call func() error) []error {
	start := make(chan struct{})
	errs := make([]error, count)
	var ready, done sync.WaitGroup
	ready.Add(count)
	done.Add(count)
	for index := range count {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			errs[index] = call()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return errs
}

// assertOneWinner fails unless exactly one call succeeded and every other one
// lost with one of losers.
func assertOneWinner(t *testing.T, errs []error, losers ...error) {
	t.Helper()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		if !slices.ContainsFunc(losers, func(loser error) bool { return errors.Is(err, loser) }) {
			t.Errorf("a concurrent attempt returned %v, want one of %v", err, losers)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent attempts produced %d winners, want exactly one", winners)
	}
}

// isNull reports whether a column value read back is SQL NULL.
func isNull(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case []byte:
		return v == nil
	case string:
		return false
	}
	return false
}

// argon2id reports whether a stored hash was written by hashing.Make.
func argon2id(hash string) bool { return strings.HasPrefix(hash, "$argon2id$") }

// recordingThrottle is a sign-in throttle that never refuses and remembers
// which of its methods were called.
type recordingThrottle struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingThrottle) Attempt(context.Context, string, string, string) (time.Duration, bool) {
	r.note("attempt")
	return 0, true
}
func (r *recordingThrottle) Refund(context.Context, string, string, string) { r.note("refund") }
func (r *recordingThrottle) Clear(context.Context, string, string, string)  { r.note("clear") }

func (r *recordingThrottle) note(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *recordingThrottle) cleared() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.calls, "clear")
}

// failingStore is a cache store that cannot count.
type failingStore struct{ cache.Store }

func (failingStore) Increment(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errors.New("store unreachable")
}
