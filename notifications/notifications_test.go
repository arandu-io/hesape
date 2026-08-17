package notifications_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// user is the recipient the tests notify: a row with an address and a language.
type user struct {
	id      string
	email   string
	live    string
	locale  string
	kindOf  string
	noEmail bool
}

func (u user) NotifiableID() string { return u.id }

func (u user) NotifiableType() string {
	if u.kindOf != "" {
		return u.kindOf
	}
	return "user"
}

func (u user) RouteFor(c notifications.ChannelName) string {
	switch c {
	case notifications.ChannelMail:
		if u.noEmail {
			return ""
		}
		return u.email
	case notifications.ChannelBroadcast:
		return u.live
	default:
		return ""
	}
}

func (u user) PreferredLocale() string { return u.locale }

// invoicePaid travels by mail and by database, and knows how to say so on both.
type invoicePaid struct {
	number string
	via    []notifications.ChannelName
}

func (invoicePaid) Key() notifications.Key { return "billing.invoice-paid" }

func (n invoicePaid) Via(notifications.Notifiable) []notifications.ChannelName {
	if n.via != nil {
		return n.via
	}
	return []notifications.ChannelName{notifications.ChannelMail, notifications.ChannelDatabase}
}

func (n invoicePaid) ToMail(notifications.Notifiable) messages.Mail {
	return messages.NewMail().Subject("Your invoice is paid").
		Line("We received your payment for invoice " + n.number + ".").
		Success()
}

func (n invoicePaid) ToDatabase(notifications.Notifiable) messages.Database {
	return messages.Database{Data: map[string]string{"invoice": n.number}}
}

// stub is a channel a test drives directly.
type stub struct {
	name    notifications.ChannelName
	calls   int
	receipt string
	err     error
}

func (s *stub) Name() notifications.ChannelName { return s.name }

func (s *stub) Send(context.Context, auth.Grant, notifications.Notifiable, notifications.Notification) (string, error) {
	s.calls++
	return s.receipt, s.err
}

func sendGrant(t *testing.T) auth.Grant {
	t.Helper()
	g := auth.SystemGrant(notifications.ActionSend, "acme")
	if err := g.Check(notifications.ActionSend); err != nil {
		t.Fatalf("the send grant is not usable: %v", err)
	}
	return g
}

func grantFor(t *testing.T, a auth.Action, tenant string) auth.Grant {
	t.Helper()
	g := auth.SystemGrant(a, tenant)
	if err := g.Check(a); err != nil {
		t.Fatalf("the grant for %s is not usable: %v", a, err)
	}
	return g
}

func TestKeyValid(t *testing.T) {
	valid := []notifications.Key{"auth.password-reset", "billing.invoice_paid", "a", "x9.y"}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("%q should be a key", string(k))
		}
	}
	invalid := []notifications.Key{"", " auth.reset", "Auth.Reset", "auth reset", "auth/reset", "auth:reset"}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("%q should not be a key", string(k))
		}
	}
}

func TestAnonymousRoutesAndRefusesTheDatabase(t *testing.T) {
	to := notifications.Route(notifications.ChannelMail, "ada@example.com").
		Route(notifications.ChannelBroadcast, "guest.7")

	if got := to.RouteFor(notifications.ChannelMail); got != "ada@example.com" {
		t.Fatalf("mail route = %q", got)
	}
	if got := to.RouteFor(notifications.ChannelDatabase); got != "" {
		t.Fatalf("an anonymous recipient has no database route, got %q", got)
	}
	if got := to.NotifiableType(); got != "anonymous" {
		t.Fatalf("type = %q", got)
	}
	if got := to.NotifiableID(); got != "" {
		t.Fatalf("an anonymous recipient has no id, got %q", got)
	}

	want := []notifications.ChannelName{notifications.ChannelBroadcast, notifications.ChannelMail}
	got := to.Channels()
	if len(got) != len(want) {
		t.Fatalf("channels = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("channels = %v, want %v", got, want)
		}
	}
}

