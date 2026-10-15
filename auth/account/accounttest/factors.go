package accounttest

import (
	"errors"
	"testing"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/otp"
)

func factorCases[U account.Account]() []testCase[U] {
	return []testCase[U]{
		{"TheSecondFactorLifecycle", secondFactorLifecycle[U]},
		{"ConcurrentAuthenticatorVerificationHasExactlyOneWinner", concurrentAuthenticator[U]},
		{"ConcurrentRecoveryRedemptionHasExactlyOneWinner", concurrentRecovery[U]},
		{"TheSecondFactorStaysInsideItsTenant", secondFactorTenant[U]},
		{"OnlyTheOwnerManagesAnEnrolment", onlyTheOwner[U]},
	}
}

// secondFactorLifecycle walks one account through every write the second
// factor makes: an unfinished enrolment that may be replaced, the confirmation
// that may happen once, recovery codes that are spent once and replaced
// together, and the removal of all of it.
func secondFactorLifecycle[U account.Account](t *testing.T, e *env[U]) {
	users := e.users(account.Hooks[U]{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	actor := subject(user)
	factors := e.factors(users, cache.NewArrayStore())

	if _, err := factors.Begin(e.ctx, actor, issuer); err != nil {
		t.Fatalf("beginning an enrolment: %v", err)
	}
	// An unfinished enrolment is replaced by the next one, and it is stored
	// unconfirmed: NULL, which is what the confirmation asks for.
	provisioning, err := factors.Begin(e.ctx, actor, issuer)
	if err != nil {
		t.Fatalf("replacing an unfinished enrolment: %v", err)
	}
	if provisioning.Account != "ana@example.test" || provisioning.Issuer != issuer {
		t.Fatalf("the enrolment names %q at %q, want the account's address", provisioning.Account, provisioning.Issuer)
	}
	if n := e.count(`SELECT count(*) FROM user_two_factor WHERE user_id = ? AND confirmed_at IS NULL`, user.AccountID()); n != 1 {
		t.Fatalf("unconfirmed enrolments = %d, want 1", n)
	}
	if required, err := factors.Required(e.ctx, tenantA, user.AccountID()); err != nil || required {
		t.Fatalf("Required before confirmation = %v, %v; want false", required, err)
	}
	if enabled, err := factors.Enabled(e.ctx, actor); err != nil || enabled {
		t.Fatalf("Enabled before confirmation = %v, %v; want false", enabled, err)
	}

	code, err := otp.Default().Generate(provisioning.Secret, time.Now())
	if err != nil {
		t.Fatalf("generating the first code: %v", err)
	}
	codes, err := factors.Confirm(e.ctx, actor, code)
	if err != nil {
		t.Fatalf("confirming the enrolment: %v", err)
	}
	if len(codes) != twofactor.DefaultRecoveryCodes {
		t.Fatalf("the confirmation returned %d recovery codes, want %d", len(codes), twofactor.DefaultRecoveryCodes)
	}
	if required, err := factors.Required(e.ctx, tenantA, user.AccountID()); err != nil || !required {
		t.Fatalf("Required after confirmation = %v, %v; want true", required, err)
	}
	if enabled, err := factors.Enabled(e.ctx, actor); err != nil || !enabled {
		t.Fatalf("Enabled after confirmation = %v, %v; want true", enabled, err)
	}
	if _, err := factors.Begin(e.ctx, actor, issuer); !errors.Is(err, account.ErrTwoFactorAlreadyEnabled) {
		t.Fatalf("beginning over a working factor answered %v, want ErrTwoFactorAlreadyEnabled", err)
	}
	if _, err := factors.Confirm(e.ctx, actor, code); !errors.Is(err, account.ErrTwoFactorAlreadyEnabled) {
		t.Fatalf("confirming twice answered %v, want ErrTwoFactorAlreadyEnabled", err)
	}
	if got := e.eventCount(user.AccountID(), account.TwoFactorEnabled); got != 1 {
		t.Fatalf("the confirmation stored %d %s events, want one", got, account.TwoFactorEnabled)
	}

	if err := factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), codes[0]); err != nil {
		t.Fatalf("spending a recovery code: %v", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), codes[0]); !errors.Is(err, twofactor.ErrInvalidCode) {
		t.Fatalf("spending a recovery code twice answered %v, want an invalid code", err)
	}

	replaced, err := factors.RegenerateRecoveryCodes(e.ctx, actor)
	if err != nil {
		t.Fatalf("regenerating the recovery codes: %v", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), codes[1]); !errors.Is(err, twofactor.ErrInvalidCode) {
		t.Fatalf("a code from the replaced set answered %v, want an invalid code", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), replaced[0]); err != nil {
		t.Fatalf("spending a code from the new set: %v", err)
	}
	if got := e.eventCount(user.AccountID(), account.RecoveryCodeUsed); got != 2 {
		t.Fatalf("two spent codes stored %d %s events", got, account.RecoveryCodeUsed)
	}

	if err := factors.Disable(e.ctx, actor); err != nil {
		t.Fatalf("disabling the factor: %v", err)
	}
	if required, err := factors.Required(e.ctx, tenantA, user.AccountID()); err != nil || required {
		t.Fatalf("Required after disabling = %v, %v; want false", required, err)
	}
	if n := e.count(`SELECT count(*) FROM user_recovery_codes WHERE user_id = ?`, user.AccountID()); n != 0 {
		t.Fatalf("recovery codes left after disabling = %d, want 0", n)
	}
	if err := factors.Disable(e.ctx, actor); !errors.Is(err, account.ErrTwoFactorNotEnrolled) {
		t.Fatalf("disabling twice answered %v, want ErrTwoFactorNotEnrolled", err)
	}
	if _, err := factors.RegenerateRecoveryCodes(e.ctx, actor); !errors.Is(err, account.ErrTwoFactorNotEnrolled) {
		t.Fatalf("regenerating without a factor answered %v, want ErrTwoFactorNotEnrolled", err)
	}
	for _, name := range []string{account.TwoFactorDisabled, account.RecoveryCodesRegenerated} {
		if got := e.eventCount(user.AccountID(), name); got != 1 {
			t.Errorf("stored %d %s events, want one", got, name)
		}
	}
	if secret := e.text(`SELECT count(*) FROM user_two_factor`); secret != "0" {
		t.Fatalf("enrolments left after disabling: %s", secret)
	}
}

