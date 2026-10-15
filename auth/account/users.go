package account

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/hashing"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/validation"
)

// maxPublicNames bounds one PublicNames call: a page names its authors, and a
// list longer than this is a scan of the tenant's directory.
const maxPublicNames = 200

// maxNameLength is the longest name Rename accepts, in characters.
const maxNameLength = 120

// closedName is the name Close leaves on an account unless Hooks.CloseFields
// names another.
const closedName = "Closed account"

// Users owns the rules of an application's accounts: signing in, registering,
// verifying an address, resetting, confirming and changing a password, renaming
// and closing an account, and seeding one.
//
// What a signed-in subject does to an account is authorized by the
// application's user policy and runs under the Grant it issues. What happens
// before anybody is signed in -- a password check, a verification link, a
// reset link, a seeder -- has no subject, and runs under a system grant bound
// to the tenant and the account the flow already proved.
//
// It touches no session. Whatever follows a verified password -- a second
// factor, a new session -- is the caller's.
type Users struct {
	db       *database.DB
	outbox   *events.Outbox
	query    func() *model.Builder
	policy   auth.Policy[Account]
	throttle auth.SignInThrottle
	hooks    Hooks
}

// NewUsers returns the account rules over the application's users table.
//
// DB, Query and Policy are required: without a policy a signed-in operation
// would have nothing to authorize it.
func NewUsers(cfg Config) (*Users, error) {
	if cfg.DB == nil {
		return nil, errors.New("account: Users needs the database the users table lives in")
	}
	if cfg.Query == nil {
		return nil, errors.New("account: Users needs Query, the application's users query")
	}
	if cfg.Policy == nil {
		return nil, errors.New("account: Users needs Policy, the application's user policy")
	}
	outbox := cfg.Outbox
	if outbox == nil {
		outbox = events.NewOutbox(outboxHandle{cfg.DB})
	}
	throttle := cfg.Throttle
	if throttle == nil {
		throttle = auth.NewMemoryThrottle()
	}
	return &Users{
		db: cfg.DB, outbox: outbox, query: cfg.Query, policy: cfg.Policy, throttle: throttle, hooks: cfg.Hooks,
	}, nil
}

// VerifyCredentials checks a password and returns its account, without
// creating a request identity or a session.
//
// A wrong password, an unknown address and an account Hooks.Admit refused all
// match auth.ErrInvalidCredentials. Too many wrong passwords for one address
// from one client are refused with TooManyAttemptsError before the password is
// looked at. A hash made with weaker parameters is rewritten on the way out.
func (s *Users) VerifyCredentials(ctx context.Context, tenant, email, password, client string) (Account, error) {
	email = NormalizeEmail(email)
	retry, allowed := s.throttle.Attempt(ctx, tenant, email, client)
	if !allowed {
		return nil, TooManyAttemptsError{RetryAfter: retry}
	}

	verified, err := s.credentials(tenant).Verify(ctx, map[string]any{columnEmail: email, columnPassword: password})
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			s.throttle.Refund(ctx, tenant, email, client)
		}
		return nil, err
	}
	adapted, ok := verified.(*credentialUser)
	if !ok || !adapted.filled {
		s.throttle.Refund(ctx, tenant, email, client)
		return nil, fmt.Errorf("user: credential provider returned %T", verified)
	}
	user := adapted.user

	if admit := s.hooks.Admit; admit != nil {
		if reason := admit(ctx, user); reason != nil {
			// The password was right and the account may not sign in. It is
			// answered as a failed sign-in, so a refusal is not a password
			// oracle; whether it counts as one is the application's choice.
			if s.hooks.AdmitClears {
				s.throttle.Clear(ctx, tenant, email, client)
			}
			log.For(ctx).Warn("sign-in refused for the account", "user_id", user.AccountID(), "tenant", user.AccountTenant(), "reason", reason)
			return nil, refused{reason: reason}
		}
	}
	s.throttle.Clear(ctx, tenant, email, client)
	log.For(ctx).Info("login credentials verified", "user_id", user.AccountID(), "tenant", user.AccountTenant())
	return user, nil
}

