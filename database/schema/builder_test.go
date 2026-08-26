package schema_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/database/schema/grammars"
)

// conn is a schema.Connection that responds from canned data and remembers what
// it was asked to run.
type conn struct {
	grammar     schema.Grammar
	statements  []string
	scalar      any
	columns     []schema.ColumnInfo
	indexes     []schema.IndexInfo
	foreignKeys []schema.ForeignKeyInfo
	tables      []schema.TableInfo
}

func newConn() *conn {
	c := &conn{}
	c.grammar = grammars.NewPostgresGrammar(c)
	return c
}

func (c *conn) GetSchemaGrammar() schema.Grammar                 { return c.grammar }
func (c *conn) GetConfig(string) string                          { return "" }
func (c *conn) GetTablePrefix() string                           { return "" }
func (c *conn) GetDriverName() string                            { return "pgsql" }
func (c *conn) GetServerVersion() string                         { return "16.2" }
func (c *conn) IsMaria() bool                                    { return false }
func (c *conn) ForeignKeyConstraintsEnabled() bool               { return true }
func (c *conn) ProcessTables([]schema.Record) []schema.TableInfo { return c.tables }
func (c *conn) ProcessViews([]schema.Record) []schema.ViewInfo   { return nil }
func (c *conn) ProcessSchemas([]schema.Record) []schema.SchemaInfo {
	return nil
}
func (c *conn) ProcessColumns([]schema.Record) []schema.ColumnInfo { return c.columns }
func (c *conn) ProcessIndexes([]schema.Record) []schema.IndexInfo  { return c.indexes }
func (c *conn) ProcessForeignKeys([]schema.Record) []schema.ForeignKeyInfo {
	return c.foreignKeys
}

func (c *conn) Statement(ctx context.Context, query string) error {
	c.statements = append(c.statements, query)
	return nil
}

func (c *conn) Select(ctx context.Context, query string) ([]schema.Record, error) {
	return []schema.Record{{}}, nil
}

func (c *conn) Scalar(ctx context.Context, query string) (any, error) { return c.scalar, nil }

// TestNoSchemaMethodTakesAGrant is what replaced TestBuilderRequiresAGrant.
//
// That test asserted the opposite: every method here refused the zero Grant and
// one issued for another action. It was right about the mechanism and wrong
// about the question. DDL names a table, not a row -- there is no tenant to
// scope it by, no subject to attribute it to, and no request it came from -- so
// the only Grant this package could ever have held was one somebody invented,
// and auth.SystemGrant refuses one without a tenant, which means inventing a
// tenant to throw away.
//
// A parameter that looks like enforcement and enforces nothing is worse than no
// parameter, because a reader stops looking. So the assertion is now that the
// parameter is gone, and it is made by the compiler: this file no longer
// imports auth, and it would not build if any of these methods still asked for
// one.
//
// The path to application rows is unaffected and still cannot be reached
// without a Grant. That is proved next door, in database/model.
func TestNoSchemaMethodTakesAGrant(t *testing.T) {
	builder := schema.NewBuilder(newConn())
	ctx := context.Background()

	// Each of these compiles only because the Grant is not in the signature.
	if err := builder.Create(ctx, "users", func(*schema.Blueprint) {}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := builder.Drop(ctx, "users"); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if err := builder.Rename(ctx, "users", "people"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := builder.HasTable(ctx, "users"); err != nil {
		t.Fatalf("HasTable: %v", err)
	}
}

func TestHasTableUsesTheExistenceQuery(t *testing.T) {
	c := newConn()
	c.scalar = true
	builder := schema.NewBuilder(c)

	has, err := builder.HasTable(context.Background(), "reporting.users")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("the table was reported missing")
	}
}

func TestHasColumnAndColumnType(t *testing.T) {
	c := newConn()
	c.columns = []schema.ColumnInfo{
		{Name: "id", TypeName: "int8", Type: "bigint"},
		{Name: "email", TypeName: "varchar", Type: "character varying(255)"},
	}
	builder := schema.NewBuilder(c)
	ctx := context.Background()

	has, err := builder.HasColumn(ctx, "users", "EMAIL")
	if err != nil || !has {
		t.Fatalf("HasColumn is case sensitive: %v %v", has, err)
	}

	all, err := builder.HasColumns(ctx, "users", []string{"id", "email"})
	if err != nil || !all {
		t.Fatalf("HasColumns: %v %v", all, err)
	}

	missing, err := builder.HasColumns(ctx, "users", []string{"id", "nope"})
	if err != nil || missing {
		t.Fatalf("HasColumns reported a column the table does not have: %v %v", missing, err)
	}

	typ, err := builder.GetColumnType(ctx, "users", "email")
	if err != nil || typ != "varchar" {
		t.Fatalf("GetColumnType: %q %v", typ, err)
	}

	full, err := builder.GetColumnType(ctx, "users", "email", true)
	if err != nil || full != "character varying(255)" {
		t.Fatalf("GetColumnType(full): %q %v", full, err)
	}

	if _, err := builder.GetColumnType(ctx, "users", "nope"); err == nil {
		t.Fatal("a column that does not exist reported a type")
	}
}

func TestHasIndex(t *testing.T) {
	c := newConn()
	c.indexes = []schema.IndexInfo{
		{Name: "users_email_unique", Columns: []string{"email"}, Unique: true, Type: "btree"},
		{Name: "users_pkey", Columns: []string{"id"}, Primary: true, Unique: true, Type: "btree"},
	}
	builder := schema.NewBuilder(c)
	ctx := context.Background()

	for _, c := range []struct {
		index any
		typ   []string
		want  bool
	}{
		{"users_email_unique", nil, true},
		{[]string{"email"}, nil, true},
		{[]string{"email"}, []string{"unique"}, true},
		{[]string{"email"}, []string{"primary"}, false},
		{[]string{"id"}, []string{"PRIMARY"}, true},
		{"nope", nil, false},
	} {
		got, err := builder.HasIndex(ctx, "users", c.index, c.typ...)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("HasIndex(%v, %v) = %v, want %v", c.index, c.typ, got, c.want)
		}
	}
}

