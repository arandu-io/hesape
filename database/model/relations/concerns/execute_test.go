package concerns

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

// columnsOf pulls the column list out of a compiled insert: the text between
// the first pair of parentheses, unquoted and split.
//
// The test that uses it is about two lists agreeing, so it reads one of them
// from the statement rather than from a second copy of the expectation.
func columnsOf(t *testing.T, sql string) []string {
	t.Helper()

	open := strings.Index(sql, "(")
	closing := strings.Index(sql, ")")
	if open < 0 || closing < open {
		t.Fatalf("no column list in %q", sql)
	}

	columns := strings.Split(sql[open+1:closing], ",")
	for i, column := range columns {
		columns[i] = strings.Trim(strings.TrimSpace(column), `"`)
	}
	return columns
}

// TestInsertFlattensBindingsInTheOrderTheColumnsWereWritten is the corrupt-row
// test.
//
// The grammar writes the column list by sorting the keys of the first record.
// insert flattens the bindings by walking each record's keys. Go randomises map
// iteration, so bindings gathered in map order pair value 1 with column 3 on
// some runs and not others -- and the statement is valid SQL either way. What
// comes back is not an error but a row with every value in the wrong column.
//
// So this asserts the two lists against each other rather than against a second
// copy of the expectation, and uses five columns: an implementation that walked
// the map would have to draw the sorted permutation twice in a row to pass.
func TestInsertFlattensBindingsInTheOrderTheColumnsWereWritten(t *testing.T) {
	conn := &fakeConnection{}
	q := newQuery(conn, "role_user")

	rows := []map[string]any{
		{"user_id": 1, "role_id": "admin", "tenant_id": "acme", "created_at": "c1", "updated_at": "u1"},
		{"user_id": 2, "role_id": "editor", "tenant_id": "acme", "created_at": "c2", "updated_at": "u2"},
	}

	if err := insert(context.Background(), q, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ran := conn.only(t)
	if ran.kind != "insert" {
		t.Fatalf("insert ran a %s", ran.kind)
	}

	columns := columnsOf(t, ran.sql)
	if len(columns) != 5 {
		t.Fatalf("the statement names %d columns, want 5: %s", len(columns), ran.sql)
	}
	if len(ran.bindings) != len(columns)*len(rows) {
		t.Fatalf("%d bindings for %d columns over %d rows: %s",
			len(ran.bindings), len(columns), len(rows), ran.sql)
	}

	for row, record := range rows {
		for i, column := range columns {
			got := ran.bindings[row*len(columns)+i]
			want := record[column]
			if got != want {
				t.Fatalf("row %d binding %d is %v, and column %q holds %v. "+
					"Every value after this one lands in the wrong column too, and the row is written without an error.",
					row, i, got, column, want)
			}
		}
	}
}

// TestInsertOfNothingRunsNothing: an empty batch compiles to "insert into t
// default values", which writes a row of defaults. Attach with an empty id list
// reaches here, and it must not create a pivot row out of nothing.
func TestInsertOfNothingRunsNothing(t *testing.T) {
	conn := &fakeConnection{}

	if err := insert(context.Background(), newQuery(conn, "role_user"), nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("an empty insert ran %#v", conn.statements)
	}
}

// TestInsertLeavesOutTheBindingForAnExpression: a raw expression is written
// into the statement itself, so passing its value as a binding as well would
// shift every binding after it by one.
func TestInsertLeavesOutTheBindingForAnExpression(t *testing.T) {
	conn := &fakeConnection{}
	q := newQuery(conn, "role_user")

	rows := []map[string]any{
		{"user_id": 1, "created_at": query.Raw("CURRENT_TIMESTAMP"), "role_id": "admin"},
	}

	if err := insert(context.Background(), q, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ran := conn.only(t)
	if len(ran.bindings) != 2 {
		t.Fatalf("%d bindings for two values and one expression: %#v", len(ran.bindings), ran.bindings)
	}
	if !reflect.DeepEqual(ran.bindings, []any{"admin", 1}) {
		t.Fatalf("bindings are %#v, want the two non-expression values in column order", ran.bindings)
	}
	if !strings.Contains(ran.sql, "CURRENT_TIMESTAMP") {
		t.Fatalf("the expression was parameterised instead of written in: %s", ran.sql)
	}
}

// TestInsertReportsTheConnectionsError: the four helpers here are the only path
// past the builder's terminals, so a failure on this path has nothing else to
// report it.
func TestInsertReportsTheConnectionsError(t *testing.T) {
	boom := errors.New("unique constraint violated")
	conn := &fakeConnection{err: boom}

	err := insert(context.Background(), newQuery(conn, "role_user"), []map[string]any{{"user_id": 1}})
	if !errors.Is(err, boom) {
		t.Fatalf("insert answered %v, want the connection's own error", err)
	}
}

// TestUpdateSendsTheWhereBindingsAfterTheValues: an update's placeholders are
// the set list first and the where clause second. Reversed, the row updated is
// chosen by the new value and set to the old key.
func TestUpdateSendsTheWhereBindingsAfterTheValues(t *testing.T) {
	conn := &fakeConnection{affected: 3}
	q := newQuery(conn, "role_user").Where("user_id", 7).Where("tenant_id", "acme")

	affected, err := update(context.Background(), q, map[string]any{"expires_at": "2026-09-01"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if affected != 3 {
		t.Fatalf("update reported %d rows, and the connection said 3", affected)
	}

	ran := conn.only(t)
	if ran.kind != "update" {
		t.Fatalf("update ran a %s", ran.kind)
	}
	if !reflect.DeepEqual(ran.bindings, []any{"2026-09-01", 7, "acme"}) {
		t.Fatalf("bindings are %#v, want the set value then the two wheres", ran.bindings)
	}
}

// TestDeleteFromKeepsTheWhereBindings: a delete whose bindings were dropped is
// a delete of the whole table, and on a pivot table shared by tenants that is
// every customer's rows.
func TestDeleteFromKeepsTheWhereBindings(t *testing.T) {
	conn := &fakeConnection{affected: 2}
	q := newQuery(conn, "role_user").Where("user_id", 7).Where("tenant_id", "acme")

	affected, err := deleteFrom(context.Background(), q)
	if err != nil {
		t.Fatalf("deleteFrom: %v", err)
	}
	if affected != 2 {
		t.Fatalf("deleteFrom reported %d rows, and the connection said 2", affected)
	}

	ran := conn.only(t)
	if ran.kind != "delete" {
		t.Fatalf("deleteFrom ran a %s", ran.kind)
	}
	if !reflect.DeepEqual(ran.bindings, []any{7, "acme"}) {
		t.Fatalf("bindings are %#v, want both wheres", ran.bindings)
	}
	if !strings.Contains(ran.sql, "where") {
		t.Fatalf("the delete carries no where clause: %s", ran.sql)
	}
}

// countingProcessor records that it saw the rows and marks each one, so a test
// can tell "the processor ran" from "the rows came straight back".
type countingProcessor struct{ calls int }

func (p *countingProcessor) ProcessSelect(_ *query.Builder, results []query.Record) []query.Record {
	p.calls++
	out := make([]query.Record, 0, len(results))
	for _, row := range results {
		copied := query.Record{}
		for key, value := range row {
			copied[key] = value
		}
		copied["processed"] = true
		out = append(out, copied)
	}
	return out
}

func (p *countingProcessor) ProcessInsertGetID(context.Context, *query.Builder, string, []any, string) (int64, error) {
	return 0, nil
}

// TestSelectRowsRunsTheRowsThroughTheProcessor: the processor is where a driver
// fixes up what it returned, and skipping it here would give the pivot read a
// different row shape than every other read in the collection.
func TestSelectRowsRunsTheRowsThroughTheProcessor(t *testing.T) {
	conn := &fakeConnection{rows: []query.Record{{"role_id": "admin"}}}
	q := newQuery(conn, "role_user")

	processor := &countingProcessor{}
	q.Processor = processor

	rows, err := selectRows(context.Background(), q)
	if err != nil {
		t.Fatalf("selectRows: %v", err)
	}
	if processor.calls != 1 {
		t.Fatalf("the processor ran %d times", processor.calls)
	}
	if len(rows) != 1 || rows[0]["processed"] != true {
		t.Fatalf("the rows came back unprocessed: %#v", rows)
	}
}

// TestSelectRowsWithoutAProcessorReturnsTheRows: Processor is a field a caller
// can leave nil, and a nil check that was not there would panic on the pivot
// read rather than on the line that forgot it.
func TestSelectRowsWithoutAProcessorReturnsTheRows(t *testing.T) {
	conn := &fakeConnection{rows: []query.Record{{"role_id": "admin"}}}
	q := newQuery(conn, "role_user")
	q.Processor = nil

	rows, err := selectRows(context.Background(), q)
	if err != nil {
		t.Fatalf("selectRows: %v", err)
	}
	if len(rows) != 1 || rows[0]["role_id"] != "admin" {
		t.Fatalf("the rows came back as %#v", rows)
	}
}

// TestSelectRowsReportsTheConnectionsError, and does not hand the caller the
// canned rows beside it.
func TestSelectRowsReportsTheConnectionsError(t *testing.T) {
	boom := errors.New("no such table")
	conn := &fakeConnection{rows: []query.Record{{"role_id": "admin"}}, err: boom}

	rows, err := selectRows(context.Background(), newQuery(conn, "role_user"))
	if !errors.Is(err, boom) {
		t.Fatalf("selectRows answered %v, want the connection's own error", err)
	}
	if rows != nil {
		t.Fatalf("selectRows answered %#v beside an error", rows)
	}
}

// TestSortedKeysIsTotalOrderNotInsertionOrder is the invariant the insert
// pairing rests on, asserted on its own so a failure says which of the two
// broke.
func TestSortedKeysIsTotalOrderNotInsertionOrder(t *testing.T) {
	got := sortedKeys(map[string]any{"user_id": 1, "created_at": 2, "role_id": 3, "tenant_id": 4})
	want := []string{"created_at", "role_id", "tenant_id", "user_id"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedKeys = %v, want %v", got, want)
	}
}