// Register creates one unverified, unprivileged account, authorized by the
// user policy for a guest of the tenant, and stores UserRegistered with it.
//
// Hooks.Prepare runs first and may refuse, complete the registration or fill
// the application's own columns. A name, an address and a password of
// hashing.MinPasswordLen characters are required after it. The insert,
// Hooks.AfterCreate, Hooks.AfterRegister and the event share one transaction -- the caller's, when
// ctx already carries one -- so an account is never stored without its event.
func (s *Users) Register(ctx context.Context, tenant string, registration Registration) (Account, error) {
	candidate, err := s.probe(map[string]any{columnTenant: tenant})
	if err != nil {
		return nil, err
	}
	if prepare := s.hooks.Prepare; prepare != nil {
		if err := prepare(ctx, &registration, candidate); err != nil {
			return nil, err
		}
	}
	name, email := strings.TrimSpace(registration.Name), NormalizeEmail(registration.Email)
	if name == "" || email == "" || len(registration.Password) < hashing.MinPasswordLen {
		return nil, fmt.Errorf("user: invalid registration input")
	}
	if err := candidate.ForceFill(map[string]any{columnName: name, columnEmail: email}); err != nil {
		return nil, err
	}
	grant, err := auth.Authorize(ctx, s.policy, auth.Guest(tenant), ActionUserCreate, candidate)
	if err != nil {
		return nil, err
	}
	hash, err := hashing.Make(registration.Password)
	if err != nil {
		return nil, fmt.Errorf("user: hashing password: %w", err)
	}
	if err := candidate.ForceFill(map[string]any{columnPassword: hash}); err != nil {
		return nil, err
	}

	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		if err := s.create(ctx, grant, candidate); err != nil {
			return err
		}
		if after := s.hooks.AfterRegister; after != nil {
			carried := registration
			carried.Password = ""
			if err := after(ctx, grant, candidate, carried); err != nil {
				return err
			}
		}
		return s.record(ctx, grant, UserRegistered, candidate)
	})
	if err != nil {
		return nil, err
	}
	return candidate, nil
}

// FindForAuthentication reads an account during a pre-authentication flow: a
// pending second factor, a verification or reset link the caller already
// checked.
func (s *Users) FindForAuthentication(ctx context.Context, tenant, userID string) (Account, error) {
	//arandu:system-grant credential-bound authentication and recovery reads have no Grant-bearing subject; tenant and user ID bind the row
	return s.find(ctx, auth.SystemGrant(ActionUserView, tenant), ActionUserView, userID)
}

// Lookup reads an account by its normalized address, for the flows that start
// from an address: a reset request, a verification resend, a seeder.
func (s *Users) Lookup(ctx context.Context, tenant, email string) (Account, error) {
	//arandu:system-grant pre-authentication and seeding lookups have no subject; tenant and normalized email bind this read
	grant := auth.SystemGrant(ActionUserView, tenant)
	if err := grant.Check(ActionUserView); err != nil {
		return nil, err
	}
	entity, err := s.query().Where(columnEmail, "=", NormalizeEmail(email)).FirstOrFail(ctx, grant)
	if err != nil {
		return nil, err
	}
	return s.cast(entity)
}

// PublicNames resolves the names a page shows for account ids, after the user
// policy allows the reader ActionUserNamesPublic in their tenant. An account
// with no name is shown by the part of its address before the @. At most 200
// ids are read.
func (s *Users) PublicNames(ctx context.Context, reader auth.Subject, ids []string) (map[string]string, error) {
	resource, err := s.probe(map[string]any{columnTenant: reader.Tenant})
	if err != nil {
		return nil, err
	}
	grant, err := auth.Authorize(ctx, s.policy, reader, ActionUserNamesPublic, resource)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	if len(ids) > maxPublicNames {
		ids = ids[:maxPublicNames]
	}
	values := make([]any, len(ids))
	for i := range ids {
		values[i] = ids[i]
	}
	rows, err := s.query().WhereIn(columnID, values).Get(ctx, grant, columnID, columnName, columnEmail)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, entity := range rows {
		user, err := s.cast(entity)
		if err != nil {
			return nil, err
		}
		name := user.AccountName()
		if name == "" {
			name, _, _ = strings.Cut(user.AccountEmail(), "@")
		}
		out[user.AccountID()] = name
	}
	return out, nil
}

