package session_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/session/middleware"
)

// TestTheDriverRefusesAnUnsafeConfigurationBeforeItBuildsASession proves the
// refusal is on the path every request takes, not only on the caller who thinks
// to ask.
//
// Driver is that path: the middleware calls it once per request, before there is
// a session id and long before there is a cookie. A check that only a boot
// sequence ran would be a check that a manager built anywhere else skips.
func TestTheDriverRefusesAnUnsafeConfigurationBeforeItBuildsASession(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "array", Cookie: "arandu_session"}, nil)

	store, err := manager.Driver("")
	if err == nil {
		t.Fatal("the manager built a session over a configuration that leaves the id readable by script")
	}
	if store != nil {
		t.Error("the manager returned a session alongside the refusal, and a caller that checks the store instead of the error proceeds")
	}

	var refusal *session.ConfigError
	if !errors.As(err, &refusal) || refusal.Field != "HTTPOnly" {
		t.Errorf("Driver refused with %v, want a *ConfigError naming HTTPOnly", err)
	}
}

// TestNoSessionCookieIsWrittenWhenTheConfigurationIsRefused runs the real
// middleware over a refused configuration and reads the response.
//
// The refusal is only worth anything if it lands before the cookie. This asserts
// the outcome rather than the call: no Set-Cookie of any name reaches the
// browser, and the handler does not run, so nothing downstream believes it has a
// session.
func TestNoSessionCookieIsWrittenWhenTheConfigurationIsRefused(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "array", Cookie: "arandu_session"}, nil)

	reached := false
	handler := middleware.NewStartSession(manager, nil).Handle(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.Write([]byte("hello"))
		}),
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "text/html")
	handler.ServeHTTP(w, r)

	if reached {
		t.Error("the handler ran over a refused session configuration")
	}
	if cookies := w.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("the response carries %d cookies over a refused configuration: %v", len(cookies), w.Header())
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d: a request that cannot have a session is not a request that was served",
			w.Code, http.StatusInternalServerError)
	}
}

// TestASafeConfigurationStillServesASession is the other half: a check that
// refused everything would pass every assertion above and ship nothing.
func TestASafeConfigurationStillServesASession(t *testing.T) {
	manager := session.NewSessionManager(session.Config{
		Driver: "array", Cookie: "arandu_session",
		HTTPOnly: true, SameSite: http.SameSiteLaxMode,
	}, nil)

	handler := middleware.NewStartSession(manager, nil).Handle(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("hello"))
		}),
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "text/html")
	handler.ServeHTTP(w, r)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("a safe configuration wrote no session cookie: %v", w.Header())
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Errorf("the cookie carries HttpOnly=%v SameSite=%v, and the configuration asked for true and Lax",
			cookies[0].HttpOnly, cookies[0].SameSite)
	}
}

// docLink matches a doc link to an exported name, optionally with the member it
// names: [Config], [Config.Check]. It requires an initial capital so that
// [2]int, []byte and [key] are not read as links, which is also how a reader
// tells them apart.
var docLink = regexp.MustCompile(`\[([A-Z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)?)\]`)

// elsewhere maps a doc link this package writes unqualified to the sibling
// directory that declares it.
//
// The entries are not exemptions: the test parses each named directory and fails
// when the symbol is not there either. What they record is that the link points
// at code, written short, rather than at nothing.
var elsewhere = map[string]string{
	"StartSession": "middleware",
}

