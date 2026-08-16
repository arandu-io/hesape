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

// verifyEmailCallbacks is the package state behind this notification's two
// setters. See [resetPasswordCallbacks] for why it carries a mutex.
var verifyEmailCallbacks struct {
	mu sync.RWMutex
	// createURL is the callback that should be used to create the verify email
	// URL.
	createURL func(notifiable hnotifications.Notifiable) string
	// toMail is the callback that should be used to build the mail message.
	toMail func(notifiable hnotifications.Notifiable, verificationURL string) messages.Mail
}

// VerifyEmail is the message with the confirmation link in it, sent when an
// account is created.
//
// auth/listeners.SendEmailVerificationNotification is what triggers it, through
// the account's own SendEmailVerificationNotification.
//
// It carries nothing, because everything it needs -- the id and the address --
// is on the recipient.
type VerifyEmail struct {
	hnotifications.NotificationBase
}

// KeyVerifyEmail is the stable name this notification is stored under. See
// [KeyResetPassword].
const KeyVerifyEmail hnotifications.Key = "auth.verify-email"

// NewVerifyEmail returns the notification.
//
// It takes nothing, and exists so that the two notifications in this package
// are built the same way at a call site.
func NewVerifyEmail() VerifyEmail { return VerifyEmail{} }

// Key is [KeyVerifyEmail]. See [ResetPassword.Key].
func (VerifyEmail) Key() hnotifications.Key { return KeyVerifyEmail }

// Via gets the notification's channels.
//
// It is mail and nothing else: the whole purpose is to prove that the address
// works, so it can only go to the address.
func (VerifyEmail) Via(hnotifications.Notifiable) []hnotifications.ChannelName {
	return []hnotifications.ChannelName{hnotifications.ChannelMail}
}

// ToMail builds the mail representation of the notification.
//
// Note the order, which matters: the URL is built BEFORE the ToMailUsing
// callback is consulted, and handed to it -- so a project that rewrites the
// message still gets the link this notification would have used.
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
// given URL, when nothing was set with ToMailUsing.
func (n VerifyEmail) buildMailMessage(link string) messages.Mail {
	return messages.NewMail().
		Subject("Verify your email address").
		Line("Please click the button below to verify your email address.").
		Action("Verify Email Address", link).
		Line("If you did not create an account, no further action is required.")
}

// verificationURL gets the verification URL for the given notifiable.
//
// There is no URL generator and no application key here, so without a callback
// this answers a PATH -- no host, and no signature.
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
// It is a method on the zero value that sets package state -- see the package
// comment for why. Passing nil restores the built-in URL.
//
// The expiry of a signed link belongs to whatever signs it, so it is the
// callback's and not this package's.
func (VerifyEmail) CreateUrlUsing(callback func(notifiable hnotifications.Notifiable) string) {
	verifyEmailCallbacks.mu.Lock()
	verifyEmailCallbacks.createURL = callback
	verifyEmailCallbacks.mu.Unlock()
}

// ToMailUsing sets a callback that should be used when building the
// notification mail message.
//
// It is a method on the zero value that sets package state. The URL it receives
// is the one [VerifyEmail.verificationURL] produced.
func (VerifyEmail) ToMailUsing(callback func(notifiable hnotifications.Notifiable, verificationURL string) messages.Mail) {
	verifyEmailCallbacks.mu.Lock()
	verifyEmailCallbacks.toMail = callback
	verifyEmailCallbacks.mu.Unlock()
}

// emailForVerification is the address the recipient asked to have confirmed,
// falling back to the one it is routed at on the mail channel.
func emailForVerification(notifiable hnotifications.Notifiable) string {
	if verifiable, ok := notifiable.(auth.MustVerifyEmail); ok {
		if address := verifiable.GetEmailForVerification(); address != "" {
			return address
		}
	}
	return notifiable.RouteFor(hnotifications.ChannelMail)
}

// emailHash is the SHA-1 of the address being confirmed.
//
// SHA-1 and not something current, on purpose: the digest is not a security
// primitive here and is not being asked to resist anything. It is an identifier
// that changes when the address changes, so that a link mailed to an old
// address stops confirming a new one.
//
// What keeps a verification link from being forged is the SIGNATURE, which is
// the caller's -- see [VerifyEmail.CreateUrlUsing].
func emailHash(address string) string {
	sum := sha1.Sum([]byte(address))
	return hex.EncodeToString(sum[:])
}
