package seeds_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database/console/seeds"
)

// run drives one of the commands the way a binary does, and answers what it
// printed and what it returned.
func run(t *testing.T, deps seeds.Deps, name string, args ...string) (string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	app := console.NewApplication(&out, &errOut, strings.NewReader(""))
	app.Add(seeds.Commands(deps)...)

	err := app.Call(context.Background(), name, args...)
	return out.String() + errOut.String() + app.Output(), err
}

// TestTheClassFlagSaysWhatToTypeInstead.
//
// A name that is sometimes a flag and sometimes a word is two spellings of one
// thing, so --class= is refused with the word to use instead. That refusal was
// documented and unreachable: the flag set is parsed first and answers "flag
// provided but not defined: -class", which is true and useless, and the sentence
// somebody reads and retypes never ran.
func TestTheClassFlagSaysWhatToTypeInstead(t *testing.T) {
	seeded := ""
	deps := seeds.Deps{
		Seed: func(_ context.Context, name string, _ []string) (string, error) {
			seeded = name
			return name, nil
		},
	}

	for _, arg := range []string{"--class=PostSeeder", "--class", "-class"} {
		t.Run(arg, func(t *testing.T) {
			seeded = "not called"

			_, err := run(t, deps, "db:seed", arg)
			if err == nil {
				t.Fatalf("db:seed %s was accepted", arg)
			}
			if strings.Contains(err.Error(), "not defined") {
				t.Fatalf("the flag parser answered before the refusal did: %v", err)
			}
			if !strings.Contains(err.Error(), "aru db:seed ") {
				t.Errorf("the refusal does not say what to type instead: %v", err)
			}
			if seeded != "not called" {
				t.Errorf("it seeded %q on the way to refusing", seeded)
			}
		})
	}

	// The value is carried into the suggestion, so the line can be retyped as
	// it is rather than translated.
	_, err := run(t, deps, "db:seed", "--class=PostSeeder")
	if err == nil || !strings.Contains(err.Error(), "aru db:seed PostSeeder") {
		t.Errorf("the refusal does not name the seeder that was asked for: %v", err)
	}
}

// TestTheNameIsPositionalAndTheRestReachesTheSeeder.
//
// Everything after the name is the seeder's business -- this package does not
// know that a UserSeeder has a -p -- so it travels unparsed.
func TestTheNameIsPositionalAndTheRestReachesTheSeeder(t *testing.T) {
	var gotName string
	var gotArgs []string

	deps := seeds.Deps{
		Seed: func(_ context.Context, name string, args []string) (string, error) {
			gotName, gotArgs = name, args
			return "UserSeeder", nil
		},
	}

	out, err := run(t, deps, "db:seed", "UserSeeder", "-e", "ada@example.test")
	if err != nil {
		t.Fatalf("db:seed: %v", err)
	}
	if gotName != "UserSeeder" {
		t.Errorf("the name reached the seeder as %q", gotName)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "-e" || gotArgs[1] != "ada@example.test" {
		t.Errorf("the rest of the line reached the seeder as %v", gotArgs)
	}
	if !strings.Contains(out, "UserSeeder") {
		t.Errorf("the output does not name what ran:\n%s", out)
	}
}

// TestSeedingWithNoNameRunsTheRootSeeder: the empty name is what the application
// answers with its own default, and the command does not invent one.
func TestSeedingWithNoNameRunsTheRootSeeder(t *testing.T) {
	called := false
	deps := seeds.Deps{
		Seed: func(_ context.Context, name string, _ []string) (string, error) {
			called = true
			if name != "" {
				t.Errorf("the command invented the name %q", name)
			}
			return "DatabaseSeeder", nil
		},
	}

	if _, err := run(t, deps, "db:seed"); err != nil {
		t.Fatalf("db:seed: %v", err)
	}
	if !called {
		t.Error("db:seed with no name ran nothing")
	}
}

// TestSeedingWithoutASeedRegisteredSaysSo: a nil Seed is an application that
// registered no seeders, and it says that rather than doing nothing quietly.
func TestSeedingWithoutASeedRegisteredSaysSo(t *testing.T) {
	_, err := run(t, seeds.Deps{}, "db:seed")
	if err == nil {
		t.Fatal("db:seed with no seeders reported success")
	}
	if !strings.Contains(err.Error(), "no seeders") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}
