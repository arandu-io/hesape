package accounttest

import (
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/otp"
)

// skeletonRows are values an application wrote before this package existed:
// a password hash and the fingerprint its reset links carried, a TOTP secret
// sealed under the application key, and a hashed recovery code. They were
// written by that code and are never regenerated, so a change to any format
// here fails this suite instead of locking every enrolled account out.
//
//go:embed skeleton_rows.json
var skeletonRows []byte

// rows is the decoded form of the stored compatibility fixture.
type rows struct {
	AppKey               string            `json:"app_key"`
	Password             string            `json:"password"`
	PasswordHash         string            `json:"password_hash"`
	PasswordFingerprint  string            `json:"password_fingerprint"`
	TOTPSecretBase32     string            `json:"totp_secret_base32"`
	TOTPSecretCiphertext string            `json:"totp_secret_ciphertext"`
	RecoveryCode         string            `json:"recovery_code"`
	RecoveryCodeHash     string            `json:"recovery_code_hash"`
	Fingerprints         map[string]string `json:"fingerprints"`
}

func loadRows(t *testing.T) rows {
	t.Helper()
	var r rows
	if err := json.Unmarshal(skeletonRows, &r); err != nil {
		t.Fatalf("reading the stored rows: %v", err)
	}
	if r.AppKey != string(appKey) {
		t.Fatalf("the stored rows were sealed under another key")
	}
	return r
}

func compatibilityCases[U account.Account]() []testCase[U] {
	return []testCase[U]{
		{"RowsWrittenBeforeThisPackageStillSignIn", compatibilitySignIn[U]},
	}
}

// compatibilitySignIn stores the rows as they were written and signs the
// account in through every factor: the password, the fingerprint a reset link
// carried, the authenticator, and the recovery code.
func compatibilitySignIn[U account.Account](t *testing.T, e *env[U]) {
	r := loadRows(t)
	users := e.users(account.Hooks[U]{}, nil)
	user := e.register(users, tenantA, "Ana", "ana@example.test")
	if _, err := e.f.DB.ExecContext(e.ctx, `UPDATE users SET password = ? WHERE id = ?`, r.PasswordHash, user.AccountID()); err != nil {
		t.Fatalf("storing the old password hash: %v", err)
	}
	e.seedFactor(tenantA, user.AccountID(), r.TOTPSecretCiphertext, true)
	e.seedRecoveryHash(tenantA, user.AccountID(), "recovery-old", r.RecoveryCodeHash)
	factors := e.factors(users, cache.NewArrayStore())

	signedIn, err := users.VerifyCredentials(e.ctx, tenantA, "ana@example.test", r.Password, "127.0.0.1")
	if err != nil {
		t.Fatalf("the old password hash answered %v", err)
	}
	if got := account.Fingerprint(signedIn.PasswordHash()); got != r.PasswordFingerprint {
		t.Fatalf("the old hash's fingerprint is %q, the links in flight carry %q", got, r.PasswordFingerprint)
	}
	for hash, want := range r.Fingerprints {
		if got := account.Fingerprint(hash); got != want {
			t.Errorf("Fingerprint(%q) = %q, the links in flight carry %q", hash, got, want)
		}
	}
	if required, err := factors.Required(e.ctx, tenantA, user.AccountID()); err != nil || !required {
		t.Fatalf("Required for the old enrolment = %v, %v", required, err)
	}

	secret, err := otp.DecodeSecret(r.TOTPSecretBase32)
	if err != nil {
		t.Fatalf("decoding the old secret: %v", err)
	}
	code, err := otp.Default().Generate(secret, time.Now())
	if err != nil {
		t.Fatalf("generating a code: %v", err)
	}
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, user.AccountID(), code); err != nil {
		t.Fatalf("the old sealed secret answered %v", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantA, user.AccountID(), " abcde fghjk "); err != nil {
		t.Fatalf("the old recovery code, typed as a person types it, answered %v", err)
	}

	if _, err := users.ResetPassword(e.ctx, tenantA, user.AccountID(), "ana@example.test", r.PasswordFingerprint, "a-brand-new-password"); err != nil {
		t.Fatalf("a reset link minted before the move answered %v", err)
	}
}
