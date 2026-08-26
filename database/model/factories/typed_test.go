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

// TestMakeTakesNoGrantAndTouchesNothing is the assertion behind the signature.
//
// The model is built with a nil connection, so a factory that reached the
// database would panic rather than pass. That is the point: Make has to be
// provably offline, not documented as offline.
func TestMakeTakesNoGrantAndTouchesNothing(t *testing.T) {
	rows := newFactory().Count(3).Make()

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
	first := newFactory().Count(5).Seed(99).Make()
	second := newFactory().Count(5).Seed(99).Make()

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("row %d differs between runs of the same seed:\n  %+v\n  %+v", i, first[i], second[i])
		}
	}

	other := newFactory().Count(5).Seed(100).Make()
	if other[0] == first[0] {
		t.Error("two seeds made the same first row; the seed is not reaching the generator")
	}
}

// TestStatesRunInOrderAndTheLastOneWins pins the ordering, because a caller who
// adds a state later means it to win.
func TestStatesRunInOrderAndTheLastOneWins(t *testing.T) {
	row := newFactory().
		State(func(u *user) { u.Name = "first" }).
		State(func(u *user) { u.Name = "second" }).
		MakeOne()

	if row.Name != "second" {
		t.Errorf("Name = %q, want the later state to win", row.Name)
	}
}

// TestAFactoryIsAValue is the property that makes a shared factory safe: Count
// and State answer a new factory and leave the one they were called on alone.
func TestAFactoryIsAValue(t *testing.T) {
	base := newFactory()
	suspended := base.State(func(u *user) { u.Active = false })

	if !base.MakeOne().Active {
		t.Error("a state added to a derived factory reached the one it came from")
	}
	if suspended.MakeOne().Active {
		t.Error("the derived factory did not get the state")
	}
	if got := len(base.Count(7).Make()); len(base.Make()) != 1 || got != 7 {
		t.Errorf("Count mutated its receiver: base makes %d", len(base.Make()))
	}
}

// TestSequenceCyclesRatherThanStopping: three states over ten rows is ten rows.
func TestSequenceCyclesRatherThanStopping(t *testing.T) {
	rows := newFactory().Count(10).Sequence(
		func(u *user) { u.Name = "a" },
		func(u *user) { u.Name = "b" },
		func(u *user) { u.Name = "c" },
	).Make()

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
	rows := newFactory().Count(4).AfterMaking(func(u *user) {
		seen++
		u.Name = strings.ToUpper(u.Name)
	}).Make()

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
