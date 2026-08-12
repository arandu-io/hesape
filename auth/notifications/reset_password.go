package notifications

import (
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	hnotifications "github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// ResetLinkLifetime is how long a reset link is said to last.
//
// PHP reads it from config, `auth.passwords.<broker>.expire`, in minutes, whose
// shipped value is 60. It is only the sentence in the message -- the broker's
// token repository is what actually refuses an old token -- so a broker
// configured differently sets [ResetPassword.Expire] rather than changing this.
const ResetLinkLifetime = 60 * time.Minute

// resetPasswordCallbacks holds what PHP keeps in two static properties.
//
// A mutex rather than a bare variable: these are written once at boot and read
// on every send, which is exactly the shape -race flags the moment a test writes
// one while another goroutine is sending.
var resetPasswordCallbacks struct {
	mu sync.RWMutex
	// createURL is the callback that should be used to create the reset
	// password URL.
	createURL func(notifiable hnotifications.Notifiable, token string) string
	// toMail is the callback that should be used to build the mail message.
	toMail func(notifiable hnotifications.Notifiable, token string) messages.Mail
}

// ResetPassword is Illuminate\Auth\Notifications\ResetPassword.
//
// It carries the reset token and turns into the e-mail with the link in it. The
// token is minted and stored by the password broker; this is only the message.
type ResetPassword struct {
	hnotifications.NotificationBase

	// Token is the password reset token.
	//
	// PHP marks the constructor parameter #[\SensitiveParameter], because
	// whoever holds this string can set the account's password. Go has no such
	// attribute, so the rule lives here: it must not be logged, and it must not
	// be put anywhere a link is not.
	Token string

	// Expire is how long the link is said to last, for the sentence in the
	// message. Zero means [ResetLinkLifetime].
	//
	// PHP reaches config() for it. There is no global configuration to reach
	// (ADR 0001), so the broker that builds the notification passes its own
	// expiry -- which is the one that will actually be enforced, and the reason
	// the two can never disagree here.
	Expire time.Duration
}

// KeyResetPassword is the stable name this notification is stored under.
//
// hesape/notifications keys a notification by name rather than by class, so that
// renaming the Go type does not rewrite every row already written. See
// notifications.Key.
const KeyResetPassword hnotifications.Key = "auth.password-reset"

// NewResetPassword creates a notification instance.
//
// It is ResetPassword::__construct.
func NewResetPassword(token string) ResetPassword { return ResetPassword{Token: token} }

// Key is [KeyResetPassword].
//
// It has no counterpart in PHP, where the stored type is the class name. See
// notifications.Key for why this collection names its notifications instead.
func (ResetPassword) Key() hnotifications.Key { return KeyResetPassword }

// Via gets the notification's channels.
//
// It is ResetPassword::via, which answers ['mail'] and nothing else: a password
// reset that arrived as a row in a bell menu is a password reset for somebody
// who is already signed in.
func (ResetPassword) Via(hnotifications.Notifiable) []hnotifications.ChannelName {
	return []hnotifications.ChannelName{hnotifications.ChannelMail}
}

// ToMail builds the mail representation of the notification.
//
// It is ResetPassword::toMail.
func (n ResetPassword) ToMail(notifiable hnotifications.Notifiable) messages.Mail {
	resetPasswordCallbacks.mu.RLock()
	build := resetPasswordCallbacks.toMail
	resetPasswordCallbacks.mu.RUnlock()

	if build != nil {
		return build(notifiable, n.Token)
	}
	return n.buildMailMessage(n.resetURL(notifiable))
}

// buildMailMessage gets the reset password notification mail message for the
// given URL.
//
// It is ResetPassword::buildMailMessage, which is protected. The four lines are
// Illuminate's, in Illuminate's order, minus the Lang::get around each of them
// -- see the package comment.
func (n ResetPassword) buildMailMessage(link string) messages.Mail {
	expire := n.Expire
	if expire <= 0 {
		expire = ResetLinkLifetime
	}

	return messages.NewMail().
		Subject("Reset your password").
		Line("You are receiving this email because we received a password reset request for your account.").
		Action("Reset Password", link).
		Line("This password reset link will expire in " + minutes(expire) + " minutes.").
		Line("If you did not request a password reset, no further action is required.")
}

// resetURL gets the reset URL for the given notifiable.
//
// It is ResetPassword::resetUrl, which is protected. Without a callback it
// answers the PATH of Laravel's password.reset route, which is not absolute and
// which messages.Mail.Validate refuses -- see the package comment for why that
// is the right failure.
func (n ResetPassword) resetURL(notifiable hnotifications.Notifiable) string {
	resetPasswordCallbacks.mu.RLock()
	create := resetPasswordCallbacks.createURL
	resetPasswordCallbacks.mu.RUnlock()

	if create != nil {
		return create(notifiable, n.Token)
	}

	link := "/reset-password/" + url.PathEscape(n.Token)
	if address := emailForPasswordReset(notifiable); address != "" {
		link += "?email=" + url.QueryEscape(address)
	}
	return link
}

// CreateUrlUsing sets a callback that should be used when creating the reset
// password button URL.
//
// It is ResetPassword::createUrlUsing, which is static -- see the package
// comment for why a static became a method on the zero value. Passing nil
// restores the built-in URL, which is what a test does when it is finished.
//
//	notifications.ResetPassword{}.CreateUrlUsing(func(to hnotifications.Notifiable, token string) string {
//		return "https://app.example.com/reset-password/" + token
//	})
func (ResetPassword) CreateUrlUsing(callback func(notifiable hnotifications.Notifiable, token string) string) {
	resetPasswordCallbacks.mu.Lock()
	resetPasswordCallbacks.createURL = callback
	resetPasswordCallbacks.mu.Unlock()
}

// ToMailUsing sets a callback that should be used when building the
// notification mail message.
//
// It is ResetPassword::toMailUsing, which is static. It replaces the whole
// message, link included, so a project that only wants a different address uses
// [ResetPassword.CreateUrlUsing] instead.
func (ResetPassword) ToMailUsing(callback func(notifiable hnotifications.Notifiable, token string) messages.Mail) {
	resetPasswordCallbacks.mu.Lock()
	resetPasswordCallbacks.toMail = callback
	resetPasswordCallbacks.mu.Unlock()
}

// minutes renders a duration the way PHP's :count placeholder receives it: a
// whole number of minutes.
//
// It rounds up, so a link that lasts ninety seconds is announced as two minutes
// rather than as one -- a sentence that promises less time than the link has is
// merely cautious, and one that promises more is a person clicking a dead link.
func minutes(d time.Duration) string {
	count := int64(d / time.Minute)
	if d%time.Minute != 0 {
		count++
	}
	return strconv.FormatInt(count, 10)
}

// emailForPasswordReset is $notifiable->getEmailForPasswordReset(), falling back
// to the address the recipient is routed at on the mail channel.
//
// The fallback is what makes an on-demand recipient work: an Anonymous carries
// an address and no account, and a reset link mailed to an address somebody
// typed into a form is the case every application has.
func emailForPasswordReset(notifiable hnotifications.Notifiable) string {
	if resettable, ok := notifiable.(auth.CanResetPassword); ok {
		if address := resettable.GetEmailForPasswordReset(); address != "" {
			return address
		}
	}
	return notifiable.RouteFor(hnotifications.ChannelMail)
}
