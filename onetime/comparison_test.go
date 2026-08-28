package onetime_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// secretBearingNames are the expressions this package binds a code or the
// digest of one to. Comparing any of them with == or != is the regression this
// file exists to catch, so the list has to stay in step with the source:
// renaming one of them without renaming it here would leave the scan looking at
// nothing.
var secretBearingNames = map[string]bool{
	"code":               true,
	"stored":             true,
	"c.key":              true,
	"outstanding.Digest": true,
	"previous.Digest":    true,
	"fresh.Digest":       true,
}

// earlyReturningComparisons compare two strings of bytes and stop at the first
// one that differs. None of them has another use in this package, so they are
// refused outright rather than inspected.
var earlyReturningComparisons = map[string]bool{
	"bytes.Equal":       true,
	"bytes.Compare":     true,
	"strings.Compare":   true,
	"strings.EqualFold": true,
}

// TestTheCodeIsComparedInConstantTime reads this package's published sources and
// fails if the decision "is this the right code" is made by anything that
// returns as soon as it knows the answer is no.
//
// It cannot be proved by running the code: a comparison that leaks timing
// returns exactly the answers one that does not returns, and measuring the
// difference from a test is a benchmark with a flake in it. So it is proved by
// reading, and the limit of that is worth naming -- this catches the comparison
// being rewritten the ordinary way, which is how it would actually be rewritten
// by somebody tidying up, and not every conceivable leaking comparison.
//
// The length check in Consume is untouched by it: that compares len(code), and
// how long a code is is the size of the box on the screen.
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
				if earlyReturningComparisons[called] {
					t.Errorf("%s calls %s, which stops at the first byte that differs, and a one-time "+
						"code is compared in this package", path, called)
				}
			case *ast.BinaryExpr:
				if node.Op != token.EQL && node.Op != token.NEQ {
					return true
				}
				comparisons++
				for _, operand := range []ast.Expr{node.X, node.Y} {
					if text := source(operand); secretBearingNames[text] {
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

// publishedSources returns the .go files of this package that a reader of the
// documentation sees: everything but the tests.
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
