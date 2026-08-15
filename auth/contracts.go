package auth

import "context"

// This file holds the Illuminate\Contracts\Auth interfaces. They live in the
// package that implements them rather than in a contracts package of their own,
// which is ADR 0045: PHP needs the separate namespace because a class can only
// declare the interfaces it implements at the top of the file, and Go satisfies
// an interface structurally.
//
// Every method that touches storage takes a context.Context that PHP does not
// have. It is the fifth mechanical change, alongside the four in ADR 0044:
// without it a database lookup in a guard cannot be cancelled when the request
// is, and the caller has no way to pass one in later.

// Authenticatable mirrors Illuminate\Contracts\Auth\Authenticatable.
//
// It is what a guard hands back, and what a user provider retrieves. An
// application's own user type satisfies it by having the seven methods; nothing
// has to be embedded and nothing has to be registered.
type Authenticatable interface {
	// GetAuthIdentifierName is getAuthIdentifierName: the column holding the id.
	GetAuthIdentifierName() string

	// GetAuthIdentifier is getAuthIdentifier: the id itself.
	GetAuthIdentifier() any

	// GetAuthPasswordName is getAuthPasswordName: the column holding the hash.
	GetAuthPasswordName() string

	// GetAuthPassword is getAuthPassword: the hash, never the plain password.
	GetAuthPassword() string

	// GetRememberToken is getRememberToken.
	GetRememberToken() string

	// SetRememberToken is setRememberToken.
	SetRememberToken(token string)

	// GetRememberTokenName is getRememberTokenName: the column holding it.
	GetRememberTokenName() string
}

// BroadcastsIdentifier is the one method of the Illuminate\Auth\Authenticatable
// trait that is not in the Contracts\Auth\Authenticatable interface:
// getAuthIdentifierForBroadcasting.
//
// It is separate here for the same reason it is separate there. The trait
// answers it with getAuthIdentifier and every model inherits that, so PHP can
// add it without breaking anybody; adding a method to the Go interface would
// break every user type an application already wrote. So this is an interface
// of its own, and [AuthIdentifierForBroadcasting] falls back to the identifier
// for a type that does not implement it -- which is exactly what the trait's
// body does.
//
// What it is for: the id that goes out on a presence channel, which a person
// other than its owner will read. A model whose primary key is a sequential
// integer leaks how many customers there are and how new this one is, so this
// is the seam where a UUID or a hashid is substituted.
type BroadcastsIdentifier interface {
	// GetAuthIdentifierForBroadcasting is getAuthIdentifierForBroadcasting.
	GetAuthIdentifierForBroadcasting() any
}

// AuthIdentifierForBroadcasting returns the id to publish on a channel.
//
// It is the trait method as a function, because Go has no trait: a user type
// that implements [BroadcastsIdentifier] answers with its own value, and every
// other one answers with GetAuthIdentifier, as the PHP body does.
func AuthIdentifierForBroadcasting(user Authenticatable) any {
	if user == nil {
		return nil
	}
	if b, ok := user.(BroadcastsIdentifier); ok {
		return b.GetAuthIdentifierForBroadcasting()
	}
	return user.GetAuthIdentifier()
}

// UserProvider mirrors Illuminate\Contracts\Auth\UserProvider.
//
// It is the seam between a guard and wherever the users are kept. A guard knows
// how a session or a token proves identity; a provider knows how to find the
// row and how to check the password.
type UserProvider interface {
	// RetrieveByID is retrieveById.
	RetrieveByID(ctx context.Context, identifier any) (Authenticatable, error)

	// RetrieveByToken is retrieveByToken: the remember-me cookie's half.
	RetrieveByToken(ctx context.Context, identifier any, token string) (Authenticatable, error)

	// UpdateRememberToken is updateRememberToken.
	UpdateRememberToken(ctx context.Context, user Authenticatable, token string) error

	// RetrieveByCredentials is retrieveByCredentials. It must not look at the
	// password key -- finding a user BY their password is a lookup nobody can
	// index and a comparison that is not constant time.
	RetrieveByCredentials(ctx context.Context, credentials map[string]any) (Authenticatable, error)

	// ValidateCredentials is validateCredentials.
	ValidateCredentials(ctx context.Context, user Authenticatable, credentials map[string]any) bool

	// RehashPasswordIfRequired is rehashPasswordIfRequired: it upgrades a hash
	// that was made with weaker parameters, on a sign-in that already proved the
	// plain password.
	RehashPasswordIfRequired(ctx context.Context, user Authenticatable, credentials map[string]any, force bool) error
}

// Guard mirrors Illuminate\Contracts\Auth\Guard.
//
// It answers who is acting. It decides nothing about what they may do -- that
// is the Policy, and the Grant is the proof it ran.
type Guard interface {
	// Check is check: somebody is signed in.
	Check() bool

	// Guest is guest: nobody is.
	Guest() bool

	// User is user: the authenticated user, or nil.
	User() Authenticatable

	// ID is id: the authenticated user's identifier, or nil.
	ID() any

	// Validate is validate: are these credentials good, without signing in.
	Validate(ctx context.Context, credentials map[string]any) bool

	// HasUser is hasUser: a user is already resolved, without resolving one.
	HasUser() bool

	// SetUser is setUser.
	SetUser(user Authenticatable)
}

