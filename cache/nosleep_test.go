package cache_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoSuiteSleepsThroughAnExpiry: the stores, the rate limiter, the lock and
// the session record table read the time through support.Now, so their tests
// and the contract suites wait out a ttl by travelling with the fake clock.
// A sleep put back is a test that takes as long as its ttl, and fails on a
// loaded machine before the store does -- which is what every one of them
// was until they moved to the clock.
//
// It reads every file of this package's tree that a test runs -- the tests,
// and cachetest, whose files are all test code -- and the session record
// table's suite in database/conformance. A store whose entries expire on a
// server's clock waits through cachetest.ServerClock, in its own adapter.
func TestNoSuiteSleepsThroughAnExpiry(t *testing.T) {
	var files []string
	for _, root := range []string{".", filepath.Join("..", "database", "conformance")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			testCode := strings.HasSuffix(path, "_test.go") ||
				strings.HasSuffix(path, ".go") && (filepath.Base(filepath.Dir(path)) == "cachetest" || filepath.Base(filepath.Dir(path)) == "conformance")
			if !d.IsDir() && testCode && filepath.Base(path) != "nosleep_test.go" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) < 10 {
		t.Fatalf("read only %d files: the walk is not reaching the suites", len(files))
	}
	for _, path := range files {
		for _, at := range timeWaits(t, path) {
			t.Errorf("%s waits on the wall clock; travel with support.Travel instead", at)
		}
	}
}

// timeWaits answers where a file calls time.Sleep, under whatever name it
// imports the time package. A select that waits a bounded time for something
// that must not happen -- a refresh a read-only Grant may not set off -- is
// not an expiry, and stays.
func timeWaits(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	name := ""
	for _, spec := range file.Imports {
		if p, _ := strconv.Unquote(spec.Path.Value); p == "time" {
			name = "time"
			if spec.Name != nil {
				name = spec.Name.Name
			}
		}
	}
	if name == "" {
		return nil
	}
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == name && sel.Sel.Name == "Sleep" {
			found = append(found, fset.Position(call.Pos()).String())
		}
		return true
	})
	return found
}
