package account

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/otp"
)

const (
	// ChallengeAttempts is how many codes one account may offer to the sign-in
	// challenge within ChallengeWindow, authenticator and recovery codes
	// together.
	//
	// The count belongs to the account and not to the pending sign-in that
	// carries the attempt. A budget per pending sign-in is a budget per
	// password entry, and whoever has the password can sign in as often as
	// they like; the second factor is the one thing they do not have.
	ChallengeAttempts = 5

	// ChallengeWindow is how long the count lasts, from the first code offered.
	// It outlasts the pending sign-in, so a challenge refused for too many codes
	// cannot be resumed when the count expires: the person has to sign in
	// again.
	ChallengeWindow = 15 * time.Minute
)

// FactorUsers is what Factors reads the accounts it enrols through. *Users
// implements it, so the account a factor belongs to is read by the same rules
// as every other read of it.
type FactorUsers interface {
	// Owner reads the account a signed-in subject is, authorized by the user
	// policy for ActionUserView.
	Owner(ctx context.Context, subject auth.Subject) (Account, error)
	// Holder reads the account a pending sign-in names, before there is a
	// subject.
	Holder(ctx context.Context, tenant, userID string) (Account, error)
}

// Factors owns the authenticator second factor of an application's accounts:
// enrolling one, confirming it with its first code, disabling it, issuing
// recovery codes, and the sign-in challenge that checks a code.
//
// The secret is stored encrypted under the application key, and the recovery
// codes as password hashes. Only the account that owns an enrolment may read
// or change it; the challenge, which runs before a session exists, runs under
// a system grant bound to the tenant and account the pending sign-in carries.
type Factors struct {
	db        *database.DB
	store     enrolments
	policy    enrolmentPolicy
	users     FactorUsers
	encrypter *encryption.Encrypter
	outbox    *events.Outbox
	attempts  cache.Store
}

// NewFactors returns the second factor of the accounts users reads, with
// secrets encrypted under appKey.
//
// attempts is the store the codes each account offers to the sign-in
// challenge are counted in. It is required: without it a pending sign-in could
// be tried against every code for as long as it lives. Pass the store the
// request throttle counts in, so every replica spends one budget per account.
// A nil outbox is one over db.
func NewFactors(db *database.DB, appKey []byte, attempts cache.Store, outbox *events.Outbox, users FactorUsers) (*Factors, error) {
	if db == nil {
		return nil, errors.New("two-factor: Factors needs the database its tables live in")
	}
	if attempts == nil {
		return nil, errors.New("two-factor: the sign-in challenge needs a cache store to count attempts in")
	}
	if users == nil {
		return nil, errors.New("two-factor: Factors needs the users whose accounts it enrols")
	}
	encrypter, err := encryption.NewEncrypter(appKey, encryption.AES256GCM)
	if err != nil {
		return nil, err
	}
	if outbox == nil {
		outbox = events.NewOutbox(outboxHandle{db})
	}
	return &Factors{
		db: db, store: enrolments{db: db}, users: users,
		encrypter: encrypter, outbox: outbox, attempts: attempts,
	}, nil
}

// Required reports whether sign-in must finish a second factor.
func (s *Factors) Required(ctx context.Context, tenant, userID string) (bool, error) {
	//arandu:system-grant password verification established this pending identity before session creation; tenant and user ID bind the factor read
	return s.store.required(ctx, auth.SystemGrant(ActionTwoFactorRead, tenant), userID)
}

// Enabled reports whether the signed-in subject has a confirmed second factor,
// for the settings screen that offers to enable or disable it.
func (s *Factors) Enabled(ctx context.Context, actor auth.Subject) (bool, error) {
	read, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorRead, &twoFactor{UserID: actor.ID, TenantID: actor.Tenant})
	if err != nil {
		return false, err
	}
	return s.store.required(ctx, read, actor.ID)
}

// Begin stores an unconfirmed encrypted secret and returns the provisioning
// data an authenticator application is enrolled with. An unfinished enrolment
// is replaced; a confirmed one is refused with ErrTwoFactorAlreadyEnabled.
func (s *Factors) Begin(ctx context.Context, actor auth.Subject, issuer string) (twofactor.Provisioning, error) {
	manage, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorManage, &twoFactor{UserID: actor.ID, TenantID: actor.Tenant})
	if err != nil {
		return twofactor.Provisioning{}, err
	}
	user, err := s.users.Owner(ctx, actor)
	if err != nil {
		return twofactor.Provisioning{}, err
	}

	provisioning := twofactor.Provisioning{Issuer: issuer, Account: user.AccountEmail(), Secret: otp.NewSecret()}
	if _, err := provisioning.URI(); err != nil {
		return twofactor.Provisioning{}, err
	}
	secret, err := s.encryptSecret(provisioning.Secret)
	if err != nil {
		return twofactor.Provisioning{}, err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		return s.store.enrol(ctx, manage, actor.ID, secret)
	})
	if err != nil {
		return twofactor.Provisioning{}, err
	}
	return provisioning, nil
}