// StatefulGuard mirrors Illuminate\Contracts\Auth\StatefulGuard.
//
// It is a Guard that can also start and end a session.
type StatefulGuard interface {
	Guard

	// Attempt is attempt.
	Attempt(ctx context.Context, credentials map[string]any, remember bool) bool

	// Once is once: authenticate for this request only, no session.
	Once(ctx context.Context, credentials map[string]any) bool

	// Login is login.
	Login(ctx context.Context, user Authenticatable, remember bool)

	// LoginUsingID is loginUsingId.
	LoginUsingID(ctx context.Context, id any, remember bool) Authenticatable

	// OnceUsingID is onceUsingId.
	OnceUsingID(ctx context.Context, id any) Authenticatable

	// ViaRemember is viaRemember: this session came from the cookie, not from a
	// password typed in this browser session.
	ViaRemember() bool

	// Logout is logout.
	Logout(ctx context.Context)
}

// SupportsBasicAuth is the optional half of a guard that can sign a caller in
// straight from the Authorization: Basic header, with no login form and no
// session to start. A guard declares it when a username and a password on every
// request is a way in: the machine calling an endpoint, and the internal tool
// nobody wants to build a sign-in screen for.
//
// Both methods take the column the username is matched against -- the e-mail
// address by default -- and extra conditions the lookup must also satisfy, and
// both answer nil when the request may carry on. Basic starts a session and
// does nothing when somebody is already signed in; OnceBasic authenticates for
// this request only and writes nothing.
//
// It is a separate interface and not more methods on Guard, so that a guard
// with no business accepting a password on every request simply does not have
// them. [github.com/arandu-io/hesape/auth/middleware.AuthenticateWithBasicAuth]
// asserts for it and answers 500 when the assertion fails, because a guard
// wired where basic auth was expected is a configuration mistake and not a bad
// password.
//
// Answers Illuminate\Contracts\Auth\SupportsBasicAuth.
type SupportsBasicAuth interface {
	// Basic is basic.
	Basic(ctx context.Context, field string, extraConditions map[string]any) error

	// OnceBasic is onceBasic.
	OnceBasic(ctx context.Context, field string, extraConditions map[string]any) error
}

// CanResetPassword is what a user type must satisfy to take part in a password
// reset. It answers with the address a reset link is sent to, and it knows how
// to send one. A type without it can sign in and cannot recover an account: the
// broker refuses it with passwords.ErrCannotResetPassword.
//
// The address is asked for rather than read off a field because it is not
// necessarily the sign-in identifier, and because it is what the stored record
// is keyed by -- both token repositories hash it to find the token again.
// Sending is asked of the user rather than done for them because what arrives
// is an application's own message, on its own template, over its own transport.
//
// Answers Illuminate\Contracts\Auth\CanResetPassword.
type CanResetPassword interface {
	// GetEmailForPasswordReset is getEmailForPasswordReset.
	GetEmailForPasswordReset() string

	// SendPasswordResetNotification is sendPasswordResetNotification.
	SendPasswordResetNotification(ctx context.Context, token string) error
}

// MustVerifyEmail is what a user type must satisfy for an application to hold
// it to a confirmed e-mail address. It reports whether the address has been
// confirmed, stamps it as confirmed, sends the message that asks, and answers
// with the address that message goes to.
//
// It is a type assertion and not a flag on the user. The
// [github.com/arandu-io/hesape/auth/middleware.EnsureEmailIsVerified]
// middleware asks whether the signed-in user implements this interface and lets
// a user type that does not through untouched, so an application with two kinds
// of account only confirms the addresses of the kind that has one.
//
// [MustVerifyEmailTrait] is the ready implementation a user type embeds to
// answer all four methods.
//
// Answers Illuminate\Contracts\Auth\MustVerifyEmail.
type MustVerifyEmail interface {
	// HasVerifiedEmail is hasVerifiedEmail.
	HasVerifiedEmail() bool

	// MarkEmailAsVerified is markEmailAsVerified.
	MarkEmailAsVerified(ctx context.Context) error

	// SendEmailVerificationNotification is sendEmailVerificationNotification.
	SendEmailVerificationNotification(ctx context.Context) error

	// GetEmailForVerification is getEmailForVerification.
	GetEmailForVerification() string
}

// Hasher is the half of Illuminate\Contracts\Hashing\Hasher that authentication
// uses. It is declared here, in the consuming package, so that the root of auth
// keeps importing nothing but the standard library -- hesape/hashing satisfies
// it without either side importing the other.
type Hasher interface {
	// Make is make.
	Make(value string) (string, error)

	// Check is check.
	Check(value, hashedValue string) bool

	// NeedsRehash is needsRehash.
	NeedsRehash(hashedValue string) bool
}
