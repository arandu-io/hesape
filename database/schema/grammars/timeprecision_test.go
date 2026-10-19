package grammars_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/database/schema"
)

// TestAnExplicitTimePrecisionIsKept: the default is microseconds, and a
// migration that names a precision -- whole seconds included -- gets exactly
// that one.
func TestAnExplicitTimePrecisionIsKept(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.Create()
		table.Timestamp("seconds", 0)
		table.Timestamp("millis", 3)
		table.Time("starts")
	}
	assertSQL(t, compile(t, newFake("pgsql"), "slots", build),
		`create table "slots" (`+
			`"seconds" timestamp(0) without time zone not null, `+
			`"millis" timestamp(3) without time zone not null, `+
			`"starts" time(6) without time zone not null)`)
	assertSQL(t, compile(t, newFake("mysql"), "slots", build),
		"create table `slots` (`seconds` timestamp not null, `millis` timestamp(3) not null, `starts` time(6) not null)")
}

// TestWidenTimePrecisionOnPostgres alters, in one statement, only the
// date-time columns below the precision asked for, and leaves everything else
// about them alone.
func TestWidenTimePrecisionOnPostgres(t *testing.T) {
	conn := newFake("pgsql")
	conn.columns = []schema.ColumnInfo{
		{Name: "id", Type: "character varying(255)"},
		{Name: "occurred_at", Type: "timestamp(0) without time zone"},
		{Name: "published_at", Type: "timestamp(0) without time zone", Nullable: true},
		{Name: "seen_at", Type: "timestamp(3) with time zone"},
		{Name: "starts", Type: "time(0) without time zone"},
		{Name: "already", Type: "timestamp(6) without time zone"},
		{Name: "engine_default", Type: "timestamp without time zone"},
		{Name: "day", Type: "date"},
	}
	altered, err := schema.NewBuilder(conn).WidenTimePrecision(context.Background(), "outbox", 6)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"occurred_at", "published_at", "seen_at", "starts"}; !reflect.DeepEqual(altered, want) {
		t.Fatalf("altered %v, want %v", altered, want)
	}
	assertSQL(t, conn.statements,
		`alter table "outbox" alter column "occurred_at" type timestamp(6) without time zone, `+
			`alter column "published_at" type timestamp(6) without time zone, `+
			`alter column "seen_at" type timestamp(6) with time zone, `+
			`alter column "starts" type time(6) without time zone`)

	// Nothing left below the precision: nothing is run.
	conn.statements = nil
	conn.columns = []schema.ColumnInfo{{Name: "occurred_at", Type: "timestamp(6) without time zone"}}
	altered, err = schema.NewBuilder(conn).WidenTimePrecision(context.Background(), "outbox", 6)
	if err != nil || altered != nil || conn.statements != nil {
		t.Fatalf("a table already at the precision answered %v, %v and ran %v", altered, err, conn.statements)
	}
}

// TestWidenTimePrecisionOnMySQL restates each column whole, because MODIFY
// replaces the definition: nullability, default, on-update and comment come
// back as they were, with CURRENT_TIMESTAMP at the new precision.
func TestWidenTimePrecisionOnMySQL(t *testing.T) {
	conn := newFake("mysql")
	conn.rawColumns = []schema.Record{
		{"name": "id", "type": "bigint unsigned", "nullable": "NO", "default": nil, "extra": "auto_increment", "comment": ""},
		{"name": "created_at", "type": "timestamp", "nullable": "YES", "default": nil, "extra": "", "comment": ""},
		{"name": "published_at", "type": "timestamp", "nullable": "NO", "default": "CURRENT_TIMESTAMP",
			"extra": "DEFAULT_GENERATED on update CURRENT_TIMESTAMP", "comment": "when it went out"},
		{"name": "due", "type": []byte("datetime"), "nullable": "NO", "default": "2026-01-01 00:00:00", "extra": "", "comment": ""},
		{"name": "fine", "type": "datetime(6)", "nullable": "YES", "default": nil, "extra": "", "comment": ""},
	}
	altered, err := schema.NewBuilder(conn).WidenTimePrecision(context.Background(), "posts", 6)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"created_at", "published_at", "due"}; !reflect.DeepEqual(altered, want) {
		t.Fatalf("altered %v, want %v", altered, want)
	}
	assertSQL(t, conn.statements,
		"alter table `posts` modify `created_at` timestamp(6) null, "+
			"modify `published_at` timestamp(6) not null default CURRENT_TIMESTAMP(6) on update CURRENT_TIMESTAMP(6) comment 'when it went out', "+
			"modify `due` datetime(6) not null default '2026-01-01 00:00:00'")
}

// TestWidenTimePrecisionLeavesSQLiteAndRefusesNonsense: SQLite keeps the
// fraction it was given, and a precision outside 1..6 is a mistake.
func TestWidenTimePrecisionLeavesSQLiteAndRefusesNonsense(t *testing.T) {
	conn := newFake("sqlite")
	altered, err := schema.NewBuilder(conn).WidenTimePrecision(context.Background(), "outbox", 6)
	if err != nil || altered != nil || len(conn.statements)+len(conn.queries) != 0 {
		t.Fatalf("SQLite answered %v, %v after %v %v", altered, err, conn.statements, conn.queries)
	}
	for _, precision := range []int{0, 7} {
		if _, err := schema.NewBuilder(newFake("pgsql")).WidenTimePrecision(context.Background(), "outbox", precision); err == nil {
			t.Errorf("precision %d was accepted", precision)
		}
	}
}
