package factories

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/faker"
)

// Factory builds rows of one table, for tests and for seeding.
//
// It is one type for every table: the rows it makes are entities, and the typed
// factory an application calls -- the one whose definition returns its own
// struct and whose states take a pointer to it -- is generated beside the
// entity and converts at the boundary. This is the part that does the work, and
// it is compiled once.
//
// # Every method returns a new Factory
//
// A factory is a value a caller keeps and reuses, so Count(10) has to answer a
// factory of ten and leave the one it was called on alone. Otherwise the second
// caller of a shared factory inherits the first caller's states, and the bug is
// a row that is wrong in one test because of another.
type Factory struct {
	query  *model.Builder
	define func(faker.Faker, model.Entity)

	// embedded is where model.Model sits in the entity, so that a definition
	// assigning the whole struct cannot take the row's wiring with it.
	embedded int

	count int
	seed  int64

	states        []func(model.Entity)
	sequence      []func(model.Entity)
	afterMaking   []func(model.Entity)
	afterCreating []func(context.Context, auth.Grant, model.Entity) error

	// resolvers run before the rows are built, and each answers a state.
	//
	// It is what ForParent needs: the parent has to exist before a child can
	// name it, so the row that names it cannot be built until the statement
	// that creates the parent has run.
	resolvers []func(context.Context, auth.Grant) (func(model.Entity), error)
}

// DefaultSeed is the seed a factory uses when none is given.
//
// It is fixed rather than drawn from the clock, and that is the decision: a
// factory that generates different rows on every run produces a test that fails
// on Tuesdays. A caller that wants variation asks for it by name, with Seed.
const DefaultSeed int64 = 1

// New returns a factory over the table q queries, with define as its default
// state.
//
// The rows are made on q's table and connection, as q.NewModelInstance makes
// them, so a made row is a row that can be saved. define receives a Faker and an
// empty row, and fills the row: it may set fields one by one or assign the whole
// struct, and the row's embedded model.Model is put back afterwards either way.
// Everything the caller cares about is set afterwards, by a state; everything
// else the definition fills.
//
// It panics when q or define is nil, or when q's rows do not embed model.Model,
// because a factory is declared once, where a panic stops the test run before
// anything is built.
func New(q *model.Builder, define func(f faker.Faker, row model.Entity)) *Factory {
	if q == nil || define == nil {
		panic("factories: New needs the query and the definition")
	}
	sample, err := q.NewModelInstance(nil)
	if err != nil {
		panic(fmt.Sprintf("factories: New: %v", err))
	}
	return &Factory{query: q, define: define, embedded: embeddedIndex(sample), count: 1, seed: DefaultSeed}
}

// modelType is model.Model's reflect.Type, compared against an entity's fields.
var modelType = reflect.TypeFor[model.Model]()

// embeddedIndex is where model.Model is embedded in the entity e points at.
func embeddedIndex(e model.Entity) int {
	t := reflect.TypeOf(e).Elem()
	for i := range t.NumField() {
		if f := t.Field(i); f.Anonymous && f.Type == modelType {
			return i
		}
	}
	panic(fmt.Sprintf("factories: %s does not embed model.Model", t))
}

// clone is what keeps a factory a value rather than a builder somebody mutated.
func (f *Factory) clone() *Factory {
	out := *f
	out.states = slices.Clone(f.states)
	out.sequence = slices.Clone(f.sequence)
	out.afterMaking = slices.Clone(f.afterMaking)
	out.afterCreating = slices.Clone(f.afterCreating)
	out.resolvers = slices.Clone(f.resolvers)
	return &out
}

// Count returns a factory that makes n rows. A negative n makes none.
func (f *Factory) Count(n int) *Factory {
	out := f.clone()
	out.count = max(n, 0)
	return out
}

// Seed returns a factory whose generator starts from seed.
//
// The seed is what makes a failure reproducible. A test that prints it can be
// re-run against the same rows; one that does not has a failure nobody can get
// back.
func (f *Factory) Seed(seed int64) *Factory {
	out := f.clone()
	out.seed = seed
	return out
}

// State returns a factory that applies fn to every row after the definition.
//
// States run in the order they were added, so a later one overrides an earlier
// one -- which is what a caller means by adding it later.
func (f *Factory) State(fn func(row model.Entity)) *Factory {
	out := f.clone()
	out.states = append(out.states, fn)
	return out
}

// Sequence returns a factory that cycles through states, one per row.
//
// Three states over ten rows gives 1,2,3,1,2,3,1,2,3,1 -- the cycle repeats
// rather than stopping, because a caller asking for ten rows of three kinds
// means ten rows.
func (f *Factory) Sequence(states ...func(row model.Entity)) *Factory {
	out := f.clone()
	out.sequence = append(out.sequence, states...)
	return out
}

// AfterMaking returns a factory that runs fn on each row once it is built.
//
// It takes no context and no Grant because Make touches nothing.
func (f *Factory) AfterMaking(fn func(row model.Entity)) *Factory {
	out := f.clone()
	out.afterMaking = append(out.afterMaking, fn)
	return out
}

