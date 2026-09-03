package processors_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
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
	file string
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

	var files []*ast.File
	for _, path := range publishedSources(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		files = append(files, file)
	}

	for _, found := range undeclaredReachers(fset, files) {
		t.Errorf("%s:%d: %s reaches the connection, takes no authorization credential, and "+
			"does not say so. Give it the credential, or write %q in its doc comment the way "+
			"ConnectionInterface does.",
			found.file, found.line, found.name, escapeHatchDeclaration)
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

	found := reachersIn(t, map[string]string{"planted.go": planted})
	if len(found) != 1 {
		t.Fatalf("the check reported %d findings on a planted undeclared reacher, want 1; "+
			"a check that misses this one reads nothing on the real sources either", len(found))
	}
	if found[0].name != "ProcessTruncate" {
		t.Errorf("the check named %q, want ProcessTruncate", found[0].name)
	}
}

// TestTheDeclarationCheckFollowsACallEdge plants the reach one call away.
//
// An exported function that hands the work to an unexported helper reaches the
// connection exactly as much as one that asks for it inline, and a reader of
// pkg.go.dev sees less: the helper is not published, so its entry says nothing
// and the exported entry says nothing either. A check that reads one function
// body sees the first shape and misses this one, which is the shape a symbol
// takes after anybody refactors it.
func TestTheDeclarationCheckFollowsACallEdge(t *testing.T) {
	const planted = `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessIndirectReacher empties a table.
func (p *Processor) ProcessIndirectReacher(q *query.Builder, sql string) error {
	return p.runOnConnection(q, sql)
}

func (p *Processor) runOnConnection(q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`

	found := reachersIn(t, map[string]string{"planted.go": planted})
	if len(found) != 1 {
		t.Fatalf("the check reported %d findings on a reacher one call away, want 1; "+
			"a symbol reaches the database through the helper it calls, and the helper "+
			"is not what a reader of the package documentation is shown", len(found))
	}
	if found[0].name != "ProcessIndirectReacher" {
		t.Errorf("the check named %q, want ProcessIndirectReacher", found[0].name)
	}
}

// TestTheDeclarationCheckFollowsACallEdgeAcrossFiles plants the helper in the
// file next door, which is where a helper shared by two processors goes.
func TestTheDeclarationCheckFollowsACallEdgeAcrossFiles(t *testing.T) {
	found := reachersIn(t, map[string]string{
		"planted.go": `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessIndirectReacher empties a table.
func (p *Processor) ProcessIndirectReacher(q *query.Builder, sql string) error {
	return p.runOnConnection(q, sql)
}
`,
		"helper.go": `package processors

import "github.com/arandu-io/hesape/database/query"

func (p *Processor) runOnConnection(q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`,
	})

	if len(found) != 1 {
		t.Fatalf("the check reported %d findings on a reacher whose helper is in another "+
			"file of the same package, want 1", len(found))
	}
	if found[0].name != "ProcessIndirectReacher" {
		t.Errorf("the check named %q, want ProcessIndirectReacher", found[0].name)
	}
}

// TestTheDeclarationCheckCatchesAHandedConnection plants the reach with no call
// to the door at all.
//
// A function taking a connection has already been handed what GetConnection
// returns, so looking for the door finds nothing while the function runs
// whatever statement it likes. It is the same leak with the reach moved into
// the signature, and the signature is the half a reader of pkg.go.dev does see.
func TestTheDeclarationCheckCatchesAHandedConnection(t *testing.T) {
	const planted = `package processors

import (
	"context"

	"github.com/arandu-io/hesape/database"
)

// ProcessOnConnection empties a table.
func (p *Processor) ProcessOnConnection(ctx context.Context, c database.ConnectionInterface, sql string) error {
	_, err := c.Insert(ctx, sql, nil)
	return err
}
`

	found := reachersIn(t, map[string]string{"planted.go": planted})
	if len(found) != 1 {
		t.Fatalf("the check reported %d findings on a function handed a connection, want 1", len(found))
	}
	if found[0].name != "ProcessOnConnection" {
		t.Errorf("the check named %q, want ProcessOnConnection", found[0].name)
	}
}

// reachersIn parses a planted package and runs the check over all of it, which
// is the unit the check reads: a helper lives wherever its author put it.
func reachersIn(t *testing.T, sources map[string]string) []undeclared {
	t.Helper()

	fset := token.NewFileSet()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make([]*ast.File, 0, len(sources))
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, sources[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing the planted source %s: %v", name, err)
		}
		files = append(files, file)
	}

	return undeclaredReachers(fset, files)
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
		"declared one call away": `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessTruncate empties a table.
//
// It takes no credential and runs the statement it is given: a caller that
// reaches this is ` + escapeHatchDeclaration + `.
func (p *Processor) ProcessTruncate(q *query.Builder, sql string) error {
	return p.runOnConnection(q, sql)
}

func (p *Processor) runOnConnection(q *query.Builder, sql string) error {
	return p.run(q.GetConnection(), sql)
}
`,
		"declared while handed a connection": `package processors

import (
	"context"

	"github.com/arandu-io/hesape/database"
)

// ProcessOnConnection empties a table.
//
// It takes no credential and runs the statement it is given: a caller that
// reaches this is ` + escapeHatchDeclaration + `.
func (p *Processor) ProcessOnConnection(ctx context.Context, c database.ConnectionInterface, sql string) error {
	_, err := c.Insert(ctx, sql, nil)
	return err
}
`,
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			if found := reachersIn(t, map[string]string{"planted.go": source}); len(found) != 0 {
				t.Errorf("the check reported %d findings on a %s reacher, want 0", len(found), name)
			}
		})
	}
}

