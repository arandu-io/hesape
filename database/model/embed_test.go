package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// account is the shape an application writes: its own struct, with the model
// embedded, so the entity and the model are one value.
type account struct {
	Model

	ID       int64  `db:"id"`
	Name     string `db:"name"`
	Email    string `db:"email"`
	TenantID string `db:"tenant_id"`
}

func newAccountTable(configure ...func(*TableSpec)) *Table {
	spec := TableSpec{Name: "accounts", New: func() Entity { return new(account) }}
	for _, c := range configure {
		c(&spec)
	}
	return NewTable(spec)
}

func newAccountModel(configure ...func(*TableSpec)) (*account, *testConnection) {
	conn := newTestConnection()
	return newAccountTable(configure...).New(conn).(*account), conn
}

// TestTheEntityIsTheModel.
//
// An embedded value has no way to name the value that embeds it, so the model
// cannot find its own entity by asking the compiler: in PHP $this answers this
// for free and in Go nothing does. The row is wired entity-first for that
// reason, and this test is the proof that the model reaches the entity it is
// inside rather than a second value that happens to agree.
//
// If it did not, everything below would still compile and Save would write the
// zero row.
func TestTheEntityIsTheModel(t *testing.T) {
	model, _ := newAccountModel()

	instance, err := instanceOf(model, nil, false)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	// The model the row holds has to be the one inside the entity, and the row
	// has to point back at that entity. Comparing the pointers is the whole
	// assertion: two values with equal fields would pass any check on their
	// contents and fail every write.
	if instance.base() != &instance.Model {
		t.Fatal("base does not hand back the model embedded in the entity")
	}
	if instance.r.self != Entity(instance) {
		t.Fatal("the row does not point back at the entity it is inside")
	}

	// Writing a field is writing what the model reads, which is what makes
	// user.Name = "..." followed by user.Save() mean anything.
	instance.Name = "Ada"
	if got := instance.GetAttribute("name"); got != "Ada" {
		t.Errorf("a field set on the entity reads back through the model as %v", got)
	}

	// And the configuration reaches the entity, so the promoted methods find a
	// table and a connection rather than a zero model.
	if got := instance.Table().Name(); got != "accounts" {
		t.Errorf("the entity's table is %q, want accounts", got)
	}
}

// TestHydrationWiresEveryRow: what the framework returns comes wired.
//
// A row that hydrated into an entity with no way back to itself would be a row
// the caller can read and cannot save, and nothing about it would look wrong.
func TestHydrationWiresEveryRow(t *testing.T) {
	model, conn := newAccountModel()
	conn.queue(
		query.Record{"id": int64(1), "name": "Ada", "email": "ada@example.test", "tenant_id": "t-1"},
		query.Record{"id": int64(2), "name": "Grace", "email": "grace@example.test", "tenant_id": "t-1"},
	)

	rows, err := newQuery(model.base()).Get(context.Background(), grantForTenant("t-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Get returned %d rows, want 2", len(rows))
	}

	for i, row := range rows {
		if row == nil {
			t.Fatalf("row %d hydrated as nothing", i)
		}
		if err := wired(row.base()); err != nil {
			t.Errorf("row %d is not wired: %v", i, err)
		}
		if row.base().Table().Name() != "accounts" {
			t.Errorf("row %d reached the caller without its configuration", i)
		}
		if !row.base().Exists() {
			t.Errorf("row %d came back from the database and does not say it exists", i)
		}
	}
	if rows[0].(*account).Name != "Ada" || rows[1].(*account).Name != "Grace" {
		t.Errorf("the rows hydrated as %q and %q", rows[0].(*account).Name, rows[1].(*account).Name)
	}
}

