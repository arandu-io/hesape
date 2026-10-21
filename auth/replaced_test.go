package auth_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// replaced is every name that has a replacement elsewhere in the collection,
// keyed by its package directory relative to the root of the module, with the
// words its "Deprecated:" paragraph must contain to name that replacement.
//
// A name is a top-level identifier, "Type.Method" for a method or for a method
// of an interface, or "package" for the package clause.
var replaced = map[string]map[string]string{
	"auth/middleware": {
		"package":                      "hesape/http/middleware",
		"Authenticate":                 "RequireAuth",
		"NewAuthenticate":              "RequireAuth",
		"AuthenticateWithBasicAuth":    "RequireToken",
		"NewAuthenticateWithBasicAuth": "RequireToken",
		"Authorize":                    "auth.Authorize",
		"NewAuthorize":                 "Authorize",
		"Gate":                         "Authorize",
		"RedirectIfAuthenticated":      "hesape/http/middleware.RedirectIfAuthenticated",
		"NewRedirectIfAuthenticated":   "hesape/http/middleware.RedirectIfAuthenticated",
		"EnsureEmailIsVerified":        "auth.Subject.Verified",
		"NewEnsureEmailIsVerified":     "EnsureEmailIsVerified",
		"VerificationNoticeURI":        "EnsureEmailIsVerified",
		"Factory":                      "hesape/http/middleware",
		"SubjectResolver":              "RequireAuth",
		"RequirePassword":              "RequireConfirmedPassword",
		"NewRequirePassword":           "RequireConfirmedPassword",
		"PasswordConfirmURI":           "RequireConfirmedPassword",
	},
	"auth/access": {
		"package":               "auth.Policy",
		"Ability":               "auth.Authorize",
		"BeforeCallback":        "auth.Authorize",
		"AfterCallback":         "auth.Authorize",
		"Condition":             "auth.Authorize",
		"Gate":                  "auth.Authorize",
		"NewGate":               "auth.Authorize",
		"Response":              "auth.ErrForbidden",
		"NewResponse":           "auth.ErrForbidden",
		"Allow":                 "auth.ErrForbidden",
		"Deny":                  "auth.ErrForbidden",
		"DenyWithStatus":        "auth.ErrForbidden",
		"DenyAsNotFound":        "auth.ErrForbidden",
		"AuthorizationError":    "auth.ErrForbidden",
		"NewAuthorizationError": "auth.ErrForbidden",
		"DefaultDenialMessage":  "auth.ErrForbidden",
		"HandlesAuthorization":  "Gate",
	},
	"auth/access/events": {
		"package":       "access.Gate",
		"GateEvaluated": "access.Gate",
	},
	"auth/users": {
		"Connection":              "auth/account.Users",
		"DatabaseUserProvider":    "auth/account.Users",
		"NewDatabaseUserProvider": "auth/account.Users",
		"GenericUser":             "auth/account.Users",
		"NewGenericUser":          "auth/account.Users",
	},
	"events": {
		"Dispatcher.Publish": "[Listeners]",
		"Module.Routes":      "foundation.Routable",
	},
	"foundation": {
		"Module.Routes": "[Routable]",
	},
	"queue": {
		"Module.Routes": "foundation.Routable",
	},
	"console/scheduling": {
		"TaskModule.Routes": "foundation.Routable",
	},
}

// TestEveryReplacedNameSaysWhatReplacesIt: a name with a replacement carries a
// "Deprecated:" paragraph naming it, where an editor and the reference read
// it. A listed name that is not declared fails too, so the list cannot rot.
func TestEveryReplacedNameSaysWhatReplacesIt(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for dir, names := range replaced {
		docs := docsOf(t, filepath.Join(root, filepath.FromSlash(dir)))
		for name, replacement := range names {
			doc, ok := docs[name]
			if !ok {
				t.Errorf("%s.%s: listed as replaced, and not declared", dir, name)
				continue
			}
			_, paragraph, found := strings.Cut(doc, "Deprecated:")
			if !found {
				t.Errorf("%s.%s: no \"Deprecated:\" paragraph", dir, name)
				continue
			}
			if !strings.Contains(strings.Join(strings.Fields(paragraph), " "), replacement) {
				t.Errorf("%s.%s: the \"Deprecated:\" paragraph does not name %s", dir, name, replacement)
			}
		}
	}
}

// docsOf reads the doc comment of every top-level name, method and interface
// method declared in the non-test files of dir.
func docsOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if file.Doc != nil {
			docs["package"] += file.Doc.Text()
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				key := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					key = receiverName(d.Recv.List[0].Type) + "." + key
				}
				docs[key] = d.Doc.Text()
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						docs[s.Name.Name] = docOr(s.Doc, d.Doc)
						if iface, ok := s.Type.(*ast.InterfaceType); ok {
							for _, m := range iface.Methods.List {
								for _, n := range m.Names {
									docs[s.Name.Name+"."+n.Name] = m.Doc.Text()
								}
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							docs[n.Name] = docOr(s.Doc, d.Doc)
						}
					}
				}
			}
		}
	}
	return docs
}

// docOr is the spec's own doc, or the declaration's when the spec has none.
func docOr(spec, decl *ast.CommentGroup) string {
	if spec != nil {
		return spec.Text()
	}
	return decl.Text()
}

// receiverName is the type a method is declared on, without its pointer.
func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