// TestNoCommentInThisPackageNamesASymbolThatDoesNotExist reads the published
// comments and fails when a doc link names something no package declares.
//
// A comment that names a mitigation is read as evidence the mitigation is there,
// and this package shipped one that was not: the cookie attributes pointed at
// Config.Check for several releases while nothing of that name existed, so the
// unsafe combination the comment said was refused was in fact accepted. The
// comment is what pkg.go.dev publishes, and the reader most likely to believe it
// is the one who never opens the file.
//
// So a link either resolves here, or names the sibling that declares it and is
// checked there.
func TestNoCommentInThisPackageNamesASymbolThatDoesNotExist(t *testing.T) {
	declared := declaredSymbols(t, ".")

	fset := token.NewFileSet()
	for _, path := range publishedSources(t, ".") {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		for _, group := range file.Comments {
			for _, comment := range group.List {
				for _, match := range docLink.FindAllStringSubmatch(comment.Text, -1) {
					name := match[1]
					if declared[name] {
						continue
					}

					dir, ok := elsewhere[strings.SplitN(name, ".", 2)[0]]
					if !ok {
						t.Errorf("%s:%d names [%s], and nothing of that name is declared here. Either "+
							"the symbol is missing and the comment promises what the code does not do, or "+
							"it lives elsewhere and belongs in the elsewhere table with the directory that "+
							"has it.",
							path, fset.Position(comment.Slash).Line, name)
						continue
					}
					if !declaredSymbols(t, dir)[name] {
						t.Errorf("%s:%d names [%s], which the elsewhere table sends to %s, and %s does not "+
							"declare it either",
							path, fset.Position(comment.Slash).Line, name, dir, dir)
					}
				}
			}
		}
	}
}

// TestThePhantomSymbolCheckCatchesOne runs the resolution against a planted
// comment and fails when it reports nothing.
//
// The published sources satisfy the check, and a check that read nothing would
// satisfy it too. The planted source is the difference: it is the shape the
// cookie attributes had -- a doc link to a method of a type the package
// declares, where the type has no such method.
func TestThePhantomSymbolCheckCatchesOne(t *testing.T) {
	const planted = `package session

// Config is a session configuration.
//
// [Config.Verify] is what refuses an unsafe combination.
type Config struct {
	// Cookie is the name the session id travels under.
	Cookie string
}

// Check refuses an unsafe combination.
func (c Config) Check() error { return nil }
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "planted.go", planted, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing the planted source: %v", err)
	}

	declared := map[string]bool{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 {
				declared[receiverName(d.Recv.List[0].Type)+"."+d.Name.Name] = true
				continue
			}
			declared[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if s, ok := spec.(*ast.TypeSpec); ok {
					declared[s.Name.Name] = true
					for _, member := range typeMembers(s.Type) {
						declared[s.Name.Name+"."+member] = true
					}
				}
			}
		}
	}

	var unresolved []string
	for _, group := range file.Comments {
		for _, comment := range group.List {
			for _, match := range docLink.FindAllStringSubmatch(comment.Text, -1) {
				if !declared[match[1]] {
					unresolved = append(unresolved, match[1])
				}
			}
		}
	}

	if len(unresolved) != 1 || unresolved[0] != "Config.Verify" {
		t.Fatalf("the resolution reported %v on a planted phantom, want [Config.Verify]; it also "+
			"has to resolve Config, Config.Cookie and Config.Check, and reporting more or fewer "+
			"means it reads something other than what a reader follows", unresolved)
	}
}

// declaredSymbols returns every name a reader can follow a doc link to in dir:
// the top-level declarations, and the members of the types declared there.
func declaredSymbols(t *testing.T, dir string) map[string]bool {
	t.Helper()

	declared := map[string]bool{}
	fset := token.NewFileSet()

	for _, path := range publishedSources(t, dir) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && len(d.Recv.List) == 1 {
					declared[receiverName(d.Recv.List[0].Type)+"."+d.Name.Name] = true
					continue
				}
				declared[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
						for _, member := range typeMembers(s.Type) {
							declared[s.Name.Name+"."+member] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							declared[name.Name] = true
						}
					}
				}
			}
		}
	}

	return declared
}

// typeMembers returns the field or method names a struct or interface declares.
func typeMembers(expr ast.Expr) []string {
	var fields *ast.FieldList
	switch t := expr.(type) {
	case *ast.StructType:
		fields = t.Fields
	case *ast.InterfaceType:
		fields = t.Methods
	default:
		return nil
	}
	if fields == nil {
		return nil
	}

	var names []string
	for _, field := range fields.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

// receiverName returns the type name a method is declared on, past the pointer
// and past the type parameters.
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// publishedSources returns the .go files in dir that ship to a reader of the
// package documentation: everything but the tests.
//
// It fails rather than returning nothing, because a check that reads no files
// reports no findings and passes.
func publishedSources(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}

	if len(paths) == 0 {
		t.Fatalf("no published sources under %s, so this test read nothing and would pass on anything", dir)
	}
	return paths
}
