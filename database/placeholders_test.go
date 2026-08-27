package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// TestABuiltQueryIsRenumberedForPostgres is the whole of the placeholder
// portability layer measured where it is used rather than where it is written.
//
// Every grammar compiles a placeholder as "?", Postgres included, and the
// renumbering to $1, $2 happens on the connection. This asserts the statement
// that reaches the driver, which is the only place the claim can be checked:
// a query that arrives at pgx still carrying "?" is a syntax error at the
// server, and the test that compiles SQL without running it cannot see it.
func TestABuiltQueryIsRenumberedForPostgres(t *testing.T) {
	handle, state := newFakeDB()
	connection := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

	g := auth.SystemGrant("widget.list", "acme")
	if _, err := connection.Table("widgets").
		Where("status", "=", "open").
		Get(context.Background(), g); err != nil {
		t.Fatalf("Get: %v", err)
	}

	statements := state.statements()
	if len(statements) != 1 {
		t.Fatalf("the driver saw %d statements, want 1: %q", len(statements), statements)
	}
	if strings.Contains(statements[0], "?") {
		t.Fatalf("the statement reached the driver unbound: %q", statements[0])
	}
	if !strings.Contains(statements[0], "$1") || !strings.Contains(statements[0], "$2") {
		t.Fatalf("the statement was not renumbered: %q", statements[0])
	}
}

// TestABuiltQueryKeepsItsPlaceholdersForMySQL: MySQL and SQLite spell a
// placeholder "?" already, so the same query goes through untouched. Without
// this the renumbering could be applied to every dialect and nothing would
// say so.
func TestABuiltQueryKeepsItsPlaceholdersForMySQL(t *testing.T) {
	handle, state := newFakeDB()
	connection := database.NewConnection(handle, "app", "", map[string]any{"driver": "mysql"})

	g := auth.SystemGrant("widget.list", "acme")
	if _, err := connection.Table("widgets").
		Where("status", "=", "open").
		Get(context.Background(), g); err != nil {
		t.Fatalf("Get: %v", err)
	}

	statements := state.statements()
	if len(statements) != 1 {
		t.Fatalf("the driver saw %d statements, want 1: %q", len(statements), statements)
	}
	if !strings.Contains(statements[0], "?") {
		t.Fatalf("the statement was rewritten: %q", statements[0])
	}
}

// TestACursorIsRenumberedForPostgres: Cursor hands rows back as they arrive
// and therefore issues its query itself rather than through the wrapper the
// other verbs share. It is the one place the renumbering has to be repeated,
// which is the one place it can be forgotten.
func TestACursorIsRenumberedForPostgres(t *testing.T) {
	handle, state := newFakeDB()
	connection := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

	for range connection.Cursor(context.Background(),
		`SELECT id FROM widgets WHERE tenant_id = ? AND status = ?`,
		[]any{"acme", "open"}, true) {
		break
	}

	statements := state.statements()
	if len(statements) != 1 {
		t.Fatalf("the driver saw %d statements, want 1: %q", len(statements), statements)
	}
	if !strings.Contains(statements[0], "tenant_id = $1") {
		t.Fatalf("the statement reached the driver unbound: %q", statements[0])
	}
}

// TestAStatementWithNoValuesIsLeftAlone: a statement that carries no bindings
// has no placeholder to number, so a "?" in it is an operator -- Postgres
// spells jsonb containment that way -- or a character in a literal. Rewriting
// either would corrupt SQL somebody wrote by hand.
func TestAStatementWithNoValuesIsLeftAlone(t *testing.T) {
	handle, state := newFakeDB()
	connection := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

	create := `CREATE INDEX widgets_open_idx ON widgets ((payload ? 'open'))`
	if _, err := connection.Statement(context.Background(), create, nil); err != nil {
		t.Fatalf("Statement: %v", err)
	}

	if !state.sawStatement(create) {
		t.Fatalf("the statement was rewritten: %q", state.statements())
	}
}

// TestAWriteIsRenumberedForPostgres covers the other three verbs: an insert,
// an update and a delete each go out through a different Connection method,
// and a fix applied to the read path only would leave every write broken.
func TestAWriteIsRenumberedForPostgres(t *testing.T) {
	ctx := context.Background()
	g := auth.SystemGrant("widget.write", "acme")

	cases := []struct {
		name string
		run  func(*database.Connection) error
	}{
		{
			name: "insert",
			run: func(c *database.Connection) error {
				_, err := c.Table("widgets").Insert(ctx, g, map[string]any{"id": "w-1", "status": "open"})
				return err
			},
		},
		{
			name: "update",
			run: func(c *database.Connection) error {
				_, err := c.Table("widgets").Where("id", "=", "w-1").
					Update(ctx, g, map[string]any{"status": "closed"})
				return err
			},
		},
		{
			name: "delete",
			run: func(c *database.Connection) error {
				_, err := c.Table("widgets").Where("id", "=", "w-1").Delete(ctx, g)
				return err
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handle, state := newFakeDB()
			connection := database.NewConnection(handle, "app", "", map[string]any{"driver": "pgsql"})

			if err := c.run(connection); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}

			statements := state.statements()
			if len(statements) == 0 {
				t.Fatalf("the driver saw nothing")
			}
			for _, statement := range statements {
				if strings.Contains(statement, "?") {
					t.Fatalf("the statement reached the driver unbound: %q", statement)
				}
			}
		})
	}
}
