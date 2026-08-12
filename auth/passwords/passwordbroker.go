package passwords

import (
	"context"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/support"
)

// The statuses a broker answers with. They answer the five constants of
// Illuminate\Contracts\Auth\PasswordBroker, values unchanged, because they are
// translation keys: the application looks each one up to put a sentence on the
// screen, and changing the string here would silently un-translate every one of
// them.
//
// They are declared with the broker rather than in a contracts package, which is
// ADR 0045.
const (
	// ResetLinkSent answers PasswordBroker::RESET_LINK_SENT.
	ResetLinkSent = "passwords.sent"

	// PasswordReset answers PasswordBroker::PASSWORD_RESET.
	PasswordReset = "passwords.reset"

	// InvalidUser answers PasswordBroker::INVALID_USER: no account matched the
	// credentials. It is a status and not an error, and the sign-in form is
	// expected to render it identically to ResetLinkSent -- an anonymous caller
	// must not learn from this form which addresses have accounts.
	InvalidUser = "passwords.user"

	// InvalidToken answers PasswordBroker::INVALID_TOKEN: no live token for this
	// account matches the one offered. Expired and wrong are the same answer, on
	// purpose.
	InvalidToken = "passwords.token"

	// ResetThrottled answers PasswordBroker::RESET_THROTTLED: a token was minted
	// for this account too recently to mint another.
	ResetThrottled = "passwords.throttled"
)

// DefaultTimeboxDuration answers the PHP's default $timeboxDuration: 200
// milliseconds, in microseconds, which is what support.Timebox counts.
const DefaultTimeboxDuration = 200000

// ErrCannotResetPassword answers the UnexpectedValueException that
// PasswordBroker::getUser throws.
//
// It fires when the user provider is configured to return a user type that
// cannot be sent a reset link. That is a wiring mistake and not a failed reset,
// so it is an error rather than a status: a status would be shown to whoever
// typed the address, and the person who needs to see this is the one who wired
// it.
var ErrCannotResetPassword = errors.New("passwords: the user must implement auth.CanResetPassword")

// PasswordBroker answers Illuminate\Auth\Passwords\PasswordBroker: the two
// halves of a password reset, and the only thing a controller talks to.
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
// DefaultTimeboxDuration has passed. Reset calls returnEarly once it has
// committed the new password, as the PHP does: by then the answer is already
// public knowledge to whoever holds the token.
//
// # What a status is, and what an error is
//
// A method answers with one of the five status constants and, separately, with
// an error. The status is the outcome of the reset -- something the person who
// filled in the form is told, in their language. The error is a store that did
// not answer, or a broker that was wired wrong: nobody types their way into one,
// and none of them should reach the screen.
type PasswordBroker struct {
	// tokens answers $tokens.
	tokens TokenRepository

	// users answers $users.
	users auth.UserProvider

	// timebox answers $timebox.
	timebox *support.Timebox

	// timeboxDuration answers $timeboxDuration, in microseconds.
	timeboxDuration int
}

// NewPasswordBroker answers PasswordBroker::__construct.
//
// A nil timebox is the PHP's `$timebox ?: new Timebox`, and a timeboxDuration of
// zero or less is its default argument of 200000 -- neither of which Go has
// syntax for.
//
// The PHP's third parameter, the event dispatcher, is not here. See the package
// doc: the event it dispatches belongs to a package this one does not own.
func NewPasswordBroker(tokens TokenRepository, users auth.UserProvider, timebox *support.Timebox, timeboxDuration int) *PasswordBroker {
	if timebox == nil {
		timebox = support.NewTimebox()
	}
	if timeboxDuration <= 0 {
		timeboxDuration = DefaultTimeboxDuration
	}
	return &PasswordBroker{tokens: tokens, users: users, timebox: timebox, timeboxDuration: timeboxDuration}
}

// SendResetLink answers PasswordBroker::sendResetLink.
//
// The order is the PHP's, and each step can only refuse: find the account, ask
// the throttle, mint the token, deliver it. The callback is the PHP's optional
// $callback -- it receives the user and the plain token and takes delivery over,
// which is what an application that sends its own mail passes. A callback that
// answers with an empty status means ResetLinkSent, which is the PHP's `??`.
//
// Without a callback the notification is the user's own
// sendPasswordResetNotification, which is where the token becomes a link.
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

// Reset answers PasswordBroker::reset.
//
// The order of operations is the whole method and it is the PHP's exactly:
// validate the credentials and the token, then call the callback with the new
// password, then delete the token, then return early from the timebox.
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

// validateReset answers the protected PasswordBroker::validateReset.
//
// The PHP returns either a user or a status string, which one return value can
// hold there and cannot here. The status is empty when the user is good, which
// is the shape every caller of it checks anyway.
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

// GetUser answers PasswordBroker::getUser.
//
// The token is taken out of the credentials before they are handed to the user
// provider -- it is not a column and would become a where clause that matches
// nobody. The provider drops the password keys itself, which is why they are
// still in here.
//
// A user type that cannot be sent a reset link is ErrCannotResetPassword, which
// is the PHP's UnexpectedValueException.
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

// CreateToken answers PasswordBroker::createToken.
func (b *PasswordBroker) CreateToken(ctx context.Context, user auth.CanResetPassword) (string, error) {
	return b.tokens.Create(ctx, user)
}

// DeleteToken answers PasswordBroker::deleteToken.
func (b *PasswordBroker) DeleteToken(ctx context.Context, user auth.CanResetPassword) error {
	return b.tokens.Delete(ctx, user)
}

// TokenExists answers PasswordBroker::tokenExists.
func (b *PasswordBroker) TokenExists(ctx context.Context, user auth.CanResetPassword, token string) (bool, error) {
	return b.tokens.Exists(ctx, user, token)
}

// GetRepository answers PasswordBroker::getRepository.
func (b *PasswordBroker) GetRepository() TokenRepository { return b.tokens }

// GetTimebox answers PasswordBroker::getTimebox.
func (b *PasswordBroker) GetTimebox() *support.Timebox { return b.timebox }

// timeboxed is the $this->timebox->call(..., $this->timeboxDuration) that wraps
// both public methods.
//
// support.Timebox.Call carries its result as an any, because a method cannot
// take a type parameter in Go; this puts the string back and keeps the two
// callers from repeating the assertion. An error comes back after the box has
// been waited out, so a failing store does not answer faster than a working one
// either.
//
// # Why the call gets a timebox of its own
//
// The PHP calls $this->timebox, one instance for the life of the broker, and
// gets away with it because a PHP process serves one request: the container
// builds a broker, reset() sets earlyReturn on its timebox, and the process
// forgets both.
//
// Here the broker outlives the request and is shared by every goroutine serving
// one. A flag held on it is a flag one request sets and the next reads -- the
// reset link request that followed a successful reset would answer without
// waiting, which is exactly the timing signal this class exists to remove -- and
// two resets at once would be a data race on it.
//
// So the flag is per call, copied from the broker's timebox. GetTimebox still
// answers with the broker's, as the PHP's getTimebox does.
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
