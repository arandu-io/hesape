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
