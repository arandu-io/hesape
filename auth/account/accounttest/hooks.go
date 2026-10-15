package accounttest

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/hashing"
)

func hookCases() []testCase {
	return []testCase{
		{"AdmitRefusesASignInAsInvalidCredentials", admitRefuses},
		{"AdmitClearsDecidesWhetherARefusalCounts", admitClears},
		{"AdmitRefusesAResetLinkAsSpent", admitRefusesReset},
		{"PrepareCanRefuseARegistration", prepareRefuses},
		{"PrepareCanCompleteARegistration", prepareFills},
		{"AFailingAfterCreateRollsBackTheAccountAndItsEvent", afterCreateRollsBack},
		{"AfterCreateRunsInTheTransactionOfTheInsert", afterCreateInTransaction},
		{"ProvisionAcceptsAnImportedHashOnlyWhenItCanBeRead", provisionImportedHash},
		{"CloseFieldsAndPayloadAreTheApplications", closeFieldsAndPayload},
	}
}

var errSuspended = errors.New("the account is suspended")

func admitRefuses(t *testing.T, e *env) {
	var admitted []string
	users := e.users(account.Hooks{Admit: func(_ context.Context, user account.Account) error {
		admitted = append(admitted, user.AccountID())
		if user.AccountName() == "Suspended" {
			return errSuspended
		}
		return nil
	}}, nil)
	e.register(users, tenantA, "Suspended", "sus@example.test")
	fine := e.register(users, tenantA, "Fine", "fine@example.test")

	_, err := users.VerifyCredentials(e.ctx, tenantA, "sus@example.test", password, "127.0.0.1")
	if !errors.Is(err, auth.ErrInvalidCredentials) || !errors.Is(err, account.ErrAccountRefused) || !errors.Is(err, errSuspended) {
		t.Fatalf("a refused account answered %v, want ErrAccountRefused matching auth.ErrInvalidCredentials and the hook's reason", err)
	}
	if err.Error() != account.ErrAccountRefused.Error() {
		t.Fatalf("the refusal reads %q, want %q: the reason is for the log, not the form", err, account.ErrAccountRefused)
	}
	if _, err := users.VerifyCredentials(e.ctx, tenantA, "sus@example.test", "not-the-password-at-all", "127.0.0.1"); !errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, account.ErrAccountRefused) {
		t.Fatalf("a wrong password for a refused account answered %v, want plain invalid credentials", err)
	}
	if user, err := users.VerifyCredentials(e.ctx, tenantA, "fine@example.test", password, "127.0.0.1"); err != nil || user.AccountID() != fine.AccountID() {
		t.Fatalf("an admitted account answered %v", err)
	}
	if len(admitted) != 2 {
		t.Fatalf("Admit ran %d times, want twice: once per right password and never for a wrong one", len(admitted))
	}
}

func admitClears(t *testing.T, e *env) {
	refuse := func(context.Context, account.Account) error { return errSuspended }
	for _, clears := range []bool{false, true} {
		throttle := &recordingThrottle{}
		users := e.users(account.Hooks{Admit: refuse, AdmitClears: clears}, throttle)
		email := "counted@example.test"
		if clears {
			email = "cleared@example.test"
		}
		e.register(users, tenantA, "Ana", email)
		if _, err := users.VerifyCredentials(e.ctx, tenantA, email, password, "127.0.0.1"); !errors.Is(err, account.ErrAccountRefused) {
			t.Fatalf("the refused sign-in answered %v", err)
		}
		if throttle.cleared() != clears {
			t.Fatalf("with AdmitClears %v the throttle was cleared = %v", clears, throttle.cleared())
		}
	}
}

