package notifications_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/channels"
	"github.com/arandu-io/hesape/notifications/messages"
)

// member is a model that embeds the two traits, the way a Laravel model uses
// them.
type member struct {
	notifications.RoutesNotifications
	notifications.HasDatabaseNotifications
	id string
}

func (m member) NotifiableID() string   { return m.id }
func (m member) NotifiableType() string { return "user" }

// localeNote is a notification that has been told which language to use.
type localeNote struct {
	notifications.NotificationBase
}

func (localeNote) Key() notifications.Key { return "billing.invoice-paid" }

func (localeNote) Via(notifications.Notifiable) []notifications.ChannelName {
	return []notifications.ChannelName{notifications.ChannelMail}
}

func (localeNote) ToMail(notifications.Notifiable) messages.Mail {
	return messages.NewMail().Subject("Paid").Line("Thank you.")
}

// spyMailer keeps the last message it was given.
type spyMailer struct {
	to   string
	last messages.Mail
	n    int
}

func (s *spyMailer) Send(_ context.Context, to string, m messages.Mail) (string, error) {
	s.to, s.last, s.n = to, m, s.n+1
	return "id-1", nil
}

// readGrant is grantFor for the tenant every test in this file uses.
func readGrant(t *testing.T, action auth.Action) auth.Grant {
	t.Helper()
	return grantFor(t, action, "acme")
}

