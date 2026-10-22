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

func TestImpersonateActingWritesAndStillNamesWhoActs(t *testing.T) {
	acting, err := auth.ImpersonateActing(operator(), customer())
	if err != nil {
		t.Fatalf("ImpersonateActing refused an operator and a customer: %v", err)
	}
	if !acting.Acting || acting.Impersonator == nil || acting.Impersonator.ID != "op-1" || acting.ID != "user-9" {
		t.Fatalf("the acting subject is %+v", acting)
	}
	if acting.Impersonator.Acting {
		t.Fatal("the kept actor carries the acting mark")
	}
	g, err := auth.Authorize(context.Background(), allowEverything{}, acting, "invoice.create", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Writable(g); err != nil {
		t.Fatalf("a grant of an acting impersonation refused a write: %v", err)
	}
	if g.ReadOnly() {
		t.Fatal("a grant of an acting impersonation reads as read-only")
	}
	if who := g.Impersonator(); who == nil || who.ID != "op-1" {
		t.Fatalf("the grant does not name who really acts: %+v", who)
	}
	back, err := auth.Leave(acting)
	if err != nil || back.ID != "op-1" || back.Acting || back.Impersonator != nil {
		t.Fatalf("leaving answered %+v, %v; want the operator, plain", back, err)
	}

	// The acting mark survives the session store's round trip, as the
	// impersonator does.
	raw, err := json.Marshal(acting)
	if err != nil {
		t.Fatal(err)
	}
	var stored auth.Subject
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Acting || stored.Impersonator == nil {
		t.Fatalf("the stored subject lost the acting mark or the actor: %+v", stored)
	}
}

func TestImpersonateActingRefusesWhatImpersonateRefuses(t *testing.T) {
	viewed, _ := auth.Impersonate(operator(), customer())
	for name, pair := range map[string][2]auth.Subject{
		"a guest acting":             {auth.Guest("platform"), customer()},
		"nobody to act as":           {operator(), {}},
		"an actor already viewing":   {viewed, operator()},
		"a target already viewed":    {operator(), viewed},
		"an actor acting as himself": {operator(), operator()},
	} {
		if _, err := auth.ImpersonateActing(pair[0], pair[1]); !errors.Is(err, auth.ErrImpersonation) {
			t.Errorf("%s answered %v, want ErrImpersonation", name, err)
		}
	}
}

func TestAPlainImpersonationStillOnlyReads(t *testing.T) {
	viewed, _ := auth.Impersonate(operator(), customer())
	g, _ := auth.Authorize(context.Background(), allowEverything{}, viewed, "invoice.view", struct{}{})
	if !errors.Is(auth.Writable(g), auth.ErrReadOnly) || !g.ReadOnly() {
		t.Fatal("a viewing impersonation became writable")
	}
	// Acting set by hand on a subject nobody is viewing as gives it nothing
	// it did not have.
	plain := customer()
	plain.Acting = true
	g, _ = auth.Authorize(context.Background(), allowEverything{}, plain, "invoice.view", struct{}{})
	if g.Impersonator() != nil || g.ReadOnly() || auth.Writable(g) != nil {
		t.Fatal("the acting mark changed an ordinary subject")
	}
}

// A job pushed while an operator acts in an account runs in a worker under a
// SystemGrant. Without the operator carried into it, everything the job writes
// is recorded as the system's and the person who caused it disappears.
func TestSystemGrantActingNamesTheOperatorAndWrites(t *testing.T) {
	g := auth.SystemGrantActing("invoice.send", "platform", "op-1")
	if err := g.Check("invoice.send"); err != nil {
		t.Fatalf("the rebuilt grant fails its own action: %v", err)
	}
	if g.Impersonator() == nil || g.Impersonator().ID != "op-1" {
		t.Fatalf("the rebuilt grant does not name the operator: %+v", g.Impersonator())
	}
	if g.ReadOnly() || auth.Writable(g) != nil {
		t.Fatal("the rebuilt grant of an acting impersonation refuses to write")
	}
	if !g.Subject().HasRole("system") || auth.Tenant(g) != "platform" {
		t.Fatalf("the rebuilt grant is not the system subject of the tenant: %+v", g.Subject())
	}

	plain := auth.SystemGrantActing("invoice.send", "platform", "")
	if plain.Impersonator() != nil || plain.Check("invoice.send") != nil {
		t.Fatal("an empty impersonator did not answer what SystemGrant answers")
	}
	if err := auth.SystemGrantActing("invoice.send", "", "op-1").Check("invoice.send"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a missing tenant was accepted because an impersonator was named: %v", err)
	}
}
