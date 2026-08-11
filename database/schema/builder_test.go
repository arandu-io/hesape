package schema_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/database/schema/grammars"
)

func grant() auth.Grant { return auth.SystemGrant(schema.ActionMigrate, "acme") }

// conn is a schema.Connection that answers from canned data and remembers what
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

// TestBuilderRequiresAGrant is RULE 17 on the executing half. Every method that
// reaches the connection refuses the zero Grant, and refuses one issued for
// another action.
func TestBuilderRequiresAGrant(t *testing.T) {
	builder := schema.NewBuilder(newConn())
	ctx := context.Background()

	cases := map[string]func(auth.Grant) error{
		"Create": func(g auth.Grant) error {
			return builder.Create(ctx, g, "users", func(*schema.Blueprint) {})
		},
		"Drop":        func(g auth.Grant) error { return builder.Drop(ctx, g, "users") },
		"Rename":      func(g auth.Grant) error { return builder.Rename(ctx, g, "users", "people") },
		"DropColumns": func(g auth.Grant) error { return builder.DropColumns(ctx, g, "users", []string{"a"}) },
		"HasTable": func(g auth.Grant) error {
			_, err := builder.HasTable(ctx, g, "users")
			return err
		},
		"GetColumnListing": func(g auth.Grant) error {
			_, err := builder.GetColumnListing(ctx, g, "users")
			return err
		},
		"GetIndexes": func(g auth.Grant) error {
			_, err := builder.GetIndexes(ctx, g, "users")
			return err
		},
		"GetForeignKeys": func(g auth.Grant) error {
			_, err := builder.GetForeignKeys(ctx, g, "users")
			return err
		},
		"EnableForeignKeyConstraints": func(g auth.Grant) error {
			return builder.EnableForeignKeyConstraints(ctx, g)
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(auth.Grant{}); !errors.Is(err, auth.ErrForbidden) {
				t.Fatalf("the zero Grant was accepted: %v", err)
			}
			if err := call(auth.SystemGrant("customer.list", "acme")); !errors.Is(err, auth.ErrForbidden) {
				t.Fatalf("a Grant for another action was accepted: %v", err)
			}
		})
	}
}

func TestHasTableUsesTheExistenceQuery(t *testing.T) {
	c := newConn()
	c.scalar = true
	builder := schema.NewBuilder(c)

	has, err := builder.HasTable(context.Background(), grant(), "reporting.users")
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

	has, err := builder.HasColumn(ctx, grant(), "users", "EMAIL")
	if err != nil || !has {
		t.Fatalf("HasColumn is case sensitive: %v %v", has, err)
	}

	all, err := builder.HasColumns(ctx, grant(), "users", []string{"id", "email"})
	if err != nil || !all {
		t.Fatalf("HasColumns: %v %v", all, err)
	}

	missing, err := builder.HasColumns(ctx, grant(), "users", []string{"id", "nope"})
	if err != nil || missing {
		t.Fatalf("HasColumns reported a column the table does not have: %v %v", missing, err)
	}

	typ, err := builder.GetColumnType(ctx, grant(), "users", "email")
	if err != nil || typ != "varchar" {
		t.Fatalf("GetColumnType: %q %v", typ, err)
	}

	full, err := builder.GetColumnType(ctx, grant(), "users", "email", true)
	if err != nil || full != "character varying(255)" {
		t.Fatalf("GetColumnType(full): %q %v", full, err)
	}

	if _, err := builder.GetColumnType(ctx, grant(), "users", "nope"); err == nil {
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
		got, err := builder.HasIndex(ctx, grant(), "users", c.index, c.typ...)
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
	err := builder.WithoutForeignKeyConstraints(context.Background(), grant(), func() error { return want })

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

// TestDefaults covers the three package level settings that stand in for
// Illuminate's static properties on the Builder.
func TestDefaults(t *testing.T) {
	c := newConn()

	schema.DefaultStringLength(64)
	t.Cleanup(func() { schema.DefaultStringLength(255) })

	blueprint := schema.NewBlueprint(c, "users", func(table *schema.Blueprint) {
		table.Create()
		table.String("email")
	})
	sql, err := blueprint.ToSQL(context.Background(), grant())
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
	sql, err = morphs.ToSQL(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql[0], `"taggable_id" char(26)`) {
		t.Fatalf("MorphUsingULIDs was ignored: %s", sql[0])
	}
}
