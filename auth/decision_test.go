package auth_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/log"
)

// TestAuthorizeRecordsTheDecisionInTheRequestCollector: a request that carries
// a Collector -- development, or a test trace -- holds every Policy decision
// it was answered, with who asked, in which tenant, for which action, the
// answer and the reason for a refusal. Without it a failing journey shows a
// 403 and nothing about which policy said no.
func TestAuthorizeRecordsTheDecisionInTheRequestCollector(t *testing.T) {
	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	if _, err := auth.Authorize(ctx, allowOwner{}, auth.Subject{ID: "u1", Tenant: "t1"}, actionView, resource{owner: "u1"}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if _, err := auth.Authorize(ctx, allowOwner{}, auth.Subject{ID: "u2", Tenant: "t1"}, actionDelete, resource{owner: "u1"}); err == nil {
		t.Fatal("Authorize allowed a subject who is not the owner")
	}
	if _, err := auth.Authorize(ctx, allowOwner{}, auth.Subject{Tenant: "t1"}, actionView, resource{}); err == nil {
		t.Fatal("Authorize allowed an empty subject")
	}
	if _, err := auth.Authorize(ctx, allowOwner{}, auth.Guest("t1"), actionView, resource{}); err != nil {
		t.Fatalf("Authorize of a guest on an open resource: %v", err)
	}

	got := col.Decisions()
	if len(got) != 4 {
		t.Fatalf("recorded %d decisions, want 4: %+v", len(got), got)
	}
	want := []struct {
		subject, tenant, action string
		allowed                 bool
		reason                  string
	}{
		{"u1", "t1", string(actionView), true, ""},
		{"u2", "t1", string(actionDelete), false, "not the owner"},
		{"", "t1", string(actionView), false, "anonymous subject"},
		{"", "t1", string(actionView), true, ""},
	}
	for i, w := range want {
		d := got[i]
		if d.Subject != w.subject || d.Tenant != w.tenant || d.Action != w.action || d.Allowed != w.allowed {
			t.Errorf("decision %d = %+v, want subject %q tenant %q action %q allowed %v", i, d, w.subject, w.tenant, w.action, w.allowed)
		}
		if w.reason == "" && d.Reason != "" {
			t.Errorf("decision %d carries the reason %q on an allowed request", i, d.Reason)
		}
		if w.reason != "" && !strings.Contains(d.Reason, w.reason) {
			t.Errorf("decision %d has the reason %q, want it to say %q", i, d.Reason, w.reason)
		}
		// The caller is the code that asked for the Grant, not the
		// package that ran the policy: that is the line a reader opens.
		if filepath.Base(d.Caller.File) != "decision_test.go" {
			t.Errorf("decision %d was recorded as called from %s:%d, want this file", i, d.Caller.File, d.Caller.Line)
		}
	}
}

// TestAllowsRecordsNoDecision: Allows asks for a view, which draws one button
// per row, and issues no Grant. Recording it would bury the decision that
// issued a Grant under one line per button.
func TestAllowsRecordsNoDecision(t *testing.T) {
	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	if !auth.Allows(ctx, allowOwner{}, auth.Subject{ID: "u1", Tenant: "t1"}, actionView, resource{owner: "u1"}) {
		t.Fatal("Allows refused the owner")
	}
	if auth.Allows(ctx, allowOwner{}, auth.Subject{ID: "u2", Tenant: "t1"}, actionView, resource{owner: "u1"}) {
		t.Fatal("Allows answered yes to a subject who is not the owner")
	}
	if got := col.Decisions(); len(got) != 0 {
		t.Fatalf("Allows recorded %+v, want nothing", got)
	}
}

// TestAuthorizeWithoutACollectorAllocatesNothing: production runs without a
// Collector, and Authorize runs on every request, often more than once. Asking
// whether there is one to record into must cost nothing on the path that
// issues the Grant.
func TestAuthorizeWithoutACollectorAllocatesNothing(t *testing.T) {
	ctx := context.Background()
	s := auth.Subject{ID: "u1", Tenant: "t1"}
	r := resource{owner: "u1"}
	allocs := testing.AllocsPerRun(1000, func() {
		if _, err := auth.Authorize(ctx, allowOwner{}, s, actionView, r); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Authorize allocated %.0f times per call without a Collector, want 0", allocs)
	}
}
