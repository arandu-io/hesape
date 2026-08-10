package channels_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/channels"
	"github.com/arandu-io/hesape/notifications/messages"
)

type person struct {
	id     string
	email  string
	live   string
	locale string
}

func (p person) NotifiableID() string   { return p.id }
func (p person) NotifiableType() string { return "user" }

func (p person) RouteFor(c notifications.ChannelName) string {
	switch c {
	case notifications.ChannelMail:
		return p.email
	case notifications.ChannelBroadcast:
		return p.live
	default:
		return ""
	}
}

func (p person) PreferredLocale() string { return p.locale }

// full travels every way there is.
type full struct{}

func (full) Key() notifications.Key { return "billing.invoice-paid" }

func (full) Via(notifications.Notifiable) []notifications.ChannelName {
	return []notifications.ChannelName{notifications.ChannelMail, notifications.ChannelDatabase, notifications.ChannelBroadcast}
}

func (full) ToMail(notifications.Notifiable) messages.Mail {
	return messages.Mail{Subject: "Your invoice is paid"}.Line("Thank you.")
}

func (full) ToDatabase(notifications.Notifiable) messages.Database {
	return messages.Database{Data: map[string]string{"invoice": "2026-114"}}
}

func (full) ToBroadcast(notifications.Notifiable) messages.Broadcast {
	return messages.Broadcast{Data: map[string]string{"invoice": "2026-114"}}
}

// bare names channels it has no body for.
type bare struct{}

func (bare) Key() notifications.Key                                   { return "billing.invoice-paid" }
func (bare) Via(notifications.Notifiable) []notifications.ChannelName { return nil }

// noSubject builds a message that cannot be sent.
type noSubject struct{ full }

func (noSubject) ToMail(notifications.Notifiable) messages.Mail {
	return messages.Mail{}.Line("a message nobody will open")
}

type fakeMailer struct {
	to   string
	sent messages.Mail
	hits int
}

func (m *fakeMailer) Send(_ context.Context, to string, msg messages.Mail) (string, error) {
	m.hits++
	m.to = to
	m.sent = msg
	return "provider-7", nil
}

type fakeHub struct {
	channel string
	event   string
	payload json.RawMessage
}

func (h *fakeHub) Push(_ context.Context, channel, event string, payload json.RawMessage) error {
	h.channel, h.event, h.payload = channel, event, payload
	return nil
}

func sendGrant(t *testing.T) auth.Grant {
	t.Helper()
	g := auth.SystemGrant(notifications.ActionSend, "acme")
	if err := g.Check(notifications.ActionSend); err != nil {
		t.Fatalf("the send grant is not usable: %v", err)
	}
	return g
}

func TestMailChannelSendsToTheRoutedAddress(t *testing.T) {
	mailer := &fakeMailer{}
	receipt, err := channels.NewMail(mailer).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com", locale: "pt-BR"}, full{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt != "provider-7" {
		t.Fatalf("receipt = %q: the provider's message id was dropped", receipt)
	}
	if mailer.to != "ada@example.com" {
		t.Fatalf("addressed to %q", mailer.to)
	}
	if mailer.sent.Locale != "pt-BR" {
		t.Fatalf("locale = %q: the recipient's language was not carried", mailer.sent.Locale)
	}
}

func TestMailChannelSkipsARecipientWithNoAddress(t *testing.T) {
	mailer := &fakeMailer{}
	_, err := channels.NewMail(mailer).Send(context.Background(), sendGrant(t), person{id: "u1"}, full{})
	if !errors.Is(err, notifications.ErrNotAddressed) {
		t.Fatalf("want ErrNotAddressed, got %v", err)
	}
	if mailer.hits != 0 {
		t.Fatal("the mailer was called with no address")
	}
}

func TestMailChannelRefusesANotificationWithNoBody(t *testing.T) {
	mailer := &fakeMailer{}
	_, err := channels.NewMail(mailer).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com"}, bare{})
	if err == nil || !strings.Contains(err.Error(), "ToMail") {
		t.Fatalf("the error should name the missing method, got %v", err)
	}

	_, err = channels.NewMail(mailer).Send(context.Background(), sendGrant(t),
		person{id: "u1", email: "ada@example.com"}, noSubject{})
	if err == nil {
		t.Fatal("a message with no subject reached the mailer")
	}
	if mailer.hits != 0 {
		t.Fatal("the mailer was called with an invalid message")
	}
}

func TestDatabaseChannelStoresTheRow(t *testing.T) {
	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := person{id: "u1"}

	id, err := channels.NewDatabase(store).Send(ctx, sendGrant(t), ada, full{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id == "" {
		t.Fatal("the row id was not returned")
	}

	list := auth.SystemGrant(notifications.ActionList, "acme")
	rows, err := store.Unread(ctx, list, ada, 0)
	if err != nil {
		t.Fatalf("Unread: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	if rows[0].Key != "billing.invoice-paid" || rows[0].NotifiableID != "u1" {
		t.Fatalf("row = %+v", rows[0])
	}
	if string(rows[0].Data) != `{"invoice":"2026-114"}` {
		t.Fatalf("payload = %s", rows[0].Data)
	}
}

func TestDatabaseChannelRefusesAnAnonymousRecipient(t *testing.T) {
	store := notifications.NewMemoryStore()
	to := notifications.Route(notifications.ChannelMail, "ada@example.com")

	_, err := channels.NewDatabase(store).Send(context.Background(), sendGrant(t), to, full{})
	if !errors.Is(err, notifications.ErrAnonymous) {
		t.Fatalf("want ErrAnonymous, got %v", err)
	}
}

func TestBroadcastChannelPushesToTheSubscribedChannel(t *testing.T) {
	hub := &fakeHub{}
	_, err := channels.NewBroadcast(hub).Send(context.Background(), sendGrant(t),
		person{id: "u1", live: "user.1"}, full{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if hub.channel != "user.1" {
		t.Fatalf("pushed to %q", hub.channel)
	}
	if hub.event != "billing.invoice-paid" {
		t.Fatalf("event = %q: it should fall back to the notification key", hub.event)
	}
	if string(hub.payload) != `{"invoice":"2026-114"}` {
		t.Fatalf("payload = %s", hub.payload)
	}
}

func TestBroadcastChannelSkipsARecipientWithNoConnection(t *testing.T) {
	_, err := channels.NewBroadcast(&fakeHub{}).Send(context.Background(), sendGrant(t), person{id: "u1"}, full{})
	if !errors.Is(err, notifications.ErrNotAddressed) {
		t.Fatalf("want ErrNotAddressed, got %v", err)
	}
}
