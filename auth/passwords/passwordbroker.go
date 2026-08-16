package passwords

import (
	"context"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/support"
)

// The statuses a broker answers with. Their values are translation keys: the
// application looks each one up to put a sentence on the screen, so changing a
// string here would silently un-translate every message behind it.
const (
	// ResetLinkSent means the link went out.
	ResetLinkSent = "passwords.sent"

	// PasswordReset means the new password was stored and the token destroyed.
	PasswordReset = "passwords.reset"

	// InvalidUser means no account matched the credentials. It is a status and
	// not an error, and the form is expected to render it identically to
	// ResetLinkSent -- an anonymous caller must not learn from this form which
	// addresses have accounts.
	InvalidUser = "passwords.user"

	// InvalidToken means no live token for this account matches the one offered.
	// Expired and wrong are the same answer, on purpose.
	InvalidToken = "passwords.token"

	// ResetThrottled means a token was minted for this account too recently to
	// mint another.
	ResetThrottled = "passwords.throttled"
)

// DefaultTimeboxDuration is how long a broker method is held open for when no
// duration is given: 200 milliseconds, in the microseconds support.Timebox
// counts.
const DefaultTimeboxDuration = 200000

// ErrCannotResetPassword is what [PasswordBroker.GetUser] answers for a user
// type that cannot take part in a reset.
//
// It fires when the user provider is configured to return a user type that
// cannot be sent a reset link. That is a wiring mistake and not a failed reset,
// so it is an error rather than a status: a status would be shown to whoever
// typed the address, and the person who needs to see this is the one who wired
// it.
var ErrCannotResetPassword = errors.New("passwords: the user must implement auth.CanResetPassword")

// PasswordBroker is the two halves of a password reset, and the only thing a
// controller talks to.
//
// # Why every method runs inside a timebox
//
// Both methods take the same time whether or not they found an account, because
// otherwise they answer a question nobody was allowed to ask. Looking up a user
// and hashing a token take milliseconds; not finding one takes none. A caller
// timing the reset form learns which addresses have accounts -- one request per
// address, no rate limit hit, no log line that looks wrong.
//
// So the work happens inside support.Timebox, which does not return before
// DefaultTimeboxDuration has passed. Reset asks to return early once it has
// committed the new password: by then the answer is already public knowledge to
// whoever holds the token.
//
// # What a status is, and what an error is
//
// A method answers with one of the five status constants and, separately, with
// an error. The status is the outcome of the reset -- something the person who
// filled in the form is told, in their language. The error is a store that did
// not answer, or a broker that was wired wrong: nobody types their way into one,
// and none of them should reach the screen.
type PasswordBroker struct {
	// tokens is where a minted token is hashed into and looked up again.
	tokens TokenRepository

	// users is where the account behind an address is found.
	users auth.UserProvider

	// timebox is the one every call copies its flag from.
	timebox *support.Timebox

	// timeboxDuration is how long each call is held open for, in microseconds.
	timeboxDuration int
}

// NewPasswordBroker returns a broker over tokens and users.
//
// A nil timebox becomes support.NewTimebox, and a timeboxDuration of zero or
// less becomes [DefaultTimeboxDuration].
//
// There is no event dispatcher argument: nothing here announces that a link was
// sent.
func NewPasswordBroker(tokens TokenRepository, users auth.UserProvider, timebox *support.Timebox, timeboxDuration int) *PasswordBroker {
	if timebox == nil {
		timebox = support.NewTimebox()
	}
	if timeboxDuration <= 0 {
		timeboxDuration = DefaultTimeboxDuration
	}
	return &PasswordBroker{tokens: tokens, users: users, timebox: timebox, timeboxDuration: timeboxDuration}
}

// SendResetLink finds the account, asks the throttle, mints the token and
// delivers it. Each step can only refuse.
//
// The callback is optional: it receives the user and the plain token and takes
// delivery over, which is what an application that sends its own mail passes. A
// callback that answers with an empty status means [ResetLinkSent].
//
// Without a callback, delivery is the user's own
// SendPasswordResetNotification, which is where the token becomes a link.
func (b *PasswordBroker) SendResetLink(
	ctx context.Context,
	credentials map[string]any,
	callback func(user auth.CanResetPassword, token string) (string, error),
) (string, error) {
	return b.timeboxed(func(*support.Timebox) (string, error) {
		// First we will check to see if we found a user at the given credentials
		// and if we did not we will answer with the status that says so -- which
		// the form is expected to render exactly like a success.
		user, err := b.GetUser(ctx, credentials)
		if err != nil {
			return "", err
		}
		if user == nil {
			return InvalidUser, nil
		}

		recent, err := b.tokens.RecentlyCreatedToken(ctx, user)
		if err != nil {
			return "", err
		}
		if recent {
			return ResetThrottled, nil
		}

		token, err := b.tokens.Create(ctx, user)
		if err != nil {
			return "", err
		}

		if callback != nil {
			status, err := callback(user, token)
			if err != nil {
				return "", err
			}
			if status == "" {
				return ResetLinkSent, nil
			}
			return status, nil
		}

		// Once we have the reset token we are ready to send the message out to
		// this user with a link to reset their password.
		if err := user.SendPasswordResetNotification(ctx, token); err != nil {
			return "", err
		}
		return ResetLinkSent, nil
	})
}

