package processors_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/processors"
)

// connection is a query.connection that records what it was asked to run and
// returns what the test put in it. It reports a last insert identifier,
// which is what MySQL and SQLite need and Postgres does not.
type connection struct {
	rows []query.Record
	last int64

	insertedSQL      string
	insertedBindings []any
	selectedSQL      string
	usedReadReplica  bool
}

func (c *connection) Select(_ context.Context, sql string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	c.selectedSQL = sql
	c.usedReadReplica = useReadPDO
	return c.rows, nil
}

func (c *connection) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	c.insertedSQL = sql
	c.insertedBindings = bindings
	return true, nil
}

func (c *connection) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	return 0, nil
}
func (c *connection) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	return 0, nil
}
func (c *connection) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	return true, nil
}

func (c *connection) GetLastInsertID(sequence string) (int64, error) { return c.last, nil }

func TestProcessSelectPassesResultsThrough(t *testing.T) {
	rows := []query.Record{{"id": int64(1)}, {"id": int64(2)}}

	got := processors.NewProcessor().ProcessSelect(nil, rows)

	if len(got) != 2 || got[0]["id"] != int64(1) {
		t.Fatalf("the rows came back changed: %v", got)
	}
}

// TestProcessInsertGetIDAsksTheConnection is the MySQL and SQLite route: the
// insert runs, and the identifier comes back out of band.
func TestProcessInsertGetIDAsksTheConnection(t *testing.T) {
	conn := &connection{last: 42}
	q := query.NewBuilder(conn, nil, nil)

	id, err := processors.NewProcessor().ProcessInsertGetID(context.Background(), q, "insert into `users` (`email`) values (?)", []any{"a@b.c"}, "")
	if err != nil {
		t.Fatalf("ProcessInsertGetID: %v", err)
	}

	if id != 42 {
		t.Errorf("the identifier is %d, wanted 42", id)
	}
	if conn.insertedSQL == "" || len(conn.insertedBindings) != 1 {
		t.Errorf("the insert did not run as written: %q %v", conn.insertedSQL, conn.insertedBindings)
	}
}

// TestProcessInsertGetIDWithoutAConnectionThatCanAnswer: a zero would read like
// an identifier, so it refuses instead.
func TestProcessInsertGetIDWithoutAConnectionThatCanAnswer(t *testing.T) {
	q := query.NewBuilder(nopConnection{}, nil, nil)

	if _, err := processors.NewProcessor().ProcessInsertGetID(context.Background(), q, "insert into t (a) values (?)", []any{1}, ""); err == nil {
		t.Fatal("a connection that cannot report an identifier returned one anyway")
	}
}

type nopConnection struct{}

func (nopConnection) Select(context.Context, string, []any, bool) ([]query.Record, error) {
	return nil, nil
}
func (nopConnection) Insert(context.Context, string, []any) (bool, error)    { return true, nil }
func (nopConnection) Update(context.Context, string, []any) (int64, error)   { return 0, nil }
func (nopConnection) Delete(context.Context, string, []any) (int64, error)   { return 0, nil }
func (nopConnection) Statement(context.Context, string, []any) (bool, error) { return true, nil }

// TestPostgresProcessInsertGetIDReadsTheReturnedRow is the other route: the
// grammar compiled a returning clause, so the identifier arrives as a row --
// and the statement has to run against the writer, because a replica has not
// seen it yet.
func TestPostgresProcessInsertGetIDReadsTheReturnedRow(t *testing.T) {
	conn := &connection{rows: []query.Record{{"id": int64(7)}}}
	q := query.NewBuilder(conn, nil, nil)

	id, err := processors.NewPostgresProcessor().ProcessInsertGetID(context.Background(), q, `insert into "users" ("email") values (?) returning "id"`, []any{"a@b.c"}, "")
	if err != nil {
		t.Fatalf("ProcessInsertGetID: %v", err)
	}

	if id != 7 {
		t.Errorf("the identifier is %d, wanted 7", id)
	}
	if conn.usedReadReplica {
		t.Error("the insert was read back from a replica, which has not seen the row")
	}
}

