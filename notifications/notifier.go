package notifications

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
	notifyevents "github.com/arandu-io/hesape/notifications/events"
)

// Channel delivers a notification one way.
//
// Writing one is small on purpose: everything above it -- authorization, the
// choice of channels, suppression, the events -- has already happened, so a
// channel is "turn this notification into the shape my transport wants, and
// hand it over".
type Channel interface {
	// Name is what a Notification's Via has to return to reach this channel.
	Name() ChannelName
	// Send delivers, and answers with a receipt: whatever identifies the
	// delivery on the other side -- a provider message id, the id of the row
	// that was written. The empty string is fine for a channel that has
	// nothing to identify a delivery by.
	//
	// A channel that cannot reach this recipient returns ErrNotAddressed, and
	// the Notifier moves on to the next channel rather than failing the send:
	// a user with no e-mail address still gets the row in their bell menu.
	Send(ctx context.Context, g auth.Grant, to Notifiable, n Notification) (string, error)
}

// EventRecorder is where the Notifier reports what it did.
//
// It names the one method it needs rather than taking *events.Recorder, so a
// test can watch the three events without an outbox and a database behind it.
type EventRecorder interface {
	Record(e events.Event)
}

// Notifier sends a Notification to a Notifiable over the channels it was given.
//
// It is Illuminate's ChannelManager and NotificationSender in one type, minus
// the manager half: there is no driver to resolve from configuration, because
// the channels an application has are the slice passed to New. A channel that
// is not in the slice is a channel a notification cannot name by accident.
type Notifier struct {
	byName map[ChannelName]Channel
	events EventRecorder

	mu             sync.RWMutex
	suppressed     map[Key]bool
	defaultChannel ChannelName
	locale         string
}

// Option configures a Notifier at construction.
type Option func(*Notifier)

// WithEvents records notification.sending, notification.sent and
// notification.failed into r.
//
// Without it the Notifier records nothing, which is the right default for a
// command-line tool and the wrong one for an application: "the customer says
// they never got it" is answered by these three rows and by nothing else.
func WithEvents(r EventRecorder) Option {
	return func(n *Notifier) { n.events = r }
}

// New returns a Notifier that can reach the given channels.
//
// Two channels answering to the same name is a configuration mistake that would
// otherwise show up as "half the notifications went to the wrong place": the
// last one wins here, and Channels reports what is actually wired.
func New(channels []Channel, opts ...Option) *Notifier {
	n := &Notifier{
		byName:         make(map[ChannelName]Channel, len(channels)),
		defaultChannel: ChannelMail,
	}
	for _, c := range channels {
		if c == nil {
			continue
		}
		n.byName[c.Name()] = c
	}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Channel returns one wired channel by name.
//
// An empty name returns the default one, which is Illuminate's `channel(null)`
// resolving to the default driver. A name nothing answers to is ErrNoChannel
// rather than a nil Channel, because a nil Channel is a panic two frames later.
func (n *Notifier) Channel(name ChannelName) (Channel, error) {
	if name == "" {
		name = n.GetDefaultDriver()
	}
	c, ok := n.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q, and the notifier was built with %v", ErrNoChannel, name, n.Channels())
	}
	return c, nil
}

// GetDefaultDriver is the channel used when nothing names one. It is "mail",
// which is Illuminate's default too.
func (n *Notifier) GetDefaultDriver() ChannelName {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.defaultChannel
}

// DeliversVia is GetDefaultDriver.
//
// Illuminate declares both on ChannelManager and so does this: `deliversVia` is
// the one that reads well next to `deliverVia`, and `getDefaultDriver` is the
// one the manager contract requires.
func (n *Notifier) DeliversVia() ChannelName { return n.GetDefaultDriver() }

// DeliverVia sets the channel used when nothing names one.
func (n *Notifier) DeliverVia(name ChannelName) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.defaultChannel = name
}

// Locale sets the language every notification this Notifier sends is rendered
// in, whatever the recipient's own preference.
//
// It is for the process that has one answer for all of them: a report generated
// for an operator, a batch of invoices for one market. A notification that sets
// its own locale still wins, which is Illuminate's order.
func (n *Notifier) Locale(locale string) *Notifier {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.locale = locale
	return n
}

