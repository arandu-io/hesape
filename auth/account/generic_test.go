package account_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNothingButPolicyOfIsGeneric: Go compiles every method of an
// instantiated generic type into every package that names the
// instantiation, so the rules here are written over the Account interface and
// compiled once. A type parameter added to Users, Factors, Config or the
// contract suite would put all of their code back into each package of every
// application that holds them -- measured at about twice the size of a
// consumer package. PolicyOf is the one exception, and its body is a type
// assertion.
func TestNothingButPolicyOfIsGeneric(t *testing.T) {
	for _, dir := range []string{".", "accounttest"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Type.TypeParams != nil && !(dir == "." && d.Recv == nil && d.Name.Name == "PolicyOf") {
						t.Errorf("%s: %s has type parameters", path, d.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if ts, ok := spec.(*ast.TypeSpec); ok && ts.TypeParams != nil {
							t.Errorf("%s: type %s has type parameters", path, ts.Name.Name)
						}
					}
				}
			}
		}
	}
}
