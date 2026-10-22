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
	module := newModule(t, webhook.Config{Server: webhook.Server{Secrets: map[string]string{"partners": testKey}}}, nil)

	err = module.Outgoing().URL("https://partner.example.test/hooks").UseSecret("partners").Payload(map[string]string{"id": "1"}).Dispatch(context.Background(), g)
	if !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Outgoing.Dispatch with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
	if _, err := module.Send(context.Background(), g, "ep_1", webhook.Message{Event: "order.paid", Body: []byte(`{}`)}); !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Send with a viewing Grant answered %v, want auth.ErrReadOnly", err)
	}
}
