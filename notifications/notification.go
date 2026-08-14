package notifications

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// Key is the stable name of a kind of notification: "auth.password-reset",
// "billing.invoice-paid".
//
// Laravel stores the class name in the type column, which means renaming a
// class rewrites history: every row already written says something that no
// longer exists. A Key is chosen once and written down, so the Go type behind
// it can be renamed, moved or split without touching a single stored row.
//
// It is also what Suppress silences and what a test asserts on, both of which
// want a name rather than a type.
type Key string

// ChannelName is which way a notification travels.
type ChannelName string

// The channels that ship with the collection. A project may declare its own --
// a ChannelName is a string, and Notifier looks the name up in the slice it was
// given -- but these three are the ones the framework itself implements.
const (
	// ChannelMail is e-mail, delivered by hesape/notifications/channels.Mail.
	ChannelMail ChannelName = "mail"
	// ChannelDatabase is a row in the notifications table, for the bell menu.
	ChannelDatabase ChannelName = "database"
	// ChannelBroadcast is a live push to a connected browser.
	ChannelBroadcast ChannelName = "broadcast"
)

// ErrNoChannel is returned when a notification asks for a channel the Notifier
// was not given. It is an error rather than a skip: a notification that names
// "sms" in a system with no SMS channel is a notification nobody receives, and
// silence is how that goes unnoticed for a quarter.
var ErrNoChannel = errors.New("notifications: no such channel")

// ErrNotAddressed is returned by a channel when the notifiable has no route on
// it -- no e-mail address for the mail channel, no connection for broadcast.
//
// The Notifier treats it as "this one was not reachable here" and carries on
// with the other channels, so a user with no e-mail address still gets the row
// in their bell menu.
var ErrNotAddressed = errors.New("notifications: the notifiable has no route on this channel")

// ErrAnonymous is returned by the database channel when it is handed an
// Anonymous notifiable.
//
// An on-demand notification has nobody to belong to: the row would name a
// notifiable that does not exist, and nothing could ever read it back.
var ErrAnonymous = errors.New("notifications: the database channel needs a notifiable that exists")

// Notification is one thing an application has to tell somebody.
//
// The two methods are all the Notifier needs. What a notification looks like on
// a given channel is an optional interface satisfied per channel -- ToMail on
// the mail channel, ToDatabase on the database channel -- so a notification
// that never goes by e-mail does not carry an empty ToMail, and adding a
// channel to a project does not touch the notifications that do not use it.
type Notification interface {
	// Key names the kind. It is written to the type column and is what
	// Suppress matches on.
	Key() Key
	// Via is which channels this notification takes for this recipient.
	//
	// The recipient is an argument because the answer depends on them: a user
	// who has turned e-mail off gets the database row and nothing else, and
	// that decision belongs next to the notification rather than in a filter
	// somewhere downstream.
	Via(to Notifiable) []ChannelName
}

// Notifiable is somebody a notification can reach.
//
// In Laravel this is a trait on a model and the routing is a method named after
// the channel, found by string. Here it is three methods, so a type that cannot
// be notified does not compile at the call rather than returning null at
// midnight.
type Notifiable interface {
	// NotifiableID is the primary key of the row being notified.
	NotifiableID() string
	// NotifiableType names what kind of row it is: "user", "team".
	//
	// It is stored next to the id because the table holds notifications for
	// every kind of notifiable, and an id alone does not say which table it
	// came from. It is Laravel's morph type, spelled out.
	NotifiableType() string
	// RouteFor is the address on a channel: an e-mail address for ChannelMail,
	// a channel name for ChannelBroadcast. The empty string means "not
	// reachable there", and the channel is skipped rather than failing.
	RouteFor(c ChannelName) string
}

// Localized is the optional half of Notifiable: a recipient who has a language.
//
// A channel that renders words -- mail today -- reads it and carries the locale
// on the message, so the body is drawn in the language the person chose rather
// than in the language of whoever triggered the send. It is Laravel's
// HasLocalePreference.
type Localized interface {
	// PreferredLocale is a BCP 47 tag: "pt-BR", "en". The empty string means
	// the recipient has no preference and the application default stands.
	PreferredLocale() string
}

// Anonymous is a recipient with no row behind it: an address somebody typed
// into a form, a webhook that has to be told once.
//
// It is Laravel's AnonymousNotifiable, and it exists for the notification that
// goes to a person the system does not have an account for -- the invitation
// e-mail being the case everybody hits.
type Anonymous struct {
	// Notifier is who Notify and NotifyNow send with. In PHP the notifiable
	// finds the dispatcher in the container; here it is handed one (ADR 0001),
	// which is also what lets a test hand it a Capture.
	Notifier *Notifier

	routes map[ChannelName]string
}

// Route starts an anonymous recipient, addressed on one channel.
//
// It is AnonymousNotifiable::route, reached as a constructor: PHP writes
// `Notification::route('mail', $addr)` on the facade, and the facade is what
// ADR 0002 removed, so the package function is where the first route goes.
//
//	notifier.Send(ctx, g, notifications.Route(notifications.ChannelMail, addr), Invite{})
//
// Chain it for a second channel. Routing an Anonymous at ChannelDatabase is
// accepted here and refused by the database channel with ErrAnonymous, because
// that is where the reason is legible: the row would name nobody.
func Route(c ChannelName, to string) *Anonymous {
	return (&Anonymous{}).Route(c, to)
}

