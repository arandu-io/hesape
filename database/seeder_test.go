package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database"
)

// deps is what a project decides a seeder may touch. The framework never
// declares it; this is the stand-in that proves the contract works over any
// shape, including one carrying the leftover arguments.
type deps struct {
	Tenant string
	Args   []string
}

type recordingSeeder struct {
	name string
	ran  *deps
	err  error
}

func (s recordingSeeder) Name() string { return s.name }

func (s recordingSeeder) Run(_ context.Context, d deps) error {
	*s.ran = d
	return s.err
}

func registry(ran *deps, err error) []database.Seeder[deps] {
	return []database.Seeder[deps]{
		recordingSeeder{name: "DatabaseSeeder", ran: ran},
		recordingSeeder{name: "UserSeeder", ran: ran, err: err},
	}
}

func build(rest []string) deps { return deps{Tenant: "acme", Args: rest} }

func TestSeedRunsTheFallbackWhenNothingIsNamed(t *testing.T) {
	var ran deps

	name, err := database.Seed(context.Background(), registry(&ran, nil), "DatabaseSeeder", nil, build)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if name != "DatabaseSeeder" {
		t.Fatalf("ran %q, want the fallback", name)
	}
	if ran.Tenant != "acme" {
		t.Errorf("the deps the project built did not reach the seeder: %+v", ran)
	}
}

// TestSeedIsCaseInsensitive: the name is typed by hand on a command line, and
// refusing "userseeder" teaches nothing.
func TestSeedIsCaseInsensitive(t *testing.T) {
	var ran deps

	name, err := database.Seed(context.Background(), registry(&ran, nil), "DatabaseSeeder", []string{"userseeder"}, build)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if name != "UserSeeder" {
		t.Fatalf("ran %q, want UserSeeder", name)
	}
}

// TestWhatFollowsTheNameReachesTheSeeder is why deps is a function: the project
// carries the leftover arguments on its own type, and this package never learns
// the field exists.
func TestWhatFollowsTheNameReachesTheSeeder(t *testing.T) {
	var ran deps

	_, err := database.Seed(context.Background(), registry(&ran, nil), "DatabaseSeeder",
		[]string{"UserSeeder", "-e", "a@b.com", "-p", "secret"}, build)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if got, ok := database.Flag(ran.Args, "e"); !ok || got != "a@b.com" {
		t.Errorf("-e = %q (%v), want a@b.com", got, ok)
	}
	if got, ok := database.Flag(ran.Args, "p"); !ok || got != "secret" {
		t.Errorf("-p = %q (%v), want secret", got, ok)
	}
}

// TestFlagFormIsRefusedWithTheWordToUse: a name that is sometimes a flag and
// sometimes a word is two spellings of one thing, and refusing it quietly would
// run the wrong seeder.
func TestFlagFormIsRefusedWithTheWordToUse(t *testing.T) {
	var ran deps

	_, err := database.Seed(context.Background(), registry(&ran, nil), "DatabaseSeeder",
		[]string{"--class=UserSeeder"}, build)
	if err == nil {
		t.Fatal("--class= was accepted")
	}
	if !strings.Contains(err.Error(), "aru db:seed UserSeeder") {
		t.Errorf("the error does not say what to write instead: %v", err)
	}
}

func TestAnUnknownSeederListsTheOnesThereAre(t *testing.T) {
	var ran deps

	_, err := database.Seed(context.Background(), registry(&ran, nil), "DatabaseSeeder",
		[]string{"PostSeeder"}, build)
	if err == nil {
		t.Fatal("an unknown seeder ran")
	}
	for _, want := range []string{"PostSeeder", "DatabaseSeeder", "UserSeeder"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// TestTheSeederErrorCarriesItsName: a failure in a run of ten seeders has to say
// which one, or the message is a stack trace with no address.
func TestTheSeederErrorCarriesItsName(t *testing.T) {
	var ran deps
	sentinel := errors.New("the tenant has no plan")

	name, err := database.Seed(context.Background(), registry(&ran, sentinel), "DatabaseSeeder",
		[]string{"UserSeeder"}, build)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the seeder's error", err)
	}
	if name != "UserSeeder" || !strings.Contains(err.Error(), "UserSeeder") {
		t.Errorf("the failure does not name the seeder: %q / %v", name, err)
	}
}

func TestFlagAcceptsEveryFormSomebodyTypes(t *testing.T) {
	for _, args := range [][]string{
		{"-e", "a@b.com"},
		{"--e", "a@b.com"},
		{"-e=a@b.com"},
		{"--e=a@b.com"},
	} {
		got, ok := database.Flag(args, "e")
		if !ok || got != "a@b.com" {
			t.Errorf("Flag(%v) = %q, %v", args, got, ok)
		}
	}

	// Present with nothing after it. Reporting it as absent would turn a
	// typed-but-empty password into the demo fallback.
	if got, ok := database.Flag([]string{"-p"}, "p"); ok != true || got != "" {
		t.Errorf(`Flag(["-p"]) = %q, %v; want "", true`, got, ok)
	}
	if _, ok := database.Flag([]string{"-e", "a@b.com"}, "p"); ok {
		t.Error("an absent flag reported present")
	}
}

func TestSwitch(t *testing.T) {
	if !database.Switch([]string{"-f", "--force"}, "force") {
		t.Error("--force was not seen")
	}
	if !database.Switch([]string{"-force"}, "force") {
		t.Error("-force was not seen")
	}
	if database.Switch([]string{"--forced"}, "force") {
		t.Error("a longer flag matched a shorter name")
	}
}