// MarkVerified stamps verified_at on the account, conditionally on the address
// captured when its code was issued, and stores EmailVerified once.
//
// The boolean reports whether this call made the change: of several
// concurrent verifications exactly one does, and the others answer the
// verified account with false.
func (s *Users) MarkVerified(ctx context.Context, tenant, userID, capturedEmail string) (Account, bool, error) {
	user, err := s.FindForAuthentication(ctx, tenant, userID)
	if err != nil {
		return nil, false, err
	}
	if NormalizeEmail(user.AccountEmail()) != NormalizeEmail(capturedEmail) {
		return nil, false, ErrVerificationAddressChanged
	}
	if user.IsVerified() {
		return user, false, nil
	}

	//arandu:system-grant a consumed verification code has no session subject; tenant, user ID, and captured email bind the conditional update
	grant := auth.SystemGrant(ActionUserUpdate, tenant)
	if err := grant.Check(ActionUserUpdate); err != nil {
		return nil, false, err
	}
	at := time.Now().UTC()
	var changed int64
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		changed, err = s.query().Where(columnID, "=", userID).
			Where(columnEmail, "=", NormalizeEmail(capturedEmail)).WhereNull(columnVerifiedAt).
			Update(ctx, grant, map[string]any{columnVerifiedAt: at})
		if err != nil || changed == 0 {
			return err
		}
		if err := user.ForceFill(map[string]any{columnVerifiedAt: at}); err != nil {
			return err
		}
		return s.record(ctx, grant, EmailVerified, user)
	})
	if err != nil {
		return nil, false, err
	}
	if changed == 0 {
		fresh, err := s.FindForAuthentication(ctx, tenant, userID)
		if err != nil {
			return nil, false, err
		}
		if NormalizeEmail(fresh.AccountEmail()) != NormalizeEmail(capturedEmail) {
			return nil, false, ErrVerificationAddressChanged
		}
		return fresh, false, nil
	}
	return user, true, nil
}

// ResetPassword replaces the password of the account a reset link was minted
// for, conditionally on the address and the password Fingerprint the link
// captured, and stores PasswordReset.
//
// Any change to either since the link was minted -- including the reset it
// already made -- answers ErrResetLinkSpent, and so does an account
// Hooks.Admit refuses.
func (s *Users) ResetPassword(ctx context.Context, tenant, userID, capturedEmail, capturedFingerprint, newPassword string) (Account, error) {
	user, err := s.FindForAuthentication(ctx, tenant, userID)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal([]byte(NormalizeEmail(user.AccountEmail())), []byte(NormalizeEmail(capturedEmail))) ||
		!hmac.Equal([]byte(Fingerprint(user.PasswordHash())), []byte(capturedFingerprint)) {
		return nil, ErrResetLinkSpent
	}
	if admit := s.hooks.Admit; admit != nil {
		if err := admit(ctx, user); err != nil {
			return nil, ErrResetLinkSpent
		}
	}
	return s.replacePassword(ctx, s.passwordGrant(tenant), user, newPassword, PasswordReset)
}

// SetPassword replaces the password of the account at an address, for an
// operator's command, and stores PasswordReset.
func (s *Users) SetPassword(ctx context.Context, tenant, email, password string) (Account, error) {
	user, err := s.Lookup(ctx, tenant, email)
	if err != nil {
		return nil, err
	}
	return s.replacePassword(ctx, s.passwordGrant(user.AccountTenant()), user, password, PasswordReset)
}