// AfterCreating returns a factory that runs fn on each row once it is stored.
//
// It takes the context and the Grant because whatever it does next is another
// statement, and a statement here is a statement like any other. The row it
// receives is the stored one, so the key the database generated is on it.
func (f *Factory) AfterCreating(fn func(ctx context.Context, g auth.Grant, row model.Entity) error) *Factory {
	out := f.clone()
	out.afterCreating = append(out.afterCreating, fn)
	return out
}

// Make returns the rows without storing any of them.
//
// It takes no Grant and no context, and that is a decision rather than an
// omission: nothing here reaches the database, so a Grant would be a parameter
// that authorizes nothing. Asking for one would teach the opposite of what the
// Grant means everywhere else in this collection.
//
// What comes back is what every terminal hands back -- rows of the table, wired
// to its connection, so a made row is a row that can then be saved. The error is
// the model's: a value that does not fit the field it names fails here rather
// than at the statement.
//
// Every row of one run is built with one Faker, so a value the definition asks
// of f.Unique() is not repeated across the rows. The next run starts a fresh
// Faker, with an empty memory.
func (f *Factory) Make() (model.Rows, error) {
	fake := faker.New(f.seed)
	out := make(model.Rows, 0, f.count)
	for i := range f.count {
		row, err := f.query.NewModelInstance(nil)
		if err != nil {
			return nil, err
		}

		// The definition may assign the whole struct, and the struct carries the
		// row's model: assigning over it would leave the row with no connection
		// to save through, which is the error a hand-written literal gets. So
		// the model is put back once the columns have landed.
		v := reflect.ValueOf(row).Elem()
		wiring := reflect.New(modelType).Elem()
		wiring.Set(v.Field(f.embedded))
		f.define(fake, row)
		v.Field(f.embedded).Set(wiring)

		for _, state := range f.states {
			state(row)
		}
		if n := len(f.sequence); n > 0 {
			f.sequence[i%n](row)
		}
		for _, after := range f.afterMaking {
			after(row)
		}
		out = append(out, row)
	}
	return out, nil
}

// MakeOne returns one row, whatever Count says.
func (f *Factory) MakeOne() (model.Entity, error) {
	rows, err := f.Count(1).Make()
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

// saver is the one method of a row Create calls, promoted onto every entity
// from its embedded model.
type saver interface {
	Save(ctx context.Context, g auth.Grant) (bool, error)
}

// Create stores the rows and returns them.
//
// It takes the Grant that every write in this collection takes, and the tenant
// comes off it: a factory is not a way around the policy that guards the table.
func (f *Factory) Create(ctx context.Context, g auth.Grant) (model.Rows, error) {
	// Anything that has to exist before these rows do -- a parent a child names
	// -- runs here, and each answers a state the rows are then built with.
	resolved := f
	for _, resolve := range f.resolvers {
		state, err := resolve(ctx, g)
		if err != nil {
			return nil, err
		}
		resolved = resolved.State(state)
	}

	rows, err := resolved.Make()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, err := row.(saver).Save(ctx, g); err != nil {
			return nil, err
		}
		for _, after := range resolved.afterCreating {
			if err := after(ctx, g, row); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

// CreateOne stores one row, whatever Count says.
func (f *Factory) CreateOne(ctx context.Context, g auth.Grant) (model.Entity, error) {
	created, err := f.Count(1).Create(ctx, g)
	if err != nil {
		return nil, err
	}
	return created[0], nil
}

// Has returns a factory that creates children for every row it creates.
//
//	users.Count(50).Has(posts.Count(5), func(u, p model.Entity) {
//		p.(*Post).UserID = u.(*User).ID
//	})
//
// link is the caller's, and it is what keeps the foreign key a field the
// compiler checks: inferring it would mean naming it in a string and setting it
// by reflection, and a field renamed in the struct would still compile and
// quietly stop being set.
//
// The children are created after the parent, once per parent, because the row
// they name does not have its identifier until the statement that inserts it
// has run.
func (f *Factory) Has(child *Factory, link func(parent, child model.Entity)) *Factory {
	return f.AfterCreating(func(ctx context.Context, g auth.Grant, created model.Entity) error {
		_, err := child.State(func(c model.Entity) { link(created, c) }).Create(ctx, g)
		return err
	})
}

// ForParent returns a factory whose rows belong to a parent it creates first.
//
// It is the inverse of Has, and the inverse matters: a post needs a user before
// it can name one, so the parent is created once, before any child row is built,
// and every child names that one.
//
//	posts.Count(3).ForParent(users, func(p, u model.Entity) {
//		p.(*Post).UserID = u.(*User).ID
//	})
func (f *Factory) ForParent(parent *Factory, link func(child, parent model.Entity)) *Factory {
	out := f.clone()
	out.resolvers = append(out.resolvers, func(ctx context.Context, g auth.Grant) (func(model.Entity), error) {
		created, err := parent.CreateOne(ctx, g)
		if err != nil {
			return nil, err
		}
		return func(c model.Entity) { link(c, created) }, nil
	})
	return out
}
