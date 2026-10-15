package account

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
)

var (
	// ErrUserNotFound means no account matched inside the grant's tenant. It is
	// the model's own not-found sentinel, so every lookup's error matches it
	// under errors.Is, and an action that returns it is answered 404.
	ErrUserNotFound = model.ErrModelNotFound

	// ErrEmailTaken means an address already belongs to an account in this
	// tenant. The error a write returns for it also matches
	// database.ErrUniqueViolation, so an action that returns it is answered 409.
	ErrEmailTaken = errors.New("user: email already registered in this tenant")

	// ErrVerificationAddressChanged refuses a code issued for an older address.
	ErrVerificationAddressChanged = errors.New("user: verification was issued for a different address")

	// ErrResetLinkSpent refuses reset state invalidated by an account change.
	ErrResetLinkSpent = errors.New("user: password reset is no longer valid")

	// ErrAccountRefused is a sign-in Hooks.Admit refused after the password
	// proved right. It wraps auth.ErrInvalidCredentials, so every caller that
	// answers a wrong password answers this the same way.
	ErrAccountRefused = fmt.Errorf("user: account refused: %w", auth.ErrInvalidCredentials)

	// ErrTwoFactorNotEnrolled means no confirmed factor can challenge the account.
	ErrTwoFactorNotEnrolled = errors.New("two-factor: this account has no enrolment")

	// ErrTwoFactorAlreadyEnabled refuses replacing a working enrolment implicitly.
	ErrTwoFactorAlreadyEnabled = errors.New("two-factor: this account already has an enabled enrolment")

	// ErrInvalidRecoveryCode deliberately unwraps to the native invalid-code sentinel.
	ErrInvalidRecoveryCode = fmt.Errorf("%w: recovery code is wrong or already spent", twofactor.ErrInvalidCode)

	// ErrTwoFactorLocked means the account offered too many codes to the
	// sign-in challenge. The pending sign-in is spent: the person signs in
	// again once the window has passed.
	ErrTwoFactorLocked = errors.New("two-factor: too many codes offered to the sign-in challenge")
)

// refused is what a sign-in Admit refused returns: ErrAccountRefused, and the
// reason the hook gave, both reachable under errors.Is. The message is the
// sentinel's alone, because the reason is for the log and not for the person
// at the form.
type refused struct{ reason error }

func (r refused) Error() string   { return ErrAccountRefused.Error() }
func (r refused) Unwrap() []error { return []error{ErrAccountRefused, r.reason} }

// TooManyAttemptsError reports the remaining sign-in lockout without naming an account.
type TooManyAttemptsError struct{ RetryAfter time.Duration }

// Seconds is the retry delay rounded up for an HTTP Retry-After header.
func (e TooManyAttemptsError) Seconds() int { return seconds(e.RetryAfter) }

// Error implements error.
func (e TooManyAttemptsError) Error() string {
	return "user: too many attempts, try again in " + strconv.Itoa(e.Seconds()) + " seconds"
}

// HTTPStatus is 429, which is what an action that returns this error answers.
func (TooManyAttemptsError) HTTPStatus() int { return http.StatusTooManyRequests }

// TwoFactorLockedError reports that the sign-in challenge refused a code
// because the account spent its attempts, and the longest it may have to wait
// before trying again.
//
// It matches ErrTwoFactorLocked and twofactor.ErrInvalidCode under errors.Is,
// so a caller that only knows how to refuse a wrong code still refuses this
// one; a caller that knows the difference sends the person back to sign in.
type TwoFactorLockedError struct{ RetryAfter time.Duration }

// Seconds is the retry delay rounded up for an HTTP Retry-After header.
func (e TwoFactorLockedError) Seconds() int { return seconds(e.RetryAfter) }

// Error implements error.
func (e TwoFactorLockedError) Error() string {
	return ErrTwoFactorLocked.Error() + ", sign in again in " + strconv.Itoa(e.Seconds()) + " seconds"
}

// HTTPStatus is 429, which is what an action that returns this error answers.
func (TwoFactorLockedError) HTTPStatus() int { return http.StatusTooManyRequests }

// Unwrap exposes both sentinels the error stands for.
func (e TwoFactorLockedError) Unwrap() []error {
	return []error{ErrTwoFactorLocked, twofactor.ErrInvalidCode}
}

// seconds rounds a delay up to whole seconds, and never below one: a
// Retry-After of zero tells a client to try again at once.
func seconds(delay time.Duration) int {
	n := int((delay + time.Second - 1) / time.Second)
	if n < 1 {
		return 1
	}
	return n
}
