package notifications

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"sync"

	"github.com/arandu-io/hesape/auth"
	hnotifications "github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// verifyEmailCallbacks holds what PHP keeps in two static properties. See
// [resetPasswordCallbacks] for why it carries a mutex.
var verifyEmailCallbacks struct {
	mu sync.RWMutex
	// createURL is the callback that should be used to create the verify email
	// URL.
	createURL func(notifiable hnotifications.Notifiable) string
	// toMail is the callback that should be used to build the mail message.
	toMail func(notifiable hnotifications.Notifiable, verificationURL string) messages.Mail
}

// VerifyEmail is Illuminate\Auth\Notifications\VerifyEmail.
//
// It is the message with the confirmation link in it, sent when an account is
// created. auth/listeners.SendEmailVerificationNotification is what triggers it,
// through the account's own SendEmailVerificationNotification.
//
// It carries nothing: PHP's has no constructor either, because everything it
// needs -- the id and the address -- is on the recipient.
type VerifyEmail struct {
	hnotifications.NotificationBase
}

// KeyVerifyEmail is the stable name this notification is stored under. See
// [KeyResetPassword].
const KeyVerifyEmail hnotifications.Key = "auth.verify-email"

// NewVerifyEmail returns the notification.
//
// PHP has no constructor for it; this exists so that the two notifications in
// this package are built the same way at a call site.
func NewVerifyEmail() VerifyEmail { return VerifyEmail{} }

// Key is [KeyVerifyEmail]. See [ResetPassword.Key].
func (VerifyEmail) Key() hnotifications.Key { return KeyVerifyEmail }

// Via gets the notification's channels.
//
// It is VerifyEmail::via, which answers ['mail']: the whole purpose is to prove
// that the address works, so it can only go to the address.
func (VerifyEmail) Via(hnotifications.Notifiable) []hnotifications.ChannelName {
	return []hnotifications.ChannelName{hnotifications.ChannelMail}
}

// ToMail builds the mail representation of the notification.
//
// It is VerifyEmail::toMail. Note the order, which is PHP's and matters: the URL
// is built BEFORE the toMail callback is consulted, and handed to it -- so a
// project that rewrites the message still gets the link this notification would
// have used.
func (n VerifyEmail) ToMail(notifiable hnotifications.Notifiable) messages.Mail {
	link := n.verificationURL(notifiable)

	verifyEmailCallbacks.mu.RLock()
	build := verifyEmailCallbacks.toMail
	verifyEmailCallbacks.mu.RUnlock()

	if build != nil {
		return build(notifiable, link)
	}
	return n.buildMailMessage(link)
}

// buildMailMessage gets the verify email notification mail message for the
// given URL.
//
// It is VerifyEmail::buildMailMessage, which is protected. The lines are
// Illuminate's, minus Lang::get -- see the package comment.
func (n VerifyEmail) buildMailMessage(link string) messages.Mail {
	return messages.NewMail().
		Subject("Verify your email address").
		Line("Please click the button below to verify your email address.").
		Action("Verify Email Address", link).
		Line("If you did not create an account, no further action is required.")
}

// verificationURL gets the verification URL for the given notifiable.
//
// It is VerifyEmail::verificationUrl, which is protected. PHP mints a temporary
// signed route; there is no URL generator and no application key here, so
// without a callback this answers the PATH of Laravel's verification.verify
// route, unsigned.
//
// That link is not safe to send. See the package comment: an unsigned
// id-and-hash link can be replayed by anybody who knows the address, because the
// hash is a digest of the address. CreateUrlUsing is not optional in production.
func (n VerifyEmail) verificationURL(notifiable hnotifications.Notifiable) string {
	verifyEmailCallbacks.mu.RLock()
	create := verifyEmailCallbacks.createURL
	verifyEmailCallbacks.mu.RUnlock()

	if create != nil {
		return create(notifiable)
	}

	return "/verify-email/" +
		url.PathEscape(notifiable.NotifiableID()) + "/" +
		emailHash(emailForVerification(notifiable))
}

// CreateUrlUsing sets a callback that should be used when creating the email
// verification URL.
//
// It is VerifyEmail::createUrlUsing, which is static -- see the package comment
// for why a static became a method on the zero value. Passing nil restores the
// built-in URL.
//
// The expiry PHP reads from auth.verification.expire belongs to whatever signs
// the link, so it is the callback's, not this package's.
func (VerifyEmail) CreateUrlUsing(callback func(notifiable hnotifications.Notifiable) string) {
	verifyEmailCallbacks.mu.Lock()
	verifyEmailCallbacks.createURL = callback
	verifyEmailCallbacks.mu.Unlock()
}

// ToMailUsing sets a callback that should be used when building the
// notification mail message.
//
// It is VerifyEmail::toMailUsing, which is static. The URL it receives is the
// one [VerifyEmail.verificationURL] produced.
func (VerifyEmail) ToMailUsing(callback func(notifiable hnotifications.Notifiable, verificationURL string) messages.Mail) {
	verifyEmailCallbacks.mu.Lock()
	verifyEmailCallbacks.toMail = callback
	verifyEmailCallbacks.mu.Unlock()
}

// emailForVerification is $notifiable->getEmailForVerification(), falling back
// to the address the recipient is routed at on the mail channel.
func emailForVerification(notifiable hnotifications.Notifiable) string {
	if verifiable, ok := notifiable.(auth.MustVerifyEmail); ok {
		if address := verifiable.GetEmailForVerification(); address != "" {
			return address
		}
	}
	return notifiable.RouteFor(hnotifications.ChannelMail)
}

// emailHash is PHP's sha1($notifiable->getEmailForVerification()).
//
// SHA-1 and not something current, on purpose: the digest is not a security
// primitive here and is not being asked to resist anything. It is an identifier
// that changes when the address changes, so that a link mailed to an old address
// stops confirming a new one -- and it is SHA-1 so that a link minted by a
// Laravel application and a link minted by this one are the same string, which
// is what lets a project move over without invalidating every unsent message.
//
// What keeps a verification link from being forged is the SIGNATURE, which is
// the caller's -- see [VerifyEmail.CreateUrlUsing].
func emailHash(address string) string {
	sum := sha1.Sum([]byte(address))
	return hex.EncodeToString(sum[:])
}