// TestAnEagerLoadIsReachableFromTheRowATerminalHandedBack.
//
// The eager load attaches what it matched to the model, and a terminal hands
// back the row. The two are one value, so the relation is reachable from what
// the caller holds -- which is the whole of what the embedding buys on this
// side.
func TestAnEagerLoadIsReachableFromTheRowATerminalHandedBack(t *testing.T) {
	model, conn := newAccountModel()
	withPostsOn(model, "account_id")

	conn.queue(query.Record{"id": int64(1), "name": "Ada", "tenant_id": "t-1"})
	conn.queue(query.Record{"id": int64(9), "account_id": int64(1), "title": "child", "tenant_id": "t-1"})

	rows, err := newQuery(model.base()).With("posts").Get(context.Background(), grantForTenant("t-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	posts, ok := rows[0].base().Related("posts")
	if !ok {
		t.Fatal("the eager load is not reachable from the row the terminal returned")
	}
	if len(posts) != 1 || posts[0].(*post).ID != 9 {
		t.Fatalf("posts = %v, want the one row the relation matched", posts)
	}

	// And the lazy half, which is the same reach through a promoted method.
	conn.queue(query.Record{"id": int64(9), "account_id": int64(1), "title": "child", "tenant_id": "t-1"})
	if err := rows[0].(*account).Load(context.Background(), grantForTenant("t-1"), "posts"); err != nil {
		t.Fatalf("Load on the row: %v", err)
	}
}

// TestALiteralEntityIsNotWiredAndDoesNotPanic.
//
// This is the one difference a Laravel developer learns at this layer, so it has
// to be a sentence and not a stack trace. A struct written by hand has a nil
// model inside it: no connection, no way back to itself. Calling a terminal on
// it says so, and names the way to make one.
func TestALiteralEntityIsNotWiredAndDoesNotPanic(t *testing.T) {
	literal := &account{Name: "Ada"}

	if literal.Table() != nil || literal.Exists() {
		t.Error("a literal reports a table or a stored row, and a terminal on it would write the zero row")
	}

	saved, err := literal.Save(context.Background(), grantForTenant("t-1"))
	if !errors.Is(err, ErrUnwired) {
		t.Fatalf("saving an unwired entity = %v, want ErrUnwired", err)
	}
	if saved {
		t.Error("saving an unwired entity reported that it wrote a row")
	}
	if !strings.Contains(err.Error(), "Table.New") {
		t.Errorf("error = %q, want it to name a way to make a wired row", err)
	}
}

// TestAValueCopyOfARowRefusesToWrite: copying an entity copies the pointer to
// its row, so the copy reaches the row of the entity it was copied from. A write
// through the copy would write that other entity's row with the copy's intent --
// the model has no way to read the copy's fields -- so every write refuses.
func TestAValueCopyOfARowRefusesToWrite(t *testing.T) {
	model, conn := newAccountModel()
	original, err := fromRecord(model, query.Record{"id": int64(7), "name": "Ada", "tenant_id": "t-1"})
	if err != nil {
		t.Fatalf("fromRecord: %v", err)
	}

	copied := *original
	copied.Name = "Grace"

	g := grantForTenant("t-1")
	if _, err := copied.Save(context.Background(), g); !errors.Is(err, ErrUnwired) {
		t.Errorf("Save on a copy = %v, want ErrUnwired", err)
	}
	if _, err := copied.Delete(context.Background(), g); !errors.Is(err, ErrUnwired) {
		t.Errorf("Delete on a copy = %v, want ErrUnwired", err)
	}
	if _, err := copied.Update(context.Background(), g, map[string]any{"name": "Grace"}); !errors.Is(err, ErrUnwired) {
		t.Errorf("Update on a copy = %v, want ErrUnwired", err)
	}
	if err := copied.Refresh(context.Background(), g); !errors.Is(err, ErrUnwired) {
		t.Errorf("Refresh on a copy = %v, want ErrUnwired", err)
	}
	if len(conn.sqls()) != 0 {
		t.Fatalf("a copy reached the connection: %v", conn.sqls())
	}

	// The original is untouched by all of it, and still writes.
	if _, err := original.Save(context.Background(), g); err != nil {
		t.Fatalf("Save on the original: %v", err)
	}
}

// TestNewTableRefusesAnEntityThatDoesNotEmbedTheModel: the row type is checked
// once, where the table is declared, rather than on the first row hydrated.
func TestNewTableRefusesAnEntityThatDoesNotEmbedTheModel(t *testing.T) {
	type nested struct{ account }

	cases := map[string]TableSpec{
		"no name":     {New: func() Entity { return new(account) }},
		"no new":      {Name: "accounts"},
		"nil row":     {Name: "accounts", New: func() Entity { return nil }},
		"nested":      {Name: "accounts", New: func() Entity { return new(nested) }},
		"global with": {Name: "accounts", New: func() Entity { return new(account) }, Global: true, TenantColumn: "team_id"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("NewTable accepted a spec that cannot work")
				}
			}()
			NewTable(spec)
		})
	}
}

// TestTheEmbeddedModelIsNotColumns.
//
// The model holds the row's bookkeeping. Walked as an embedded struct it would
// add whatever it exported to the insert, and an insert on an entity that once
// embedded a model with exported fields tried to write table, primary_key,
// entity and grammar alongside the two the developer declared.
//
// The columns of an entity are the fields the developer wrote, and nothing the
// model brought with it.
func TestTheEmbeddedModelIsNotColumns(t *testing.T) {
	model, _ := newAccountModel()
	instance, err := instanceOf(model, nil, false)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	columns := instance.GetAttributes()
	for want := range map[string]bool{"id": true, "name": true, "email": true, "tenant_id": true} {
		if _, ok := columns[want]; !ok {
			t.Errorf("the entity has no %q column", want)
		}
	}
	if len(columns) != 4 {
		t.Fatalf("the entity has %d columns, want its own four: %v", len(columns), columns)
	}
}

// grantForTenant is a grant good enough for the reads above: the tenant is what
// every statement is scoped by, and that is what these tests need it to carry.
func grantForTenant(tenant string) auth.Grant {
	return auth.SystemGrant("account.read", tenant)
}
