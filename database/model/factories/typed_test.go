package factories_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/model/factories"
	"github.com/arandu-io/hesape/faker"
)

// user is the entity the tests build. Ordinary on purpose.
type user struct {
	model.Model

	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Active bool   `db:"active"`
}

var users = tableOf("users", func() model.Entity { return new(user) })

// columns is a user's columns as plain values, for comparing two rows without
// comparing the models inside them.
type columns struct {
	ID     int64
	Name   string
	Email  string
	Active bool
}

func columnsOf(u *user) columns { return columns{u.ID, u.Name, u.Email, u.Active} }

func definition(f faker.Faker) user {
	return user{Name: f.Name(), Email: f.Unique().Email(), Active: true}
}

// userFactory is the typed factory as it is generated beside an entity: the
// definition answers the struct, the states take a pointer to it, and every
// method is one forward to the core with the conversion at the boundary.
type userFactory struct{ f *factories.Factory }

func newUserFactory(db model.DB, define func(faker.Faker) user) *userFactory {
	return &userFactory{f: factories.New(users.Query(db), func(f faker.Faker, row model.Entity) {
		*row.(*user) = define(f)
	})}
}

func (x *userFactory) Count(n int) *userFactory     { return &userFactory{f: x.f.Count(n)} }
func (x *userFactory) Seed(seed int64) *userFactory { return &userFactory{f: x.f.Seed(seed)} }

func (x *userFactory) State(fn func(*user)) *userFactory {
	return &userFactory{f: x.f.State(func(row model.Entity) { fn(row.(*user)) })}
}

func (x *userFactory) Sequence(states ...func(*user)) *userFactory {
	adapted := make([]func(model.Entity), len(states))
	for i, state := range states {
		adapted[i] = func(row model.Entity) { state(row.(*user)) }
	}
	return &userFactory{f: x.f.Sequence(adapted...)}
}

func (x *userFactory) AfterMaking(fn func(*user)) *userFactory {
	return &userFactory{f: x.f.AfterMaking(func(row model.Entity) { fn(row.(*user)) })}
}

func usersOf(rows model.Rows) []*user {
	out := make([]*user, len(rows))
	for i, row := range rows {
		out[i] = row.(*user)
	}
	return out
}

func (x *userFactory) Make() ([]*user, error) {
	rows, err := x.f.Make()
	return usersOf(rows), err
}

func (x *userFactory) MakeOne() (*user, error) {
	row, err := x.f.MakeOne()
	u, _ := row.(*user)
	return u, err
}

func (x *userFactory) Create(ctx context.Context, g auth.Grant) ([]*user, error) {
	rows, err := x.f.Create(ctx, g)
	return usersOf(rows), err
}

func newFactory() *userFactory { return newUserFactory(sqliteDB{}, definition) }

// made and madeOne are Make and MakeOne with the error read, so that the tests
// below say what they are about rather than what they had to check first.
func made(t *testing.T, f *userFactory) []*user {
	t.Helper()
	rows, err := f.Make()
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	return rows
}

func madeOne(t *testing.T, f *userFactory) *user {
	t.Helper()
	row, err := f.MakeOne()
	if err != nil {
		t.Fatalf("MakeOne: %v", err)
	}
	return row
}

// TestMakeTakesNoGrantAndTouchesNothing is the assertion behind the signature.
//
// The table is queried through a DB with no connection behind it, so a factory
// that reached the database would panic rather than pass. That is the point: Make has to be
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
			t.Errorf("row %d came back empty: %+v", i, columnsOf(row))
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
		if columnsOf(first[i]) != columnsOf(second[i]) {
			t.Fatalf("row %d differs between runs of the same seed:\n  %+v\n  %+v", i, columnsOf(first[i]), columnsOf(second[i]))
		}
	}

	other := made(t, newFactory().Count(5).Seed(100))
	if columnsOf(other[0]) == columnsOf(first[0]) {
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
	conn := newRecordingConnection()
	_, err := newUserFactory(sqliteDB{conn}, definition).Create(context.Background(), auth.Grant{})
	if !errors.Is(err, model.ErrNoTenant) {
		t.Fatalf("Create under a grant with no tenant = %v, want ErrNoTenant", err)
	}
	if conn.inserts() != 0 {
		t.Fatalf("it ran %d inserts anyway", conn.inserts())
	}
}

