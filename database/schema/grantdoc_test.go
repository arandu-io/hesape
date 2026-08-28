package schema_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// grantName matches the exported type an authorization credential is spelled
// with, and authQualified matches any name qualified with the package that
// declares it. Both are word anchored: Grammar is not Grant, and oauth. is not
// auth.
var (
	grantName     = regexp.MustCompile(`\bGrant\b`)
	authQualified = regexp.MustCompile(`\bauth\.`)
)

// componentDirs are the directories whose published comments this test reads:
// the schema component and the grammars under it.
var componentDirs = []string{".", "grammars"}

// TestNoCommentPromisesAnAuthorizationCheckThisPackageCannotMake reads the
// published sources of this component and fails if a comment describes an
// authorization check that the code here has no way to perform.
//
// TestNoSchemaMethodTakesAGrant proves the signatures by compiling, and the
// compiler reads no comments. So the doc comment on Builder was free to say for
// several releases that every method took a credential and checked it against a
// named action -- while the package imported nothing that could hold one, and
// the action it named existed in that sentence and nowhere else. That text is
// what pkg.go.dev publishes, so the reader most likely to believe it is the one
// who never opens the file.
//
// The two assertions together are the property. A comment may promise an
// authorization check only if the code could make one, so:
//
//   - no published file imports the package that declares the credential, which
//     is what makes the check impossible here rather than merely absent, and
//   - given that, no comment may name the credential's type or qualify a name
//     with its package, because both are references to code this component
//     cannot reach.
//
// Describing the absence in prose is unaffected: "no method here asks for an
// authorization credential" names no type and passes. Naming the type is what
// this refuses, because the false claim and the true one are one word apart and
// only the type name tells them apart mechanically.
//
// If DDL ever does carry a credential, the first assertion fails, and that is
// the intended way to reach this test: the comments and this file are then both
// rewritten together, deliberately.
func TestNoCommentPromisesAnAuthorizationCheckThisPackageCannotMake(t *testing.T) {
	for _, dir := range componentDirs {
		t.Run(dir, func(t *testing.T) {
			fset := token.NewFileSet()

			for _, path := range publishedSources(t, dir) {
				file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if err != nil {
					t.Fatalf("parsing %s: %v", path, err)
				}

				for _, spec := range file.Imports {
					imported, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						t.Fatalf("%s: import path %s is not a quoted string", path, spec.Path.Value)
					}
					if imported == "auth" || strings.HasSuffix(imported, "/auth") {
						t.Errorf("%s imports %q, so this component can now hold an authorization "+
							"credential and the comments about not having one are stale. Rewrite them "+
							"and rewrite this test to say what is true instead.",
							path, imported)
					}
				}

				for _, group := range file.Comments {
					for _, comment := range group.List {
						line := fset.Position(comment.Slash).Line
						text := strings.TrimSpace(comment.Text)

						if grantName.MatchString(comment.Text) {
							t.Errorf("%s:%d names the Grant type in a comment, and no signature in "+
								"this component takes one: %s\nSay what the code does. To describe the "+
								"absence, write it without the type name.",
								path, line, text)
						}
						if authQualified.MatchString(comment.Text) {
							t.Errorf("%s:%d qualifies a name with the auth package, which this "+
								"component does not import: %s\nA reader follows a qualified name to "+
								"code, and there is none to follow from here.",
								path, line, text)
						}
					}
				}
			}
		})
	}
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
