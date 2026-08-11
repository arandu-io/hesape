package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func frozenCreator(t *testing.T, custom string) *MigrationCreator {
	t.Helper()
	creator := NewMigrationCreator(custom)
	creator.now = func() time.Time { return time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC) }
	return creator
}

func TestCreateWritesAMigrationThatRegistersItself(t *testing.T) {
	dir := t.TempDir()
	creator := frozenCreator(t, "")

	path, err := creator.Create("create_invoices_table", dir, "invoices", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := filepath.Join(dir, "2026_08_11_093000_create_invoices_table.go")
	if path != want {
		t.Fatalf("Create wrote %q, want %q", path, want)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	source := string(body)

	for _, needle := range []string{
		"func init() { migrations.Register(CreateInvoicesTable{}) }",
		`return "2026_08_11_093000_create_invoices_table"`,
		"CREATE TABLE invoices",
		"DROP TABLE invoices",
	} {
		if !strings.Contains(source, needle) {
			t.Fatalf("the generated migration does not contain %q:\n%s", needle, source)
		}
	}
}

func TestCreateUsesTheUpdateStubForAnAlter(t *testing.T) {
	dir := t.TempDir()
	creator := frozenCreator(t, "")

	path, err := creator.Create("add_paid_at_to_invoices", dir, "invoices", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "ALTER TABLE invoices") {
		t.Fatalf("an alter migration got the create stub:\n%s", body)
	}
}

func TestCreateRefusesASecondMigrationWithTheSameName(t *testing.T) {
	dir := t.TempDir()
	creator := frozenCreator(t, "")

	if _, err := creator.Create("create_invoices_table", dir, "invoices", true); err != nil {
		t.Fatalf("Create: %v", err)
	}

	creator.now = func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }

	if _, err := creator.Create("create_invoices_table", dir, "invoices", true); err == nil {
		t.Fatal("a second migration with the same name and a later prefix was written; it would compile and apply twice")
	}
}

func TestCustomStubWinsOverTheBuiltIn(t *testing.T) {
	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "migration.stub"), []byte("// {{ class }} / {{ name }}\n"), 0o644); err != nil {
		t.Fatalf("writing the custom stub: %v", err)
	}

	dir := t.TempDir()
	creator := frozenCreator(t, stubs)

	path, err := creator.Create("fix_the_thing", dir, "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	body, _ := os.ReadFile(path)
	if string(body) != "// FixTheThing / 2026_08_11_093000_fix_the_thing\n" {
		t.Fatalf("the custom stub was not used, or not filled in:\n%s", body)
	}
}

func TestAfterCreateHookSeesTheTableAndThePath(t *testing.T) {
	dir := t.TempDir()
	creator := frozenCreator(t, "")

	var gotTable, gotPath string
	creator.AfterCreate(func(table, path string) { gotTable, gotPath = table, path })

	path, err := creator.Create("create_invoices_table", dir, "invoices", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotTable != "invoices" || gotPath != path {
		t.Fatalf("the hook saw (%q, %q)", gotTable, gotPath)
	}
}

func TestGetDatePrefixIsUTC(t *testing.T) {
	creator := frozenCreator(t, "")

	if got := creator.GetDatePrefix(); got != "2026_08_11_093000" {
		t.Fatalf("GetDatePrefix = %q", got)
	}
}
