package model

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// account is the shape an application writes: its own struct, with the model
// embedded, so the entity and the model are one value.
type account struct {
	Model[account]

	ID       int64  `db:"id"`
	Name     string `db:"name"`
	Email    string `db:"email"`
	TenantID string `db:"tenant_id"`
}

func newAccountModel() (*Model[account], *testConnection) {
	conn := newTestConnection()
	return NewModel[account]("accounts", conn, newTestGrammar(), &testProcessor{conn: conn}), conn
}

// TestTheEntityIsTheModel.
//
// An embedded value has no way to name the value that embeds it, so the model
// cannot find its own entity by asking the compiler: in PHP $this answers this
// for free and in Go nothing does. The instance is built entity-first for that
// reason, and this test is the proof that the two are one allocation rather than
// two that happen to agree.
//
// If they were two, everything below would still compile and Save would write
// the zero row.
func TestTheEntityIsTheModel(t *testing.T) {
	model, _ := newAccountModel()

	instance, err := model.NewInstance(nil, false)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	entity := instance.Entity
	if entity == nil {
		t.Fatal("the instance has no entity")
	}

	// The model the caller holds has to be the one inside the entity. Comparing
	// the pointers is the whole assertion: two models with equal fields would
	// pass any check on their contents and fail every write.
	if inside := modelIn(entity, embeddedIndex[account]()); inside != instance {
		t.Fatalf("the model is not the one embedded in the entity: %p vs %p", inside, instance)
	}

	// Writing through the entity is writing through the model, which is what
	// makes user.Name = "..." followed by user.Save() mean anything.
	entity.Name = "Ada"
	if instance.Entity.Name != "Ada" {
		t.Error("a field set on the entity is not visible through the model")
	}

	// And the configuration reaches the entity, so the promoted methods find a
	// table and a connection rather than a zero model.
	if entity.GetTable() != "accounts" {
		t.Errorf("the entity's table is %q, want accounts", entity.GetTable())
	}
}

// TestHydrationWiresEveryRow: what the framework returns comes wired.
//
// A row that hydrated into an entity with a nil back pointer would be a row the
// caller can read and cannot save, and nothing about it would look wrong.
func TestHydrationWiresEveryRow(t *testing.T) {
	model, conn := newAccountModel()
	conn.queue(
		query.Record{"id": int64(1), "name": "Ada", "email": "ada@example.test", "tenant_id": "t-1"},
		query.Record{"id": int64(2), "name": "Grace", "email": "grace@example.test", "tenant_id": "t-1"},
	)

	rows, err := model.NewQuery().Get(context.Background(), grantForTenant("t-1"))
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
		// What Get hands back is the row, and the row is the model: the model
		// inside it has to be the one the framework filled, and its Entity has to
		// point back at the row rather than at a second allocation.
		inside := modelIn(row, embeddedIndex[account]())
		if inside == nil || inside.Entity != row {
			t.Errorf("row %d is not the model embedded in itself", i)
		}
		if row.GetTable() != "accounts" {
			t.Errorf("row %d reached the caller without its configuration", i)
		}
		if !row.Exists {
			t.Errorf("row %d came back from the database and does not say it exists", i)
		}
	}
	if rows[0].Name != "Ada" || rows[1].Name != "Grace" {
		t.Errorf("the rows hydrated as %q and %q", rows[0].Name, rows[1].Name)
	}
}

// TestAnEagerLoadIsReachableFromTheRowATerminalHandedBack.
//
// The eager load attaches what it matched to the model, and a terminal hands
// back the row. For the entity that embeds its model the two are one value, so
// the relation is reachable from what the caller holds -- which is the whole of
// what the embedding buys on this side.
func TestAnEagerLoadIsReachableFromTheRowATerminalHandedBack(t *testing.T) {
	model, conn := newAccountModel()
	child, _ := newAccountModel()
	loaded, err := child.NewFromBuilder(map[string]any{"id": int64(9), "name": "child"})
	if err != nil {
		t.Fatalf("NewFromBuilder: %v", err)
	}
	withPostsOn(model, &fakeRelation{
		table: "posts", foreign: "posts.account_id", local: "accounts.id",
		matched: map[any]any{int64(1): Collection[account]{loaded.Entity}},
	})
	conn.queue(query.Record{"id": int64(1), "name": "Ada", "tenant_id": "t-1"})

	rows, err := model.NewQuery().With("posts").Get(context.Background(), grantForTenant("t-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	posts, ok := Related[account, account](rows[0], "posts")
	if !ok {
		t.Fatal("the eager load is not reachable from the row the terminal returned")
	}
	if len(posts) != 1 || posts[0].ID != 9 {
		t.Fatalf("posts = %v, want the one row the relation matched", posts)
	}

	// And the lazy half, which is the same reach through a promoted method.
	if err := rows[0].Load(context.Background(), grantForTenant("t-1"), "posts"); err != nil {
		t.Fatalf("Load on the row: %v", err)
	}
}

// TestALiteralEntityIsNotWiredAndDoesNotPanic.
//
// This is the one difference a Laravel developer learns at this layer, so it has
// to be a sentence and not a stack trace. A struct written by hand has a zero
// model inside it: no connection, no back pointer. Calling a terminal on it says
// so, and names the way to make one.
func TestALiteralEntityIsNotWiredAndDoesNotPanic(t *testing.T) {
	literal := &account{Name: "Ada"}

	if ModelOf(literal) != nil {
		t.Error("a literal reports itself wired, and the terminal on it would write the zero row")
	}

	saved, err := literal.Save(context.Background(), grantForTenant("t-1"))
	if err == nil {
		t.Fatal("saving an unwired entity reported success")
	}
	if saved {
		t.Error("saving an unwired entity reported that it wrote a row")
	}
}

// TestAPlainEntityStillWorks: a T that does not embed Model[T] is the shape the
// relation machinery and most of this package's tests use, and it is not a lesser
// one -- the model is a value of its own beside the entity.
func TestAPlainEntityStillWorks(t *testing.T) {
	model, _ := newUserModel()

	instance, err := model.NewInstance(map[string]any{"name": "Ada"}, false)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if instance.Entity == nil {
		t.Fatal("a plain entity was not allocated")
	}
	if instance.Entity.Name != "Ada" {
		t.Errorf("the plain entity filled as %q", instance.Entity.Name)
	}
	if embeddedIndex[user]() != -1 {
		t.Error("user embeds Model[user], and this test is no longer about the plain shape")
	}
}

// TestTheEmbeddedModelIsNotColumns.
//
// Every field of Model[T] is exported, because a Go value has no subtype to
// override them in. Walked as an embedded struct they become columns, and an
// insert on an entity that embeds one tried to write table, primary_key, entity
// and grammar alongside the two the developer declared.
//
// The columns of an entity are the fields the developer wrote, and nothing the
// model brought with it.
func TestTheEmbeddedModelIsNotColumns(t *testing.T) {
	model, _ := newAccountModel()
	instance, err := model.NewInstance(nil, false)
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