// ConfirmPassword checks the password of a signed-in subject again, before a
// sensitive action. It counts wrong passwords like a sign-in does, and answers
// one with auth.ErrInvalidCredentials.
func (s *Users) ConfirmPassword(ctx context.Context, subject auth.Subject, password, client string) error {
	if subject.ID == "" || subject.Tenant == "" {
		return fmt.Errorf("user: password confirmation needs a subject")
	}
	identity := "confirm:" + subject.ID
	retry, allowed := s.throttle.Attempt(ctx, subject.Tenant, identity, client)
	if !allowed {
		return TooManyAttemptsError{RetryAfter: retry}
	}
	user, err := s.FindForAuthentication(ctx, subject.Tenant, subject.ID)
	if err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			s.throttle.Refund(ctx, subject.Tenant, identity, client)
			return err
		}
		return auth.ErrInvalidCredentials
	}
	if err := hashing.Check(password, user.PasswordHash()); err != nil {
		return auth.ErrInvalidCredentials
	}
	s.throttle.Clear(ctx, subject.Tenant, identity, client)
	return nil
}

// ChangePassword replaces the signed-in subject's password once the current
// one is confirmed, under the Grant the user policy issues for ActionUserUpdate,
// and stores PasswordChanged.
//
// A new password shorter than hashing.MinPasswordLen is refused as a
// validation error on the password field before anything is checked.
func (s *Users) ChangePassword(ctx context.Context, subject auth.Subject, current, next, client string) (Account, error) {
	if len(next) < hashing.MinPasswordLen {
		return nil, validation.Errors{columnPassword: {"the new password is too short"}}
	}
	grant, err := s.authorizeSelf(ctx, subject, ActionUserUpdate)
	if err != nil {
		return nil, err
	}
	if err := s.ConfirmPassword(ctx, subject, current, client); err != nil {
		return nil, err
	}
	user, err := s.find(ctx, grant, ActionUserUpdate, subject.ID)
	if err != nil {
		return nil, err
	}
	return s.replacePassword(ctx, grant, user, next, PasswordChanged)
}

// Rename changes the name the signed-in subject's account is shown by, under
// the Grant the user policy issues for ActionUserUpdate, and stores
// ProfileUpdated.
func (s *Users) Rename(ctx context.Context, subject auth.Subject, name string) (Account, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return nil, validation.Errors{columnName: {fmt.Sprintf("provide a name of up to %d characters", maxNameLength)}}
	}
	grant, err := s.authorizeSelf(ctx, subject, ActionUserUpdate)
	if err != nil {
		return nil, err
	}
	var renamed Account
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		user, err := s.find(ctx, grant, ActionUserUpdate, subject.ID)
		if err != nil {
			return err
		}
		if _, err := s.query().Where(columnID, "=", user.AccountID()).Update(ctx, grant, map[string]any{columnName: name}); err != nil {
			return err
		}
		if err := user.ForceFill(map[string]any{columnName: name}); err != nil {
			return err
		}
		renamed = user
		return s.record(ctx, grant, ProfileUpdated, user)
	})
	if err != nil {
		return nil, err
	}
	return renamed, nil
}

