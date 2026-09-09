package database_test

import (
	"testing"
	"time"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// TestEveryGrammarSpellsAPointerLikeAValue holds the two Go shapes of one
// column to one spelling, under every grammar.
//
// The grammars inherit one date format today, so a single behaviour satisfies
// all of them -- and that is exactly why the assertion is written per grammar
// rather than once. An engine that later needed its own layout would get it by
// overriding that method, and an override is written thinking about the value
// case; the pointer case is the one nobody remembers, which is how the two
// spellings appeared in the first place.
func TestEveryGrammarSpellsAPointerLikeAValue(t *testing.T) {
	at := time.Date(2026, 9, 9, 23, 14, 4, 0, time.UTC)

	for name, grammar := range map[string]query.Grammar{
		"sqlite":   grammars.NewSQLiteGrammar(),
		"mysql":    grammars.NewMySQLGrammar(),
		"mariadb":  grammars.NewMariaDBGrammar(),
		"postgres": grammars.NewPostgresGrammar(),
	} {
		t.Run(name, func(t *testing.T) {
			connection := database.NewConnection(nil, "", "", nil).SetQueryGrammar(grammar)

			got := connection.PrepareBindings([]any{at, &at})

			if got[0] != got[1] {
				t.Errorf("a time came out as %v and a pointer to it as %v", got[0], got[1])
			}
			if got[0] != at.Format(grammar.GetDateFormat()) {
				t.Errorf("the spelling is %v, want the one this grammar declares: %v",
					got[0], at.Format(grammar.GetDateFormat()))
			}

			var absent *time.Time
			if null := connection.PrepareBindings([]any{absent}); null[0] != nil {
				t.Errorf("a nil pointer came out as %v, want NULL", null[0])
			}
		})
	}
}
