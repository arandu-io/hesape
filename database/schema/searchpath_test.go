package schema_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/database/schema/grammars"
)

// TestDropAllTablesListsOnlyTheSearchPath is the defect that shipped: the
// wipe listed its tables with no schema, the Postgres grammar read that as
// every non-system schema of the database, and migrate:fresh on a connection
// whose search_path was one schema dropped the tables of every other schema
// beside it. The listing must name the session's schemas and nothing else.
func TestDropAllTablesListsOnlyTheSearchPath(t *testing.T) {
	c := newConn()
	c.searchPath = []schema.Record{{"name": "tenant_a"}}
	c.tables = []schema.TableInfo{{Name: "users", Schema: "tenant_a", SchemaQualifiedName: "tenant_a.users"}}

	if err := schema.NewBuilder(c).DropAllTables(context.Background()); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}

	listing := lastSelect(t, c, "from pg_class c, pg_namespace n")
	if !strings.Contains(listing, "n.nspname in ('tenant_a')") {
		t.Fatalf("the tables were listed across schemas the session does not search:\n%s", listing)
	}
	if strings.Contains(listing, "not like 'pg\\_%'") {
		t.Fatalf("the listing still reaches every non-system schema:\n%s", listing)
	}
	if len(c.statements) != 1 || c.statements[0] != `drop table "tenant_a"."users" cascade` {
		t.Fatalf("statements = %q, want the one table of the search path dropped", c.statements)
	}
}

// TestDropAllTablesOnMySQLListsOnlyTheSelectedDatabase: MySQL had the same
// reach -- with no schema named, its listing is every database the account can
// read but the system ones -- and a wipe stays in the database the connection
// selected.
func TestDropAllTablesOnMySQLListsOnlyTheSelectedDatabase(t *testing.T) {
	c := newConn()
	c.grammar = grammars.NewMySQLGrammar(c)
	c.searchPath = []schema.Record{{"name": []byte("app")}}
	c.tables = []schema.TableInfo{{Name: "users", Schema: "app", SchemaQualifiedName: "app.users"}}

	if err := schema.NewBuilder(c).DropAllTables(context.Background()); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}

	listing := lastSelect(t, c, "from information_schema.tables")
	if !strings.Contains(listing, "table_schema in ('app')") {
		t.Fatalf("the tables were listed across databases the connection did not select:\n%s", listing)
	}
	if len(c.statements) != 1 || c.statements[0] != "drop table `app`.`users`" {
		t.Fatalf("statements = %q, want the one table of the selected database dropped", c.statements)
	}
}

// TestDropAllViewsListsOnlyTheSearchPath: the views are wiped with the same
// reach as the tables, so a view in a schema beside the path survives too.
func TestDropAllViewsListsOnlyTheSearchPath(t *testing.T) {
	c := newConn()
	c.searchPath = []schema.Record{{"name": "tenant_a"}, {"name": "shared"}}

	if err := schema.NewBuilder(c).DropAllViews(context.Background()); err != nil {
		t.Fatalf("DropAllViews: %v", err)
	}

	listing := lastSelect(t, c, "from pg_views")
	if !strings.Contains(listing, "schemaname in ('tenant_a', 'shared')") {
		t.Fatalf("the views were listed across schemas the session does not search:\n%s", listing)
	}
}

// TestDropAllTablesWithAnEmptySearchPathDropsNothing: a path whose every
// schema is missing names nowhere this connection would create a table, and
// the empty list must not fall back to the listing with no schema -- which
// is every schema, the reach this exists to remove.
func TestDropAllTablesWithAnEmptySearchPathDropsNothing(t *testing.T) {
	c := newConn()
	c.tables = []schema.TableInfo{{Name: "users", Schema: "elsewhere", SchemaQualifiedName: "elsewhere.users"}}

	if err := schema.NewBuilder(c).DropAllTables(context.Background()); err != nil {
		t.Fatalf("DropAllTables: %v", err)
	}
	if len(c.statements) != 0 {
		t.Fatalf("statements = %q, want nothing dropped when the search path names no schema", c.statements)
	}
	for _, query := range c.selects {
		if strings.Contains(query, "from pg_class c, pg_namespace n") {
			t.Fatalf("the tables were listed with no schema after the search path came back empty:\n%s", query)
		}
	}
}

// TestSearchPathReadsTheSessionInOrder: the schemas come back in the order the
// session searches them, a driver that answers bytes is read as text, and a
// null -- MySQL with no database selected -- is no schema rather than "".
func TestSearchPathReadsTheSessionInOrder(t *testing.T) {
	c := newConn()
	c.searchPath = []schema.Record{{"name": "tenant_a"}, {"name": []byte("public")}, {"name": nil}}

	got, err := schema.NewBuilder(c).SearchPath(context.Background())
	if err != nil {
		t.Fatalf("SearchPath: %v", err)
	}
	if strings.Join(got, ",") != "tenant_a,public" {
		t.Fatalf("SearchPath = %q, want [tenant_a public]", got)
	}
}

// TestSearchPathRefusesAGrammarThatCannotNameIt: a grammar written outside
// this collection does not compile the session query, and SearchPath says so
// rather than answering an empty path that reads as "no schema".
func TestSearchPathRefusesAGrammarThatCannotNameIt(t *testing.T) {
	c := newConn()
	c.grammar = grammarWithoutSearchPath{c.grammar}

	if _, err := schema.NewBuilder(c).SearchPath(context.Background()); !errors.Is(err, schema.ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

// grammarWithoutSearchPath hides CompileCurrentSchemas, the way a grammar
// written before it existed lacks it.
type grammarWithoutSearchPath struct{ schema.Grammar }

func lastSelect(t *testing.T, c *conn, marker string) string {
	t.Helper()
	for i := len(c.selects) - 1; i >= 0; i-- {
		if strings.Contains(c.selects[i], marker) {
			return c.selects[i]
		}
	}
	t.Fatalf("no query containing %q was run; ran %q", marker, c.selects)
	return ""
}
