package account_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/validation"
)

// What this file proves runs without a database: the strings an application
// already stores and compares, and that every refusal happens before the first
// statement. What the rules do to rows is proved by accounttest, which an
// application and the connector modules run against a real one.

// member is a users table row as an application declares it.
type member struct {
	model.Model

	ID         string     `db:"id"`
	TenantID   string     `db:"tenant_id"`
	Name       string     `db:"name"`
	Email      string     `db:"email"`
	Password   string     `db:"password"`
	VerifiedAt *time.Time `db:"verified_at"`
	CreatedAt  time.Time  `db:"created_at"`
}

var members = model.NewTable(model.TableSpec{
	Name:            "users",
	New:             func() model.Entity { return new(member) },
	UniqueIDs:       true,
	UpdatedAtColumn: model.NoColumn,
})

func (m *member) AccountID() string     { return m.ID }
func (m *member) AccountTenant() string { return m.TenantID }
func (m *member) AccountEmail() string  { return m.Email }
func (m *member) AccountName() string   { return m.Name }
func (m *member) PasswordHash() string  { return m.Password }
func (m *member) IsVerified() bool      { return m.VerifiedAt != nil }

// memberPolicy lets a guest register and a subject act on itself.
type memberPolicy struct{}

func (memberPolicy) Can(_ context.Context, s auth.Subject, a auth.Action, m *member) error {
	if m.TenantID != "" && m.TenantID != s.Tenant {
		return errors.New("another tenant")
	}
	switch {
	case a == account.ActionUserCreate && s.IsGuest():
		return nil
	case a == account.ActionUserNamesPublic && s.ID != "":
		return nil
	case (a == account.ActionUserView || a == account.ActionUserUpdate) && s.ID != "" && s.ID == m.ID:
		return nil
	}
	return fmt.Errorf("insufficient role for %s", a)
}

// statementCounter is a database/sql driver that refuses every statement and
// counts them, so a test can say a refusal happened before the first one.
type statementCounter struct{ n atomic.Int64 }

func (c *statementCounter) Open(string) (driver.Conn, error) { return counterConn{c}, nil }
func (c *statementCounter) Connect(context.Context) (driver.Conn, error) {
	return counterConn{c}, nil
}
func (c *statementCounter) Driver() driver.Driver { return c }

type counterConn struct{ c *statementCounter }

var errStatement = errors.New("a statement reached the database")

func (k counterConn) Prepare(string) (driver.Stmt, error) { k.c.n.Add(1); return nil, errStatement }
func (k counterConn) Close() error                        { return nil }
func (k counterConn) Begin() (driver.Tx, error)           { k.c.n.Add(1); return nil, errStatement }

// noDatabase is a handle on which every statement fails and is counted.
func noDatabase(t *testing.T) (*database.DB, *statementCounter) {
	t.Helper()
	counter := &statementCounter{}
	handle := sql.OpenDB(counter)
	t.Cleanup(func() { _ = handle.Close() })
	return database.Wrap(handle, database.DialectSQLite), counter
}

func newUsers(t *testing.T, db *database.DB, hooks account.Hooks) *account.Users {
	t.Helper()
	users, err := account.NewUsers(account.Config{
		DB: db, Query: func() *model.Builder { return members.Query(db) }, Policy: account.PolicyOf(memberPolicy{}), Hooks: hooks,
	})
	if err != nil {
		t.Fatalf("NewUsers: %v", err)
	}
	return users
}

func TestNewUsersRefusesAConfigItCannotAuthorizeWith(t *testing.T) {
	db, _ := noDatabase(t)
	query := func() *model.Builder { return members.Query(db) }
	for name, cfg := range map[string]account.Config{
		"no database": {Query: query, Policy: account.PolicyOf(memberPolicy{})},
		"no query":    {DB: db, Policy: account.PolicyOf(memberPolicy{})},
		"no policy":   {DB: db, Query: query},
	} {
		if _, err := account.NewUsers(cfg); err == nil {
			t.Errorf("a config with %s built the account rules", name)
		}
	}
}

