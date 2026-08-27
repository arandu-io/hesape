package migrations

import (
	"go/ast"
	"go/parser"
	"go/token"
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
		`conn.Schema().Create(ctx, "invoices"`,
		`conn.Schema().DropIfExists(ctx, "invoices")`,
	} {
		if !strings.Contains(source, needle) {
			t.Fatalf("the generated migration does not contain %q:\n%s", needle, source)
		}
	}

	// The stub is Go, and a stub that does not parse is a generator that fails
	// at the user's build rather than here. Parsing it is what catches an
	// import the stub names and does not declare -- which is how the Blueprint
	// was nearly added to three templates and one import block.
	assertItCompiles(t, path, source)
}

// assertItCompiles reads the generated file the way the user's build will.
//
// It parses, which catches a stub whose braces do not close, and then checks
// that every package the body names is one the file imports. The second half is
// there because the first does not catch it: a stub that says schema.Blueprint
// and does not import schema parses perfectly and fails at `go build` in the
// project the generator was run in, which is the worst place to find out.
func assertItCompiles(t *testing.T, path, source string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated migration does not parse: %v\n%s", err, source)
	}

	imported := map[string]bool{}
	for _, spec := range file.Imports {
		value := strings.Trim(spec.Path.Value, `"`)
		name := value[strings.LastIndex(value, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imported[name] = true
	}

	declared := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.TypeSpec:
			declared[d.Name.Name] = true
		case *ast.FuncDecl:
			declared[d.Name.Name] = true
		}
		return true
	})

	ast.Inspect(file, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, ok := selector.X.(*ast.Ident)
		// A lowercase root with no object is a package qualifier: a local
		// variable resolves to its declaration and an exported name is a type.
		if !ok || root.Obj != nil || declared[root.Name] {
			return true
		}
		if !imported[root.Name] && root.Name != "ctx" && root.Name != "conn" && root.Name != "table" {
			t.Errorf("the stub names %s.%s and does not import %s:\n%s",
				root.Name, selector.Sel.Name, root.Name, source)
		}
		return true
	})
}

func TestCreateUsesTheUpdateStubForAnAlter(t *testing.T) {
	dir := t.TempDir()
	creator := frozenCreator(t, "")

	path, err := creator.Create("add_paid_at_to_invoices", dir, "invoices", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	body, _ := os.ReadFile(path)
	source := string(body)
	// The update stub alters rather than creates, and the difference is which
	// Blueprint entry point it opens with.
	if !strings.Contains(source, `conn.Schema().Table(ctx, "invoices"`) {
		t.Fatalf("an alter migration does not open the table:\n%s", source)
	}
	if strings.Contains(source, "conn.Schema().Create(") {
		t.Fatalf("an alter migration got the create stub:\n%s", source)
	}
	assertItCompiles(t, path, source)
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
