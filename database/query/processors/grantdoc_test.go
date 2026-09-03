package processors_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escapeHatchDeclaration is the sentence a symbol below the authorization line
// carries. It is the wording ConnectionInterface already uses, quoted rather
// than paraphrased: a reader who has seen it once recognises it, and a checker
// that accepted paraphrases would accept anything.
const escapeHatchDeclaration = "below the authorization layer, not outside it"

// connectionDoor is the method that hands a processor the connection. It is the
// only route from a *query.Builder to something that runs statements, so an
// exported function that calls it is an exported function that reaches the
// database.
const connectionDoor = "GetConnection"

// undeclared names an exported function that reaches the connection while
// taking no authorization credential and saying nothing about it.
type undeclared struct {
	name string
	line int
}

// TestEveryExportedSymbolThatReachesTheConnectionSaysWhereAuthorizationIs reads
// the published sources of this package and fails when an exported function
// reaches the connection with neither an auth.Grant parameter nor the
// declaration that it sits below the authorization line.
//
// The package documentation says where authorization is, and a reader who opens
// one file rather than the package never sees it. ProcessInsertGetID takes raw
// SQL and runs it; on pkg.go.dev it sits next to methods that only reshape a map
// of results, with nothing in its own entry to tell the two apart. A declared
// escape hatch and a leak read identically when the declaration is one directory
// away.
//
// So the property is per symbol: a function that can reach the database either
// asks for the credential in its signature, where the compiler enforces it, or
// says in its own doc comment that the credential was checked above it. Anything
// else is a third state, and the third state is the one nobody notices.
func TestEveryExportedSymbolThatReachesTheConnectionSaysWhereAuthorizationIs(t *testing.T) {
	fset := token.NewFileSet()

	for _, path := range publishedSources(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		for _, found := range undeclaredReachers(fset, file) {
			t.Errorf("%s:%d: %s reaches the connection, takes no authorization credential, and "+
				"does not say so. Give it the credential, or write %q in its doc comment the way "+
				"ConnectionInterface does.",
				path, found.line, found.name, escapeHatchDeclaration)
		}
	}
}

// TestTheDeclarationCheckCatchesAnUndeclaredReacher runs the check against a
// planted function and fails when the check reports nothing.
//
// A check over sources that already satisfy it passes whether it reads the
// sources or returns early, and the two are indistinguishable from a green run.
// The planted source is the difference: it reaches the connection, it takes no
// credential, its doc comment is silent, and the check has to say so.
func TestTheDeclarationCheckCatchesAnUndeclaredReacher(t *testing.T) {
	const planted = `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessTruncate empties a table.
func (p *Processor) ProcessTruncate(q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "planted.go", planted, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing the planted source: %v", err)
	}

	found := undeclaredReachers(fset, file)
	if len(found) != 1 {
		t.Fatalf("the check reported %d findings on a planted undeclared reacher, want 1; "+
			"a check that misses this one reads nothing on the real sources either", len(found))
	}
	if found[0].name != "ProcessTruncate" {
		t.Errorf("the check named %q, want ProcessTruncate", found[0].name)
	}
}

// TestTheDeclarationSatisfiesTheCheck runs the check against the same planted
// function once with the declaration and once with a credential, and fails when
// either is still reported.
//
// Without this, the check could be one that rejects every reacher, and the way
// to satisfy it would be to stop reaching the connection rather than to say
// where authorization is.
func TestTheDeclarationSatisfiesTheCheck(t *testing.T) {
	sources := map[string]string{
		"declared": `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessTruncate empties a table.
//
// It takes no credential and runs the statement it is given: a caller that
// reaches this is ` + escapeHatchDeclaration + `.
func (p *Processor) ProcessTruncate(q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`,
		"credentialed": `package processors

import (
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// ProcessTruncate empties a table.
func (p *Processor) ProcessTruncate(g auth.Grant, q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`,
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "planted.go", source, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing the planted source: %v", err)
			}

			if found := undeclaredReachers(fset, file); len(found) != 0 {
				t.Errorf("the check reported %d findings on a %s reacher, want 0", len(found), name)
			}
		})
	}
}

// undeclaredReachers returns the exported functions in file that reach the
// connection while carrying neither an authorization credential nor the
// declaration.
func undeclaredReachers(fset *token.FileSet, file *ast.File) []undeclared {
	var found []undeclared

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !fn.Name.IsExported() {
			continue
		}
		if !reachesConnection(fn.Body) {
			continue
		}
		if takesGrant(fn.Type) || strings.Contains(fn.Doc.Text(), escapeHatchDeclaration) {
			continue
		}

		found = append(found, undeclared{
			name: fn.Name.Name,
			line: fset.Position(fn.Pos()).Line,
		})
	}

	return found
}

// reachesConnection reports whether body asks the builder for the connection.
func reachesConnection(body *ast.BlockStmt) bool {
	reaches := false

	ast.Inspect(body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == connectionDoor {
			reaches = true
			return false
		}
		return !reaches
	})

	return reaches
}

// takesGrant reports whether the signature carries an authorization credential.
func takesGrant(signature *ast.FuncType) bool {
	if signature.Params == nil {
		return false
	}

	for _, field := range signature.Params.List {
		selector, ok := field.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok {
			continue
		}
		if pkg.Name == "auth" && selector.Sel.Name == "Grant" {
			return true
		}
	}

	return false
}

// publishedSources returns the .go files of this package that ship to a reader
// of the package documentation: everything but the tests.
//
// It fails rather than returning nothing, because a check that reads no files
// reports no findings and passes.
func publishedSources(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(".", name))
	}

	if len(paths) == 0 {
		t.Fatal("no published sources in this package, so this test read nothing and would pass on anything")
	}
	return paths
}