// TestPostgresProcessInsertGetIDReadsANamedSequence: a table whose key is not
// called id says so, and the grammar returned that column.
func TestPostgresProcessInsertGetIDReadsANamedSequence(t *testing.T) {
	conn := &connection{rows: []query.Record{{"user_id": []byte("9")}}}
	q := query.NewBuilder(conn, nil, nil)

	id, err := processors.NewPostgresProcessor().ProcessInsertGetID(context.Background(), q, "insert ...", nil, "user_id")
	if err != nil {
		t.Fatalf("ProcessInsertGetID: %v", err)
	}
	if id != 9 {
		t.Errorf("the identifier is %d, wanted 9", id)
	}
}

func TestPostgresProcessInsertGetIDWithoutARow(t *testing.T) {
	q := query.NewBuilder(&connection{}, nil, nil)

	if _, err := processors.NewPostgresProcessor().ProcessInsertGetID(context.Background(), q, "insert ...", nil, ""); err == nil {
		t.Fatal("an insert that returned nothing reported an identifier anyway")
	}
}

func TestMySQLProcessColumns(t *testing.T) {
	got := processors.NewMySQLProcessor().ProcessColumns([]query.Record{{
		"name":       "id",
		"type_name":  "bigint",
		"type":       "bigint unsigned",
		"collation":  nil,
		"nullable":   "NO",
		"default":    nil,
		"extra":      "auto_increment",
		"comment":    "",
		"expression": nil,
	}})

	column := got[0]

	if column["auto_increment"] != true {
		t.Error("the auto incrementing column was not reported as one")
	}
	if column["nullable"] != false {
		t.Error("a NOT NULL column was reported nullable")
	}
	if column["comment"] != nil {
		t.Errorf("an empty comment came back as %v rather than absent", column["comment"])
	}
	if column["generation"] != nil {
		t.Errorf("a plain column came back generated: %v", column["generation"])
	}
}

func TestMySQLProcessIndexes(t *testing.T) {
	got := processors.NewMySQLProcessor().ProcessIndexes([]query.Record{
		{"name": "PRIMARY", "columns": "id", "type": "BTREE", "unique": int64(1)},
		{"name": "users_email_unique", "columns": "email,team_id", "type": "BTREE", "unique": int64(1)},
	})

	if got[0]["primary"] != true || got[0]["name"] != "primary" {
		t.Errorf("the primary key was not recognised: %v", got[0])
	}
	if got[1]["primary"] != false {
		t.Errorf("an ordinary index was reported as the primary key: %v", got[1])
	}

	columns, _ := got[1]["columns"].([]string)
	if len(columns) != 2 || columns[1] != "team_id" {
		t.Errorf("the column list is %v, wanted [email team_id]", got[1]["columns"])
	}
}

// TestSQLiteProcessColumns: SQLite keeps the collation and the expression of a
// generated column only in the text the table was declared with, so the
// processor reads them back out of it.
func TestSQLiteProcessColumns(t *testing.T) {
	sql := "create table users (id integer primary key, email varchar collate nocase, " +
		"upper_email varchar as (upper(email)) stored)"

	got := processors.NewSQLiteProcessor().ProcessColumns([]query.Record{
		{"name": "id", "type": "INTEGER", "nullable": int64(0), "default": nil, "primary": int64(1), "extra": int64(0)},
		{"name": "email", "type": "varchar", "nullable": int64(1), "default": nil, "primary": int64(0), "extra": int64(0)},
		{"name": "upper_email", "type": "varchar", "nullable": int64(1), "default": nil, "primary": int64(0), "extra": int64(3)},
	}, sql)

	if got[0]["auto_increment"] != true {
		t.Error("the integer primary key was not reported as auto incrementing")
	}
	if got[0]["type_name"] != "integer" {
		t.Errorf("the type name is %v, wanted integer", got[0]["type_name"])
	}
	if got[1]["collation"] != "nocase" {
		t.Errorf("the collation is %v, wanted nocase", got[1]["collation"])
	}

	generation, _ := got[2]["generation"].(query.Record)
	if generation == nil || generation["type"] != "stored" || generation["expression"] != "upper(email)" {
		t.Errorf("the generated column came back as %v", got[2]["generation"])
	}
}