func TestNewFactorsRefusesWhatTheChallengeCannotRunWithout(t *testing.T) {
	db, _ := noDatabase(t)
	users := newUsers(t, db, account.Hooks{})
	key := []byte("0123456789abcdef0123456789abcdef")
	store := cache.NewArrayStore()
	for name, build := range map[string]func() (*account.Factors, error){
		"no database":         func() (*account.Factors, error) { return account.NewFactors(nil, key, store, nil, users) },
		"no attempts store":   func() (*account.Factors, error) { return account.NewFactors(db, key, nil, nil, users) },
		"no users":            func() (*account.Factors, error) { return account.NewFactors(db, key, store, nil, nil) },
		"a key AES-256 lacks": func() (*account.Factors, error) { return account.NewFactors(db, key[:16], store, nil, users) },
	} {
		if _, err := build(); err == nil {
			t.Errorf("the second factor was built with %s", name)
		}
	}
}

// TestRefusalsHappenBeforeTheFirstStatement: a refusal that ran a query first
// is a refusal that read somebody's row on the way.
func TestRefusalsHappenBeforeTheFirstStatement(t *testing.T) {
	db, counter := noDatabase(t)
	prepared := errors.New("refused by the application")
	users := newUsers(t, db, account.Hooks{})
	preparing := newUsers(t, db, account.Hooks{Prepare: func(context.Context, *account.Registration, account.Account) error { return prepared }})
	factors, err := account.NewFactors(db, []byte("0123456789abcdef0123456789abcdef"), failingStore{cache.NewArrayStore()}, nil, users)
	if err != nil {
		t.Fatalf("NewFactors: %v", err)
	}
	ctx := context.Background()
	nobody := auth.Subject{}
	guest := auth.Guest("tenant-a")
	ana := auth.Subject{ID: "ana", Tenant: "tenant-a"}
	valid := account.Registration{Name: "Ana", Email: "ana@example.test", Password: "a-long-enough-password"}

	for name, call := range map[string]func() error{
		"public names for nobody": func() error { _, err := users.PublicNames(ctx, nobody, []string{"a"}); return err },
		"a registration with no name": func() error {
			_, err := users.Register(ctx, "tenant-a", account.Registration{Email: "a@b.c", Password: valid.Password})
			return err
		},
		"a registration with a short secret": func() error {
			_, err := users.Register(ctx, "tenant-a", account.Registration{Name: "A", Email: "a@b.c", Password: "short"})
			return err
		},
		"a registration Prepare refused": func() error { _, err := preparing.Register(ctx, "tenant-a", valid); return err },
		"a draft with an unreadable hash": func() error {
			_, err := users.Provision(ctx, "tenant-a", account.Draft{Email: "a@b.c", PasswordHash: "plain-text-password"})
			return err
		},
		"a draft with no password": func() error {
			_, err := users.Provision(ctx, "tenant-a", account.Draft{Email: "a@b.c"})
			return err
		},
		"renaming nobody":      func() error { _, err := users.Rename(ctx, nobody, "Mallory"); return err },
		"renaming to nothing":  func() error { _, err := users.Rename(ctx, ana, " "); return err },
		"a guest renaming":     func() error { _, err := users.Rename(ctx, guest, "Mallory"); return err },
		"a short new password": func() error { _, err := users.ChangePassword(ctx, ana, "x", "short", "ip"); return err },
		// Seven characters and fourteen bytes: counted in bytes it passed,
		// and the current password was checked and the throttle cleared
		// before hashing refused it.
		"a short new password of accented characters": func() error {
			_, err := users.ChangePassword(ctx, ana, "x", "ççççççç", "ip")
			return err
		},
		"a new password over the longest": func() error {
			_, err := users.ChangePassword(ctx, ana, "x", strings.Repeat("a", 129), "ip")
			return err
		},
		"confirming for nobody":      func() error { return users.ConfirmPassword(ctx, nobody, "x", "ip") },
		"closing for nobody":         func() error { return users.Close(ctx, nobody, "x", "ip") },
		"enrolling nobody":           func() error { _, err := factors.Begin(ctx, nobody, "Arandu"); return err },
		"enrolling a guest":          func() error { _, err := factors.Begin(ctx, guest, "Arandu"); return err },
		"confirming for nobody's":    func() error { _, err := factors.Confirm(ctx, nobody, "123456"); return err },
		"disabling nobody's":         func() error { return factors.Disable(ctx, nobody) },
		"regenerating nobody's":      func() error { _, err := factors.RegenerateRecoveryCodes(ctx, nobody); return err },
		"asking whether nobody has":  func() error { _, err := factors.Enabled(ctx, nobody); return err },
		"an uncounted authenticator": func() error { return factors.VerifyAuthenticator(ctx, "tenant-a", "ana", "123456") },
		"an uncounted recovery code": func() error { return factors.ConsumeRecovery(ctx, "tenant-a", "ana", "ABCDE-FGHJK") },
		"a factor read with no tenant": func() error {
			_, err := factors.Required(ctx, "", "ana")
			return err
		},
		"an account read with no tenant": func() error { _, err := users.FindForAuthentication(ctx, "", "ana"); return err },
		"a lookup with no tenant":        func() error { _, err := users.Lookup(ctx, "", "a@b.c"); return err },
	} {
		before := counter.n.Load()
		if err := call(); err == nil || errors.Is(err, errStatement) {
			t.Errorf("%s answered %v, want a refusal of its own", name, err)
		}
		if after := counter.n.Load(); after != before {
			t.Errorf("%s ran %d statements before it refused", name, after-before)
		}
	}
}

