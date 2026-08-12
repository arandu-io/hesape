package auth

import (
	"context"
	"errors"
	"time"
)

// MustVerifyEmailTrait is Illuminate\Auth\MustVerifyEmail: the column, the
// check and the notification behind "please confirm your address".
//
// The PHP is a trait a user model writes `use MustVerifyEmail` for, and here it
// is a struct the model embeds, which is how this repository carries every
// trait. The name is the only thing that moved, and it moved by one suffix:
// PHP has this trait in Illuminate\Auth and the contract of the same name in
// Illuminate\Contracts\Auth, two namespaces; ADR 0045 puts the contract in this
// package, and Go has one namespace per package, so the trait takes the suffix
// that ADR 0044 allows when a name is already spoken for. The contract is
// [MustVerifyEmail] in contracts.go and this satisfies it.
//
// Two of the trait's methods reach out of the model -- save() and notify() --
// and a struct in Go cannot reach the struct it was embedded in. They are the
// Save and Notify fields, which is the house pattern for a trait's abstract
// method, and the model sets them where it is built.
type MustVerifyEmailTrait struct {
	// Email is the model's $email, which getEmailForVerification returns.
	Email string

	// EmailVerifiedAt is the model's email_verified_at column. The zero time is
	// the PHP's null: not verified.
	EmailVerifiedAt time.Time

	// Save is the model's save(), which markEmailAsVerified and
	// markEmailAsUnverified call through forceFill()->save(). The PHP answers
	// with save()'s bool; the contract answers with an error, so this does.
	Save func(ctx context.Context) error

	// Notify is RoutesNotifications::notify, which the PHP reaches through the
	// model. It is given [MustVerifyEmailTrait.VerifyEmail].
	Notify func(ctx context.Context, notification any) error

	// VerifyEmail is the `new VerifyEmail` the PHP hands to notify(). The class
	// is Illuminate\Auth\Notifications\VerifyEmail, which is hesape/auth/
	// notifications -- a package the root of auth cannot import, because it
	// imports nothing outside the standard library (see doc.go). The model puts
	// the notification here, and Notify is what knows how to send it.
	VerifyEmail any
}

var _ MustVerifyEmail = (*MustVerifyEmailTrait)(nil)

// HasVerifiedEmail is MustVerifyEmail::hasVerifiedEmail.
func (m *MustVerifyEmailTrait) HasVerifiedEmail() bool {
	return !m.EmailVerifiedAt.IsZero()
}

// MarkEmailAsVerified is MustVerifyEmail::markEmailAsVerified.
//
// The PHP stamps the column with the model's freshTimestamp(); a struct has no
// model to ask, so it is time.Now.
func (m *MustVerifyEmailTrait) MarkEmailAsVerified(ctx context.Context) error {
	m.EmailVerifiedAt = time.Now()

	return m.save(ctx)
}

// MarkEmailAsUnverified is MustVerifyEmail::markEmailAsUnverified.
func (m *MustVerifyEmailTrait) MarkEmailAsUnverified(ctx context.Context) error {
	m.EmailVerifiedAt = time.Time{}

	return m.save(ctx)
}

// SendEmailVerificationNotification is
// MustVerifyEmail::sendEmailVerificationNotification.
func (m *MustVerifyEmailTrait) SendEmailVerificationNotification(ctx context.Context) error {
	if m.Notify == nil {
		return errors.New("auth: MustVerifyEmailTrait.Notify is not set, so the verification notification has nowhere to go")
	}
	return m.Notify(ctx, m.VerifyEmail)
}

// GetEmailForVerification is MustVerifyEmail::getEmailForVerification.
func (m *MustVerifyEmailTrait) GetEmailForVerification() string {
	return m.Email
}

// save is the ->save() the two mark methods end with.
func (m *MustVerifyEmailTrait) save(ctx context.Context) error {
	if m.Save == nil {
		return errors.New("auth: MustVerifyEmailTrait.Save is not set, so the verification stamp was not written")
	}
	return m.Save(ctx)
}
