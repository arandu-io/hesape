package migrations_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
	migrationcommands "github.com/arandu-io/hesape/database/console/migrations"
	"github.com/arandu-io/hesape/database/migrations"
)

// TestMakeMigrationSaysTheImportInBootstrapIsWhatRunsIt: a migration registers
// itself from init(), so the package it lands in runs only when something
// blank-imports it -- and that import is in bootstrap/app.go. main.go hands the
// command to the kernel and imports nothing else, so a hint naming it sends the
// reader to a file where the line does not belong.
func TestMakeMigrationSaysTheImportInBootstrapIsWhatRunsIt(t *testing.T) {
	c := migrationcommands.MigrateMakeCommand(migrationcommands.Deps{
		Creator:       migrations.NewMigrationCreator(""),
		MigrationPath: t.TempDir(),
	})

	var out, errOut bytes.Buffer
	o := console.NewIO(c.Name, []string{"create_invoices_table"}, &out, &errOut, strings.NewReader(""))
	if err := c.Run(context.Background(), o); err != nil {
		t.Fatalf("make:migration: %v\n%s", err, errOut.String())
	}

	said := out.String() + errOut.String()
	if !strings.Contains(said, "blank-imported in bootstrap/app.go") {
		t.Errorf("make:migration does not name the import that runs the migration:\n%s", said)
	}
	if strings.Contains(said, "main.go") {
		t.Errorf("make:migration still sends the reader to main.go:\n%s", said)
	}
}