// TestAMadeRowCanBeSaved is what Make handing back the row is for.
//
// The definition assigns a whole user, and a user carries its model inside it.
// Assigning the definition over the row would leave the model with no
// connection, so the row would come back readable and unsavable -- which is the
// shape of a value nobody built.
func TestAMadeRowCanBeSaved(t *testing.T) {
	conn := newRecordingConnection()

	row, err := newUserFactory(sqliteDB{conn}, definition).MakeOne()
	if err != nil {
		t.Fatalf("MakeOne: %v", err)
	}
	if row.Name == "" {
		t.Error("the definition did not reach the made row")
	}
	if row.Table() != users {
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

	rows, err := newUserFactory(sqliteDB{conn}, definition).Create(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	row := rows[0]
	if !row.Exists() || row.ID == 0 {
		t.Fatalf("a created row does not say it is stored: exists %v, id %d", row.Exists(), row.ID)
	}
	if conn.inserts() != 1 {
		t.Fatalf("Create ran %d inserts, want 1", conn.inserts())
	}

	row.Name = "Grace"
	if _, err := row.Save(context.Background(), auth.SystemGrant("write", "acme")); err != nil {
		t.Fatalf("Save on a created row: %v", err)
	}
}

// The relation halves. They need a connection, because creating a parent and
// then its children is two statements and the point is the order they run in.

type post struct {
	model.Model

	ID     int64  `db:"id"`
	UserID int64  `db:"user_id"`
	Title  string `db:"title"`
}

var posts = tableOf("posts", func() model.Entity { return new(post) })

func postsOn(db model.DB) *factories.Factory {
	return factories.New(posts.Query(db), func(f faker.Faker, row model.Entity) {
		row.(*post).Title = f.Sentence(3)
	})
}

func usersOn(db model.DB) *factories.Factory {
	return factories.New(users.Query(db), func(f faker.Faker, row model.Entity) {
		*row.(*user) = definition(f)
	})
}

// TestHasCreatesChildrenForEveryParent, and creates them after, because the row
// they name has no identifier until it has been inserted.
func TestHasCreatesChildrenForEveryParent(t *testing.T) {
	conn := newRecordingConnection()
	db := sqliteDB{conn}

	linked := 0
	created, err := usersOn(db).Count(2).Has(postsOn(db).Count(3), func(u, p model.Entity) {
		linked++
		p.(*post).UserID = u.(*user).ID
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
	if conn.first() != "users" {
		t.Errorf("the first table written was %q, want users", conn.first())
	}
}

// TestForParentCreatesOneParentBeforeAnyChild is the inverse, and the ordering
// is the whole of it.
func TestForParentCreatesOneParentBeforeAnyChild(t *testing.T) {
	conn := newRecordingConnection()
	db := sqliteDB{conn}

	created, err := postsOn(db).Count(3).ForParent(usersOn(db), func(p, u model.Entity) {
		p.(*post).UserID = u.(*user).ID
	}).Create(context.Background(), auth.SystemGrant("write", "acme"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(created) != 3 {
		t.Fatalf("Create gave %d children, want 3", len(created))
	}
	for i, row := range created {
		if row.(*post).UserID == 0 {
			t.Errorf("child %d names no parent", i)
		}
	}
	// One parent, three children.
	if got := conn.inserts(); got != 4 {
		t.Errorf("it ran %d inserts, want 4 -- one parent for all three", got)
	}
	if conn.first() != "users" {
		t.Errorf("the first table written was %q, want users -- the parent has to exist first", conn.first())
	}
}

// tenanted is a row with a tenant column, which the factory never fills.
type tenanted struct {
	model.Model

	ID       int64  `db:"id"`
	TenantID string `db:"tenant_id"`
	Name     string `db:"name"`
}

// TestCreateHandsBackTheGrantsTenant: the rows are inserted with the Grant's
// tenant, and the rows Create returns carry it, whatever the definition said.
func TestCreateHandsBackTheGrantsTenant(t *testing.T) {
	conn := newRecordingConnection()
	things := tableOf("things", func() model.Entity { return new(tenanted) })
	f := factories.New(things.Query(sqliteDB{conn}), func(f faker.Faker, row model.Entity) {
		*row.(*tenanted) = tenanted{Name: f.Name(), TenantID: "globex"}
	})
	g := auth.SystemGrant("write", "acme")

	rows, err := f.Count(3).Create(context.Background(), g)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i, row := range rows {
		if got := row.(*tenanted).TenantID; got != auth.Tenant(g) {
			t.Fatalf("row %d: TenantID = %q, want %q", i, got, auth.Tenant(g))
		}
	}
}

// TestUniqueHoldsAcrossTheRowsOfOneRun: the definition runs once per row and
// asks Unique each time, over a domain barely larger than the run.
func TestUniqueHoldsAcrossTheRowsOfOneRun(t *testing.T) {
	names := make([]string, 60)
	for i := range names {
		names[i] = "name-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	f := newUserFactory(sqliteDB{}, func(f faker.Faker) user {
		return user{Name: f.Unique().Pick(names...)}
	})

	rows := made(t, f.Count(50))
	seen := map[string]bool{}
	for i, row := range rows {
		if seen[row.Name] {
			t.Fatalf("row %d repeated %q", i, row.Name)
		}
		seen[row.Name] = true
	}
}

// TestNewRefusesWhatCannotWork: a factory is declared once, and a missing query
// or definition stops the run there rather than at the first row.
func TestNewRefusesWhatCannotWork(t *testing.T) {
	for name, build := range map[string]func(){
		"no query":      func() { factories.New(nil, func(faker.Faker, model.Entity) {}) },
		"no definition": func() { factories.New(users.Query(sqliteDB{}), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New accepted it")
				}
			}()
			build()
		})
	}
}
