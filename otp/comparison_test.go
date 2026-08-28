package otp_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// codeCarryingNames are the identifiers this package binds a one-time code to.
// Comparing either of them with == or != is the regression this file exists to
// catch, so the list has to stay in step with the source: renaming one of them
// without renaming it here would leave this test looking at nothing.
var codeCarryingNames = map[string]bool{
	"code":      true,
	"candidate": true,
}

// nonConstantTimeComparisons compare two byte strings and stop at the first
// byte that differs. None of them has another use in this package, so they are
// refused outright rather than inspected.
var nonConstantTimeComparisons = map[string]bool{
	"bytes.Equal":       true,
	"bytes.Compare":     true,
	"strings.Compare":   true,
	"strings.EqualFold": true,
}

// TestTheCodeIsComparedInConstantTime reads this package's published sources and
// fails if the decision "does this code match" is made by anything that returns
// early.
//
// The property cannot be proved by running the code: a comparison that leaks
// timing returns the same answers as one that does not, and measuring the
// difference from a test is a benchmark with a flake in it. So it is proved
// structurally, and the limit of that is worth naming -- this catches the
// comparison being written the ordinary way, which is how it would actually be
// rewritten by someone tidying up, and not every conceivable leaking
// comparison.
//
// What is asserted:
//
//   - some function here calls hmac.Equal, so a constant-time comparison exists
//     at all, and
//   - no == or != in the package names an identifier that holds a code, and
//   - no early-returning comparison helper is called.
//
// The length check in Verify is untouched by this: it compares len(code), not
// code, and the length of a code is the size of the box on the screen.
func TestTheCodeIsComparedInConstantTime(t *testing.T) {
	fset := token.NewFileSet()

	source := func(node ast.Node) string {
		var b strings.Builder
		if err := printer.Fprint(&b, fset, node); err != nil {
			t.Fatalf("printing a node: %v", err)
		}
		return b.String()
	}

	constantTimeCalls := 0
	comparisons := 0

	for _, path := range publishedSources(t) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				called := source(node.Fun)
				if called == "hmac.Equal" || called == "subtle.ConstantTimeCompare" {
					constantTimeCalls++
				}
				if nonConstantTimeComparisons[called] {
					t.Errorf("%s calls %s, which stops at the first byte that differs, and a "+
						"one-time code is compared in this package", path, called)
				}
			case *ast.BinaryExpr:
				if node.Op != token.EQL && node.Op != token.NEQ {
					return true
				}
				comparisons++
				for _, operand := range []ast.Expr{node.X, node.Y} {
					if text := source(operand); codeCarryingNames[text] {
						t.Errorf("%s compares %s with %s. That returns at the first character that "+
							"differs, which tells whoever is guessing how much of the guess was "+
							"right. Use hmac.Equal.", path, text, node.Op)
					}
				}
			}
			return true
		})
	}

	if constantTimeCalls == 0 {
		t.Error("no constant-time comparison is called anywhere in the package")
	}
	if comparisons == 0 {
		t.Error("no equality comparison was found at all, so this scan is not reading the sources")
	}
}

// publishedSources returns the .go files of this package that ship to a reader
// of the documentation: everything but the tests.
//
// It fails rather than returning nothing, because a scan that reads no files
// reports no findings and passes.
func publishedSources(t *testing.T) []string {
	t.Helper()

	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing the sources: %v", err)
	}

	var published []string
	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			published = append(published, path)
		}
	}
	if len(published) == 0 {
		t.Fatal("no published source was found next to this test")
	}
	return published
}
