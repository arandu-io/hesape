package auth_test

import (
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// A role is what somebody is; an action is what they may do. They are separate
// lists because they are separate questions, and the reason they had to be
// separated is a defect that reached a consumer.
//
// There was one list. A module that administers permissions resolved group
// membership into a flat list of actions and wrote it into Roles, because Roles
// was the only field there was. From that moment every policy asking
// HasRole("admin") answered false -- and both sides are []string, so nothing in
// the build said a word. The application saw administrators lose everything,
// with no compiler error to point at.

func TestRolesAndActionsAreSeparateQuestions(t *testing.T) {
	t.Parallel()

	subject := auth.Subject{
		ID:      "person-1",
		Tenant:  "acme",
		Roles:   []string{"admin", "member"},
		Actions: []auth.Action{"user.view", "invoice.create"},
	}

	for _, role := range []string{"admin", "member"} {
		if !subject.HasRole(role) {
			t.Errorf("HasRole(%q) = false, and the subject carries it", role)
		}
	}
	for _, action := range []auth.Action{"user.view", "invoice.create"} {
		if !subject.Can(action) {
			t.Errorf("Can(%q) = false, and the subject carries it", action)
		}
	}

	// Neither answers the other's question. This is the assertion the defect
	// was about: filling one list did not make the other's answers true, and
	// filling the wrong list made them all false.
	if subject.HasRole("user.view") {
		t.Error(`HasRole("user.view") = true: an action is being answered as a role`)
	}
	if subject.Can("admin") {
		t.Error(`Can("admin") = true: a role is being answered as an action`)
	}
}

// TestASubjectWithOnlyRolesIsUnaffected is the promise this change makes to
// every application that already existed: adding the second list changes
// nothing for one that does not use it.
func TestASubjectWithOnlyRolesIsUnaffected(t *testing.T) {
	t.Parallel()

	subject := auth.Subject{ID: "person-1", Tenant: "acme", Roles: []string{"admin"}}

	if !subject.HasRole("admin") {
		t.Error("an application that decides by role stopped working")
	}
	if subject.Can("anything.at.all") {
		t.Error("a subject with no actions answered true to one")
	}
}

// TestASubjectWithOnlyActionsAnswersNoRole is the other half, and it is the
// shape a module that administers permissions produces.
func TestASubjectWithOnlyActionsAnswersNoRole(t *testing.T) {
	t.Parallel()

	subject := auth.Subject{ID: "person-1", Tenant: "acme", Actions: []auth.Action{"user.view"}}

	if !subject.Can("user.view") {
		t.Error("a subject carrying an action was refused it")
	}
	if subject.HasRole("admin") {
		t.Error(`HasRole("admin") = true on a subject with no roles`)
	}
	if subject.HasRole("user.view") {
		t.Error("an action leaked into the role list")
	}
}

// TestTheEmptySubjectAnswersNothing keeps the direction of failure. A subject
// that carries nothing is refused everything, by both questions.
func TestTheEmptySubjectAnswersNothing(t *testing.T) {
	t.Parallel()

	var subject auth.Subject

	if subject.HasRole("admin") || subject.Can("user.view") {
		t.Error("an empty subject was granted something")
	}
}