// Close closes the signed-in subject's account once its password is
// confirmed: the address is released, the name replaced and a password nobody
// knows is stored, under the Grant the user policy issues for ActionUserUpdate.
// AccountClosed is stored with the account's id and tenant and nothing else.
//
// The row stays, because what the account did still names its id.
// Hooks.CloseFields writes the application's own columns in the same
// statement: a document number to clear, a blocked_at to stamp.
func (s *Users) Close(ctx context.Context, subject auth.Subject, password, client string) error {
	grant, err := s.authorizeSelf(ctx, subject, ActionUserUpdate)
	if err != nil {
		return err
	}
	if err := s.ConfirmPassword(ctx, subject, password, client); err != nil {
		return err
	}
	return database.Transaction(ctx, s.db, func(ctx context.Context) error {
		user, err := s.find(ctx, grant, ActionUserUpdate, subject.ID)
		if err != nil {
			return err
		}
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		hash, err := hashing.Make(hex.EncodeToString(random))
		if err != nil {
			return err
		}
		columns := map[string]any{
			columnName:     closedName,
			columnEmail:    "closed+" + user.AccountID() + "@accounts.invalid",
			columnPassword: hash,
		}
		if extra := s.hooks.CloseFields; extra != nil {
			for column, value := range extra(user) {
				columns[column] = value
			}
		}
		if _, err := s.query().Where(columnID, "=", user.AccountID()).Update(ctx, grant, columns); err != nil {
			return err
		}
		if err := user.ForceFill(columns); err != nil {
			return err
		}
		var payload any = UserEvent{UserID: user.AccountID(), Tenant: user.AccountTenant()}
		if custom := s.hooks.Payload; custom != nil {
			payload = custom(user)
		}
		return s.store(ctx, grant, AccountClosed, user.AccountID(), payload)
	})
}

