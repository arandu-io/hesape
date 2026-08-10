package view_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// retired are names the registry decided against or dissolved. Each one was
// true once, which is exactly why they survive in comments.
//
//   - sqlc never entered: the SQL is written by hand in the generator's
//     templates (ADR 0024). Two doc comments here promised queries generated
//     from .sql files, and both are published on pkg.go.dev, where a reader has
//     no way to check.
//   - porang was a repository, dissolved into this one (ADR 0021).
//   - marandu was going to be the inspector UI; the console shipped as core.
//   - templ was the template engine, replaced by kyse (ADR 0020).
var retired = []string{"sqlc", "porang", "marandu", "templ"}

// retiredWords is the same list, matched as whole words.
//
// A substring match reads "sqlc" inside MySqlConnector.php, which the database
// package names in the list of Illuminate sources it answers to, and "templ"
// inside every "template" -- two reports about comments that make no claim
// about either. The trailing space that used to stand in for a word boundary
// held only while the name happened to be followed by one, and a name at the
// end of a sentence is not.
var retiredWords = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(retired))
	for i, name := range retired {
		out[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	}
	return out
}()

// TestNoDocCommentAssertsSomethingRetired reads the comments this module
// publishes and refuses a retired name that is not marked as history.
//
// The rule is not "never mention it" -- explaining why a thing is gone is worth
// writing. The rule is that the same comment has to say where that was decided,
// so a name in the present tense fails and a name next to its ADR passes. That
// is the whole difference between a record and a leftover.
//
// It reads only non-test files: pkg.go.dev publishes those, and a name in a test
// is not a claim to anybody outside this repository.
func TestNoDocCommentAssertsSomethingRetired(t *testing.T) {
	root := ".."

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		for _, group := range file.Comments {
			text := group.Text()
			for i, word := range retiredWords {
				if !word.MatchString(text) {
					continue
				}
				if strings.Contains(text, "ADR") {
					continue
				}
				t.Errorf("%s: a comment names %q without saying where it was retired:\n\t%s",
					path, retired[i], firstLineWith(text, word))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func firstLineWith(text string, word *regexp.Regexp) string {
	for _, line := range strings.Split(text, "\n") {
		if word.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
