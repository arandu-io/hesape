package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/console"
	migrationcommands "github.com/arandu-io/hesape/database/console/migrations"
	"github.com/arandu-io/hesape/database/migrations"
)

// schemaEmptyingGroup prefixes the registry group each fixture registers its
// migration in. The group is the fixture's own -- the test and the environment
// are in it -- so a reset reaches only what that fixture registered, and the
// Down it records is that fixture's.
const schemaEmptyingGroup = "development_only"

// emptyingFixture is one applied migration, a record of every Down that ran
// and of every wipe, over the deps the three commands take.
type emptyingFixture struct {
	repository *testRepository
	downs      *[]string
	wipes      *int
	deps       migrationcommands.Deps
}

func newEmptyingFixture(t *testing.T, env config.Env) emptyingFixture {
	t.Helper()

	group := schemaEmptyingGroup + "/" + t.Name() + "/" + string(env)
	downs := &[]string{}
	migrations.Register(indexMigration{name: "2026_02_01_000000_create_invoices", downs: downs}, group)

	repository := &testRepository{exists: true}
	migrator := migrations.NewMigrator(repository, &testResolver{}, nil)
	if _, err := migrator.Run(context.Background(), []string{group}, migrations.Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wipes := new(int)
	return emptyingFixture{
		repository: repository,
		downs:      downs,
		wipes:      wipes,
		deps: migrationcommands.Deps{
			Migrator:        migrator,
			MigrationGroups: []string{group},
			Wipe:            func(context.Context, string) error { *wipes++; return nil },
			Environment:     env,
		},
	}
}

func runCommand(c console.Command, args ...string) error {
	out, errOut := &strings.Builder{}, &strings.Builder{}
	return c.Run(context.Background(), console.NewIO(c.Name, args, out, errOut, nil))
}

// schemaEmptying are the three commands no argument narrows.
func schemaEmptying(deps migrationcommands.Deps) []console.Command {
	return []console.Command{
		migrationcommands.FreshCommand(deps),
		migrationcommands.ResetCommand(deps),
		migrationcommands.RefreshCommand(deps),
	}
}

// TestTheCommandsThatEmptyTheSchemaRefuseOutsideDevelopment: fresh drops every
// table, reset runs every Down, refresh does reset and builds an empty schema
// back. A deployed binary that wired them must not be one command away from
// losing every row, and the refusal has to come before anything is touched --
// including under --pretend, and including a binary that never said where it
// runs.
func TestTheCommandsThatEmptyTheSchemaRefuseOutsideDevelopment(t *testing.T) {
	for _, env := range []config.Env{config.EnvProd, config.EnvStaging, ""} {
		fixture := newEmptyingFixture(t, env)

		for _, command := range schemaEmptying(fixture.deps) {
			for _, args := range [][]string{nil, {"--pretend"}} {
				err := runCommand(command, args...)
				if err == nil {
					t.Fatalf("%s %v ran with Environment %q", command.Name, args, env)
				}
				if !strings.Contains(err.Error(), "only runs with APP_ENV=dev") {
					t.Fatalf("%s refused with %q, which does not say where it may run", command.Name, err)
				}
				if !strings.Contains(err.Error(), "migrate:rollback") {
					t.Fatalf("%s refused with %q, which does not name the command for undoing a release", command.Name, err)
				}
			}
		}

		if *fixture.wipes != 0 {
			t.Fatalf("with Environment %q a refused command still wiped the schema %d time(s)", env, *fixture.wipes)
		}
		if len(*fixture.downs) != 0 {
			t.Fatalf("with Environment %q a refused command still ran Down for %v", env, *fixture.downs)
		}
		if len(fixture.repository.records) != 1 {
			t.Fatalf("with Environment %q the repository holds %v, want the one applied migration", env, fixture.repository.records)
		}
	}
}

// TestFreshRunsInDevelopment: the refusal is about where, not about the
// command, so in development fresh wipes and migrates again.
func TestFreshRunsInDevelopment(t *testing.T) {
	fixture := newEmptyingFixture(t, config.EnvDev)

	if err := runCommand(migrationcommands.FreshCommand(fixture.deps)); err != nil {
		t.Fatalf("migrate:fresh in development: %v", err)
	}
	if *fixture.wipes != 1 {
		t.Fatalf("migrate:fresh wiped %d time(s), want 1", *fixture.wipes)
	}
}

// TestResetRunsInDevelopment: in development reset runs every Down.
func TestResetRunsInDevelopment(t *testing.T) {
	fixture := newEmptyingFixture(t, config.EnvDev)

	if err := runCommand(migrationcommands.ResetCommand(fixture.deps)); err != nil {
		t.Fatalf("migrate:reset in development: %v", err)
	}
	if strings.Join(*fixture.downs, ",") != "2026_02_01_000000_create_invoices" {
		t.Fatalf("Down ran for %v, want the one applied migration", *fixture.downs)
	}
}

// TestRefreshRunsInDevelopment: in development refresh undoes and re-applies.
func TestRefreshRunsInDevelopment(t *testing.T) {
	fixture := newEmptyingFixture(t, config.EnvDev)

	if err := runCommand(migrationcommands.RefreshCommand(fixture.deps)); err != nil {
		t.Fatalf("migrate:refresh in development: %v", err)
	}
	if len(*fixture.downs) != 1 || len(fixture.repository.records) != 1 {
		t.Fatalf("refresh ran Down for %v and left %v applied, want one of each", *fixture.downs, fixture.repository.records)
	}
}

// TestRollbackStillRunsInProduction: undoing the batch a bad release applied
// is what a deployment needs, so the one scoped schema-down command is not
// swept up with the three that empty everything.
func TestRollbackStillRunsInProduction(t *testing.T) {
	fixture := newEmptyingFixture(t, config.EnvProd)

	if err := runCommand(migrationcommands.RollbackCommand(fixture.deps)); err != nil {
		t.Fatalf("migrate:rollback in production: %v", err)
	}
	if len(*fixture.downs) != 1 {
		t.Fatalf("Down ran for %v, want the batch the release applied", *fixture.downs)
	}
}
