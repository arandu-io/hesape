package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/hashing"
)

// The tables the second factor is stored in. Their names are the ones every
// application already holds rows in, which is what lets an enrolled account
// keep signing in when its application moves to this package.
const (
	twoFactorTableName    = "user_two_factor"
	recoveryCodeTableName = "user_recovery_codes"
)

// recoveryCodePrefix is put in front of a recovery code before it is hashed, so
// a recovery hash can never be offered as a password hash. Changing it would
// make every recovery code already issued fail.
const recoveryCodePrefix = "arandu:two-factor-recovery:"

// twoFactor is one account's authenticator enrolment.
//
// ConfirmedAt is nil until the first code proves the enrolment. It is a pointer
// because the column is NULL then, and the conditional writes ask for exactly
// that: a zero time would be stored as a date and match nothing.
type twoFactor struct {
	model.Model

	UserID       string     `db:"user_id"`
	TenantID     string     `db:"tenant_id"`
	Secret       string     `db:"secret"`
	ConfirmedAt  *time.Time `db:"confirmed_at"`
	LastUsedStep uint64     `db:"last_used_step"`
	CreatedAt    time.Time  `db:"created_at"`
}

// twoFactorTable keys an enrolment by the account's id: one enrolment per
// account, written by this package and never generated. The table has no
// updated_at column.
var twoFactorTable = model.NewTable(model.TableSpec{
	Name:            twoFactorTableName,
	New:             func() model.Entity { return new(twoFactor) },
	PrimaryKey:      "user_id",
	ManualKey:       true,
	UpdatedAtColumn: model.NoColumn,
})

// enabled reports whether the enrolment was proved with its first code.
func (t *twoFactor) enabled() bool { return t.ConfirmedAt != nil && !t.ConfirmedAt.IsZero() }

// MarshalJSON keeps the encrypted secret out of responses and debug dumps.
func (t *twoFactor) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		UserID   string `json:"user_id"`
		TenantID string `json:"tenant_id"`
		Enabled  bool   `json:"enabled"`
	}{UserID: t.UserID, TenantID: t.TenantID, Enabled: t.enabled()})
}

// LogValue records only the account and whether its factor is active.
func (t *twoFactor) LogValue() slog.Value {
	return slog.GroupValue(slog.String("user", t.UserID), slog.String("tenant", t.TenantID), slog.Bool("enabled", t.enabled()))
}

