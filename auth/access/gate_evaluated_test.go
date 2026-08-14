package access_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/access"
	"github.com/arandu-io/hesape/auth/access/events"
)

func subject() auth.Subject { return auth.Subject{ID: "7", Tenant: "acme"} }

// Every decision is recorded, including the ones that deny.
//
// An audit trail that only sees the allowances answers the wrong question: what
// somebody was refused is the more interesting half.
func TestEveryDecisionReachesTheObserverIncludingDenials(t *testing.T) {
	var seen []events.GateEvaluated

	g := access.NewGate().
		Define("edit", func(_ context.Context, _ auth.Subject, _ ...any) any { return true }).
		Define("delete", func(_ context.Context, _ auth.Subject, _ ...any) any { return false }).
		Observe(func(e events.GateEvaluated) { seen = append(seen, e) })

	g.Raw(context.Background(), subject(), "edit")
	g.Raw(context.Background(), subject(), "delete")

	if len(seen) != 2 {
		t.Fatalf("observer saw %d decisions, want 2", len(seen))
	}
	if seen[0].Ability != "edit" || seen[0].Result != true {
		t.Errorf("first: %+v", seen[0])
	}
	if seen[1].Ability != "delete" || seen[1].Result != false {
		t.Errorf("second: %+v", seen[1])
	}
}

// "Nobody answered" and "answered no" are different facts, and the raw result
// keeps them apart. An audit that reduced both to false could not tell a
// missing policy from a denial.
func TestAnUndefinedAbilityIsRecordedAsUnansweredAndNotAsDenied(t *testing.T) {
	var seen []events.GateEvaluated
	g := access.NewGate().Observe(func(e events.GateEvaluated) { seen = append(seen, e) })

	g.Raw(context.Background(), subject(), "somethingnobodydefined")

	if len(seen) != 1 {
		t.Fatalf("observer saw %d, want 1", len(seen))
	}
	if seen[0].Result == false {
		t.Error("an ability nobody defined was recorded as a denial; the two are different facts")
	}
}

// Observe answers a copy, so a Gate handed to two places cannot have its audit
// trail redirected by one of them.
func TestObserveAnswersACopy(t *testing.T) {
	base := access.NewGate().Define("edit", func(_ context.Context, _ auth.Subject, _ ...any) any { return true })

	var count int
	watched := base.Observe(func(events.GateEvaluated) { count++ })

	base.Raw(context.Background(), subject(), "edit")
	if count != 0 {
		t.Error("the original Gate fired the observer; Observe must not mutate")
	}

	watched.Raw(context.Background(), subject(), "edit")
	if count != 1 {
		t.Errorf("the observed Gate fired %d times, want 1", count)
	}
}

// A Gate nobody observes does the work it always did and allocates nothing
// extra.
func TestAGateWithNoObserverStillDecides(t *testing.T) {
	g := access.NewGate().Define("edit", func(_ context.Context, _ auth.Subject, _ ...any) any { return true })

	if g.Raw(context.Background(), subject(), "edit") != true {
		t.Error("an unobserved Gate stopped answering")
	}
}
