package arandutest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// moved is what every package and exported symbol of this tree says: the kit
// lives on in the framework, and an application finds that out from its
// editor and from staticcheck rather than from a build that stops compiling.
const moved = "github.com/arandu-io/framework/test"

// TestEveryPackageAndSymbolSaysWhereTheKitMoved walks this package and every
// package under it, and refuses a package comment or an exported declaration
// whose doc has no Deprecated paragraph naming where the kit went.
func TestEveryPackageAndSymbolSaysWhereTheKitMoved(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		says := func(doc *ast.CommentGroup) bool {
			if doc == nil {
				return false
			}
			text := doc.Text()
			i := strings.Index(text, "Deprecated:")
			return i >= 0 && strings.Contains(text[i:], moved)
		}
		if f.Doc != nil && !says(f.Doc) {
			t.Errorf("%s: the package comment does not say the kit moved to %s", path, moved)
		}
		for _, decl := range f.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() || !exportedReceiver(decl) {
					continue
				}
				checked++
				if !says(decl.Doc) {
					t.Errorf("%s: %s is not deprecated in favour of %s", fset.Position(decl.Pos()), decl.Name.Name, moved)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					var doc *ast.CommentGroup
					var names []*ast.Ident
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						doc, names = spec.Doc, []*ast.Ident{spec.Name}
					case *ast.ValueSpec:
						doc, names = spec.Doc, spec.Names
					default:
						continue
					}
					if !decl.Lparen.IsValid() {
						doc = decl.Doc
					}
					for _, name := range names {
						if !name.IsExported() {
							continue
						}
						checked++
						if !says(doc) {
							t.Errorf("%s: %s is not deprecated in favour of %s", fset.Position(name.Pos()), name.Name, moved)
						}
						break
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("checked %d exported declarations: the walk is not reaching the kit", checked)
	}
}

// exportedReceiver reports whether a function is free or a method of an
// exported type, which is what a caller outside the package can name.
func exportedReceiver(fn *ast.FuncDecl) bool {
	if fn.Recv == nil {
		return true
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	switch generic := typ.(type) {
	case *ast.IndexExpr:
		typ = generic.X
	case *ast.IndexListExpr:
		typ = generic.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && id.IsExported()
}
