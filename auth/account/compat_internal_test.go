package account

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/otp"
)

// These read the formats a stored row is in, which are unexported on purpose:
// nothing outside the package writes them. Each is held against values the
// skeleton's own code wrote before this package existed, so a change here
// fails a test instead of locking enrolled accounts out.

type storedRows struct {
	AppKey               string `json:"app_key"`
	TOTPSecretBase32     string `json:"totp_secret_base32"`
	TOTPSecretCiphertext string `json:"totp_secret_ciphertext"`
	RecoveryCode         string `json:"recovery_code"`
	RecoveryCodeHash     string `json:"recovery_code_hash"`
}

func readStoredRows(t *testing.T) storedRows {
	t.Helper()
	raw, err := os.ReadFile("accounttest/skeleton_rows.json")
	if err != nil {
		t.Fatalf("reading the stored rows: %v", err)
	}
	var rows storedRows
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decoding the stored rows: %v", err)
	}
	return rows
}

func TestAStoredRecoveryHashStillMatchesItsCode(t *testing.T) {
	rows := readStoredRows(t)
	for _, typed := range []string{rows.RecoveryCode, " abcde fghjk ", "ABCDEFGHJK"} {
		if !recoveryCodeMatches(typed, rows.RecoveryCodeHash) {
			t.Errorf("the stored recovery hash does not match %q", typed)
		}
	}
	if recoveryCodeMatches("ABCDE-FGHJL", rows.RecoveryCodeHash) {
		t.Error("the stored recovery hash matched another code")
	}
	if recoveryCodePrefix != "arandu:two-factor-recovery:" {
		t.Errorf("the recovery prefix is %q; every code ever issued was hashed under arandu:two-factor-recovery:", recoveryCodePrefix)
	}
	hash, err := hashRecoveryCode(rows.RecoveryCode)
	if err != nil || !recoveryCodeMatches(rows.RecoveryCode, hash) {
		t.Fatalf("a freshly issued code does not match its own hash (%v)", err)
	}
}

func TestAStoredSealedSecretStillOpens(t *testing.T) {
	rows := readStoredRows(t)
	factors, err := NewFactors(database.Wrap(nil, database.DialectSQLite), []byte(rows.AppKey), cache.NewArrayStore(), nil, noUsers{})
	if err != nil {
		t.Fatalf("NewFactors: %v", err)
	}
	secret, err := factors.decryptSecret(rows.TOTPSecretCiphertext)
	if err != nil {
		t.Fatalf("opening the stored secret: %v", err)
	}
	if otp.EncodeSecret(secret) != rows.TOTPSecretBase32 {
		t.Fatalf("the stored secret opened as %q, want %q", otp.EncodeSecret(secret), rows.TOTPSecretBase32)
	}
	sealed, err := factors.encryptSecret(secret)
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}
	again, err := factors.decryptSecret(sealed)
	if err != nil || otp.EncodeSecret(again) != rows.TOTPSecretBase32 {
		t.Fatalf("a secret sealed now does not open to itself (%v)", err)
	}
}

func TestTheChallengeKeyIsTheOneCountersAreAlreadyIn(t *testing.T) {
	if got, want := challengeKey("acme", "u-1"), `two-factor-challenge:"acme":"u-1"`; got != want {
		t.Fatalf("challengeKey = %s, want %s", got, want)
	}
	if challengeKey("a:b", "c") == challengeKey("a", "b:c") {
		t.Fatal("two pairs of ids spell the same counter")
	}
}

func TestAnEnrolmentIsItsOwnersAlone(t *testing.T) {
	ctx := context.Background()
	mine := &twoFactor{UserID: "ana", TenantID: "acme"}
	for _, action := range []auth.Action{ActionTwoFactorRead, ActionTwoFactorManage} {
		if _, err := auth.Authorize(ctx, enrolmentPolicy{}, auth.Subject{ID: "ana", Tenant: "acme"}, action, mine); err != nil {
			t.Errorf("the owner was refused %s: %v", action, err)
		}
		for name, other := range map[string]auth.Subject{
			"another account":           {ID: "bea", Tenant: "acme"},
			"an administrator":          {ID: "root", Tenant: "acme", Roles: []string{"admin"}},
			"the same id elsewhere":     {ID: "ana", Tenant: "other"},
			"a guest":                   auth.Guest("acme"),
			"a subject holding nothing": {Tenant: "acme"},
		} {
			if _, err := auth.Authorize(ctx, enrolmentPolicy{}, other, action, mine); err == nil {
				t.Errorf("%s was allowed %s on ana's enrolment", name, action)
			}
		}
	}
	if _, err := auth.Authorize(ctx, enrolmentPolicy{}, auth.Subject{ID: "ana", Tenant: "acme"}, ActionUserUpdate, mine); err == nil {
		t.Error("the enrolment policy allowed an action that is not the second factor's")
	}
}

type noUsers struct{}

func (noUsers) Owner(context.Context, auth.Subject) (Account, error)    { return nil, nil }
func (noUsers) Holder(context.Context, string, string) (Account, error) { return nil, nil }
