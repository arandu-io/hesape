package channels_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/channels"
)

type allowSend struct{}

func (allowSend) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// grantViewedBy is a send Grant issued to a customer an operator is viewing,
// only looking or, with acting, acting in the account.
func grantViewedBy(t *testing.T, acting bool) auth.Grant {
	t.Helper()
	operator := auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"support"}}
	customer := auth.Subject{ID: "u1", Tenant: "acme"}
	view := auth.Impersonate
	if acting {
		view = auth.ImpersonateActing
	}
	viewed, err := view(operator, customer)
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowSend{}, viewed, notifications.ActionSend, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// An operator only viewing an account sends nothing in its owner's name: no
// e-mail, no row in the bell menu, no push. It held in the database channel
// alone, through the database's own refusal, and the mail went out.
func TestAViewingGrantSendsThroughNoChannel(t *testing.T) {
	ctx := context.Background()
	mailer, hub, store := &fakeMailer{}, &fakeHub{}, notifications.NewMemoryStore()
	notifier := notifications.New([]notifications.Channel{
		channels.NewMail(mailer), channels.NewDatabase(store), channels.NewBroadcast(hub),
	})
	ada := person{id: "u1", email: "ada@example.com", live: "user.1"}
	g := grantViewedBy(t, false)

	if err := notifier.Send(ctx, g, ada, full{}); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Notifier.Send with a viewing Grant = %v, want auth.ErrReadOnly", err)
	}
	if err := notifier.SendNow(ctx, g, ada, full{}, notifications.ChannelMail); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Notifier.SendNow over mail with a viewing Grant = %v, want auth.ErrReadOnly", err)
	}
	for name, ch := range map[string]notifications.Channel{
		"mail": channels.NewMail(mailer), "database": channels.NewDatabase(store), "broadcast": channels.NewBroadcast(hub),
	} {
		if _, err := ch.Send(ctx, g, ada, full{}); !errors.Is(err, auth.ErrReadOnly) {
			t.Errorf("the %s channel with a viewing Grant = %v, want auth.ErrReadOnly", name, err)
		}
	}

	if mailer.hits != 0 {
		t.Errorf("the mailer was asked %d times", mailer.hits)
	}
	if hub.channel != "" {
		t.Errorf("a push went to %q", hub.channel)
	}
	rows, err := store.For(ctx, auth.SystemGrant(notifications.ActionList, "acme"), ada, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("%d rows were stored", len(rows))
	}
}

// An operator acting in the account sends, as its owner would.
func TestAnActingGrantSends(t *testing.T) {
	mailer := &fakeMailer{}
	notifier := notifications.New([]notifications.Channel{channels.NewMail(mailer)})
	err := notifier.SendNow(context.Background(), grantViewedBy(t, true), person{id: "u1", email: "ada@example.com"}, full{}, notifications.ChannelMail)
	if err != nil || mailer.hits != 1 {
		t.Errorf("an acting Grant sent %d e-mails: %v", mailer.hits, err)
	}
}
