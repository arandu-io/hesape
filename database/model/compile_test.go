package model_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestTheModelCannotBeReachedWithoutAGrant is the model layer's half of exit
// criterion 2 of phase 1.
//
// The repository has had this test since phase 1, and it is what the claim of
// the framework rests on: the path from a handler to the database cannot be
// written without a Grant. The model is becoming that path, so it needs the
// same proof rather than the same intention -- and a claim about compilation
// can only be proven by attempting a compilation.
//
// Each fixture has to fail for the stated reason. A fixture that fails because
// a package moved, or because a name was misspelled, proves nothing at all and
// is worse than no fixture: it is a green test guarding nothing.
func TestTheModelCannotBeReachedWithoutAGrant(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture with the go tool")
	}

	cases := []struct {
		fixture string
		reason  string
		want    string
	}{
		{
			fixture: "./testdata/missing_grant",
			reason:  "reading through the model without a Grant",
			want:    "not enough arguments in call to",
		},
		{
			fixture: "./testdata/missing_context",
			reason:  "running a statement with no context to cancel it",
			want:    "not enough arguments in call to",
		},
		{
			fixture: "./testdata/forged_grant",
			reason:  "building a valid Grant outside the auth package",
			want:    "cannot refer to unexported field valid",
		},
		{
			fixture: "./testdata/raw_connection",
			reason:  "reaching the connection under the model as a field",
			want:    "users.Connection undefined",
		},
		{
			fixture: "./testdata/connection_accessor",
			reason:  "reaching the connection under the model through an accessor",
			want:    "users.GetConnection undefined",
		},
	}

	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			cmd := exec.Command("go", "vet", c.fixture)
			// hesape is not a member of the workspace, so a go.work above the
			// checkout would refuse the fixture with a message about the
			// workspace rather than the one under test.
			cmd.Env = append(os.Environ(), "GOWORK=off")

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s compiled. The framework thesis is broken.\n%s", c.reason, out)
			}
			if !strings.Contains(string(out), c.want) {
				t.Fatalf("the fixture failed for the wrong reason.\nwant a message containing: %s\ngot:\n%s", c.want, out)
			}
		})
	}
}

// TestAModuleCanReachTheRelationSurface is the positive fixture, and it is here
// rather than beside the tests that run because what it measures is compilation
// from outside the package.
//
// The relation surface was unreachable while every test inside the package
// passed. The builder asked relations for Match(grant, keys, constraints) and
// GetRelationExistenceQuery(*query.Builder, any); the twelve constructors in
// model/relations answer Match(models, results, relation) and
// GetRelationExistenceQuery(Builder, Builder, ...any). Nothing satisfied the
// first pair except a stand-in in a test file -- so RelationResolvers could not
// be populated from HasManyOf or any of its siblings, and With, Load, Has,
// WhereHas and WithCount could not be called by an application at all.
//
// A test inside the package cannot catch that, because the stand-in it holds is
// the thing that made the interface look inhabited. This compiles a program that
// only imports the package, which is the position an application is in.
func TestAModuleCanReachTheRelationSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture with the go tool")
	}

	cmd := exec.Command("go", "vet", "./testdata/relation_surface")
	cmd.Env = append(os.Environ(), "GOWORK=off")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a module that registers a relation and eager loads it does not compile:\n%s", out)
	}
}