// Channels is which channel names are wired, for a diagnostic.
func (n *Notifier) Channels() []ChannelName {
	out := make([]ChannelName, 0, len(n.byName))
	for name := range n.byName {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Suppress silences a kind of notification for the life of this Notifier.
//
// It is for the process that must not send: an import that touches ten thousand
// rows, a seeder, a replay of yesterday's queue. Laravel's answer is a fake
// wired in the container; here it is a list of keys on the object that would do
// the sending, so the suppression is visible where the sending is.
//
// There is no Unsuppress. A process that suppresses does so because sending
// would be wrong for the whole of it, and a switch that goes both ways is a
// switch somebody flips in the middle of a loop.
func (n *Notifier) Suppress(keys ...Key) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.suppressed == nil {
		n.suppressed = make(map[Key]bool, len(keys))
	}
	for _, k := range keys {
		n.suppressed[k] = true
	}
}

// Suppressed reports whether a key is silenced.
func (n *Notifier) Suppressed(k Key) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.suppressed[k]
}

// Send delivers one notification to one recipient, over every channel the
// notification names for them.
//
// A channel that fails does not stop the others: the errors are joined and
// returned together, so "the mail provider was down" does not also mean "and
// the row was never written". Laravel throws on the first one, which is how a
// transient SMTP failure loses the copy the user would have seen in the
// morning.
func (n *Notifier) Send(ctx context.Context, g auth.Grant, to Notifiable, note Notification) error {
	return n.SendNow(ctx, g, to, note)
}

// SendNow delivers one notification immediately, over the channels given rather
// than the ones the notification names.
//
// With no channels it is Send. With them it is the escape hatch Illuminate's
// sendNow has: "this one, over these, whatever the notification usually does" --
// the resend button on a support screen, and the retry of one channel that was
// down when the rest went out.
func (n *Notifier) SendNow(ctx context.Context, g auth.Grant, to Notifiable, note Notification, channels ...ChannelName) error {
	if err := g.Check(ActionSend); err != nil {
		return err
	}
	if to == nil {
		return errors.New("notifications: no recipient")
	}
	if note == nil {
		return errors.New("notifications: no notification")
	}
	key := note.Key()
	if !key.Valid() {
		return fmt.Errorf("notifications: %q is not a key: lowercase letters, digits, dot, dash and underscore", string(key))
	}
	if n.Suppressed(key) {
		return nil
	}

	via := channels
	if len(via) == 0 {
		via = note.Via(to)
	}
	to = n.localized(to, note)

	var errs []error
	for _, name := range via {
		ch, ok := n.byName[name]
		if !ok {
			errs = append(errs, fmt.Errorf("%w: %s asked for %q, and the notifier was built with %v", ErrNoChannel, key, name, n.Channels()))
			continue
		}

		payload := notifyevents.Payload{
			Key:            string(key),
			Channel:        string(name),
			NotifiableType: to.NotifiableType(),
			NotifiableID:   to.NotifiableID(),
			Tenant:         auth.Tenant(g),
		}
		n.record(notifyevents.NewSending(payload))

		receipt, err := ch.Send(ctx, g, to, note)
		switch {
		case errors.Is(err, ErrNotAddressed):
			// Not a failure: this recipient is not reachable this way, which
			// the notification could not know when it named the channel.
			continue
		case err != nil:
			n.record(notifyevents.NewFailed(payload, err))
			errs = append(errs, fmt.Errorf("notifications: %s over %s: %w", key, name, err))
		default:
			n.record(notifyevents.NewSent(payload, receipt))
		}
	}
	return errors.Join(errs...)
}

// SendMany is Send for a list of recipients.
//
// It keeps going after a recipient fails, for the reason a bulk send exists at
// all: stopping at the first bad address means the other nine hundred people
// hear nothing, and nobody finds out until they ask.
func (n *Notifier) SendMany(ctx context.Context, g auth.Grant, to []Notifiable, note Notification) error {
	var errs []error
	for _, one := range to {
		if err := n.Send(ctx, g, one, note); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) record(e events.Event) {
	if n.events != nil {
		n.events.Record(e)
	}
}

// localized settles which language the channels render in, and hands the
// channels a recipient that answers with it.
//
// The order is Illuminate's, in NotificationSender::preferredLocale: the
// notification's own locale first, then the one set on the manager, then the
// recipient's preference. A channel asks the recipient, so the first two are
// applied by wrapping it.
func (n *Notifier) localized(to Notifiable, note Notification) Notifiable {
	if l, ok := note.(Localized); ok && l.PreferredLocale() != "" {
		return inLocale{Notifiable: to, locale: l.PreferredLocale()}
	}
	n.mu.RLock()
	locale := n.locale
	n.mu.RUnlock()
	if locale != "" {
		return inLocale{Notifiable: to, locale: locale}
	}
	return to
}

// inLocale is a recipient whose language has been decided for them.
type inLocale struct {
	Notifiable
	locale string
}

func (i inLocale) PreferredLocale() string { return i.locale }
