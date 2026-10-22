package notifications_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/notifications"
)

type allowEverything struct{}

func (allowEverything) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// The Notifier refuses a viewing Grant before it asks any channel, so a
// channel of the application's own that does not check is never reached.
func TestTheNotifierAsksNoChannelWithAViewingGrant(t *testing.T) {
	viewed, err := auth.Impersonate(auth.Subject{ID: "op-1", Tenant: "acme"}, auth.Subject{ID: "u1", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowEverything{}, viewed, notifications.ActionSend, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	mail := &stub{name: notifications.ChannelMail}
	db := &stub{name: notifications.ChannelDatabase}
	n := notifications.New([]notifications.Channel{mail, db})

	if err := n.Send(context.Background(), g, user{id: "u1", email: "ada@example.com"}, invoicePaid{number: "2026-114"}); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Send = %v, want auth.ErrReadOnly", err)
	}
	if mail.calls != 0 || db.calls != 0 {
		t.Errorf("the channels were asked: mail %d, database %d", mail.calls, db.calls)
	}
}
