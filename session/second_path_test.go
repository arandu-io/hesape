package session_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// module is the import path the directories below are relative to.
const module = "github.com/arandu-io/hesape"

// secondPath is every exported name of the second session path: the Store and
// what exists only on top of it. Each must carry a "Deprecated:" paragraph that
// names RecordStore, the one path.
//
// The keys are directories relative to the root of the module. A name is a
// top-level identifier, "Type.Method" for a method, or "package" for the
// package clause. A method of a listed type needs no entry of its own: the
// type's deprecation covers it.
var secondPath = map[string][]string{
	"session": {
		"TokenKey", "OldInputKey", "PreviousURLKey", "PreviousRouteKey", "PasswordConfirmedKey",
		"ErrNoPreviousURL", "SessionHandler", "ExistenceAwareInterface", "RequestAware",
		"Store", "NewStore",
		"Encrypter", "EncryptedStore", "NewEncryptedStore",
		"ArraySessionHandler", "NewArraySessionHandler",
		"NullSessionHandler", "NewNullSessionHandler",
		"FileSessionHandler", "NewFileSessionHandler",
		"CookieSessionHandler", "NewCookieSessionHandler",
		"CacheBasedSessionHandler", "NewCacheBasedSessionHandler",
		"DatabaseSessionHandler", "NewDatabaseSessionHandler",
		"CookieJar", "Cache", "Connection", "ErrBadTableName",
		"Config", "DefaultLifetime", "DefaultBlockLockSeconds", "DefaultBlockWaitSeconds",
		"ErrNoDriver", "HandlerCreator", "SessionManager", "NewSessionManager",
		"ErrUnsafeConfig", "ConfigError",
		"Table", "CreateSessionsTable", "Migrations",
	},
	"session/middleware": {
		"package",
		"WithSession", "Session", "LockFactory", "StartSession", "NewStartSession",
		"Guard", "AuthenticateSession", "NewAuthenticateSession",
	},
	"session/console": {
		"package",
		"MigrationStub", "TableName", "SessionTableCommand", "NewSessionTableCommand",
	},
	"auth": {
		"SessionGuard", "NewSessionGuard",
		"ErrCookieJarNotSet", "ErrHasherNotSet", "ErrPasswordMismatch", "ErrInvalidBasicCredentials",
		"Attempting", "Authenticated", "Validated", "Login", "Logout",
		"CurrentDeviceLogout", "OtherDeviceLogout", "Failed",
		"Session", "CookieJar", "Dispatcher", "Recaller", "NewRecaller",
		"ManagerConfig", "AuthManager", "NewAuthManager",
	},
	"auth/middleware": {
		"PasswordConfirmURI", "RequirePassword", "NewRequirePassword",
	},
	"http": {
		"Request.HasSession", "Request.Session", "Request.SetSession", "Request.GetSession",
		"RedirectResponse.With", "RedirectResponse.WithInput", "RedirectResponse.OnlyInput",
		"RedirectResponse.ExceptInput", "RedirectResponse.WithErrors",
		"RedirectResponse.GetSession", "RedirectResponse.SetSession",
	},
	"testing": {
		"TestResponse.AssertSessionHas", "TestResponse.AssertSessionHasAll",
		"TestResponse.AssertSessionMissing", "TestResponse.DumpSession", "TestResponse.DDSession",
	},
}

// wholly are the files that hold nothing but the second path, so an exported
// name declared in one of them belongs on the list whatever its signature says.
var wholly = []string{
	"session/store.go", "session/encrypted.go", "session/handlers.go",
	"session/manager.go", "session/config.go", "session/migrations.go",
	"session/middleware", "session/console",
	"auth/session_guard.go", "auth/manager.go", "auth/recaller.go",
	"auth/middleware/require_password.go",
}

