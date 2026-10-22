package jobs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

type allowAll struct{}

func (allowAll) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

func impersonatedGrant(t *testing.T, acting bool) auth.Grant {
	t.Helper()
	actor := auth.Subject{ID: "op-1", Tenant: tenant, Roles: []string{"admin"}}
	target := auth.Subject{ID: "user-9", Tenant: tenant}
	impersonate := auth.Impersonate
	if acting {
		impersonate = auth.ImpersonateActing
	}
	viewed, err := impersonate(actor, target)
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowAll{}, viewed, "invoice.send", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestAReadOnlyGrantQueuesNothing: the worker runs a job under a Grant that
// writes, so a job pushed while an operator only views an account was a way
// for the view to write.
func TestAReadOnlyGrantQueuesNothing(t *testing.T) {
	g := impersonatedGrant(t, false)
	if _, err := jobs.New(g, "", "invoice.send", nil); !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("New answered %v for a read-only Grant, want auth.ErrReadOnly", err)
	}
	if err := jobs.Authorized(g, jobs.Job{Name: "invoice.send"}); !errors.Is(err, auth.ErrReadOnly) {
		t.Fatalf("Authorized answered %v for a read-only Grant, want auth.ErrReadOnly", err)
	}
}

// TestAJobPushedWhileActingNamesTheOperator: the record named the account
// alone, and GrantFor rebuilt a system Grant that had forgotten who caused the
// work.
func TestAJobPushedWhileActingNamesTheOperator(t *testing.T) {
	g := impersonatedGrant(t, true)
	j, err := jobs.New(g, "", "invoice.send", nil)
	if err != nil {
		t.Fatalf("New refused an acting impersonation: %v", err)
	}
	if j.AuthorizedBy != "user-9" || j.Impersonator != "op-1" {
		t.Fatalf("the job records authorized by %q, impersonator %q; want the account and the operator", j.AuthorizedBy, j.Impersonator)
	}
	if err := jobs.Authorized(g, j); err != nil {
		t.Fatalf("the job New built does not match its own Grant: %v", err)
	}
	rebuilt := jobs.GrantFor(&j)
	if rebuilt.Impersonator() == nil || rebuilt.Impersonator().ID != "op-1" {
		t.Fatalf("GrantFor forgot the operator: %+v", rebuilt.Impersonator())
	}
	if err := auth.Writable(rebuilt); err != nil {
		t.Fatalf("the rebuilt Grant refuses to write: %v", err)
	}
	if err := rebuilt.Check("invoice.send"); err != nil {
		t.Fatal(err)
	}

	plain, err := jobs.New(grant(), "", "invoice.send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Impersonator != "" || jobs.GrantFor(&plain).Impersonator() != nil {
		t.Fatal("a job nobody impersonated names an impersonator")
	}
}

// TestAJobCannotClaimAnImpersonatorItsGrantLacks: Push takes a struct anybody
// can fill in, and a record that named the wrong operator is an audit trail
// that lies.
func TestAJobCannotClaimAnImpersonatorItsGrantLacks(t *testing.T) {
	forged := jobs.Job{Name: "invoice.send", Impersonator: "op-2"}
	if err := jobs.Authorized(grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("Authorized answered %v for a job naming an impersonator, want ErrForged", err)
	}
	if err := jobs.Authorized(impersonatedGrant(t, true), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("Authorized answered %v for a job naming another operator, want ErrForged", err)
	}
	if got := jobs.Prepare(impersonatedGrant(t, true), jobs.Job{Name: "invoice.send"}).Impersonator; got != "op-1" {
		t.Fatalf("Prepare recorded impersonator %q, want the operator on the Grant", got)
	}
}