// Confirm proves the first authenticator code, enables the enrolment, stores
// TwoFactorEnabled and returns the recovery codes -- the only time they are
// ever readable.
func (s *Factors) Confirm(ctx context.Context, actor auth.Subject, code string) ([]string, error) {
	read, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorRead, &twoFactor{UserID: actor.ID, TenantID: actor.Tenant})
	if err != nil {
		return nil, err
	}
	user, err := s.users.Owner(ctx, actor)
	if err != nil {
		return nil, err
	}
	enrolment, err := s.store.find(ctx, read, user.AccountID())
	if err != nil {
		return nil, err
	}
	// The loaded enrolment is authorized again: the first decision was about
	// the row the subject asked for, this one is about the row that came back.
	if _, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorRead, enrolment); err != nil {
		return nil, err
	}
	manage, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorManage, enrolment)
	if err != nil {
		return nil, err
	}
	if enrolment.enabled() {
		return nil, ErrTwoFactorAlreadyEnabled
	}
	secret, err := s.decryptSecret(enrolment.Secret)
	if err != nil {
		return nil, err
	}
	if err := (twofactor.Authenticator{Guard: replayGuard{s.store, manage}}).
		Verify(ctx, user.AccountID(), secret, code); err != nil {
		return nil, err
	}
	codes, hashes, err := recoveryCodes()
	if err != nil {
		return nil, err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		won, err := s.store.confirm(ctx, manage, user.AccountID(), time.Now().UTC())
		if err != nil {
			return err
		}
		if !won {
			return ErrTwoFactorAlreadyEnabled
		}
		if err := s.store.replaceRecoveryCodes(ctx, manage, user.AccountID(), hashes); err != nil {
			return err
		}
		return s.record(ctx, manage, TwoFactorEnabled, user)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// Disable removes the secret, the replay state and every recovery code, and
// stores TwoFactorDisabled.
func (s *Factors) Disable(ctx context.Context, actor auth.Subject) error {
	grant, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorManage, &twoFactor{UserID: actor.ID, TenantID: actor.Tenant})
	if err != nil {
		return err
	}
	user, err := s.users.Owner(ctx, actor)
	if err != nil {
		return err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if err := s.store.disable(ctx, grant, user.AccountID()); err != nil {
			return err
		}
		return s.record(ctx, grant, TwoFactorDisabled, user)
	})
	if err == nil {
		log.For(ctx).Warn("second factor disabled", "user_id", user.AccountID(), "tenant", user.AccountTenant())
	}
	return err
}

// RegenerateRecoveryCodes replaces every previous recovery code, stores
// RecoveryCodesRegenerated and returns the new ones.
func (s *Factors) RegenerateRecoveryCodes(ctx context.Context, actor auth.Subject) ([]string, error) {
	read, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorRead, &twoFactor{UserID: actor.ID, TenantID: actor.Tenant})
	if err != nil {
		return nil, err
	}
	user, err := s.users.Owner(ctx, actor)
	if err != nil {
		return nil, err
	}
	enrolment, err := s.store.find(ctx, read, user.AccountID())
	if err != nil {
		return nil, err
	}
	if _, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorRead, enrolment); err != nil {
		return nil, err
	}
	manage, err := auth.Authorize(ctx, s.policy, actor, ActionTwoFactorManage, enrolment)
	if err != nil {
		return nil, err
	}
	if !enrolment.enabled() {
		return nil, ErrTwoFactorNotEnrolled
	}
	codes, hashes, err := recoveryCodes()
	if err != nil {
		return nil, err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if err := s.store.replaceRecoveryCodes(ctx, manage, user.AccountID(), hashes); err != nil {
			return err
		}
		return s.record(ctx, manage, RecoveryCodesRegenerated, user)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// VerifyAuthenticator checks and atomically spends a confirmed TOTP time step.
//
// It is the sign-in challenge, so it spends one of the account's
// ChallengeAttempts before the code is looked at, and returns a
// TwoFactorLockedError once they are gone -- even for the right code. A
// success gives the budget back.
func (s *Factors) VerifyAuthenticator(ctx context.Context, tenant, userID, code string) error {
	return s.challenge(ctx, tenant, userID, func() error {
		return s.verifyAuthenticator(ctx, tenant, userID, code)
	})
}

func (s *Factors) verifyAuthenticator(ctx context.Context, tenant, userID, code string) error {
	//arandu:system-grant a signed pending sign-in has no session subject; its tenant and user ID bind this authenticator read
	read := auth.SystemGrant(ActionTwoFactorRead, tenant)
	enrolment, err := s.store.find(ctx, read, userID)
	if err != nil {
		return err
	}
	if !enrolment.enabled() {
		return ErrTwoFactorNotEnrolled
	}
	secret, err := s.decryptSecret(enrolment.Secret)
	if err != nil {
		return err
	}
	//arandu:system-grant a signed pending sign-in has no session subject; its tenant and user ID bind replay spending after code verification
	manage := auth.SystemGrant(ActionTwoFactorManage, tenant)
	return (twofactor.Authenticator{Guard: replayGuard{s.store, manage}}).Verify(ctx, userID, secret, code)
}

// ConsumeRecovery atomically spends one recovery code of a confirmed factor
// and stores RecoveryCodeUsed.
//
// It draws on the same ChallengeAttempts as VerifyAuthenticator: a budget per
// kind of code would be two budgets, and alternating between them would double
// it.
func (s *Factors) ConsumeRecovery(ctx context.Context, tenant, userID, code string) error {
	return s.challenge(ctx, tenant, userID, func() error {
		return s.consumeRecovery(ctx, tenant, userID, code)
	})
}

func (s *Factors) consumeRecovery(ctx context.Context, tenant, userID, code string) error {
	//arandu:system-grant a signed pending sign-in has no session subject; its tenant and user ID bind this recovery-factor read
	read := auth.SystemGrant(ActionTwoFactorRead, tenant)
	required, err := s.store.required(ctx, read, userID)
	if err != nil {
		return err
	}
	if !required {
		return ErrTwoFactorNotEnrolled
	}
	user, err := s.users.Holder(ctx, tenant, userID)
	if err != nil {
		return err
	}
	//arandu:system-grant a signed pending sign-in has no session subject; its tenant and user ID bind one recovery-code spend
	manage := auth.SystemGrant(ActionTwoFactorManage, tenant)
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		spent, err := (recoveryStore{s.store, manage}).Consume(ctx, user.AccountID(), code)
		if err != nil {
			return err
		}
		if !spent {
			return ErrInvalidRecoveryCode
		}
		return s.record(ctx, manage, RecoveryCodeUsed, user)
	})
	if err != nil {
		return err
	}
	log.For(ctx).Warn("recovery code used", "user_id", user.AccountID(), "tenant", user.AccountTenant())
	return nil
}

