package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/events"
)

type allowStore struct{}

func (allowStore) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// viewedGrant is a Grant issued to a customer an operator is viewing, acting
// in the account or only looking.
func viewedGrant(t *testing.T, acting bool) auth.Grant {
	t.Helper()
	view := auth.Impersonate
	if acting {
		view = auth.ImpersonateActing
	}
	viewed, err := view(auth.Subject{ID: "op-1", Tenant: tenant}, auth.Subject{ID: "user-9", Tenant: tenant})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowStore{}, viewed, "invoice.refund", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// An event stored while an operator acted in the account names the operator
// beside the account, as a job pushed in the same moment does: the account is
// where it happened, the operator is who did it.
func TestTheOutboxRecordsTheOperatorActingInTheAccount(t *testing.T) {
	db, table := newFakeOutbox()
	outbox := events.NewOutbox(db)
	if err := outbox.Store(context.Background(), viewedGrant(t, true), []events.Event{{Name: "invoice.refunded"}}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := outbox.Store(context.Background(), grant(tenant), []events.Event{{Name: "invoice.paid"}}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	stored := table.stored()
	if len(stored) != 2 {
		t.Fatalf("%d rows stored, want 2", len(stored))
	}
	if stored[0].AuthorizedBy != "user-9" || stored[0].Impersonator != "op-1" {
		t.Errorf("the acting operator's event is authorized by %q with impersonator %q, want user-9 and op-1", stored[0].AuthorizedBy, stored[0].Impersonator)
	}
	if stored[1].Impersonator != "" {
		t.Errorf("an ordinary event names the impersonator %q", stored[1].Impersonator)
	}

	read, err := outbox.PendingAll(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 2 || read[0].Impersonator != "op-1" || read[1].Impersonator != "" {
		t.Errorf("the relay reads %+v, want the operator on the first event and nobody on the second", read)
	}
}

// An operator only viewing the account stores no event: the outbox writes
// with statements of its own, past the database's refusal of that Grant.
func TestTheOutboxRefusesAViewingGrant(t *testing.T) {
	db, table := newFakeOutbox()
	err := events.NewOutbox(db).Store(context.Background(), viewedGrant(t, false), []events.Event{{Name: "invoice.refunded"}})
	if !errors.Is(err, auth.ErrReadOnly) {
		t.Errorf("Store with a viewing Grant = %v, want auth.ErrReadOnly", err)
	}
	if n := len(table.stored()); n != 0 {
		t.Errorf("%d rows were stored", n)
	}
}