// Provision creates an account with no person at a form behind it -- a
// seeder, an operator's command, an import -- or returns the account already
// at its address in the tenant.
//
// It runs under a system grant for the tenant, so the user policy is not
// asked: provisioning is how the first administrator comes to exist. It
// stores no event. Hooks.AfterCreate runs in the transaction of the insert.
func (s *Users) Provision(ctx context.Context, tenant string, draft Draft) (Account, error) {
	hash, err := draftHash(draft)
	if err != nil {
		return nil, err
	}
	if existing, err := s.Lookup(ctx, tenant, draft.Email); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	columns := map[string]any{
		columnTenant: tenant, columnName: strings.TrimSpace(draft.Name),
		columnEmail: NormalizeEmail(draft.Email), columnPassword: hash,
	}
	if draft.Verified {
		columns[columnVerifiedAt] = time.Now().UTC()
	}
	user, err := s.probe(columns)
	if err != nil {
		return nil, err
	}
	if draft.Fill != nil {
		if err := draft.Fill(user); err != nil {
			return nil, err
		}
	}
	//arandu:system-grant the seeder has no request subject; tenant-scoped lookup and explicit account fields bound idempotent creation
	grant := auth.SystemGrant(ActionUserCreate, tenant)
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		return s.create(ctx, grant, user)
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// draftHash is the password hash a Draft stores: its password hashed, or the
// hash it carries when the hashing package can read it.
func draftHash(draft Draft) (string, error) {
	switch {
	case draft.Password != "" && draft.PasswordHash != "":
		return "", errors.New("account: a draft carries a password or a password hash, not both")
	case draft.PasswordHash != "":
		if !hashing.IsHashed(draft.PasswordHash) {
			return "", errors.New("account: the draft's password hash is not one the hashing package can read")
		}
		return draft.PasswordHash, nil
	case draft.Password != "":
		hash, err := hashing.Make(draft.Password)
		if err != nil {
			return "", fmt.Errorf("user: hashing password: %w", err)
		}
		return hash, nil
	}
	return "", errors.New("account: a draft needs a password or a password hash")
}

// Owner reads the account a signed-in subject is, authorized by the user
// policy for ActionUserView: the settings screen's account, and the one
// Factors enrols.
func (s *Users) Owner(ctx context.Context, subject auth.Subject) (Account, error) {
	_, user, err := s.self(ctx, subject, ActionUserView)
	return user, err
}

// passwordGrant is the Grant a password is replaced under when there is no
// signed-in subject: a reset link, an operator's command.
func (s *Users) passwordGrant(tenant string) auth.Grant {
	//arandu:system-grant guest and operator password replacement has no session subject; tenant lookup plus ID/current-hash compare-and-swap bounds the write
	return auth.SystemGrant(ActionUserUpdate, tenant)
}

// replacePassword writes a new hash conditionally on the hash the account held
// when it was read, so of two concurrent replacements exactly one wins and the
// other answers ErrResetLinkSpent.
func (s *Users) replacePassword(ctx context.Context, grant auth.Grant, user Account, plain, event string) (Account, error) {
	hash, err := hashing.Make(plain)
	if err != nil {
		return nil, fmt.Errorf("user: hashing password: %w", err)
	}
	if err := grant.Check(ActionUserUpdate); err != nil {
		return nil, err
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		changed, err := s.query().Where(columnID, "=", user.AccountID()).Where(columnPassword, "=", user.PasswordHash()).
			Update(ctx, grant, map[string]any{columnPassword: hash})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrResetLinkSpent
		}
		if err := user.ForceFill(map[string]any{columnPassword: hash}); err != nil {
			return err
		}
		return s.record(ctx, grant, event, user)
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// create inserts a filled account and runs Hooks.AfterCreate, inside the
// transaction ctx carries.
//
// The tenant column is written from the Grant whatever the account holds: the
// model stamps it on every insert.
func (s *Users) create(ctx context.Context, grant auth.Grant, user Account) error {
	if err := grant.Check(ActionUserCreate); err != nil {
		return err
	}
	if user.PasswordHash() == "" {
		return fmt.Errorf("user: refusing to store an empty password hash")
	}
	if _, err := user.Save(ctx, grant); err != nil {
		if errors.Is(err, database.ErrUniqueViolation) {
			return fmt.Errorf("%w: %w", ErrEmailTaken, err)
		}
		return err
	}
	if after := s.hooks.AfterCreate; after != nil {
		return after(ctx, grant, user)
	}
	return nil
}

// self authorizes subject to act on their own account and reads it under the
// Grant that decision issued.
func (s *Users) self(ctx context.Context, subject auth.Subject, action auth.Action) (auth.Grant, Account, error) {
	grant, err := s.authorizeSelf(ctx, subject, action)
	if err != nil {
		return auth.Grant{}, nil, err
	}
	user, err := s.find(ctx, grant, action, subject.ID)
	return grant, user, err
}

// authorizeSelf asks the user policy whether subject may act on their own
// account. It reads nothing, so a write decides before it opens a transaction.
func (s *Users) authorizeSelf(ctx context.Context, subject auth.Subject, action auth.Action) (auth.Grant, error) {
	resource, err := s.probe(map[string]any{columnID: subject.ID, columnTenant: subject.Tenant})
	if err != nil {
		return auth.Grant{}, err
	}
	return auth.Authorize(ctx, s.policy, subject, action, resource)
}

func (s *Users) find(ctx context.Context, grant auth.Grant, action auth.Action, id string) (Account, error) {
	if err := grant.Check(action); err != nil {
		return nil, err
	}
	entity, err := s.query().FindOrFail(ctx, grant, id)
	if err != nil {
		return nil, err
	}
	return s.cast(entity)
}

// probe is an unsaved account holding columns, for a policy to decide about
// or for an insert to fill further.
func (s *Users) probe(columns map[string]any) (Account, error) {
	user, err := newAccount(s.query())
	if err != nil {
		return user, err
	}
	return user, user.ForceFill(columns)
}

func (s *Users) cast(entity model.Entity) (Account, error) {
	user, ok := entity.(Account)
	if !ok {
		return nil, fmt.Errorf("account: the users query read %T, which does not implement account.Account", entity)
	}
	return user, nil
}

func (s *Users) record(ctx context.Context, grant auth.Grant, name string, user Account) error {
	var payload any = eventOf(user)
	if custom := s.hooks.Payload; custom != nil {
		payload = custom(user)
	}
	return s.store(ctx, grant, name, user.AccountID(), payload)
}

func (s *Users) store(ctx context.Context, grant auth.Grant, name, id string, payload any) error {
	return s.outbox.Store(ctx, grant, []events.Event{{Name: name, Aggregate: Aggregate, AggregateID: id, Payload: payload}})
}

var _ FactorUsers = (*Users)(nil)
