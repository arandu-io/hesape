package auth_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

func TestTheSubjectSurvivesTheContext(t *testing.T) {
	ctx := auth.WithSubject(context.Background(), auth.Subject{ID: "u1", Tenant: "acme", Roles: []string{"admin"}})

	got, ok := auth.SubjectFrom(ctx)
	if !ok {
		t.Fatal("the subject the middleware put in the context is not there")
	}
	if got.ID != "u1" || got.Tenant != "acme" || !got.HasRole("admin") {
		t.Fatalf("the subject came back changed: %+v", got)
	}
}

// TestAContextWithoutASubjectSaysSoRatherThanInventingOne: the zero Subject is
// what Authorize refuses, and it has to be reachable only by asking for
// something that is not there.
func TestAContextWithoutASubjectSaysSoRatherThanInventingOne(t *testing.T) {
	got, ok := auth.SubjectFrom(context.Background())
	if ok {
		t.Fatalf("a context nobody wrote to handed back a subject: %+v", got)
	}
	if got.ID != "" || got.Tenant != "" || got.Roles != nil || got.IsGuest() {
		t.Fatalf("the missing subject is not the zero value: %+v", got)
	}
}

// TestCheckIsAboutSigningInAndNothingElse. It is the question a layout asks --
// sign-in link or account menu -- and the three answers have to stay apart: a
// request with no session, a declared visitor, and somebody who signed in.
func TestCheckIsAboutSigningInAndNothingElse(t *testing.T) {
	if auth.Check(context.Background()) {
		t.Error("a request that never loaded a session reads as signed in")
	}
	if auth.Check(auth.WithSubject(context.Background(), auth.Guest("acme"))) {
		t.Error("a declared visitor reads as signed in, and the account menu would be drawn for them")
	}
	if !auth.Check(auth.WithSubject(context.Background(), auth.Subject{ID: "u1", Tenant: "acme"})) {
		t.Error("somebody who signed in reads as anonymous")
	}
	if auth.Check(auth.WithSubject(context.Background(), auth.Subject{Tenant: "acme"})) {
		t.Error("a subject with no id reads as signed in: that is a session that failed to load, not a person")
	}
}

// TestCheckDecidesNothing keeps the two questions apart. Check answers whether
// somebody signed in; whether they may do a thing is a policy's answer, and an
// administrator who is signed in is still refused by a policy that says no.
func TestCheckDecidesNothing(t *testing.T) {
	ctx := auth.WithSubject(context.Background(), auth.Subject{ID: "u1", Tenant: "acme", Roles: []string{"admin"}})
	s, _ := auth.SubjectFrom(ctx)

	if !auth.Check(ctx) {
		t.Fatal("Check says the administrator is not signed in")
	}
	if auth.Allows(ctx, allowOwner{}, s, actionView, resource{owner: "somebody-else"}) {
		t.Fatal("being signed in was enough to pass a policy that denies")
	}
}
