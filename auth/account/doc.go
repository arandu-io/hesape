// Package account holds the rules of an application's accounts and of their
// authenticator second factor: signing in with a password, registering,
// verifying an address, resetting, confirming and changing a password,
// renaming and closing an account, seeding one, and enrolling, confirming,
// disabling and challenging a second factor with its recovery codes.
//
// # What stays the application's
//
// The user model, the users table and its migrations, the user policy, and
// the screens. The model implements Account -- six readers; the rest comes
// from embedding model.Model -- and Config hands this package the users query
// and the policy:
//
//	users, err := account.NewUsers(account.Config{
//		DB:     db,
//		Query:  func() *model.Builder { return models.Users(db).Base() },
//		Policy: account.PolicyOf(policies.UserPolicy{}),
//	})
//	factors, err := account.NewFactors(db, appKey, store, nil, users)
//
// Every method hands back an Account; it is the application's *User, built by
// its users query, and a caller that needs the type asserts it once at its
// edge: user.(*models.User).
//
// The users table has the columns id, tenant_id, name, email, password and
// verified_at, with email unique per tenant; the statements here name them.
// Anything else on it -- roles, a document number, a phone -- is written by
// the application through Hooks and Draft.Fill.
// What a registration form carries beyond the account -- a document number to
// store elsewhere, a consent -- travels in Registration.Extra: Prepare reads it
// before the insert, and Hooks.AfterRegister receives the whole Registration
// after it, in the same transaction, so nothing of the form needs a field on
// the user model to get from one to the other.
//
// # Why nothing here is generic
//
// Go compiles every method of an instantiated generic type into every package
// that names the instantiation. The rules here are written once over the
// Account interface and compiled once, in this package, whatever the number of
// applications and packages that use them. PolicyOf is the one generic
// declaration: a function whose body is a type assertion, so a package calling
// it compiles that assertion and nothing more.
//
// # What this package owns
//
// The second factor's two tables, user_two_factor and user_recovery_codes,
// their entities and every statement on them; Migrations creates them. The
// TOTP secret is stored sealed with AES-256-GCM under the application key, and
// a recovery code as a password hash. Only the account that owns an
// enrolment may read or change it.
//
// # Authorization
//
// What a signed-in subject does to an account runs under the Grant the
// application's user policy issues for it -- ActionUserView, ActionUserUpdate,
// ActionUserNamesPublic -- and a registration under the Grant it issues a
// guest of the tenant for ActionUserCreate. What runs before anybody is signed
// in has no subject to authorize: a password check, a verification or reset
// link, the second-factor challenge, a seeder. Those run under a system grant
// for the tenant, bound by what the flow already proved -- the account's id,
// its captured address, its current password hash -- and every write among
// them is conditional on that state, so a link or a code is spent once.
//
// # Events
//
// Every change stores an event in the outbox in the transaction that made it:
// UserRegistered, EmailVerified, PasswordReset, PasswordChanged,
// ProfileUpdated, AccountClosed, TwoFactorEnabled, TwoFactorDisabled,
// RecoveryCodesRegenerated and RecoveryCodeUsed, about Aggregate "user", with
// a UserEvent payload. Register, its hooks and its event join the transaction
// the caller's context already carries, so an application that creates
// something beside an account creates it in the same one.
//
// # What it does not touch
//
// Sessions. VerifyCredentials answers whether a password is right and nothing
// else; whether a second factor follows, and when a session starts, is the
// caller's. Mail is not sent here either: the verification and reset links
// are signed by the application over the address and the password
// Fingerprint they were minted for, and checked here against them.
package account