// failingStore is a cache store that cannot count.
type failingStore struct{ cache.Store }

func (failingStore) Increment(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errors.New("store unreachable")
}

// TestTheStringsAreTheOnesAlreadyStored: event names are in outbox rows and in
// every consumer, actions in every application's policy, and error messages in
// logs and screens. Each was in use before this package existed.
func TestTheStringsAreTheOnesAlreadyStored(t *testing.T) {
	for got, want := range map[string]string{
		string(account.ActionUserView):                "user.view",
		string(account.ActionUserCreate):              "user.create",
		string(account.ActionUserUpdate):              "user.update",
		string(account.ActionUserDelete):              "user.delete",
		string(account.ActionUserNamesPublic):         "user.names.public",
		string(account.ActionTwoFactorRead):           "user.two-factor.read",
		string(account.ActionTwoFactorManage):         "user.two-factor.manage",
		account.UserRegistered:                        "auth.user.registered",
		account.EmailVerified:                         "auth.email.verified",
		account.PasswordReset:                         "auth.password.reset",
		account.PasswordChanged:                       "auth.password.changed",
		account.ProfileUpdated:                        "auth.user.profile_updated",
		account.AccountClosed:                         "auth.user.closed",
		account.TwoFactorEnabled:                      "auth.two_factor.enabled",
		account.TwoFactorDisabled:                     "auth.two_factor.disabled",
		account.RecoveryCodesRegenerated:              "auth.two_factor.recovery_codes.regenerated",
		account.RecoveryCodeUsed:                      "auth.two_factor.recovery_code.used",
		account.Aggregate:                             "user",
		account.ErrEmailTaken.Error():                 "user: email already registered in this tenant",
		account.ErrVerificationAddressChanged.Error(): "user: verification was issued for a different address",
		account.ErrResetLinkSpent.Error():             "user: password reset is no longer valid",
		account.ErrTwoFactorNotEnrolled.Error():       "two-factor: this account has no enrolment",
		account.ErrTwoFactorAlreadyEnabled.Error():    "two-factor: this account already has an enabled enrolment",
		account.ErrInvalidRecoveryCode.Error():        "twofactor: the code is not valid: recovery code is wrong or already spent",
		account.ErrTwoFactorLocked.Error():            "two-factor: too many codes offered to the sign-in challenge",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if account.ErrUserNotFound != model.ErrModelNotFound {
		t.Error("ErrUserNotFound is not the model's not-found sentinel, so a lookup's error would stop matching it")
	}
	if got := account.Migrations(); len(got) != 1 || got[0].GetName() != "20260828_0003_create_two_factor" {
		t.Fatalf("Migrations() names %v, want the one name applications already recorded", got)
	}
	if _, ok := account.Migrations()[0].(migrations.ReversibleMigration); !ok {
		t.Error("the migration cannot be rolled back")
	}
	if account.ChallengeAttempts != 5 || account.ChallengeWindow != 15*time.Minute {
		t.Errorf("the challenge budget is %d in %s, want 5 in 15m", account.ChallengeAttempts, account.ChallengeWindow)
	}
}

func TestTheEventPayloadIsTheJSONConsumersRead(t *testing.T) {
	encoded, err := json.Marshal(account.UserEvent{UserID: "u1", Tenant: "acme", Email: "a@b.c"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"user_id":"u1","tenant_id":"acme","email":"a@b.c"}`; string(encoded) != want {
		t.Fatalf("payload = %s, want %s", encoded, want)
	}
	encoded, _ = json.Marshal(account.UserEvent{UserID: "u1", Tenant: "acme", Email: "a@b.c", Name: "Ana"})
	if want := `{"user_id":"u1","tenant_id":"acme","email":"a@b.c","name":"Ana"}`; string(encoded) != want {
		t.Fatalf("payload = %s, want %s", encoded, want)
	}
}

func TestErrorsMatchWhatTheirCallersAlreadyCheck(t *testing.T) {
	if !errors.Is(account.ErrAccountRefused, auth.ErrInvalidCredentials) {
		t.Error("ErrAccountRefused does not match auth.ErrInvalidCredentials, so a sign-in form would tell a suspension from a wrong password")
	}
	if !errors.Is(account.ErrInvalidRecoveryCode, twofactor.ErrInvalidCode) {
		t.Error("ErrInvalidRecoveryCode does not match twofactor.ErrInvalidCode")
	}
	locked := error(account.TwoFactorLockedError{RetryAfter: 1500 * time.Millisecond})
	if !errors.Is(locked, account.ErrTwoFactorLocked) || !errors.Is(locked, twofactor.ErrInvalidCode) {
		t.Errorf("the lock matches neither sentinel: %v", locked)
	}
	if got, want := locked.Error(), "two-factor: too many codes offered to the sign-in challenge, sign in again in 2 seconds"; got != want {
		t.Errorf("lock message %q, want %q", got, want)
	}
	tooMany := account.TooManyAttemptsError{RetryAfter: 0}
	if got, want := tooMany.Error(), "user: too many attempts, try again in 1 seconds"; got != want {
		t.Errorf("throttle message %q, want %q", got, want)
	}
	if tooMany.HTTPStatus() != http.StatusTooManyRequests || (account.TwoFactorLockedError{}).HTTPStatus() != http.StatusTooManyRequests {
		t.Error("a refusal for too many attempts is not answered 429")
	}
	if (account.TooManyAttemptsError{RetryAfter: time.Minute}).Seconds() != 60 {
		t.Error("a minute is not sixty seconds of Retry-After")
	}
	var refused validation.Errors
	db, _ := noDatabase(t)
	if _, err := newUsers(t, db, account.Hooks{}).Rename(context.Background(), auth.Subject{ID: "a", Tenant: "t"}, ""); !errors.As(err, &refused) || refused.First("name") == "" {
		t.Errorf("an empty name answered %v, want a validation error on name", err)
	}
}

// TestTheFingerprintIsTheOneLinksInFlightCarry: a reset link and a pending
// sign-in minted before an application moved to this package carry the
// fingerprint of the hash they were minted over, computed by the code the
// application had then.
func TestTheFingerprintIsTheOneLinksInFlightCarry(t *testing.T) {
	var rows struct {
		Hash        string            `json:"password_hash"`
		Fingerprint string            `json:"password_fingerprint"`
		More        map[string]string `json:"fingerprints"`
	}
	raw, err := os.ReadFile("accounttest/skeleton_rows.json")
	if err != nil {
		t.Fatalf("reading the stored rows: %v", err)
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decoding the stored rows: %v", err)
	}
	if got := account.Fingerprint(rows.Hash); got != rows.Fingerprint {
		t.Fatalf("Fingerprint = %q, the links in flight carry %q", got, rows.Fingerprint)
	}
	// These two hold the characters base64url spells differently from base64,
	// which a fingerprint with neither cannot tell apart.
	if len(rows.More) != 2 {
		t.Fatalf("the stored rows carry %d more fingerprints, want 2", len(rows.More))
	}
	for hash, want := range rows.More {
		if got := account.Fingerprint(hash); got != want {
			t.Errorf("Fingerprint(%q) = %q, the skeleton wrote %q", hash, got, want)
		}
	}
	if account.NormalizeEmail("  Ana@Example.TEST ") != "ana@example.test" {
		t.Error("an address is not normalized the way every stored one was")
	}
}

// TestPolicyOfForwardsTheApplicationsPolicyOverItsOwnType: the application's
// policy decides about its own *User, and an account of another type is
// refused rather than handed to it.
func TestPolicyOfForwardsTheApplicationsPolicyOverItsOwnType(t *testing.T) {
	policy := account.PolicyOf(memberPolicy{})
	ctx := context.Background()
	mine := &member{ID: "ana", TenantID: "acme"}
	if err := policy.Can(ctx, auth.Subject{ID: "ana", Tenant: "acme"}, account.ActionUserUpdate, mine); err != nil {
		t.Fatalf("the owner was refused: %v", err)
	}
	if err := policy.Can(ctx, auth.Subject{ID: "bea", Tenant: "acme"}, account.ActionUserUpdate, mine); err == nil {
		t.Fatal("the adapter allowed what the application's policy refuses")
	}
	if err := policy.Can(ctx, auth.Subject{ID: "ana", Tenant: "acme"}, account.ActionUserUpdate, &stranger{}); err == nil {
		t.Fatal("an account of another type was decided about")
	}
	if _, err := auth.Authorize(ctx, auth.Policy[account.Account](policy), auth.Subject{ID: "ana", Tenant: "acme"}, account.ActionUserView, account.Account(mine)); err != nil {
		t.Fatalf("Authorize through the adapter: %v", err)
	}
}

// stranger is an Account that is not the application's user type.
type stranger struct{ member }

// TestARegistrationAndADraftNeverPrintThePassword: both carry the password in
// clear, and a handler that logged one, or wrapped it in an error, printed it.
func TestARegistrationAndADraftNeverPrintThePassword(t *testing.T) {
	const secret = "a-password-nobody-may-read"
	registration := account.Registration{Name: "Ana", Email: "ana@example.test", Password: secret, Extra: map[string]string{"cpf": "12345678909"}}
	draft := account.Draft{Name: "Ana", Email: "ana@example.test", Password: secret, PasswordHash: "$argon2id$" + secret}

	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, nil))
	logger.Info("registration", "registration", registration, "pointer", &registration, "draft", draft)
	printed := logged.String()
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		printed += fmt.Sprintf(verb, registration) + fmt.Sprintf(verb, &registration) + fmt.Sprintf(verb, draft) + fmt.Sprintf(verb, &draft)
	}
	for _, leaked := range []string{secret, "ana@example.test", "12345678909"} {
		if strings.Contains(printed, leaked) {
			t.Fatalf("a registration or a draft printed %q:\n%s", leaked, printed)
		}
	}
	if !strings.Contains(logged.String(), `"password_supplied":true`) {
		t.Fatalf("the log does not say a password arrived: %s", logged.String())
	}
}
