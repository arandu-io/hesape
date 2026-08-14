package notifications

import (
	"context"
	"sync"

	"github.com/arandu-io/hesape/auth"
)

// Delivery is one notification handed to one channel, as a capturing channel
// recorded it.
//
// Nothing in Illuminate\Notifications answers to it: the equivalent is the
// $notifications array inside Illuminate\Support\Testing\Fakes\
// NotificationFake, which is a different component and is reached by swapping a
// facade binding. Every exported name in this file is in the same position.
type Delivery struct {
	// Channel is which one took it.
	Channel ChannelName
	// Key is the kind, which is what an assertion usually names.
	Key Key
	// Notification is the value itself, so a test can look at its fields
	// without the channel having had to guess what a test would want.
	Notification Notification
	// To is the recipient it was addressed to.
	To Notifiable
	// Route is what the recipient answered RouteFor with: the e-mail address,
	// the broadcast channel.
	Route string
	// Tenant is the tenant off the Grant that authorized the send.
	Tenant string
}

// Deliveries is what Capture records into.
type Deliveries struct {
	mu   sync.Mutex
	list []Delivery
}

// All returns a copy of everything recorded, in the order it happened. It has
// no PHP counterpart, for the reason [Delivery] gives.
func (d *Deliveries) All() []Delivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Delivery, len(d.list))
	copy(out, d.list)
	return out
}

// Len is how many deliveries were recorded. It has no PHP counterpart, for the
// reason [Delivery] gives.
func (d *Deliveries) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.list)
}

// For returns the deliveries of one kind of notification. It has no PHP
// counterpart, for the reason [Delivery] gives.
func (d *Deliveries) For(k Key) []Delivery {
	var out []Delivery
	for _, one := range d.All() {
		if one.Key == k {
			out = append(out, one)
		}
	}
	return out
}

// Sent reports whether a kind of notification reached a recipient on any
// channel. It is the assertion a test writes nine times out of ten.
//
// It has no PHP counterpart, for the reason [Delivery] gives: the nearest thing
// is NotificationFake::assertSentTo, in a different component.
func (d *Deliveries) Sent(k Key, to Notifiable) bool {
	if to == nil {
		return false
	}
	for _, one := range d.For(k) {
		if one.To != nil &&
			one.To.NotifiableType() == to.NotifiableType() &&
			one.To.NotifiableID() == to.NotifiableID() {
			return true
		}
	}
	return false
}

// Reset forgets everything, for a test that reuses one Notifier across cases. It
// has no PHP counterpart, for the reason [Delivery] gives.
func (d *Deliveries) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.list = nil
}

func (d *Deliveries) add(one Delivery) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.list = append(d.list, one)
}

// Capture returns channels that record instead of delivering, and the recording
// they write to.
//
//	chans, sent := notifications.Capture(notifications.ChannelMail)
//	n := notifications.New(chans)
//	// ... exercise the code under test ...
//	if !sent.Sent("billing.invoice-paid", user) {
//		t.Fatal("the customer was not told the invoice was paid")
//	}
//
// With no names it captures the three channels the collection implements, which
// is what a test of "was anything sent at all" wants.
//
// It is Laravel's Notification::fake, wired the way everything else here is
// wired: by being passed in, rather than by swapping a binding in a container
// that the code under test resolves from behind your back.
func Capture(names ...ChannelName) ([]Channel, *Deliveries) {
	if len(names) == 0 {
		names = []ChannelName{ChannelMail, ChannelDatabase, ChannelBroadcast}
	}
	recorded := &Deliveries{}
	out := make([]Channel, 0, len(names))
	for _, name := range names {
		out = append(out, &captureChannel{name: name, into: recorded})
	}
	return out, recorded
}

type captureChannel struct {
	name ChannelName
	into *Deliveries
}

func (c *captureChannel) Name() ChannelName { return c.name }

func (c *captureChannel) Send(_ context.Context, g auth.Grant, to Notifiable, n Notification) (string, error) {
	c.into.add(Delivery{
		Channel:      c.name,
		Key:          n.Key(),
		Notification: n,
		To:           to,
		Route:        to.RouteFor(c.name),
		Tenant:       auth.Tenant(g),
	})
	return "", nil
}
