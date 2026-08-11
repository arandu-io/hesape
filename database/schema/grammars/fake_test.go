package grammars_test

import (
	"context"
	"strings"

	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/database/schema/grammars"
)

// fakeConnection is the schema.Connection the grammar tests compile against.
//
// It answers what a real connection would answer and remembers what it was
// asked to run, so a test can look at the DDL without a server. Reaching for a
// real database here would only prove that the driver works; what is under test
// is the SQL, and the SQL is the same whether or not anything executes it.
type fakeConnection struct {
	driver  string
	prefix  string
	version string
	maria   bool
	config  map[string]string
	grammar schema.Grammar

	columns     []schema.ColumnInfo
	indexes     []schema.IndexInfo
	foreignKeys []schema.ForeignKeyInfo
	fkEnabled   bool

	statements []string
	queries    []string
}

func newFake(driver string) *fakeConnection {
	c := &fakeConnection{driver: driver, config: map[string]string{}}
	switch driver {
	case "mysql":
		c.version = "8.4.0"
		c.grammar = grammars.NewMySQLGrammar(c)
	case "pgsql":
		c.version = "16.2"
		c.grammar = grammars.NewPostgresGrammar(c)
	case "sqlite":
		c.version = "3.45.0"
		c.grammar = grammars.NewSQLiteGrammar(c)
	}
	return c
}

func (c *fakeConnection) GetSchemaGrammar() schema.Grammar { return c.grammar }
func (c *fakeConnection) GetConfig(option string) string   { return c.config[option] }
func (c *fakeConnection) GetTablePrefix() string           { return c.prefix }
func (c *fakeConnection) GetDriverName() string            { return c.driver }
func (c *fakeConnection) GetServerVersion() string         { return c.version }
func (c *fakeConnection) IsMaria() bool                    { return c.maria }
func (c *fakeConnection) ForeignKeyConstraintsEnabled() bool {
	return c.fkEnabled
}

func (c *fakeConnection) Statement(ctx context.Context, query string) error {
	c.statements = append(c.statements, query)
	return nil
}

func (c *fakeConnection) Select(ctx context.Context, query string) ([]schema.Record, error) {
	c.queries = append(c.queries, query)
	switch {
	case strings.Contains(query, "pragma_table_xinfo") && strings.Contains(query, "group_concat"):
		return []schema.Record{{"kind": "indexes"}}, nil
	case strings.Contains(query, "pragma_table_xinfo"), strings.Contains(query, "information_schema.columns"):
		return []schema.Record{{"kind": "columns"}}, nil
	case strings.Contains(query, "pragma_foreign_key_list"):
		return []schema.Record{{"kind": "foreignKeys"}}, nil
	default:
		return nil, nil
	}
}

func (c *fakeConnection) Scalar(ctx context.Context, query string) (any, error) {
	c.queries = append(c.queries, query)
	return nil, nil
}

func (c *fakeConnection) ProcessTables(rows []schema.Record) []schema.TableInfo { return nil }
func (c *fakeConnection) ProcessViews(rows []schema.Record) []schema.ViewInfo   { return nil }
func (c *fakeConnection) ProcessSchemas(rows []schema.Record) []schema.SchemaInfo {
	return nil
}

func (c *fakeConnection) ProcessColumns(rows []schema.Record) []schema.ColumnInfo {
	return c.columns
}

func (c *fakeConnection) ProcessIndexes(rows []schema.Record) []schema.IndexInfo {
	return c.indexes
}

func (c *fakeConnection) ProcessForeignKeys(rows []schema.Record) []schema.ForeignKeyInfo {
	return c.foreignKeys
}
