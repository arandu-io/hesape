package query_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// TestTheSearchFoldsAccentsInTheSQLOfEachEngine reads the statement the search
// of a listing compiles to on each grammar: the column and the binding go
// through the same fold, PostgreSQL's translate() and SQLite's replace(), and
// MySQL keeps lower() because its collation already ignores the marks.
func TestTheSearchFoldsAccentsInTheSQLOfEachEngine(t *testing.T) {
	for name, c := range map[string]struct {
		grammar query.Grammar
		folds   string
		absent  string
	}{
		"postgres": {grammars.NewPostgresGrammar(), "translate(lower(", "replace("},
		"sqlite":   {grammars.NewSQLiteGrammar(), "replace(", "translate("},
		"mysql":    {grammars.NewMySQLGrammar(), "", "translate("},
	} {
		t.Run(name, func(t *testing.T) {
			builder := query.NewBuilder(nil, c.grammar, nil).From("notes").ApplyListing(noteState(t, "q=c%C3%A2mera"))
			sql := builder.ToSQL()
			if c.folds != "" && strings.Count(sql, c.folds) < 4 {
				t.Errorf("the title and the notes' body and the two bindings are folded; %q appears %d times:\n%s", c.folds, strings.Count(sql, c.folds), sql)
			}
			if c.absent != "" && strings.Contains(sql, c.absent) {
				t.Errorf("%s is not this engine's fold:\n%s", c.absent, sql)
			}
			if name == "mysql" && !strings.Contains(sql, `lower(`) {
				t.Errorf("mysql lost the case fold:\n%s", sql)
			}
			if strings.Contains(sql, "âmera") {
				t.Errorf("the search text is a binding and never part of the statement:\n%s", sql)
			}
		})
	}
}

// TestTheFoldMapsEveryAccentedLetterToItsPlainOne holds the table in step with
// itself: the letter at a position of the first string folds to the letter at
// the same position of the second, so PostgreSQL's translate() is well formed
// and SQLite's replace() chain has one call for each letter.
func TestTheFoldMapsEveryAccentedLetterToItsPlainOne(t *testing.T) {
	sql := query.FoldAccentsTranslate("x")
	parts := strings.Split(sql, "'")
	if len(parts) != 5 {
		t.Fatalf("translate() has %d quoted arguments, want 2: %s", len(parts)/2, sql)
	}
	letters, bases := []rune(parts[1]), []rune(parts[3])
	if len(letters) != len(bases) {
		t.Fatalf("%d letters fold to %d letters", len(letters), len(bases))
	}
	seen := map[rune]bool{}
	for _, letter := range letters {
		if seen[letter] {
			t.Errorf("%q is in the table twice; translate() would use the first", letter)
		}
		seen[letter] = true
	}
	folded := map[rune]rune{}
	for at, letter := range letters {
		folded[letter] = bases[at]
	}
	for letter, want := range map[rune]rune{'â': 'a', 'Â': 'a', 'ç': 'c', 'Ç': 'c', 'ñ': 'n', 'Ü': 'u', 'é': 'e', 'Õ': 'o', 'ÿ': 'y', 'ž': 'z'} {
		if folded[letter] != want {
			t.Errorf("%q folds to %q, want %q", letter, folded[letter], want)
		}
	}
	if got := strings.Count(query.FoldAccentsReplace("x"), "replace("); got != len(letters) {
		t.Errorf("replace() is called %d times for %d letters", got, len(letters))
	}
}

// FoldText is what a search written by hand uses, so it must be the fold the
// listing's own search compiles to on every grammar.
func TestFoldTextIsTheFoldTheListingUses(t *testing.T) {
	for name, grammar := range map[string]query.Grammar{
		"postgres": grammars.NewPostgresGrammar(), "sqlite": grammars.NewSQLiteGrammar(), "mysql": grammars.NewMySQLGrammar(),
	} {
		b := query.NewBuilder(nil, grammar, nil).From("notes")
		sql := b.Clone().ApplyListing(noteState(t, "q=x")).ToSQL()
		if folded := b.FoldText(b.Grammar.Wrap("title")); !strings.Contains(sql, folded) {
			t.Errorf("%s: the listing does not fold the title as FoldText does:\n%s\n%s", name, folded, sql)
		}
	}
	if got := query.NewBuilder(nil, grammars.NewMySQLGrammar(), nil).FoldText("x"); got != "lower(x)" {
		t.Errorf("mysql folds with %q, want lower(x)", got)
	}
}