func concurrentAuthenticator[U account.Account](t *testing.T, e *env[U]) {
	users := e.users(account.Hooks[U]{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	secret := otp.NewSecret()
	e.seedFactor(tenantA, user.AccountID(), sealSecret(t, secret), true)
	right, _ := codes(t, secret)
	factors := e.factors(users, cache.NewArrayStore())

	errs := race(16, func() error {
		return factors.VerifyAuthenticator(e.ctx, tenantA, user.AccountID(), right)
	})
	// Sixteen at once is more than the challenge budget, so some are refused
	// as locked before they reach the replay guard. Either refusal is a loser.
	assertOneWinner(t, errs, twofactor.ErrReplayed, account.ErrTwoFactorLocked)
}

func concurrentRecovery[U account.Account](t *testing.T, e *env[U]) {
	users := e.users(account.Hooks[U]{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	e.seedFactor(tenantA, user.AccountID(), "encrypted", true)
	recovery, err := twofactor.GenerateRecoveryCodes(1)
	if err != nil {
		t.Fatalf("generating a recovery code: %v", err)
	}
	e.seedRecoveryCode(tenantA, user.AccountID(), "recovery-a", recovery[0])
	factors := e.factors(users, cache.NewArrayStore())

	errs := race(16, func() error {
		return factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), recovery[0])
	})
	assertOneWinner(t, errs, account.ErrInvalidRecoveryCode, account.ErrTwoFactorLocked)
	if got := e.eventCount(user.AccountID(), account.RecoveryCodeUsed); got != 1 {
		t.Fatalf("one spent code stored %d %s events", got, account.RecoveryCodeUsed)
	}
}

// secondFactorTenant offers tenant-a's enrolment and recovery code under
// tenant-b, with the same user id, through every operation that takes a
// tenant or a subject, and proves none of them read, changed or spent a row.
func secondFactorTenant[U account.Account](t *testing.T, e *env[U]) {
	users := e.users(account.Hooks[U]{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	secret := otp.NewSecret()
	e.seedFactor(tenantA, user.AccountID(), sealSecret(t, secret), true)
	const recovery = "ABCDE-FGHJK"
	e.seedRecoveryCode(tenantA, user.AccountID(), "recovery-a", recovery)
	right, _ := codes(t, secret)
	factors := e.factors(users, cache.NewArrayStore())
	ghost := auth.Subject{ID: user.AccountID(), Tenant: tenantB}

	if required, err := factors.Required(e.ctx, tenantB, user.AccountID()); err != nil || required {
		t.Errorf("Required under another tenant = %v, %v; want false", required, err)
	}
	if err := factors.VerifyAuthenticator(e.ctx, tenantB, user.AccountID(), right); !errors.Is(err, account.ErrTwoFactorNotEnrolled) {
		t.Errorf("VerifyAuthenticator under another tenant answered %v, want ErrTwoFactorNotEnrolled", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantB, user.AccountID(), recovery); !errors.Is(err, account.ErrTwoFactorNotEnrolled) {
		t.Errorf("ConsumeRecovery under another tenant answered %v, want ErrTwoFactorNotEnrolled", err)
	}
	if _, err := factors.Begin(e.ctx, ghost, issuer); err == nil {
		t.Error("Begin under another tenant enrolled the account")
	}
	if err := factors.Disable(e.ctx, ghost); err == nil {
		t.Error("Disable under another tenant removed the factor")
	}
	if _, err := factors.RegenerateRecoveryCodes(e.ctx, ghost); err == nil {
		t.Error("RegenerateRecoveryCodes under another tenant replaced the codes")
	}
	if enabled, err := factors.Enabled(e.ctx, ghost); err != nil || enabled {
		t.Errorf("Enabled under another tenant = %v, %v; want false", enabled, err)
	}

	if step := e.count(`SELECT last_used_step FROM user_two_factor WHERE user_id = ?`, user.AccountID()); step != 0 {
		t.Errorf("another tenant spent time step %d", step)
	}
	if n := e.count(`SELECT count(*) FROM user_recovery_codes WHERE tenant_id = ? AND used_at IS NULL`, tenantA); n != 1 {
		t.Errorf("tenant-a unspent recovery codes = %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM user_two_factor WHERE tenant_id = ? AND confirmed_at IS NOT NULL`, tenantA); n != 1 {
		t.Errorf("tenant-a confirmed enrolments = %d, want 1", n)
	}
	// The account's own tenant still passes with the same code.
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, user.AccountID(), right); err != nil {
		t.Fatalf("the account's own tenant answered %v", err)
	}
}

func onlyTheOwner[U account.Account](t *testing.T, e *env[U]) {
	users := e.users(account.Hooks[U]{}, nil)
	ana := e.register(users, tenantA, "Ana", "ana@example.test")
	factors := e.factors(users, cache.NewArrayStore())
	if _, err := factors.Begin(e.ctx, auth.Subject{Tenant: tenantA}, issuer); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("an anonymous subject answered %v, want auth.ErrForbidden", err)
	}
	if _, err := factors.Begin(e.ctx, auth.Guest(tenantA), issuer); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a guest answered %v, want auth.ErrForbidden", err)
	}
	if _, err := factors.Begin(e.ctx, auth.Subject{ID: "nobody", Tenant: tenantA}, issuer); !errors.Is(err, account.ErrUserNotFound) && !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a subject with no account answered %v, want not found or forbidden", err)
	}
	if n := e.count(`SELECT count(*) FROM user_two_factor`); n != 0 {
		t.Fatalf("refused enrolments left %d rows", n)
	}
	if _, err := factors.Begin(e.ctx, subject(ana), issuer); err != nil {
		t.Fatalf("the owner answered %v", err)
	}
	if _, err := account.NewFactors(e.f.DB, appKey, nil, nil, users); err == nil {
		t.Fatal("the second factor was built with nothing to count challenge attempts in")
	}
	if _, err := account.NewFactors(e.f.DB, []byte("short"), cache.NewArrayStore(), nil, users); err == nil {
		t.Fatal("the second factor was built with a key AES-256 cannot use")
	}
}
