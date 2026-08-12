package passwords

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/auth"
)

// ErrNoResetNotifier is what SendPasswordResetNotification answers when nothing
// was wired to deliver the link.
//
// It is an error and not a silent success, because the alternative is a form
// that reports "we have sent you a link" for an account nobody will ever hear
// from, and a support ticket that takes a week to reach the empty field that
// caused it.
var ErrNoResetNotifier = errors.New("passwords: the user has no Notify set, so the reset link cannot be delivered")

// CanResetPassword answers the Illuminate\Auth\Passwords\CanResetPassword
// trait: the two methods a user type needs before a broker will reset it.
//
// PHP has traits and Go has embedding, so what is `use CanResetPassword;` there
// is an embedded field here:
//
//	type User struct {
//		passwords.CanResetPassword
//		ID   string
//		Name string
//	}
//
// The embedded value carries the address, and a user built from a row has to be
// given it -- which is the one thing to watch. The trait reads $this->email off
// the model it is mixed into; embedding cannot reach the outer struct's fields,
// so the address is a field on this struct and the constructor of the user type
// fills it. An outer struct that declares its own Email shadows nothing this
// reads: GetEmailForPasswordReset still answers with the field below, and an
// unfilled one is empty.
//
// It satisfies auth.CanResetPassword, which is
// Illuminate\Contracts\Auth\CanResetPassword.
type CanResetPassword struct {
	// Email answers the $email property the trait reads.
	Email string

	// Notify answers `$this->notify(new ResetPassword($token))`.
	//
	// The trait can write that line because Notifiable is mixed into the same
	// model and Illuminate\Auth\Notifications\ResetPassword is one import away.
	// Here the notification is hesape/auth/notifications' and the notifier is the
	// application's, and neither belongs to this package -- so the line the trait
	// hard-codes is a function the user type is given, and the wiring decides what
	// a reset link looks like.
	Notify func(ctx context.Context, token string) error
}

// Verify at compile time that the embeddable is the contract the broker needs.
var _ auth.CanResetPassword = CanResetPassword{}

// GetEmailForPasswordReset answers CanResetPassword::getEmailForPasswordReset.
func (c CanResetPassword) GetEmailForPasswordReset() string { return c.Email }

// SendPasswordResetNotification answers
// CanResetPassword::sendPasswordResetNotification.
//
// The token is the plain one, and this is the only place it is ever seen after
// the repository minted it. It goes into a link and nowhere else -- not into a
// log line, not into an error message.
func (c CanResetPassword) SendPasswordResetNotification(ctx context.Context, token string) error {
	if c.Notify == nil {
		return ErrNoResetNotifier
	}
	return c.Notify(ctx, token)
}
