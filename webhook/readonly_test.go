package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/webhook"
)

type allowDispatch struct{}

func (allowDispatch) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// TestAViewingGrantDispatchesNothing: the read-only mode of an impersonation
// held only in the database, so an operator viewing a customer's account
// could send calls and messages in the customer's name. The module is built on
// a handle with no database behind it, so the refusal has to come before the
// first statement.
func TestAViewingGrantDispatchesNothing(t *testing.T) {
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowDispatch{}, viewed, webhook.DeliveryDispatch, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertNothingDispatched(t, g, auth.ErrReadOnly)
}

// readsOnly allows everything and says none of its actions write.
type readsOnly struct{ allowDispatch }

func (readsOnly) Writes(auth.Action) bool { return false }

// TestAReadGrantDispatchesNothing: a Grant a policy issued for an action it
// says does not write sends no call and no message, whatever the action.
func TestAReadGrantDispatchesNothing(t *testing.T) {
	g, err := auth.Authorize(context.Background(), readsOnly{}, auth.Subject{ID: "bea", Tenant: "acme"}, webhook.DeliveryDispatch, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertNothingDispatched(t, g, auth.ErrReadGrant)
}

// assertNothingDispatched dispatches and sends with g and wants both refused
// with want.
func assertNothingDispatched(t *testing.T, g auth.Grant, want error) {
	t.Helper()
	module := newModule(t, webhook.Config{Server: webhook.Server{Secrets: map[string]string{"partners": testKey}}}, nil)

	err := module.Outgoing().URL("https://partner.example.test/hooks").UseSecret("partners").Payload(map[string]string{"id": "1"}).Dispatch(context.Background(), g)
	if !errors.Is(err, want) {
		t.Errorf("Outgoing.Dispatch answered %v, want %v", err, want)
	}
	if _, err := module.Send(context.Background(), g, "ep_1", webhook.Message{Event: "order.paid", Body: []byte(`{}`)}); !errors.Is(err, want) {
		t.Errorf("Send answered %v, want %v", err, want)
	}
}