// recoveryCode is one single-use recovery code of an enrolment, stored as a
// password hash. UsedAt is nil until the code is spent.
type recoveryCode struct {
	model.Model

	ID        string     `db:"id"`
	TenantID  string     `db:"tenant_id"`
	UserID    string     `db:"user_id"`
	CodeHash  string     `db:"code_hash"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// recoveryCodeTable generates a text id on insert and has no updated_at column.
var recoveryCodeTable = model.NewTable(model.TableSpec{
	Name:            recoveryCodeTableName,
	New:             func() model.Entity { return new(recoveryCode) },
	UniqueIDs:       true,
	UpdatedAtColumn: model.NoColumn,
})

// MarshalJSON keeps the hash out of responses and debug dumps.
func (c *recoveryCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Used     bool   `json:"used"`
	}{ID: c.ID, TenantID: c.TenantID, UserID: c.UserID, Used: c.UsedAt != nil})
}

// LogValue records only the identifiers.
func (c *recoveryCode) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", c.ID), slog.String("user", c.UserID), slog.String("tenant", c.TenantID))
}

// hashRecoveryCode is the one persistence format of a recovery code. Hashing
// and checking live side by side so the two cannot drift to different
// prefixes or normalizations.
func hashRecoveryCode(code string) (string, error) {
	hash, err := hashing.Make(recoveryCodeMaterial(code))
	if err != nil {
		return "", fmt.Errorf("two-factor: hashing recovery code: %w", err)
	}
	return hash, nil
}

// recoveryCodeMatches reports whether hash was made from code.
func recoveryCodeMatches(code, hash string) bool {
	return hashing.Check(recoveryCodeMaterial(code), hash) == nil
}

func recoveryCodeMaterial(code string) string {
	return recoveryCodePrefix + twofactor.NormalizeCode(code)
}

// enrolments holds the second-factor writes that are decided by the row they
// change: a time step spent only when it is higher than the last one, an
// enrolment confirmed only while it is unconfirmed, a recovery code spent only
// while it is unspent. Each is one conditional update through the model, and
// the number of rows it changed is the answer -- which is what makes two
// concurrent attempts produce exactly one winner.
//
// Every method checks its Grant before the first query, and every query is
// scoped by the model to the Grant's tenant.
type enrolments struct{ db *database.DB }

func (r enrolments) factors() *model.Builder { return twoFactorTable.Query(r.db) }
func (r enrolments) codes() *model.Builder   { return recoveryCodeTable.Query(r.db) }

// find reads one enrolment under the supplied read grant.
func (r enrolments) find(ctx context.Context, grant auth.Grant, userID string) (*twoFactor, error) {
	if err := grant.Check(ActionTwoFactorRead); err != nil {
		return nil, err
	}
	entity, err := r.factors().FindOrFail(ctx, grant, userID)
	if errors.Is(err, model.ErrModelNotFound) {
		return nil, ErrTwoFactorNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	return entity.(*twoFactor), nil
}

// enrol atomically replaces only an unfinished enrolment.
func (r enrolments) enrol(ctx context.Context, grant auth.Grant, userID, secret string) error {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return err
	}
	if secret == "" {
		return fmt.Errorf("two-factor: refusing to store an empty secret")
	}
	if _, err := r.factors().WhereKey(userID).WhereNull("confirmed_at").Delete(ctx, grant); err != nil {
		return err
	}
	record := twoFactorTable.New(r.db).(*twoFactor)
	record.UserID = userID
	record.TenantID = auth.Tenant(grant)
	record.Secret = secret
	if _, err := record.Save(ctx, grant); err != nil {
		// A confirmed enrolment survived the delete above, and the key is the
		// account: the insert is refused rather than replacing a working factor.
		if errors.Is(err, database.ErrUniqueViolation) {
			return ErrTwoFactorAlreadyEnabled
		}
		return err
	}
	return nil
}

// confirm stamps an unfinished enrolment and reports whether this call won.
func (r enrolments) confirm(ctx context.Context, grant auth.Grant, userID string, at time.Time) (bool, error) {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return false, err
	}
	changed, err := r.factors().WhereKey(userID).WhereNull("confirmed_at").
		Update(ctx, grant, map[string]any{"confirmed_at": at.UTC()})
	return changed == 1, err
}

// required reports whether the account has a confirmed enrolment.
func (r enrolments) required(ctx context.Context, grant auth.Grant, userID string) (bool, error) {
	if err := grant.Check(ActionTwoFactorRead); err != nil {
		return false, err
	}
	factor, err := r.find(ctx, grant, userID)
	if errors.Is(err, ErrTwoFactorNotEnrolled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return factor.enabled(), nil
}

// disable removes the enrolment and all of its recovery codes.
func (r enrolments) disable(ctx context.Context, grant auth.Grant, userID string) error {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return err
	}
	if _, err := r.codes().Where("user_id", "=", userID).Delete(ctx, grant); err != nil {
		return err
	}
	removed, err := r.factors().WhereKey(userID).Delete(ctx, grant)
	if err != nil {
		return err
	}
	if removed != 1 {
		return ErrTwoFactorNotEnrolled
	}
	return nil
}

// spendStep atomically records a higher authenticator time step.
func (r enrolments) spendStep(ctx context.Context, grant auth.Grant, userID string, step uint64) (bool, error) {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return false, err
	}
	changed, err := r.factors().WhereKey(userID).Where("last_used_step", "<", int64(step)).
		Update(ctx, grant, map[string]any{"last_used_step": int64(step)})
	return changed == 1, err
}

// replaceRecoveryCodes replaces the entire recovery set with password hashes.
func (r enrolments) replaceRecoveryCodes(ctx context.Context, grant auth.Grant, userID string, hashes []string) error {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return err
	}
	if _, err := r.codes().Where("user_id", "=", userID).Delete(ctx, grant); err != nil {
		return err
	}
	tenant := auth.Tenant(grant)
	for _, hash := range hashes {
		if hash == "" {
			return fmt.Errorf("two-factor: refusing to store an empty recovery hash")
		}
		record := recoveryCodeTable.New(r.db).(*recoveryCode)
		record.TenantID = tenant
		record.UserID = userID
		record.CodeHash = hash
		if _, err := record.Save(ctx, grant); err != nil {
			return err
		}
	}
	return nil
}

// consumeRecoveryCode atomically spends a matching unspent recovery hash.
//
// The hashes are compared here, one by one, because a password hash cannot be
// looked up by value. The write that spends the match is conditional on the
// code still being unspent, so of two concurrent redemptions of one code only
// one changes the row.
func (r enrolments) consumeRecoveryCode(ctx context.Context, grant auth.Grant, userID, code string) (bool, error) {
	if err := grant.Check(ActionTwoFactorManage); err != nil {
		return false, err
	}
	if twofactor.NormalizeCode(code) == "" {
		return false, nil
	}
	unspent, err := r.codes().Where("user_id", "=", userID).WhereNull("used_at").Get(ctx, grant, "id", "code_hash")
	if err != nil {
		return false, err
	}
	for _, entity := range unspent {
		candidate := entity.(*recoveryCode)
		if !recoveryCodeMatches(code, candidate.CodeHash) {
			continue
		}
		changed, err := r.codes().WhereKey(candidate.ID).Where("user_id", "=", userID).
			WhereNull("used_at").Update(ctx, grant, map[string]any{"used_at": time.Now().UTC()})
		return changed == 1, err
	}
	return false, nil
}

// replayGuard hands the time-step write to the native authenticator check,
// with the Grant it was given.
type replayGuard struct {
	store enrolments
	grant auth.Grant
}

func (g replayGuard) Spend(ctx context.Context, subject string, step uint64) (bool, error) {
	return g.store.spendStep(ctx, g.grant, subject, step)
}

// recoveryStore hands the recovery-code write to the native check, with the
// Grant it was given.
type recoveryStore struct {
	store enrolments
	grant auth.Grant
}

func (s recoveryStore) Consume(ctx context.Context, subject, code string) (bool, error) {
	return s.store.consumeRecoveryCode(ctx, s.grant, subject, code)
}

var (
	_ twofactor.ReplayGuard   = replayGuard{}
	_ twofactor.RecoveryStore = recoveryStore{}
)

// enrolmentPolicy restricts an enrolment to the account that owns it.
type enrolmentPolicy struct{}

// Can lets the account owner read and manage its own enrolment, and nobody
// else -- an administrator included, because a second factor somebody else
// can manage proves nothing about the person signing in.
func (enrolmentPolicy) Can(_ context.Context, subject auth.Subject, action auth.Action, factor *twoFactor) error {
	if factor.TenantID != "" && factor.TenantID != subject.Tenant {
		return fmt.Errorf("resource belongs to another tenant")
	}
	if (action == ActionTwoFactorRead || action == ActionTwoFactorManage) && subject.ID != "" && subject.ID == factor.UserID {
		return nil
	}
	return fmt.Errorf("insufficient role for %s", action)
}

var _ auth.Policy[*twoFactor] = enrolmentPolicy{}
