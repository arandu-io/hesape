package grammars_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// outsideLiterals is sql with every string literal and quoted identifier
// removed, lexed the way PostgreSQL lexes them: a doubled quote is part of the
// literal, and a literal that never closes swallows the rest. What remains is
// the text the server parses as SQL.
func outsideLiterals(sql string) string {
	var out strings.Builder
	for i := 0; i < len(sql); i++ {
		quote := sql[i]
		if quote != '\'' && quote != '"' {
			out.WriteByte(quote)
			continue
		}
		for i++; i < len(sql); i++ {
			if sql[i] != quote {
				continue
			}
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}
			break
		}
		out.WriteString(" ? ")
	}
	return out.String()
}

// TestAJSONSelectorKeyCannotCloseItsLiteral: the key of a JSON path was wrapped
// in single quotes with nothing escaped, so a column name taken from input --
// a sort parameter, a filter key -- closed the literal and the rest was SQL:
// meta->x' || (select password from users limit 1) || ' read another table
// into the comparison.
func TestAJSONSelectorKeyCannotCloseItsLiteral(t *testing.T) {
	column := `meta->x' || (select password from users limit 1) || '`
	sql := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil).
		From("notes").Where(column, "=", "v").OrderBy(column).ToSQL()

	if parsed := outsideLiterals(sql); strings.Contains(parsed, "select password") {
		t.Fatalf("the key escaped its literal and became SQL:\n%s\nparsed as: %s", sql, parsed)
	}
}

// TestAJSONUpdatePathCannotCloseItsLiteral: the path jsonb_set takes is an array
// literal inside a string literal, and a key with a quote in it closed both and
// ended the statement with one of its own.
func TestAJSONUpdatePathCannotCloseItsLiteral(t *testing.T) {
	g := grammars.NewPostgresGrammar()
	b := query.NewBuilder(nil, g, nil).From("notes")
	for _, key := range []string{
		`meta->a}', '"pwn"'); update users set roles='["admin"]'; --`,
		`meta->a"}', '1'); update users set roles='x'; --`,
		`meta->a\"}', '1'); update users set roles='x'; --`,
	} {
		sql := g.CompileUpdateColumns(b, map[string]any{key: "v"})
		if parsed := outsideLiterals(sql); strings.Contains(parsed, "update users") || strings.Contains(parsed, ";") {
			t.Fatalf("the key escaped its literal and became SQL:\n%s\nparsed as: %s", sql, parsed)
		}
	}
}

// TestAJSONPathKeyIsStillTheKey: escaping changes the spelling and not the key.
func TestAJSONPathKeyIsStillTheKey(t *testing.T) {
	g := grammars.NewPostgresGrammar()
	if got, want := g.Wrap("meta->o'brien"), `"meta"->>'o''brien'`; got != want {
		t.Errorf("Wrap = %s, want %s", got, want)
	}
	b := query.NewBuilder(nil, g, nil).From("notes")
	if got, want := g.CompileUpdateColumns(b, map[string]any{`meta->say "hi"->o'k->items[0]`: "v"}),
		`"meta" = jsonb_set("meta"::jsonb, '{"say \"hi\"","o''k","items",0}', ?)`; got != want {
		t.Errorf("CompileUpdateColumns = %s, want %s", got, want)
	}
}