// TestSQLiteProcessIndexesDropsTheDuplicatePrimary: SQLite reports the implicit
// index of a composite key alongside the key itself.
func TestSQLiteProcessIndexesDropsTheDuplicatePrimary(t *testing.T) {
	got := processors.NewSQLiteProcessor().ProcessIndexes([]query.Record{
		{"name": "PRIMARY", "columns": "id", "primary": int64(1), "unique": int64(1)},
		{"name": "sqlite_autoindex_users_1", "columns": "team_id,email", "primary": int64(1), "unique": int64(1)},
	})

	if len(got) != 1 {
		t.Fatalf("both primary keys were kept: %v", got)
	}
	if got[0]["name"] != "sqlite_autoindex_users_1" {
		t.Errorf("the wrong index survived: %v", got[0])
	}
}

func TestPostgresProcessForeignKeys(t *testing.T) {
	got := processors.NewPostgresProcessor().ProcessForeignKeys([]query.Record{{
		"name":            "posts_user_id_foreign",
		"columns":         "user_id",
		"foreign_schema":  "public",
		"foreign_table":   "users",
		"foreign_columns": "id",
		"on_update":       "a",
		"on_delete":       "c",
	}})

	if got[0]["on_update"] != "no action" {
		t.Errorf("on update is %v, wanted no action", got[0]["on_update"])
	}
	if got[0]["on_delete"] != "cascade" {
		t.Errorf("on delete is %v, wanted cascade", got[0]["on_delete"])
	}
}

func TestPostgresProcessTypes(t *testing.T) {
	got := processors.NewPostgresProcessor().ProcessTypes([]query.Record{{
		"name": "status", "schema": "public", "implicit": false, "type": "e", "category": "e",
	}})

	if got[0]["type"] != "enum" || got[0]["category"] != "enum" {
		t.Errorf("the type came back as %v / %v", got[0]["type"], got[0]["category"])
	}
	if got[0]["schema_qualified_name"] != "public.status" {
		t.Errorf("the qualified name is %v", got[0]["schema_qualified_name"])
	}
}

func TestProcessTables(t *testing.T) {
	got := processors.NewProcessor().ProcessTables([]query.Record{
		{"name": "users", "schema": "public", "size": int64(8192)},
		{"name": "posts"},
	})

	if got[0]["schema_qualified_name"] != "public.users" {
		t.Errorf("the qualified name is %v", got[0]["schema_qualified_name"])
	}
	if got[0]["size"] != int64(8192) {
		t.Errorf("the size is %v", got[0]["size"])
	}
	if got[1]["schema_qualified_name"] != "posts" {
		t.Errorf("a table with no schema was qualified anyway: %v", got[1]["schema_qualified_name"])
	}
	if got[1]["size"] != nil {
		t.Errorf("an unreported size came back as %v rather than absent", got[1]["size"])
	}
}

// TestMariaDBProcessorIsMySQLs guards the one thing the empty subclass claims.
func TestMariaDBProcessorIsMySQLs(t *testing.T) {
	got := processors.NewMariaDBProcessor().ProcessIndexes([]query.Record{
		{"name": "PRIMARY", "columns": "id", "type": "BTREE", "unique": int64(1)},
	})

	if got[0]["primary"] != true {
		t.Errorf("MariaDB did not inherit MySQL's index shaping: %v", got[0])
	}
}
