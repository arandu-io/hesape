package factories_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/model/factories"
	"github.com/arandu-io/hesape/faker"
)

// user is the entity the tests build. Ordinary on purpose.
type user struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Active bool   `db:"active"`
}

func definition(f faker.Faker) user {
	return user{Name: f.Name(), Email: f.Unique().Email(), Active: true}
}

func newFactory() *factories.Factory[user] {
	return factories.For(model.NewModel[user]("users", nil, nil, nil), definition)
}

// made and madeOne are Make and MakeOne with the error read, so that the tests
// below say what they are about rather than what they had to check first.
func made(t *testing.T, f *factories.Factory[user]) []*user {
	t.Helper()
	rows, err := f.Make()
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	return rows
}

func madeOne(t *testing.T, f *factories.Factory[user]) *user {
	t.Helper()
	row, err := f.MakeOne()
	if err != nil {
		t.Fatalf("MakeOne: %v", err)
	}
	return row
}

// TestMakeTakesNoGrantAndTouchesNothing is the assertion behind the signature.
//
// The model is built with a nil connection, so a factory that reached the
// database would panic rather than pass. That is the point: Make has to be
// provably offline, not documented as offline.
func TestMakeTakesNoGrantAndTouchesNothing(t *testing.T) {
	rows, err := newFactory().Count(3).Make()
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("Make gave %d rows, want 3", len(rows))
	}
	for i, row := range rows {
		if row.Name == "" || !strings.Contains(row.Email, "@") {
			t.Errorf("row %d came back empty: %+v", i, row)
		}
		if !row.Active {
			t.Errorf("row %d did not get the definition's default", i)
		}
	}
}

// TestTheSameSeedMakesTheSameRows is what a recorded seed buys.
func TestTheSameSeedMakesTheSameRows(t *testing.T) {
	first := made(t, newFactory().Count(5).Seed(99))
	second := made(t, newFactory().Count(5).Seed(99))

	for i := range first {
		if *first[i] != *second[i] {
			t.Fatalf("row %d differs between runs of the same seed:\n  %+v\n  %+v", i, first[i], second[i])
		}
	}

	other := made(t, newFactory().Count(5).Seed(100))
	if *other[0] == *first[0] {
		t.Error("two seeds made the same first row; the seed is not reaching the generator")
	}
}

// TestStatesRunInOrderAndTheLastOneWins pins the ordering, because a caller who
// adds a state later means it to win.
func TestStatesRunInOrderAndTheLastOneWins(t *testing.T) {
	row := madeOne(t, newFactory().
		State(func(u *user) { u.Name = "first" }).
		State(func(u *user) { u.Name = "second" }))

	if row.Name != "second" {
		t.Errorf("Name = %q, want the later state to win", row.Name)
	}
}

// TestAFactoryIsAValue is the property that makes a shared factory safe: Count
// and State answer a new factory and leave the one they were called on alone.
func TestAFactoryIsAValue(t *testing.T) {
	base := newFactory()
	suspended := base.State(func(u *user) { u.Active = false })

	if !madeOne(t, base).Active {
		t.Error("a state added to a derived factory reached the one it came from")
	}
	if madeOne(t, suspended).Active {
		t.Error("the derived factory did not get the state")
	}
	if got := len(made(t, base.Count(7))); len(made(t, base)) != 1 || got != 7 {
		t.Errorf("Count mutated its receiver: base makes %d", len(made(t, base)))
	}
}

// TestSequenceCyclesRatherThanStopping: three states over ten rows is ten rows.
func TestSequenceCyclesRatherThanStopping(t *testing.T) {
	rows := made(t, newFactory().Count(10).Sequence(
		func(u *user) { u.Name = "a" },
		func(u *user) { u.Name = "b" },
		func(u *user) { u.Name = "c" },
	))

	want := []string{"a", "b", "c", "a", "b", "c", "a", "b", "c", "a"}
	for i, row := range rows {
		if row.Name != want[i] {
			t.Fatalf("row %d = %q, want %q", i, row.Name, want[i])
		}
	}
}

// TestAfterMakingRunsOnEveryRow.
func TestAfterMakingRunsOnEveryRow(t *testing.T) {
	seen := 0
	rows := made(t, newFactory().Count(4).AfterMaking(func(u *user) {
		seen++
		u.Name = strings.ToUpper(u.Name)
	}))

	if seen != 4 {
		t.Errorf("AfterMaking ran %d times, want 4", seen)
	}
	for i, row := range rows {
		if row.Name != strings.ToUpper(row.Name) {
			t.Errorf("row %d did not go through the callback: %q", i, row.Name)
		}
	}
}