// challenge spends one of the account's sign-in challenge attempts, then runs
// verify.
//
// The attempt is counted before the code is checked, never after: a count
// taken after the answer lets every request that arrives in between through,
// and parallel requests are exactly how a budget is outrun. The attempt that
// spends the last of the budget on a wrong code already refuses as locked, so
// the caller can end the pending sign-in on that same request.
//
// The count is a counter in the store and not a rate limiter's window. A
// limiter's windows are fixed to the clock, so a budget spent a second before
// one closes would be whole again a second later, with the same pending
// sign-in still valid. The counter's expiry is set by the first attempt and
// never moved, which is what makes ChallengeWindow a promise.
//
// A store that cannot count refuses the attempt. The request throttle fails
// open because it guards capacity; this guards an account.
func (s *Factors) challenge(ctx context.Context, tenant, userID string, verify func() error) error {
	key := challengeKey(tenant, userID)
	attempt, err := s.attempts.Increment(ctx, key, 1, ChallengeWindow)
	if err != nil {
		return fmt.Errorf("two-factor: counting the challenge attempt: %w", err)
	}
	if attempt > ChallengeAttempts {
		return TwoFactorLockedError{RetryAfter: ChallengeWindow}
	}

	if err := verify(); err != nil {
		if attempt == ChallengeAttempts {
			log.For(ctx).Warn("second-factor challenge locked after too many codes", "tenant", tenant, "user_id", userID)
			return TwoFactorLockedError{RetryAfter: ChallengeWindow}
		}
		return err
	}

	// The code was right and is spent. Failing to give the budget back costs
	// the person nothing they did not already have, so it is reported and the
	// sign-in goes ahead.
	if err := s.attempts.Forget(ctx, key); err != nil {
		log.For(ctx).Warn("clearing the second-factor challenge attempts", "error", err)
	}
	return nil
}

// challengeKey names the counter of one account. The tenant is part of it
// because a user id is only unique inside its tenant, and both are quoted so no
// pair of ids can spell the other's key.
//
// The key is not a tenant-prefixed cache key: the challenge runs before a
// session exists, so there is no Grant to scope it by, and the tenant is in
// the key itself.
func challengeKey(tenant, userID string) string {
	return "two-factor-challenge:" + strconv.Quote(tenant) + ":" + strconv.Quote(userID)
}

func (s *Factors) record(ctx context.Context, grant auth.Grant, name string, user Account) error {
	return s.outbox.Store(ctx, grant, []events.Event{{
		Name: name, Aggregate: Aggregate, AggregateID: user.AccountID(), Payload: eventOf(user),
	}})
}

// encryptSecret seals the base32 spelling of a TOTP secret under the
// application key: the format every enrolment already stored is in.
func (s *Factors) encryptSecret(secret []byte) (string, error) {
	return s.encrypter.EncryptString(otp.EncodeSecret(secret))
}

func (s *Factors) decryptSecret(payload string) ([]byte, error) {
	encoded, err := s.encrypter.DecryptString(payload)
	if err != nil {
		return nil, err
	}
	return otp.DecodeSecret(encoded)
}

// recoveryCodes issues a fresh set of recovery codes and the hashes they are
// stored as.
func recoveryCodes() ([]string, []string, error) {
	codes, err := twofactor.GenerateRecoveryCodes(twofactor.DefaultRecoveryCodes)
	if err != nil {
		return nil, nil, err
	}
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hash, err := hashRecoveryCode(code)
		if err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}