func TestNotifyAndNotifyNowGoThroughTheNotifier(t *testing.T) {
	t.Parallel()

	mailer := &spyMailer{}
	n := notifications.New([]notifications.Channel{channels.NewMail(mailer)})
	ada := member{
		RoutesNotifications: notifications.RoutesNotifications{
			Notifier: n,
			Routes:   map[notifications.ChannelName]string{notifications.ChannelMail: "ada@example.com"},
		},
		id: "u1",
	}

	ctx := context.Background()
	if err := ada.Notify(ctx, sendGrant(t), ada, localeNote{}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if mailer.to != "ada@example.com" || mailer.n != 1 {
		t.Fatalf("the mailer saw %q %d times", mailer.to, mailer.n)
	}
	if ada.RouteNotificationFor(notifications.ChannelMail) != "ada@example.com" {
		t.Error("RouteNotificationFor did not answer with the address")
	}

	// NotifyNow over a channel the notification does not name reaches nobody
	// here, because only the mail channel is wired.
	if err := ada.NotifyNow(ctx, sendGrant(t), ada, localeNote{}, notifications.ChannelBroadcast); err == nil {
		t.Error("a channel nobody wired was accepted")
	}
	if err := ada.NotifyNow(ctx, sendGrant(t), ada, localeNote{}, notifications.ChannelMail); err != nil {
		t.Fatalf("NotifyNow: %v", err)
	}
	if mailer.n != 2 {
		t.Errorf("the mailer was reached %d times, want 2", mailer.n)
	}
}

func TestARecipientWithNoNotifierSaysSo(t *testing.T) {
	t.Parallel()

	ada := member{id: "u1"}
	if err := ada.Notify(context.Background(), sendGrant(t), ada, localeNote{}); err == nil {
		t.Error("Notify with no notifier was accepted")
	}
	if err := ada.NotifyNow(context.Background(), sendGrant(t), ada, localeNote{}); err == nil {
		t.Error("NotifyNow with no notifier was accepted")
	}
	if _, err := ada.Notifications(context.Background(), readGrant(t, notifications.ActionList), ada, 10); err == nil {
		t.Error("Notifications with no store was accepted")
	}
}

// TestTheLocaleOrderIsTheNotificationThenTheManagerThenTheRecipient is
// Illuminate's NotificationSender::preferredLocale.
func TestTheLocaleOrderIsTheNotificationThenTheManagerThenTheRecipient(t *testing.T) {
	t.Parallel()

	mailer := &spyMailer{}
	n := notifications.New([]notifications.Channel{channels.NewMail(mailer)})
	to := speaker{id: "u1", address: "ada@example.com", locale: "pt-BR"}
	ctx := context.Background()

	if err := n.Send(ctx, sendGrant(t), to, localeNote{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if mailer.last.Locale != "pt-BR" {
		t.Errorf("locale = %q, want the recipient's own", mailer.last.Locale)
	}

	n.Locale("de")
	if err := n.Send(ctx, sendGrant(t), to, localeNote{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if mailer.last.Locale != "de" {
		t.Errorf("locale = %q, want the notifier's", mailer.last.Locale)
	}

	note := localeNote{}
	note.NotificationBase = note.NotificationBase.Locale("fr")
	if err := n.Send(ctx, sendGrant(t), to, note); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if mailer.last.Locale != "fr" {
		t.Errorf("locale = %q, want the notification's", mailer.last.Locale)
	}
}

// speaker is a recipient with a language of its own.
type speaker struct {
	id, address, locale string
}

func (s speaker) NotifiableID() string   { return s.id }
func (s speaker) NotifiableType() string { return "user" }

func (s speaker) RouteFor(c notifications.ChannelName) string {
	if c == notifications.ChannelMail {
		return s.address
	}
	return ""
}
func (s speaker) PreferredLocale() string { return s.locale }

func TestTheDefaultChannelIsMailAndCanBeChanged(t *testing.T) {
	t.Parallel()

	n := notifications.New([]notifications.Channel{
		channels.NewMail(&spyMailer{}),
		channels.NewDatabase(notifications.NewMemoryStore()),
	})

	if n.GetDefaultDriver() != notifications.ChannelMail || n.DeliversVia() != notifications.ChannelMail {
		t.Fatalf("the default channel is %q, want mail", n.GetDefaultDriver())
	}
	c, err := n.Channel("")
	if err != nil || c.Name() != notifications.ChannelMail {
		t.Fatalf("Channel(\"\") = %v (%v), want the mail channel", c, err)
	}

	n.DeliverVia(notifications.ChannelDatabase)
	if n.DeliversVia() != notifications.ChannelDatabase {
		t.Errorf("DeliverVia did not stick: %q", n.DeliversVia())
	}
	if c, err = n.Channel(""); err != nil || c.Name() != notifications.ChannelDatabase {
		t.Fatalf("Channel(\"\") = %v (%v), want the database channel", c, err)
	}
	if _, err := n.Channel("sms"); !errors.Is(err, notifications.ErrNoChannel) {
		t.Errorf("err = %v, want ErrNoChannel", err)
	}
}

func TestTheBellMenuReadsThroughTheStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := notifications.NewMemoryStore()
	ada := member{HasDatabaseNotifications: notifications.HasDatabaseNotifications{Store: store}, id: "u1"}

	for range 3 {
		if _, err := store.Save(ctx, sendGrant(t), notifications.Record{
			NotifiableType: "user", NotifiableID: "u1", Key: "billing.invoice-paid",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	list := readGrant(t, notifications.ActionList)
	all, err := ada.Notifications(ctx, list, ada, 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("Notifications = %d (%v), want 3", len(all), err)
	}
	unread, err := ada.UnreadNotifications(ctx, list, ada, 10)
	if err != nil || len(unread) != 3 {
		t.Fatalf("UnreadNotifications = %d (%v), want 3", len(unread), err)
	}

	read := readGrant(t, notifications.ActionRead)
	if err := all[0].MarkAsRead(ctx, read, store); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	got, err := ada.ReadNotifications(ctx, list, ada, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("ReadNotifications = %d (%v), want 1", len(got), err)
	}

	if err := got.MarkAsUnread(ctx, read, store); err != nil {
		t.Fatalf("MarkAsUnread: %v", err)
	}
	unread, err = ada.UnreadNotifications(ctx, list, ada, 10)
	if err != nil || len(unread) != 3 {
		t.Fatalf("UnreadNotifications after the undo = %d (%v), want 3", len(unread), err)
	}
}

func TestRecordsMarkAllAsReadAndFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := notifications.NewMemoryStore()
	var saved notifications.Records
	for range 2 {
		r, err := store.Save(ctx, sendGrant(t), notifications.Record{
			NotifiableType: "user", NotifiableID: "u1", Key: "billing.invoice-paid",
		})
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
		saved = append(saved, r)
	}

	read := readGrant(t, notifications.ActionRead)
	if err := saved.MarkAsRead(ctx, read, store); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}
	after, err := store.For(ctx, readGrant(t, notifications.ActionList), saved[0].Notifiable(), 10)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(notifications.Records(after).Read()) != 2 || len(notifications.Records(after).Unread()) != 0 {
		t.Errorf("after marking both read: %d read, %d unread",
			len(notifications.Records(after).Read()), len(notifications.Records(after).Unread()))
	}
}

func TestRecordNotifiableNamesTheRecipient(t *testing.T) {
	t.Parallel()

	to := notifications.Record{NotifiableType: "user", NotifiableID: "u1"}.Notifiable()
	if to.NotifiableType() != "user" || to.NotifiableID() != "u1" {
		t.Errorf("notifiable = %s %s", to.NotifiableType(), to.NotifiableID())
	}
	if to.RouteFor(notifications.ChannelMail) != "" {
		t.Error("a stored row knows an address it was never given")
	}
}

func TestTheScopesAreTheConditionsTheStoreUses(t *testing.T) {
	t.Parallel()

	if !strings.Contains(notifications.ScopeRead(), "read_at") || !strings.Contains(notifications.ScopeUnread(), "read_at") {
		t.Errorf("scopes = %q and %q", notifications.ScopeRead(), notifications.ScopeUnread())
	}
	if notifications.ScopeRead() == notifications.ScopeUnread() {
		t.Error("the two scopes are the same condition")
	}
}

func TestAnonymousHasNoKey(t *testing.T) {
	t.Parallel()

	to := notifications.Route(notifications.ChannelMail, "ada@example.com")
	if to.GetKey() != "" {
		t.Errorf("GetKey = %q, want empty", to.GetKey())
	}
	if to.RouteNotificationFor(notifications.ChannelMail) != "ada@example.com" {
		t.Error("RouteNotificationFor did not answer with the address")
	}
}

func TestSendQueuedNotificationsSendsToEveryRecipient(t *testing.T) {
	t.Parallel()

	mailer := &spyMailer{}
	n := notifications.New([]notifications.Channel{channels.NewMail(mailer)})
	job := notifications.SendQueuedNotifications{
		Notifiables: []notifications.Notifiable{
			speaker{id: "u1", address: "ada@example.com"},
			speaker{id: "u2", address: "grace@example.com"},
		},
		Notification: localeNote{},
		BackoffFor:   30 * time.Second,
		RetryUntilAt: time.Now().Add(time.Hour),
	}

	if err := job.Handle(context.Background(), sendGrant(t), n); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mailer.n != 2 {
		t.Errorf("the mailer was reached %d times, want 2", mailer.n)
	}
	if job.DisplayName() != "billing.invoice-paid" {
		t.Errorf("DisplayName = %q", job.DisplayName())
	}
	if job.Backoff() != 30*time.Second || job.RetryUntil().IsZero() {
		t.Errorf("backoff = %v, retryUntil = %v", job.Backoff(), job.RetryUntil())
	}

	var failed error
	job.OnFailure = func(cause error) { failed = cause }
	boom := errors.New("the provider is down")
	job.Failed(boom)
	if !errors.Is(failed, boom) {
		t.Errorf("Failed reported %v", failed)
	}

	if err := (notifications.SendQueuedNotifications{}).Handle(context.Background(), sendGrant(t), nil); err == nil {
		t.Error("a queued notification with no notifier was accepted")
	}
	if (notifications.SendQueuedNotifications{}).DisplayName() != "notification" {
		t.Error("a job with no notification has no display name to fall back on")
	}
}

func TestBroadcastOnIsEmptyByDefault(t *testing.T) {
	t.Parallel()

	if got := (notifications.NotificationBase{}).BroadcastOn(); len(got) != 0 {
		t.Errorf("BroadcastOn = %v, want none", got)
	}
	base := notifications.NotificationBase{ID: "n-1"}
	if base.NotificationID() != "n-1" || base.PreferredLocale() != "" {
		t.Errorf("base = %+v", base)
	}
}