// TestTheSecondSessionPathIsDeprecated: the session is RecordStore, and every
// name of the Store path says so where an editor and pkg.go.dev read it. A name
// on the list that is not found fails too, so the list cannot rot into naming
// something that is gone.
func TestTheSecondSessionPathIsDeprecated(t *testing.T) {
	tree := parseModule(t)

	for dir, names := range secondPath {
		pkg := tree[dir]
		if pkg == nil {
			t.Errorf("%s: no such package in the module", dir)
			continue
		}
		for _, name := range names {
			doc, ok := pkg.docOf(name)
			if !ok {
				t.Errorf("%s.%s: listed as the second session path, and not declared", dir, name)
				continue
			}
			if !deprecatedFor(doc) {
				t.Errorf("%s.%s: no \"Deprecated:\" paragraph naming RecordStore, the one session path", dir, name)
			}
		}
	}
}

// TestNothingNewJoinsTheSecondSessionPathUndeprecated: an exported name whose
// signature takes or returns a listed one, or one declared in a file of that
// path, is on the path too, and has to be on the list -- and so deprecated --
// rather than arriving as a new way into the Store.
func TestNothingNewJoinsTheSecondSessionPathUndeprecated(t *testing.T) {
	tree := parseModule(t)

	listed := map[ref]bool{}
	for dir, names := range secondPath {
		for _, name := range names {
			listed[ref{dir, name}] = true
		}
	}

	var found []string
	for dir, pkg := range tree {
		for path, file := range pkg.files {
			whole := isWholly(dir, path)
			imports := importsOf(file)
			for _, decl := range exportedDecls(file) {
				key := ref{dir, decl.name}
				if listed[key] || (decl.receiver != "" && listed[ref{dir, decl.receiver}]) {
					continue
				}
				if whole {
					found = append(found, dir+"."+decl.name+" is declared in "+path+", which holds only the second session path")
					continue
				}
				if name, ok := namesListed(decl.signature, dir, imports, listed); ok {
					found = append(found, dir+"."+decl.name+" names "+name+" in its signature")
				}
			}
		}
	}

	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s: deprecate it and add it to secondPath, or keep the second path out of it", f)
	}
}

// ref is one name in one package directory.
type ref struct{ dir, name string }

// pkg is the parsed non-test files of one package directory, keyed by path.
type pkg struct{ files map[string]*ast.File }

// parseModule parses every non-test file of the root module, keyed by the
// directory of its package relative to the root. A directory holding its own
// go.mod is another module, and testdata holds what is invalid on purpose.
func parseModule(t *testing.T) map[string]*pkg {
	t.Helper()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	tree := map[string]*pkg{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if path != root && (base == "testdata" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".kyse.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if tree[dir] == nil {
			tree[dir] = &pkg{files: map[string]*ast.File{}}
		}
		tree[dir].files[rel] = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// docOf returns the doc comment of a name in the package: "package" for the
// package clause, "Type.Method" for a method, a top-level name otherwise.
func (p *pkg) docOf(name string) (*ast.CommentGroup, bool) {
	if name == "package" {
		for _, file := range p.files {
			if file.Doc != nil {
				return file.Doc, true
			}
		}
		return nil, false
	}
	receiver, method, isMethod := strings.Cut(name, ".")
	for _, file := range p.files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if isMethod && d.Recv != nil && d.Name.Name == method && methodReceiver(d) == receiver {
					return d.Doc, true
				}
				if !isMethod && d.Recv == nil && d.Name.Name == name {
					return d.Doc, true
				}
			case *ast.GenDecl:
				if isMethod {
					continue
				}
				for _, spec := range d.Specs {
					doc := specDoc(d, spec)
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.Name == name {
							return doc, true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.Name == name {
								return doc, true
							}
						}
					}
				}
			}
		}
	}
	return nil, false
}

// specDoc is the doc comment of one spec, which for a declaration with no
// parentheses sits on the declaration rather than the spec.
func specDoc(d *ast.GenDecl, spec ast.Spec) *ast.CommentGroup {
	var doc *ast.CommentGroup
	switch s := spec.(type) {
	case *ast.TypeSpec:
		doc = s.Doc
	case *ast.ValueSpec:
		doc = s.Doc
	}
	if doc == nil && !d.Lparen.IsValid() {
		doc = d.Doc
	}
	return doc
}

// deprecatedFor reports whether the comment has a paragraph that begins
// "Deprecated: ", the form go/doc and the editors recognise, and names
// RecordStore in it.
func deprecatedFor(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, paragraph := range strings.Split(doc.Text(), "\n\n") {
		if strings.HasPrefix(paragraph, "Deprecated: ") && strings.Contains(paragraph, "RecordStore") {
			return true
		}
	}
	return false
}