func TestPolicyLetsASubjectReadOnlyTheirOwn(t *testing.T) {
	ada := auth.Subject{ID: "u1", Tenant: "acme"}
	own := notifications.Record{ID: "n1", Tenant: "acme", NotifiableType: "user", NotifiableID: "u1"}
	somebodyElses := notifications.Record{ID: "n2", Tenant: "acme", NotifiableType: "user", NotifiableID: "u2"}
	anotherTenants := notifications.Record{ID: "n3", Tenant: "globex", NotifiableType: "user", NotifiableID: "u1"}

	ctx := context.Background()
	if _, err := auth.Authorize(ctx, notifications.Policy{}, ada, notifications.ActionRead, own); err != nil {
		t.Fatalf("reading her own: %v", err)
	}
	if _, err := auth.Authorize(ctx, notifications.Policy{}, ada, notifications.ActionRead, somebodyElses); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("reading somebody else's should be forbidden, got %v", err)
	}
	if _, err := auth.Authorize(ctx, notifications.Policy{}, ada, notifications.ActionDelete, anotherTenants); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("reaching into another tenant should be forbidden, got %v", err)
	}
	// A collection action carries no row: the tenant on the Grant scopes it.
	if _, err := auth.Authorize(ctx, notifications.Policy{}, ada, notifications.ActionList, notifications.Record{}); err != nil {
		t.Fatalf("listing her own: %v", err)
	}
	// A subject with no tenant cannot reach anything.
	if _, err := auth.Authorize(ctx, notifications.Policy{}, auth.Subject{ID: "u1"}, notifications.ActionList, notifications.Record{}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a tenantless subject should be forbidden, got %v", err)
	}
}

func TestMigrationsCreateTheTable(t *testing.T) {
	list := notifications.Migrations()
	if len(list) != 1 {
		t.Fatalf("migrations = %d, want 1", len(list))
	}
	m := list[0]
	if m.GetName() == "" {
		t.Fatal("the migration has no name, and the name carries the order")
	}

	ctx := context.Background()
	statements, err := migrations.UpStatements(ctx, m)
	if err != nil {
		t.Fatalf("UpStatements: %v", err)
	}
	up := strings.Join(statements, "\n")
	if up == "" {
		t.Fatal("the migration sends nothing")
	}
	// KEY is reserved in MySQL, and the column that would have been called
	// that is why this check exists.
	if strings.Contains(up, " key ") {
		t.Fatalf("the key column has to be notification_key:\n%s", up)
	}
	for _, want := range []string{notifications.Table, "notification_key", "tenant", "read_at"} {
		if !strings.Contains(up, want) {
			t.Fatalf("the migration does not mention %q:\n%s", want, up)
		}
	}

	down, err := migrations.DownStatements(ctx, m)
	if err != nil {
		t.Fatalf("DownStatements: %v", err)
	}
	if len(down) == 0 {
		t.Fatal("the migration cannot be rolled back")
	}
}

func TestAnonymousNotifySendsThroughItsOwnNotifier(t *testing.T) {
	chans, sent := notifications.Capture(notifications.ChannelMail)
	n := notifications.New(chans)

	to := notifications.Route(notifications.ChannelMail, "ada@example.com")
	to.Notifier = n

	if err := to.Notify(context.Background(), sendGrant(t), invoicePaid{
		number: "2026-114",
		via:    []notifications.ChannelName{notifications.ChannelMail},
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	all := sent.All()
	if len(all) != 1 {
		t.Fatalf("%d deliveries, want 1", len(all))
	}
	// The recipient hands itself over, which is what RoutesNotifications cannot
	// do from an embedded struct.
	if all[0].Route != "ada@example.com" {
		t.Fatalf("delivered to %q", all[0].Route)
	}
	if all[0].To.NotifiableType() != "anonymous" {
		t.Fatalf("delivered to a %q", all[0].To.NotifiableType())
	}
}

func TestAnonymousNotifyNowOverridesTheChannels(t *testing.T) {
	chans, sent := notifications.Capture(notifications.ChannelMail, notifications.ChannelBroadcast)
	n := notifications.New(chans)

	to := notifications.Route(notifications.ChannelMail, "ada@example.com").
		Route(notifications.ChannelBroadcast, "guest.7")
	to.Notifier = n

	// The notification names mail and database; the channel argument to
	// NotifyNow is what decides instead.
	err := to.NotifyNow(context.Background(), sendGrant(t), invoicePaid{number: "2026-114"},
		notifications.ChannelBroadcast)
	if err != nil {
		t.Fatalf("NotifyNow: %v", err)
	}

	all := sent.All()
	if len(all) != 1 {
		t.Fatalf("%d deliveries, want 1", len(all))
	}
	if all[0].Channel != notifications.ChannelBroadcast {
		t.Fatalf("delivered over %q, want broadcast", all[0].Channel)
	}
}

func TestAnonymousWithNoNotifierSaysSo(t *testing.T) {
	to := notifications.Route(notifications.ChannelMail, "ada@example.com")

	err := to.Notify(context.Background(), sendGrant(t), invoicePaid{number: "2026-114"})
	if err == nil {
		t.Fatal("Notify with no notifier = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "notifier") {
		t.Fatalf("the refusal is %q, and it should name what is missing", err)
	}
	if err := to.NotifyNow(context.Background(), sendGrant(t), invoicePaid{number: "2026-114"}); err == nil {
		t.Fatal("NotifyNow with no notifier = nil, want a refusal")
	}
}
