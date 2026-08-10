package channels

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// Mailer is the little the mail channel needs to send a message.
//
// It is a seam to hesape/mail, which is layer 4 of
// docs/31-reorganizacao-hesape.md and does not exist yet. When it does, the
// adapter is a dozen lines: turn a messages.Mail into the envelope and the two
// rendered parts, and hand it to the Mailer there.
//
// It stays an interface after that. A notification channel that imported the
// whole mailer would drag the view registry and every transport behind every
// package that sends a notification, and could not be exercised without one.
type Mailer interface {
	// Send delivers one message to one address, and answers with whatever the
	// provider called it -- a message id, for the log line that has to be
	// matched against a support ticket. The empty string is fine when the
	// transport has nothing to say.
	Send(ctx context.Context, to string, m messages.Mail) (string, error)
}

// MailNotification is what a notification implements to travel by e-mail.
type MailNotification interface {
	// ToMail is the message, built for this recipient. The recipient is an
	// argument because their name goes in the greeting and their id goes in
	// the link.
	ToMail(to notifications.Notifiable) messages.Mail
}

// Mail delivers a notification as an e-mail.
type Mail struct {
	mailer Mailer
}

// NewMail returns the mail channel.
func NewMail(m Mailer) *Mail { return &Mail{mailer: m} }

var _ notifications.Channel = (*Mail)(nil)

// Name is "mail".
func (*Mail) Name() notifications.ChannelName { return notifications.ChannelMail }

// Send builds the message and hands it to the Mailer.
//
// A recipient with no address is notifications.ErrNotAddressed, which the
// Notifier treats as "not reachable this way" and not as a failure: somebody
// who never confirmed an e-mail address still gets the row in their bell menu.
func (c *Mail) Send(ctx context.Context, _ auth.Grant, to notifications.Notifiable, n notifications.Notification) (string, error) {
	address := to.RouteFor(notifications.ChannelMail)
	if address == "" {
		return "", notifications.ErrNotAddressed
	}
	buildable, ok := n.(MailNotification)
	if !ok {
		return "", fmt.Errorf("notifications: %s named the mail channel and %T has no ToMail", n.Key(), n)
	}

	message := buildable.ToMail(to)
	if message.Locale == "" {
		if localized, ok := to.(notifications.Localized); ok {
			message.Locale = localized.PreferredLocale()
		}
	}
	if err := message.Validate(); err != nil {
		return "", err
	}
	if c.mailer == nil {
		return "", fmt.Errorf("notifications: the mail channel has no mailer, and %s was addressed to %s", n.Key(), address)
	}
	return c.mailer.Send(ctx, address, message)
}