func TestWithoutForeignKeyConstraintsPutsThemBack(t *testing.T) {
	c := newConn()
	builder := schema.NewBuilder(c)

	want := errors.New("the callback failed")
	err := builder.WithoutForeignKeyConstraints(context.Background(), func() error { return want })

	if !errors.Is(err, want) {
		t.Fatalf("the callback's error was swallowed: %v", err)
	}
	if len(c.statements) != 2 ||
		c.statements[0] != "SET CONSTRAINTS ALL DEFERRED;" ||
		c.statements[1] != "SET CONSTRAINTS ALL IMMEDIATE;" {
		t.Fatalf("the constraints were not put back: %#v", c.statements)
	}
}

func TestParseSchemaAndTable(t *testing.T) {
	for _, c := range []struct {
		reference string
		schema    string
		table     string
		wantErr   bool
	}{
		{"users", "", "users", false},
		{"reporting.users", "reporting", "users", false},
		{"one.two.three", "", "", true},
	} {
		gotSchema, gotTable, err := schema.ParseSchemaAndTable(c.reference)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q was accepted", c.reference)
			}
			continue
		}
		if err != nil || gotSchema != c.schema || gotTable != c.table {
			t.Errorf("ParseSchemaAndTable(%q) = %q, %q, %v", c.reference, gotSchema, gotTable, err)
		}
	}
}

// TestDefaults covers the three package level settings the Builder reads.
func TestDefaults(t *testing.T) {
	c := newConn()

	schema.DefaultStringLength(64)
	t.Cleanup(func() { schema.DefaultStringLength(255) })

	blueprint := schema.NewBlueprint(c, "users", func(table *schema.Blueprint) {
		table.Create()
		table.String("email")
	})
	sql, err := blueprint.ToSQL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql[0], "varchar(64)") {
		t.Fatalf("DefaultStringLength was ignored: %s", sql[0])
	}

	if err := schema.DefaultMorphKeyType("guid"); err == nil {
		t.Fatal("an unknown morph key type was accepted")
	}

	schema.MorphUsingULIDs()
	t.Cleanup(func() { _ = schema.DefaultMorphKeyType("int") })

	morphs := schema.NewBlueprint(c, "tags", func(table *schema.Blueprint) {
		table.Create()
		table.Morphs("taggable")
	})
	sql, err = morphs.ToSQL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql[0], `"taggable_id" char(26)`) {
		t.Fatalf("MorphUsingULIDs was ignored: %s", sql[0])
	}
}
