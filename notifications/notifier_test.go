package notifications_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/notifications"
	notifyevents "github.com/arandu-io/hesape/notifications/events"
)

// recorder collects the events the notifier reports, without an outbox.
type recorder struct{ list []events.Event }

func (r *recorder) Record(e events.Event) { r.list = append(r.list, e) }

func (r *recorder) names() []string {
	out := make([]string, 0, len(r.list))
	for _, e := range r.list {
		out = append(out, e.Name)
	}
	return out
}

func TestSendReachesEveryChannelTheNotificationNames(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail, receipt: "provider-1"}
	db := &stub{name: notifications.ChannelDatabase}
	seen := &recorder{}
	n := notifications.New([]notifications.Channel{mail, db}, notifications.WithEvents(seen))

	if err := n.Send(context.Background(), sendGrant(t), user{id: "u1", email: "ada@example.com"}, invoicePaid{number: "2026-114"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if mail.calls != 1 || db.calls != 1 {
		t.Fatalf("calls: mail %d, database %d", mail.calls, db.calls)
	}

	want := []string{notifyevents.Sending, notifyevents.Sent, notifyevents.Sending, notifyevents.Sent}
	got := seen.names()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	payload, ok := seen.list[1].Payload.(notifyevents.Payload)
	if !ok {
		t.Fatalf("payload = %T", seen.list[1].Payload)
	}
	if payload.Receipt != "provider-1" {
		t.Fatalf("the receipt the channel answered with was dropped: %+v", payload)
	}
	if payload.Tenant != "acme" {
		t.Fatalf("tenant = %q, want acme", payload.Tenant)
	}
}

func TestSendWithoutAGrantIsRefused(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail}
	n := notifications.New([]notifications.Channel{mail})

	err := n.Send(context.Background(), auth.Grant{}, user{id: "u1", email: "ada@example.com"}, invoicePaid{})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("the zero grant should be refused, got %v", err)
	}
	if mail.calls != 0 {
		t.Fatal("the channel was reached without a grant")
	}
}

func TestSendWithAGrantForAnotherActionIsRefused(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail}
	n := notifications.New([]notifications.Channel{mail})

	err := n.Send(context.Background(), grantFor(t, notifications.ActionList, "acme"), user{id: "u1", email: "a@b.co"}, invoicePaid{})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a grant for listing should not send, got %v", err)
	}
}

func TestAChannelThatCannotReachTheRecipientIsNotAFailure(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail, err: notifications.ErrNotAddressed}
	db := &stub{name: notifications.ChannelDatabase}
	seen := &recorder{}
	n := notifications.New([]notifications.Channel{mail, db}, notifications.WithEvents(seen))

	if err := n.Send(context.Background(), sendGrant(t), user{id: "u1", noEmail: true}, invoicePaid{}); err != nil {
		t.Fatalf("a recipient with no address should still get the row: %v", err)
	}
	if db.calls != 1 {
		t.Fatal("the database channel was skipped")
	}
	for _, name := range seen.names() {
		if name == notifyevents.Failed {
			t.Fatal("not being reachable on a channel was recorded as a failure")
		}
	}
}

func TestOneBrokenChannelDoesNotStopTheOthers(t *testing.T) {
	boom := errors.New("the provider is down")
	mail := &stub{name: notifications.ChannelMail, err: boom}
	db := &stub{name: notifications.ChannelDatabase}
	seen := &recorder{}
	n := notifications.New([]notifications.Channel{mail, db}, notifications.WithEvents(seen))

	err := n.Send(context.Background(), sendGrant(t), user{id: "u1", email: "ada@example.com"}, invoicePaid{})
	if !errors.Is(err, boom) {
		t.Fatalf("the failure should be reported, got %v", err)
	}
	if db.calls != 1 {
		t.Fatal("the mail failure swallowed the stored copy")
	}
	if names := seen.names(); names[1] != notifyevents.Failed {
		t.Fatalf("events = %v", names)
	}
}

func TestANotificationCannotNameAChannelThatIsNotWired(t *testing.T) {
	db := &stub{name: notifications.ChannelDatabase}
	n := notifications.New([]notifications.Channel{db})

	err := n.Send(context.Background(), sendGrant(t), user{id: "u1", email: "ada@example.com"},
		invoicePaid{via: []notifications.ChannelName{"sms"}})
	if !errors.Is(err, notifications.ErrNoChannel) {
		t.Fatalf("an unknown channel should be an error, got %v", err)
	}
}

func TestSuppressSilencesOneKind(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail}
	n := notifications.New([]notifications.Channel{mail})
	n.Suppress("billing.invoice-paid")

	if !n.Suppressed("billing.invoice-paid") {
		t.Fatal("Suppressed says no right after Suppress")
	}
	if err := n.Send(context.Background(), sendGrant(t), user{id: "u1", email: "a@b.co"}, invoicePaid{via: []notifications.ChannelName{notifications.ChannelMail}}); err != nil {
		t.Fatalf("a suppressed send is not an error: %v", err)
	}
	if mail.calls != 0 {
		t.Fatal("a suppressed notification was delivered")
	}
}

func TestSendManyKeepsGoingAfterOneRecipientFails(t *testing.T) {
	mail := &failOnce{name: notifications.ChannelMail, failFor: "u2"}
	n := notifications.New([]notifications.Channel{mail})
	note := invoicePaid{via: []notifications.ChannelName{notifications.ChannelMail}}

	err := n.SendMany(context.Background(), sendGrant(t), []notifications.Notifiable{
		user{id: "u1", email: "a@b.co"},
		user{id: "u2", email: "c@d.co"},
		user{id: "u3", email: "e@f.co"},
	}, note)
	if err == nil {
		t.Fatal("the failed recipient should be reported")
	}
	if mail.calls != 3 {
		t.Fatalf("calls = %d, want 3: one bad address must not silence the rest", mail.calls)
	}
}

func TestAnInvalidKeyIsRefusedBeforeAnythingIsSent(t *testing.T) {
	mail := &stub{name: notifications.ChannelMail}
	n := notifications.New([]notifications.Channel{mail})

	if err := n.Send(context.Background(), sendGrant(t), user{id: "u1", email: "a@b.co"}, badKey{}); err == nil {
		t.Fatal("a key with a space in it should be refused")
	}
	if mail.calls != 0 {
		t.Fatal("it was sent anyway")
	}
}

type badKey struct{}

func (badKey) Key() notifications.Key { return "Billing Invoice" }

func (badKey) Via(notifications.Notifiable) []notifications.ChannelName {
	return []notifications.ChannelName{notifications.ChannelMail}
}

type failOnce struct {
	name    notifications.ChannelName
	failFor string
	calls   int
}

func (f *failOnce) Name() notifications.ChannelName { return f.name }

func (f *failOnce) Send(_ context.Context, _ auth.Grant, to notifications.Notifiable, _ notifications.Notification) (string, error) {
	f.calls++
	if to.NotifiableID() == f.failFor {
		return "", errors.New("that address bounced")
	}
	return "", nil
}