func admitRefusesReset(t *testing.T, e *env) {
	users := e.users(account.Hooks{Admit: func(context.Context, account.Account) error { return errSuspended }}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	_, err := users.ResetPassword(e.ctx, tenantA, registered.AccountID(), registered.AccountEmail(),
		account.Fingerprint(registered.PasswordHash()), "a-brand-new-password")
	if !errors.Is(err, account.ErrResetLinkSpent) {
		t.Fatalf("resetting a refused account answered %v, want ErrResetLinkSpent", err)
	}
	if stored := e.text(`SELECT password FROM users WHERE id = ?`, registered.AccountID()); stored != registered.PasswordHash() {
		t.Fatal("a refused account's password was replaced")
	}
}

func prepareRefuses(t *testing.T, e *env) {
	refused := errors.New("the document number is not valid")
	users := e.users(account.Hooks{Prepare: func(_ context.Context, r *account.Registration, _ account.Account) error {
		if r.Extra != "valid" {
			return refused
		}
		return nil
	}}, nil)
	_, err := users.Register(e.ctx, tenantA, account.Registration{Name: "Ana", Email: "ana@example.test", Password: password, Extra: "forged"})
	if !errors.Is(err, refused) {
		t.Fatalf("a registration Prepare refused answered %v, want its error", err)
	}
	if n := e.count(`SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("a refused registration stored %d rows", n)
	}
	if _, err := users.Register(e.ctx, tenantA, account.Registration{Name: "Ana", Email: "ana@example.test", Password: password, Extra: "valid"}); err != nil {
		t.Fatalf("a registration Prepare accepted answered %v", err)
	}
}

func prepareFills(t *testing.T, e *env) {
	var tenantSeen string
	users := e.users(account.Hooks{Prepare: func(_ context.Context, r *account.Registration, user account.Account) error {
		tenantSeen = user.AccountTenant()
		if r.Name == "" {
			r.Name = "Personal account"
		}
		return nil
	}}, nil)
	user, err := users.Register(e.ctx, tenantA, account.Registration{Email: "ana@example.test", Password: password})
	if err != nil {
		t.Fatalf("a registration Prepare completed answered %v", err)
	}
	if user.AccountName() != "Personal account" || e.text(`SELECT name FROM users WHERE id = ?`, user.AccountID()) != "Personal account" {
		t.Fatalf("the account was stored named %q, want the name Prepare gave it", user.AccountName())
	}
	if tenantSeen != tenantA {
		t.Fatalf("Prepare saw an account in tenant %q, want %s", tenantSeen, tenantA)
	}
}

func afterCreateRollsBack(t *testing.T, e *env) {
	failed := errors.New("the personal organization could not be created")
	var created string
	users := e.users(account.Hooks{AfterCreate: func(_ context.Context, g auth.Grant, user account.Account) error {
		created = user.AccountID()
		if auth.Tenant(g) != tenantA || g.Action() != account.ActionUserCreate {
			t.Errorf("AfterCreate got a Grant for %s in %q", g.Action(), auth.Tenant(g))
		}
		return failed
	}}, nil)
	_, err := users.Register(e.ctx, tenantA, account.Registration{Name: "Ana", Email: "ana@example.test", Password: password})
	if !errors.Is(err, failed) {
		t.Fatalf("the registration answered %v, want AfterCreate's error", err)
	}
	if created == "" {
		t.Fatal("AfterCreate saw no stored account")
	}
	if n := e.count(`SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("a failed AfterCreate left %d account rows", n)
	}
	if n := e.count(`SELECT count(*) FROM outbox`); n != 0 {
		t.Fatalf("a failed AfterCreate left %d outbox rows", n)
	}

	_, err = users.Provision(e.ctx, tenantA, account.Draft{Name: "Ana", Email: "ana@example.test", Password: password})
	if !errors.Is(err, failed) {
		t.Fatalf("provisioning answered %v, want AfterCreate's error", err)
	}
	if n := e.count(`SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("a failed AfterCreate left %d provisioned rows", n)
	}
}

func afterCreateInTransaction(t *testing.T, e *env) {
	users := e.users(account.Hooks{AfterCreate: func(ctx context.Context, _ auth.Grant, user account.Account) error {
		var n int64
		// The account is visible to a statement on the same context, which is
		// what an application's own insert beside it needs.
		if err := e.f.DB.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = ?`, user.AccountID()).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("the account is not visible inside AfterCreate")
		}
		return nil
	}}, nil)
	e.register(users, tenantA, "Ana", "ana@example.test")
}

func provisionImportedHash(t *testing.T, e *env) {
	users := e.users(account.Hooks{}, nil)
	imported := bcryptOf(t, password)
	user, err := users.Provision(e.ctx, tenantA, account.Draft{Name: "Ana", Email: "ana@example.test", PasswordHash: imported})
	if err != nil {
		t.Fatalf("provisioning a bcrypt hash: %v", err)
	}
	if stored := e.text(`SELECT password FROM users WHERE id = ?`, user.AccountID()); stored != imported {
		t.Fatalf("the imported hash was stored as %q", stored)
	}
	broken := imported[:len(imported)-10]
	if _, err := users.Provision(e.ctx, tenantA, account.Draft{Name: "Bea", Email: "bea@example.test", PasswordHash: broken}); err == nil {
		t.Fatal("a bcrypt hash the hashing package cannot read was provisioned")
	}
	if n := e.count(`SELECT count(*) FROM users WHERE email = ?`, "bea@example.test"); n != 0 {
		t.Fatal("the refused draft left a row")
	}
}

func closeFieldsAndPayload(t *testing.T, e *env) {
	users := e.users(account.Hooks{
		CloseFields: func(account.Account) map[string]any { return map[string]any{"name": "Gone"} },
		Payload:     func(user account.Account) any { return map[string]string{"who": user.AccountID()} },
	}, nil)
	registered := e.register(users, tenantA, "Ana", "ana@example.test")
	if err := users.Close(e.ctx, subject(registered), password, "127.0.0.1"); err != nil {
		t.Fatalf("closing: %v", err)
	}
	if name := e.text(`SELECT name FROM users WHERE id = ?`, registered.AccountID()); name != "Gone" {
		t.Fatalf("the closed account is named %q, want what CloseFields wrote", name)
	}
	want := `{"who":"` + registered.AccountID() + `"}`
	for _, event := range e.events(registered.AccountID()) {
		if event.Payload != want {
			t.Errorf("%s carries %s, want the payload Payload made", event.Name, event.Payload)
		}
	}
}

// bcryptOf is the hash an import from a bcrypt users table holds.
func bcryptOf(t *testing.T, plain string) string {
	t.Helper()
	hash, err := hashing.NewBcryptHasher().Make(plain)
	if err != nil {
		t.Fatalf("hashing with bcrypt: %v", err)
	}
	return hash
}
