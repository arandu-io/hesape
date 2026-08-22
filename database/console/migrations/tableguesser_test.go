package migrations_test

import (
	"testing"

	"github.com/arandu-io/hesape/database/console/migrations"
)

func TestGuessReadsTheTableOutOfTheMigrationName(t *testing.T) {
	for name, want := range map[string]struct {
		table  string
		create bool
	}{
		"create_users_table":       {"users", true},
		"create_users":             {"users", true},
		"add_paid_at_to_invoices":  {"invoices", false},
		"drop_notes_from_accounts": {"accounts", false},
		"change_type_in_orders":    {"orders", false},
	} {
		table, create, guessed := migrations.Guess(name)
		if !guessed {
			t.Fatalf("Guess(%q) gave up", name)
		}
		if table != want.table || create != want.create {
			t.Fatalf("Guess(%q) = (%q, %v), want (%q, %v)", name, table, create, want.table, want.create)
		}
	}
}

func TestGuessSaysWhenItCannot(t *testing.T) {
	if _, _, guessed := migrations.Guess("fix_the_thing"); guessed {
		t.Fatal("Guess claimed a table for a name that names none")
	}
}
