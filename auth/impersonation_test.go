package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

type allowEverything struct{}

func (allowEverything) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

func operator() auth.Subject {
	return auth.Subject{ID: "op-1", Tenant: "platform", Roles: []string{"admin"}, Verified: true, Remembered: true, PasswordConfirmedAt: time.Now()}
}

func customer() auth.Subject {
	return auth.Subject{ID: "user-9", Tenant: "platform", Actions: []auth.Action{"invoice.view", "invoice.create"}, Verified: true, Remembered: true, PasswordConfirmedAt: time.Now()}
}

func TestImpersonateAnswersTheTargetReadOnlyWithTheActorKept(t *testing.T) {
	viewed, err := auth.Impersonate(operator(), customer())
	if err != nil {
		t.Fatal(err)
	}
	if viewed.ID != "user-9" || !viewed.Can("invoice.create") || viewed.HasRole("admin") {
		t.Fatalf("the viewed subject is not the target as they are: %+v", viewed)
	}
	if !viewed.Impersonated() || viewed.Impersonator.ID != "op-1" {
		t.Fatalf("the actor was not kept: %+v", viewed.Impersonator)
	}
	if viewed.Remembered || !viewed.PasswordConfirmedAt.IsZero() || !viewed.Impersonator.PasswordConfirmedAt.IsZero() {
		t.Fatal("viewing as somebody carried a remembered session or a confirmed password")
	}

	back, err := auth.Leave(viewed)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != "op-1" || !back.HasRole("admin") || back.Impersonated() {
		t.Fatalf("leaving did not answer the operator back: %+v", back)
	}
	if _, err := auth.Leave(back); !errors.Is(err, auth.ErrImpersonation) {
		t.Fatalf("leaving a session that is nobody's impersonation answered %v", err)
	}
}

func TestImpersonateRefusesWhatCannotBeViewed(t *testing.T) {
	viewed, _ := auth.Impersonate(operator(), customer())
	cases := map[string]struct{ actor, target auth.Subject }{
		"anonymous actor":     {auth.Subject{}, customer()},
		"guest actor":         {auth.Guest("platform"), customer()},
		"nobody to view":      {operator(), auth.Subject{Tenant: "platform"}},
		"guest target":        {operator(), auth.Guest("platform")},
		"oneself":             {operator(), operator()},
		"already viewing":     {viewed, operator()},
		"target being viewed": {operator(), viewed},
	}
	for name, c := range cases {
		if _, err := auth.Impersonate(c.actor, c.target); !errors.Is(err, auth.ErrImpersonation) {
			t.Errorf("%s: Impersonate answered %v, want ErrImpersonation", name, err)
		}
	}
}

func TestAGrantIssuedToAViewedSubjectOnlyReads(t *testing.T) {
	ctx := context.Background()
	viewed, _ := auth.Impersonate(operator(), customer())
	g, err := auth.Authorize(ctx, allowEverything{}, viewed, "invoice.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !g.ReadOnly() || auth.Tenant(g) != "platform" {
		t.Fatalf("the grant is not read-only on the target's tenant: read-only %v, tenant %q", g.ReadOnly(), auth.Tenant(g))
	}
	err = auth.Writable(g)
	if !errors.Is(err, auth.ErrReadOnly) || !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Writable answered %v, want ErrReadOnly wrapping ErrForbidden", err)
	}

	own, _ := auth.Authorize(ctx, allowEverything{}, customer(), "invoice.create", struct{}{})
	if own.ReadOnly() || auth.Writable(own) != nil {
		t.Fatal("the customer's own grant was made read-only")
	}
	if auth.Writable(auth.SystemGrant("invoice.sweep", "platform")) != nil {
		t.Fatal("a system grant was made read-only")
	}
}

// The session store keeps the subject as JSON; the actor has to survive it,
// or the first request after viewing would be writable and Leave would fail.
func TestTheViewedSubjectSurvivesTheSessionEncoding(t *testing.T) {
	viewed, _ := auth.Impersonate(operator(), customer())
	raw, err := json.Marshal(viewed)
	if err != nil {
		t.Fatal(err)
	}
	var read auth.Subject
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	if !read.Impersonated() || read.Impersonator.ID != "op-1" || !read.Impersonator.HasRole("admin") {
		t.Fatalf("the actor did not survive the encoding: %+v", read.Impersonator)
	}
}