// TestAFunctionThatTouchesNoConnectionIsNotReported is the floor. A check whose
// call-edge walk marked everything would satisfy every test above by reporting
// every symbol, and the package is mostly symbols that only reshape a map.
func TestAFunctionThatTouchesNoConnectionIsNotReported(t *testing.T) {
	const planted = `package processors

import "github.com/arandu-io/hesape/database/query"

// ProcessTables normalises the columns of a table listing.
func (p *Processor) ProcessTables(results []query.Record) []query.Record {
	return append(results[:0:0], normalise(results)...)
}

func normalise(results []query.Record) []query.Record { return results }
`

	if found := reachersIn(t, map[string]string{"planted.go": planted}); len(found) != 0 {
		t.Errorf("the check reported %d findings on a function that reaches nothing: %+v", len(found), found)
	}
}

// undeclaredReachers returns the exported functions in the package that reach
// the connection while carrying neither an authorization credential nor the
// declaration.
//
// It takes the whole package rather than one file, because reaching the
// connection is not a property of a function body. A symbol reaches it by
// asking the builder for it, by being handed one, or by calling something in
// the package that does either -- and that something lives wherever its author
// put it. The first version of this check read one body at a time, and an
// exported method that moved its one line into an unexported helper stopped
// being reported while reaching exactly as far.
func undeclaredReachers(fset *token.FileSet, files []*ast.File) []undeclared {
	declarations := functionsIn(files)
	reaching := reachingNames(declarations)

	var found []undeclared
	for _, fn := range declarations {
		if !fn.Name.IsExported() || !reaches(fn, reaching) {
			continue
		}
		if takesGrant(fn.Type) || strings.Contains(fn.Doc.Text(), escapeHatchDeclaration) {
			continue
		}

		position := fset.Position(fn.Pos())
		found = append(found, undeclared{
			name: fn.Name.Name,
			file: position.Filename,
			line: position.Line,
		})
	}

	return found
}

// functionsIn returns every function and method declared in the package that
// has a body, in the order the files were given.
func functionsIn(files []*ast.File) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				out = append(out, fn)
			}
		}
	}
	return out
}

// reachingNames is the set of function names in the package that reach the
// connection, closed over the calls between them.
//
// Names, not declarations, because a call site names a method without saying
// whose it is: p.run is an *ast.SelectorExpr whose only certainty is the word
// "run", and knowing which run means type checking the package. Two methods
// that share a name are therefore treated as one, which over-reports and never
// under-reports. That direction is the point: a false positive is answered by
// writing the declaration in a doc comment, and a false negative is a symbol
// that reaches the database with nothing saying so.
func reachingNames(declarations []*ast.FuncDecl) map[string]bool {
	reaching := map[string]bool{}
	for _, fn := range declarations {
		if reachesDirectly(fn) {
			reaching[fn.Name.Name] = true
		}
	}

	// A call edge can point forwards, so one pass over the declarations settles
	// nothing. Repeat until a pass adds no name.
	for grew := true; grew; {
		grew = false
		for _, fn := range declarations {
			if reaching[fn.Name.Name] {
				continue
			}
			for name := range callsIn(fn.Body) {
				if reaching[name] {
					reaching[fn.Name.Name] = true
					grew = true
					break
				}
			}
		}
	}

	return reaching
}

// reaches reports whether this declaration reaches the connection, by asking
// for it, by being handed one, or by calling something that does.
func reaches(fn *ast.FuncDecl, reaching map[string]bool) bool {
	if reachesDirectly(fn) {
		return true
	}
	for name := range callsIn(fn.Body) {
		if reaching[name] {
			return true
		}
	}
	return false
}

// reachesDirectly reports whether this declaration reaches the connection
// without going through another function: it asks the builder for one, or it
// takes one as a parameter.
func reachesDirectly(fn *ast.FuncDecl) bool {
	return opensTheDoor(fn.Body) || takesConnection(fn.Type)
}

// opensTheDoor reports whether body asks the builder for the connection.
func opensTheDoor(body *ast.BlockStmt) bool {
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

// callsIn returns the names of the functions and methods body calls. A method
// call contributes the method name alone, for the reason given on
// reachingNames.
func callsIn(body *ast.BlockStmt) map[string]bool {
	names := map[string]bool{}

	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			names[fn.Name] = true
		case *ast.SelectorExpr:
			names[fn.Sel.Name] = true
		}
		return true
	})

	return names
}

// takesConnection reports whether the signature is handed something that runs
// statements.
//
// A function taking one has already been given what the door returns, so
// looking for the door finds nothing while the function runs whatever it likes.
// The names are matched unqualified as well as qualified, because this package
// is one import away from declaring its own alias for the same interface.
func takesConnection(signature *ast.FuncType) bool {
	if signature.Params == nil {
		return false
	}

	for _, field := range signature.Params.List {
		if isConnectionType(field.Type) {
			return true
		}
	}

	return false
}

// connectionTypes are the interfaces that run statements: the wide one a
// resolver hands out, and the narrow one a builder holds.
var connectionTypes = map[string]bool{
	"ConnectionInterface": true,
	"Connection":          true,
}

func isConnectionType(expr ast.Expr) bool {
	switch typ := expr.(type) {
	case *ast.StarExpr:
		return isConnectionType(typ.X)
	case *ast.Ident:
		return connectionTypes[typ.Name]
	case *ast.SelectorExpr:
		return connectionTypes[typ.Sel.Name]
	}
	return false
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
