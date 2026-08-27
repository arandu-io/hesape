package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// The call runner had no test at all, and it holds the one piece of state in
// this file: the list of what has already run, which CallOnce reads and which
// leaks between tests in the same binary unless it is forgotten.

// callDeps is what a project's Deps stands in for here. Its contents do not matter
// to the runner, which is the point of the type parameter.
type callDeps struct{ log *[]string }

type namedSeeder struct {
	name string
	run  func(ctx context.Context, d callDeps) error
}

func (s namedSeeder) Name() string { return s.name }

func (s namedSeeder) Run(ctx context.Context, d callDeps) error {
	*d.log = append(*d.log, s.name)
	if s.run != nil {
		return s.run(ctx, d)
	}
	return nil
}

func registryOf(names ...string) []database.Seeder[callDeps] {
	out := make([]database.Seeder[callDeps], 0, len(names))
	for _, name := range names {
		out = append(out, namedSeeder{name: name})
	}
	return out
}

func TestCallRunsTheNamedSeedersInOrder(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	var log []string
	ran, err := database.Call(context.Background(), registryOf("Role", "User", "Post"),
		callDeps{log: &log}, "Role", "User")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if strings.Join(ran, ",") != "Role,User" {
		t.Errorf("ran = %v, want the two named, in order", ran)
	}
	if strings.Join(log, ",") != "Role,User" {
		t.Errorf("log = %v; the third seeder ran without being asked", log)
	}
}

// TestCallStopsAtTheFirstFailureAndSaysWhichOne: a seeding run that half
// succeeded leaves a database nobody can reason about, so the first error ends
// it -- and the names that did run come back, because the caller needs to know
// how far it got.
func TestCallStopsAtTheFirstFailureAndSaysWhichOne(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	boom := errors.New("the table is not there")
	var log []string

	registry := []database.Seeder[callDeps]{
		namedSeeder{name: "Role"},
		namedSeeder{name: "User", run: func(context.Context, callDeps) error { return boom }},
		namedSeeder{name: "Post"},
	}

	ran, err := database.Call(context.Background(), registry, callDeps{log: &log}, "Role", "User", "Post")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the seeder's own error to travel", err)
	}
	if !strings.Contains(err.Error(), "User") {
		t.Errorf("err = %v, want it to name the seeder that failed", err)
	}
	if strings.Join(ran, ",") != "Role" {
		t.Errorf("ran = %v, want only what got through", ran)
	}
	if strings.Join(log, ",") != "Role,User" {
		t.Errorf("log = %v; Post ran after a failure", log)
	}
}

func TestCallReportsAnUnknownSeederByName(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	var log []string
	_, err := database.Call(context.Background(), registryOf("Role"), callDeps{log: &log}, "Nope")
	if err == nil {
		t.Fatal("an unknown seeder ran without complaint")
	}
	if !strings.Contains(err.Error(), "Nope") {
		t.Errorf("err = %v, want it to name what was asked for", err)
	}
}

// TestCallOnceSkipsWhatHasAlreadyRun is the whole reason CallOnce exists: the
// shared seeder that four others depend on inserts its rows once.
func TestCallOnceSkipsWhatHasAlreadyRun(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	var log []string
	registry := registryOf("Currency", "Role")

	if _, err := database.Call(context.Background(), registry, callDeps{log: &log}, "Currency"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	ran, err := database.CallOnce(context.Background(), registry, callDeps{log: &log}, "Currency", "Role")
	if err != nil {
		t.Fatalf("CallOnce: %v", err)
	}

	if strings.Join(ran, ",") != "Role" {
		t.Errorf("ran = %v, want only the one that had not run", ran)
	}
	if strings.Join(log, ",") != "Currency,Role" {
		t.Errorf("log = %v, want Currency exactly once", log)
	}
}

// TestCalledSeedersIsReadableAndForgettable pins the state itself, because a
// list that cannot be emptied makes every test after the first one lie.
func TestCalledSeedersIsReadableAndForgettable(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	var log []string
	if _, err := database.Call(context.Background(), registryOf("Role"), callDeps{log: &log}, "Role"); err != nil {
		t.Fatalf("Call: %v", err)
	}

	if got := database.CalledSeeders(); len(got) != 1 || got[0] != "Role" {
		t.Fatalf("CalledSeeders = %v, want [Role]", got)
	}

	// The returned slice is a copy: writing to it must not reach the list.
	database.CalledSeeders()[0] = "tampered"
	if got := database.CalledSeeders(); got[0] != "Role" {
		t.Errorf("the caller's write reached the package's list: %v", got)
	}

	database.ForgetCalledSeeders()
	if got := database.CalledSeeders(); len(got) != 0 {
		t.Errorf("CalledSeeders = %v after forgetting, want empty", got)
	}
}

// TestCallWithBuildsTheDepsFromTheArguments covers the shape this package uses
// so that it never has to know what a project's Deps carries.
func TestCallWithBuildsTheDepsFromTheArguments(t *testing.T) {
	database.ForgetCalledSeeders()
	t.Cleanup(database.ForgetCalledSeeders)

	var log []string
	seen := ""
	registry := []database.Seeder[callDeps]{
		namedSeeder{name: "User", run: func(_ context.Context, d callDeps) error {
			seen = strings.Join(*d.log, ",")
			return nil
		}},
	}

	_, err := database.CallWith(context.Background(), registry,
		func(args []string) callDeps { log = append(log, args...); return callDeps{log: &log} },
		[]string{"--force"}, "User")
	if err != nil {
		t.Fatalf("CallWith: %v", err)
	}
	if !strings.Contains(seen, "--force") {
		t.Errorf("the seeder saw %q; the arguments did not reach the Deps builder", seen)
	}
}