// TestCreateRefusesAGrantWithNoTenant: the factory is not a way around the
// policy that guards the table, and the first thing that proves it is the grant
// that carries nothing.
func TestCreateRefusesAGrantWithNoTenant(t *testing.T) {
	_, err := newFactory().Create(context.Background(), auth.Grant{})
	if err == nil {
		t.Fatal("Create ran under a grant with no tenant")
	}
}

// member is the shape the model package tells an application to write: its own
// struct with the model embedded, so the row and the model are one value.
type member struct {
	model.Model[member]

	ID   int64  `db:"id"`
	Name string `db:"name"`
}

func memberFactory(conn *recordingConnection) *factories.Factory[member] {
	return factories.For(newModelOn[member](conn, "members"), func(f faker.Faker) member {
		return member{Name: f.Name()}
	})
}

// TestAMadeRowCanBeSaved is what Make handing back the row is for.
//
// The definition answers a member, and a member carries a zero model inside it.
// Building the row and assigning the definition over it left the model with no
// connection and no back pointer, so the row came back readable and unsavable --
// which is the shape of a value nobody built.
func TestAMadeRowCanBeSaved(t *testing.T) {
	conn := newRecordingConnection()

	row, err := memberFactory(conn).MakeOne()
	if err != nil {
		t.Fatalf("MakeOne: %v", err)
	}
	if row.Name == "" {
		t.Error("the definition did not reach the made row")
	}
	if model.ModelOf(row) == nil {
		t.Fatal("a made row does not carry the model it embeds")
	}
	if conn.inserts() != 0 {
		t.Errorf("Make ran %d inserts", conn.inserts())
	}

	row.Name = "Ada"
	saved, err := row.Save(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("Save on a made row: %v", err)
	}
	if !saved {
		t.Error("Save on a made row reported that it wrote nothing")
	}
	if conn.inserts() != 1 {
		t.Errorf("Save ran %d inserts, want 1", conn.inserts())
	}
}

// TestACreatedRowComesBackWired: what Create returns has to be savable again,
// for the same reason -- the caller has one row and changes it.
func TestACreatedRowComesBackWired(t *testing.T) {
	conn := newRecordingConnection()

	row, err := memberFactory(conn).CreateOne(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	if model.ModelOf(row) == nil {
		t.Fatal("a created row does not carry the model it embeds")
	}
	if conn.inserts() != 1 {
		t.Fatalf("CreateOne ran %d inserts, want 1", conn.inserts())
	}

	row.Name = "Grace"
	if _, err := row.Save(context.Background(), auth.SystemGrant("write", "acme")); err != nil {
		t.Fatalf("Save on a created row: %v", err)
	}
}

// The relation halves. They need a connection, because creating a parent and
// then its children is two statements and the point is the order they run in.

type post struct {
	ID     int64  `db:"id"`
	UserID int64  `db:"user_id"`
	Title  string `db:"title"`
}

// TestHasCreatesChildrenForEveryParent, and creates them after, because the row
// they name has no identifier until it has been inserted.
func TestHasCreatesChildrenForEveryParent(t *testing.T) {
	conn := newRecordingConnection()
	users := factories.For(newModelOn[user](conn, "users"), definition)
	posts := factories.For(newModelOn[post](conn, "posts"), func(f faker.Faker) post {
		return post{Title: f.Sentence(3)}
	})

	linked := 0
	created, err := factories.Has(users.Count(2), posts.Count(3), func(u *user, p *post) {
		linked++
		p.UserID = u.ID
	}).Create(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(created) != 2 {
		t.Fatalf("Create gave %d parents, want 2", len(created))
	}
	if linked != 6 {
		t.Errorf("the link ran %d times, want 6 -- three children for each of two parents", linked)
	}

	// Two parents and six children, and the parent of a batch is inserted before
	// its children are.
	if got := conn.inserts(); got != 8 {
		t.Errorf("it ran %d inserts, want 8", got)
	}
}

// TestForParentCreatesOneParentBeforeAnyChild is the inverse, and the ordering
// is the whole of it.
func TestForParentCreatesOneParentBeforeAnyChild(t *testing.T) {
	conn := newRecordingConnection()
	users := factories.For(newModelOn[user](conn, "users"), definition)
	posts := factories.For(newModelOn[post](conn, "posts"), func(f faker.Faker) post {
		return post{Title: f.Sentence(3)}
	})

	created, err := factories.ForParent(posts.Count(3), users, func(p *post, u *user) {
		p.UserID = u.ID
	}).Create(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(created) != 3 {
		t.Fatalf("Create gave %d children, want 3", len(created))
	}
	// One parent, three children.
	if got := conn.inserts(); got != 4 {
		t.Errorf("it ran %d inserts, want 4 -- one parent for all three", got)
	}
	if conn.first() != "users" {
		t.Errorf("the first table written was %q, want users -- the parent has to exist first", conn.first())
	}
}