// Routes starts an anonymous recipient addressed on several channels at once.
//
// It is Notification::routes, which PHP writes on the facade as
// `Notification::routes(['mail' => $address, 'vonage' => $number])`: it makes
// one AnonymousNotifiable and calls route() for every entry. It is [Route] for
// more than one channel, and it is here as a package function for the reason
// [Route] is -- the facade it hangs off is what ADR 0002 removed.
//
//	to := notifications.Routes(map[notifications.ChannelName]string{
//		notifications.ChannelMail:      "ada@example.com",
//		notifications.ChannelBroadcast: "invoices.42",
//	})
//
// A "route" here is an address, not a URL pattern: nothing in this package
// registers an HTTP route, and none of these addresses reaches a repository.
// An empty or nil map makes a recipient with nowhere to be reached, which is
// what PHP's empty array makes too: [Anonymous.Channels] answers empty, and a
// notification whose Via reads it goes nowhere.
func Routes(to map[ChannelName]string) *Anonymous {
	a := &Anonymous{}
	for c, address := range to {
		a.Route(c, address)
	}
	return a
}

// Route adds an address on another channel and returns the same recipient.
//
// It is AnonymousNotifiable::route.
func (a *Anonymous) Route(c ChannelName, to string) *Anonymous {
	if a.routes == nil {
		a.routes = make(map[ChannelName]string)
	}
	a.routes[c] = to
	return a
}

// NotifiableID is empty: there is no row.
//
// It answers the Notifiable interface, which has no PHP counterpart:
// Illuminate finds the morph id on the Eloquent model at store time.
func (a *Anonymous) NotifiableID() string { return "" }

// GetKey is empty, for the same reason.
//
// It is AnonymousNotifiable::getKey.
//
// Illuminate declares it on AnonymousNotifiable with an empty body, so that the
// broadcast channel can derive a channel name from any notifiable without
// asking what kind it is. It answers the same here, and it is the reason the
// broadcast channel refuses an anonymous recipient with no explicit route.
func (a *Anonymous) GetKey() string { return "" }

// NotifiableType is "anonymous".
//
// It answers the Notifiable interface and has no PHP counterpart: Illuminate
// reads the morph type off the Eloquent model, and an AnonymousNotifiable never
// reaches a table.
func (a *Anonymous) NotifiableType() string { return "anonymous" }

// RouteFor answers with whatever Route recorded.
//
// It is RouteNotificationFor under the Notifiable interface's spelling; the two
// are one method in PHP, where AnonymousNotifiable::routeNotificationFor is the
// only name.
func (a *Anonymous) RouteFor(c ChannelName) string { return a.routes[c] }

// RouteNotificationFor is AnonymousNotifiable::routeNotificationFor.
//
// It is Illuminate's spelling, declared on AnonymousNotifiable and on the
// RoutesNotifications trait, and it is here so that a recipient written against
// either name works.
func (a *Anonymous) RouteNotificationFor(c ChannelName) string { return a.RouteFor(c) }

// Notify is AnonymousNotifiable::notify.
//
// PHP reaches the dispatcher through the container; ADR 0001 removed the
// container, so the Notifier is a field on the recipient, exactly as it is on
// RoutesNotifications. The recipient is not an argument the way it is there: an
// Anonymous is itself the notifiable, so it can hand itself over.
func (a *Anonymous) Notify(ctx context.Context, g auth.Grant, n Notification) error {
	if a.Notifier == nil {
		return errors.New("notifications: this recipient has no notifier to send with")
	}
	return a.Notifier.Send(ctx, g, a, n)
}

// NotifyNow is AnonymousNotifiable::notifyNow.
//
// In Illuminate the two differ because notify() queues a notification that
// implements ShouldQueue. Nothing here queues on its own -- sending on the queue
// is SendQueuedNotifications, pushed at the call site -- so what NotifyNow adds
// is the channel override, which is the second argument PHP gives it.
func (a *Anonymous) NotifyNow(ctx context.Context, g auth.Grant, n Notification, channels ...ChannelName) error {
	if a.Notifier == nil {
		return errors.New("notifications: this recipient has no notifier to send with")
	}
	return a.Notifier.SendNow(ctx, g, a, n, channels...)
}

// Channels is every channel this recipient was routed at, sorted, which is what
// a Notification's Via can return when it means "wherever this one can be
// reached".
//
// It has no PHP counterpart: Illuminate reads the $routes property straight off
// the object, and a property read in Go over an unexported map is a method.
func (a *Anonymous) Channels() []ChannelName {
	out := make([]ChannelName, 0, len(a.routes))
	for c := range a.routes {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Valid reports whether a Key is one: lowercase, dotted, no spaces.
//
// It has no PHP counterpart: Illuminate writes the notification's class name to
// the type column, and a class name is checked by the parser.
//
// It is checked before anything is stored because the Key is written to a
// column that is filtered on and read back by name, and a key with a stray
// space in it is a key that matches nothing, forever, with no error anywhere.
func (k Key) Valid() bool {
	s := string(k)
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