// Reset redeems a token and hands the new password to the callback.
//
// The order of operations is the whole method: validate the credentials and the
// token, then call the callback with the new password, then delete the token,
// then return early from the timebox.
//
// Deleting after the callback is what makes a failed store leave the token
// alive: if the callback cannot write the new password, the person still holds a
// link that works, instead of one that has been spent on nothing. And deleting
// at all is what makes a reset link single use -- a link that survives its own
// use sits in a mailbox as a permanent key to the account.
func (b *PasswordBroker) Reset(
	ctx context.Context,
	credentials map[string]any,
	callback func(user auth.CanResetPassword, password string) error,
) (string, error) {
	return b.timeboxed(func(timebox *support.Timebox) (string, error) {
		user, status, err := b.validateReset(ctx, credentials)
		if err != nil {
			return "", err
		}
		// If validateReset did not answer with a user, it answered with the
		// status saying why, and that is the answer.
		if status != "" {
			return status, nil
		}

		password, _ := credentials["password"].(string)

		// Once the reset has been validated we call the given callback with the
		// new password. This is the application's chance to store it. Then the
		// token is deleted.
		if err := callback(user, password); err != nil {
			return "", err
		}

		if err := b.tokens.Delete(ctx, user); err != nil {
			return "", err
		}

		timebox.ReturnEarly()

		return PasswordReset, nil
	})
}

// validateReset finds the account the credentials name and checks the token.
//
// It answers with either a user or the status saying why there is none: the
// status is empty when the user is good, which is the shape the caller checks.
func (b *PasswordBroker) validateReset(ctx context.Context, credentials map[string]any) (auth.CanResetPassword, string, error) {
	user, err := b.GetUser(ctx, credentials)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		return nil, InvalidUser, nil
	}

	token, _ := credentials["token"].(string)

	exists, err := b.tokens.Exists(ctx, user, token)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, InvalidToken, nil
	}
	return user, "", nil
}

// GetUser is the account these credentials name, as a resettable one.
//
// The token is taken out of the credentials before they are handed to the user
// provider -- it is not a column and would become a where clause that matches
// nobody. The provider drops the password keys itself, which is why they are
// still in here.
//
// A user type that cannot be sent a reset link is [ErrCannotResetPassword].
func (b *PasswordBroker) GetUser(ctx context.Context, credentials map[string]any) (auth.CanResetPassword, error) {
	except := make(map[string]any, len(credentials))
	for key, value := range credentials {
		if key == "token" {
			continue
		}
		except[key] = value
	}

	user, err := b.users.RetrieveByCredentials(ctx, except)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	resettable, ok := user.(auth.CanResetPassword)
	if !ok {
		return nil, fmt.Errorf("%w: %T does not", ErrCannotResetPassword, user)
	}
	return resettable, nil
}

// CreateToken mints a token for this account and stores a hash of it.
func (b *PasswordBroker) CreateToken(ctx context.Context, user auth.CanResetPassword) (string, error) {
	return b.tokens.Create(ctx, user)
}

// DeleteToken destroys this account's token.
func (b *PasswordBroker) DeleteToken(ctx context.Context, user auth.CanResetPassword) error {
	return b.tokens.Delete(ctx, user)
}

// TokenExists reports that a live token for this account matches the one
// offered.
func (b *PasswordBroker) TokenExists(ctx context.Context, user auth.CanResetPassword, token string) (bool, error) {
	return b.tokens.Exists(ctx, user, token)
}

// GetRepository is the token repository this broker was built with.
func (b *PasswordBroker) GetRepository() TokenRepository { return b.tokens }

// GetTimebox is the broker's own timebox, which every call copies its flag
// from.
func (b *PasswordBroker) GetTimebox() *support.Timebox { return b.timebox }

// timeboxed runs fn inside a timebox and puts its status back.
//
// support.Timebox.Call carries its result as an any, because a method cannot
// take a type parameter in Go; this puts the string back and keeps the two
// callers from repeating the assertion. An error comes back after the box has
// been waited out, so a failing store does not answer faster than a working one
// either.
//
// # Why the call gets a timebox of its own
//
// The broker outlives the request and is shared by every goroutine serving one.
// A return-early flag held on it is a flag one request sets and the next reads
// -- the reset link request that followed a successful reset would answer
// without waiting, which is exactly the timing signal this type exists to
// remove -- and two resets at once would be a data race on it.
//
// So the flag is per call, copied from the broker's timebox. [PasswordBroker.GetTimebox]
// still answers with the broker's.
func (b *PasswordBroker) timeboxed(fn func(*support.Timebox) (string, error)) (string, error) {
	timebox := *b.timebox
	timebox.DontReturnEarly()

	result, err := timebox.Call(func(timebox *support.Timebox) (any, error) {
		return fn(timebox)
	}, b.timeboxDuration)
	if err != nil {
		return "", err
	}
	status, _ := result.(string)
	return status, nil
}