// methodReceiver is the base type name of a method's receiver.
func methodReceiver(d *ast.FuncDecl) string {
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.IndexExpr:
		expr = e.X
	case *ast.IndexListExpr:
		expr = e.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// exported is one exported declaration: its name ("Type.Method" for a
// method), the receiver's type when it is a method, and the part of it a
// caller sees.
type exported struct {
	name, receiver string
	signature      []ast.Node
}

// exportedDecls lists the exported declarations of a file. A method counts
// only when its receiver's type is exported, and a struct contributes only its
// exported and embedded fields to its signature.
func exportedDecls(file *ast.File) []exported {
	var out []exported
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				out = append(out, exported{name: d.Name.Name, signature: []ast.Node{d.Type}})
				continue
			}
			receiver := methodReceiver(d)
			if !ast.IsExported(receiver) {
				continue
			}
			out = append(out, exported{name: receiver + "." + d.Name.Name, receiver: receiver, signature: []ast.Node{d.Type}})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, exported{name: s.Name.Name, signature: typeSurface(s)})
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if !n.IsExported() {
							continue
						}
						var signature []ast.Node
						if s.Type != nil {
							signature = append(signature, s.Type)
						}
						for _, v := range s.Values {
							signature = append(signature, v)
						}
						out = append(out, exported{name: n.Name, signature: signature})
					}
				}
			}
		}
	}
	return out
}

// typeSurface is what a type declaration shows a caller: the whole type,
// except that a struct shows only its exported and embedded fields.
func typeSurface(s *ast.TypeSpec) []ast.Node {
	surface := []ast.Node{}
	if s.TypeParams != nil {
		surface = append(surface, s.TypeParams)
	}
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		return append(surface, s.Type)
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			surface = append(surface, field.Type)
			continue
		}
		for _, n := range field.Names {
			if n.IsExported() {
				surface = append(surface, field.Type)
				break
			}
		}
	}
	return surface
}

// importsOf maps the name each import is used under to its directory relative
// to the module, for the imports that are in the module.
func importsOf(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.HasPrefix(path, module+"/") {
			continue
		}
		dir := strings.TrimPrefix(path, module+"/")
		name := dir[strings.LastIndex(dir, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		out[name] = dir
	}
	return out
}

// namesListed reports the first listed name the nodes refer to: a bare
// identifier is one of the package's own, and a selector on an import is one
// of the imported package's.
//
// The names a declaration gives -- a parameter, a field, an interface method,
// the key of a composite literal, the selected half of x.y -- are not
// references, and are skipped: an interface method called Login is not the
// Login event.
func namesListed(nodes []ast.Node, dir string, imports map[string]string, listed map[ref]bool) (string, bool) {
	var hit string
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		if hit != "" || n == nil {
			return false
		}
		switch e := n.(type) {
		case *ast.Field:
			ast.Inspect(e.Type, visit)
			return false
		case *ast.KeyValueExpr:
			if _, ok := e.Key.(*ast.Ident); !ok {
				ast.Inspect(e.Key, visit)
			}
			ast.Inspect(e.Value, visit)
			return false
		case *ast.SelectorExpr:
			if x, ok := e.X.(*ast.Ident); ok {
				if imported, ok := imports[x.Name]; ok {
					if listed[ref{imported, e.Sel.Name}] {
						hit = imported + "." + e.Sel.Name
					}
					return false
				}
			}
			ast.Inspect(e.X, visit)
			return false
		case *ast.Ident:
			if listed[ref{dir, e.Name}] {
				hit = dir + "." + e.Name
			}
		}
		return true
	}
	for _, node := range nodes {
		ast.Inspect(node, visit)
		if hit != "" {
			return hit, true
		}
	}
	return "", false
}

// isWholly reports whether a file is one of those that hold only the second
// path, by its own name or by its package's.
func isWholly(dir, path string) bool {
	for _, w := range wholly {
		if w == path || w == dir {
			return true
		}
	}
	return false
}
