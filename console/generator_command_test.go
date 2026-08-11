package console_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// generator returns a generator writing into a directory of the test's own.
func generator(t *testing.T) console.GeneratorCommand {
	t.Helper()

	return console.GeneratorCommand{
		Type:             "Model",
		Stub:             "package {{ package }}\n\ntype {{ class }} struct{}\n",
		RootModule:       "github.com/acme/app",
		BasePath:         t.TempDir(),
		DefaultDirectory: "models",
	}
}

func TestTheGeneratorRefusesAReservedName(t *testing.T) {
	g := generator(t)

	for _, name := range []string{"range", "Type", "error", "func"} {
		if !g.IsReservedName(name) {
			t.Errorf("%q was accepted, and it is a word the language owns", name)
		}
	}
	if g.IsReservedName("Invoice") {
		t.Error("Invoice was refused as reserved")
	}
}

func TestQualifyClassAddsTheGeneratorsDirectory(t *testing.T) {
	g := generator(t)

	if got := g.QualifyClass("Invoice"); got != "models/Invoice" {
		t.Errorf("QualifyClass = %q, want models/Invoice", got)
	}
	if got := g.QualifyClass("billing/Invoice"); got != "billing/Invoice" {
		t.Errorf("QualifyClass = %q, and a name that carries a directory keeps it", got)
	}
}

func TestGetPathNamesTheFileTheWayGoDoes(t *testing.T) {
	g := generator(t)

	got := g.GetPath("models/InvoiceLine")
	if filepath.Base(got) != "invoice_line.go" {
		t.Errorf("the file is %q, want invoice_line.go", filepath.Base(got))
	}
}

func TestBuildClassFillsInThePackageAndTheType(t *testing.T) {
	g := generator(t)

	got := g.BuildClass("models/Invoice")
	if !strings.Contains(got, "package models") {
		t.Errorf("the stub rendered %q, want the package", got)
	}
	if !strings.Contains(got, "type Invoice struct{}") {
		t.Errorf("the stub rendered %q, want the type", got)
	}
}

func TestSortImportsPutsTheBlockInOrder(t *testing.T) {
	g := generator(t)

	stub := "package models\n\nimport (\n\t\"time\"\n\t\"context\"\n)\n\ntype Invoice struct{}\n"
	got := g.SortImports(stub)

	if strings.Index(got, `"context"`) > strings.Index(got, `"time"`) {
		t.Errorf("SortImports left %q out of order", got)
	}
}

func TestTheGeneratorWritesTheFileAndRefusesToOverwriteIt(t *testing.T) {
	g := generator(t)

	_, arguments, options, err := console.Parse("make:model {name} {--force}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	run := func(argv ...string) error {
		in := console.NewInput(arguments, options)
		if err := in.Parse(argv); err != nil {
			return err
		}
		o, _, _ := newConsoleIO(t, "")
		o.SetInput(in)
		return g.Handle(t.Context(), o)
	}

	if err := run("Invoice"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	path := filepath.Join(g.BasePath, "models", "invoice.go")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the generator did not write %s: %v", path, err)
	}
	if !strings.Contains(string(contents), "type Invoice struct{}") {
		t.Errorf("the file holds %q", contents)
	}

	if err := run("Invoice"); err == nil {
		t.Error("the generator overwrote a file that was already there")
	}
	if err := run("Invoice", "--force"); err != nil {
		t.Errorf("--force was given and the generator still refused: %v", err)
	}
}

func TestTheGeneratorRefusesToWriteAReservedName(t *testing.T) {
	g := generator(t)

	_, arguments, options, err := console.Parse("make:model {name}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	in := console.NewInput(arguments, options)
	if err := in.Parse([]string{"range"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	o, _, _ := newConsoleIO(t, "")
	o.SetInput(in)

	if err := g.Handle(t.Context(), o); err == nil {
		t.Error("a reserved name was generated, and the file would not compile")
	}
	if _, err := os.Stat(filepath.Join(g.BasePath, "models", "range.go")); err == nil {
		t.Error("the file was written before the name was checked")
	}
}

func TestTheMigrationGeneratorWritesOnceAndThenRefuses(t *testing.T) {
	m := console.MigrationGeneratorCommand{
		MigrationTableName: "cache",
		MigrationStub:      "create table {{table}} (k text primary key);",
		MigrationPath:      t.TempDir(),
		Now:                func() string { return "2026_08_11_120000" },
	}

	o, _, _ := newConsoleIO(t, "")
	if err := m.Handle(t.Context(), o); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	path := filepath.Join(m.MigrationPath, "2026_08_11_120000_create_cache_table.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the generator did not write %s: %v", path, err)
	}
	if got := string(contents); got != "create table cache (k text primary key);" {
		t.Errorf("the migration holds %q, want the table name substituted", got)
	}

	if err := m.Handle(t.Context(), o); err == nil {
		t.Error("a second migration for the same table was written, and one of them may already have run")
	}
}
