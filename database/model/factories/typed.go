package factories

import (
	"context"
	"slices"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/faker"
)

// Factory builds rows of one entity type.
//
// It is the public way to make test and seed data, and it is typed all the way
// through: the definition returns a T, a state takes a *T, and what comes back
// is the row -- a *T, the same value a query terminal hands back. Nothing here
// takes a map of strings, and a column that does not exist on the entity does
// not compile rather than being dropped at run time.
//
//	var Users = model.NewModel[User]("users", conn, grammar, processor)
//
//	func UserFactory() *factories.Factory[User] {
//		return factories.For(Users, func(f faker.Faker) User {
//			return User{Name: f.Name(), Email: f.Unique().Email()}
//		})
//	}
//
//	users, err := UserFactory().Count(10).Create(ctx, g)
//
// # Every method returns a new Factory
//
// A factory is a value a caller keeps and reuses, so Count(10) has to answer a
// factory of ten and leave the one it was called on alone. Otherwise the second
// caller of a shared factory inherits the first caller's states, and the bug is
// a row that is wrong in one test because of another.
type Factory[T any] struct {
	model  *model.Model[T]
	define func(faker.Faker) T

	count int
	seed  int64

	states        []func(*T)
	sequence      []func(*T)
	afterMaking   []func(*T)
	afterCreating []func(context.Context, auth.Grant, *T) error

	// resolvers run before the rows are built, and each answers a state.
	//
	// It is what ForParent needs: the parent has to exist before a child can
	// name it, so the row that names it cannot be built until the statement
	// that creates the parent has run.
	resolvers []func(context.Context, auth.Grant) (func(*T), error)
}

// DefaultSeed is the seed a factory uses when none is given.
//
// It is fixed rather than drawn from the clock, and that is the decision: a
// factory that generates different rows on every run produces a test that fails
// on Tuesdays. A caller that wants variation asks for it by name, with Seed.
const DefaultSeed int64 = 1

// For returns a factory over m, with define as its default state.
//
// define receives a Faker and returns the row it would make with nothing else
// said about it. Everything the caller cares about is set afterwards, by a
// state; everything else the definition fills.
func For[T any](m *model.Model[T], define func(faker.Faker) T) *Factory[T] {
	return &Factory[T]{model: m, define: define, count: 1, seed: DefaultSeed}
}

// clone is what keeps a factory a value rather than a builder somebody mutated.
func (f *Factory[T]) clone() *Factory[T] {
	out := *f
	out.states = slices.Clone(f.states)
	out.sequence = slices.Clone(f.sequence)
	out.afterMaking = slices.Clone(f.afterMaking)
	out.afterCreating = slices.Clone(f.afterCreating)
	out.resolvers = slices.Clone(f.resolvers)
	return &out
}

// Count returns a factory that makes n rows.
func (f *Factory[T]) Count(n int) *Factory[T] {
	out := f.clone()
	if n < 0 {
		n = 0
	}
	out.count = n
	return out
}

// Seed returns a factory whose generator starts from seed.
//
// The seed is what makes a failure reproducible. A test that prints it can be
// re-run against the same rows; one that does not has a failure nobody can get
// back.
func (f *Factory[T]) Seed(seed int64) *Factory[T] {
	out := f.clone()
	out.seed = seed
	return out
}

// State returns a factory that applies fn to every row after the definition.
//
// States run in the order they were added, so a later one overrides an earlier
// one -- which is what a caller means by adding it later.
func (f *Factory[T]) State(fn func(*T)) *Factory[T] {
	out := f.clone()
	out.states = append(out.states, fn)
	return out
}

// Sequence returns a factory that cycles through states, one per row.
//
// Three states over ten rows gives 1,2,3,1,2,3,1,2,3,1 -- the cycle repeats
// rather than stopping, because a caller asking for ten rows of three kinds
// means ten rows.
func (f *Factory[T]) Sequence(states ...func(*T)) *Factory[T] {
	out := f.clone()
	out.sequence = append(out.sequence, states...)
	return out
}

// AfterMaking returns a factory that runs fn on each row once it is built.
//
// It takes no context and no Grant because Make touches nothing.
func (f *Factory[T]) AfterMaking(fn func(*T)) *Factory[T] {
	out := f.clone()
	out.afterMaking = append(out.afterMaking, fn)
	return out
}

// AfterCreating returns a factory that runs fn on each row once it is stored.
//
// It takes the context and the Grant because whatever it does next is another
// statement, and a statement here is a statement like any other. The row it
// receives is the stored one, so the key the database generated is on it.
func (f *Factory[T]) AfterCreating(fn func(context.Context, auth.Grant, *T) error) *Factory[T] {
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
// What comes back is what every terminal hands back -- the rows, built through
// the model, so a made row is a row that can then be saved. The error is the
// model's: a definition whose value does not fit the field it names fails here
// rather than at the statement.
func (f *Factory[T]) Make() ([]*T, error) {
	built, err := f.make()
	if err != nil {
		return nil, err
	}
	out := make([]*T, 0, len(built))
	for _, instance := range built {
		out = append(out, instance.Entity)
	}
	return out, nil
}

// make is Make with the models still in hand, which is what Create needs: a row
// reaches its model only when T embeds Model[T], and the storing has to work for
// both shapes.
func (f *Factory[T]) make() ([]*model.Model[T], error) {
	fake := faker.New(f.seed)
	out := make([]*model.Model[T], 0, f.count)
	for i := range f.count {
		instance, err := f.model.NewInstance(nil, false)
		if err != nil {
			return nil, err
		}

		// The definition answers a T, and a T that embeds Model[T] carries a
		// zero model inside it. Assigning the whole struct overwrites the model
		// this instance IS -- for that shape the two are one allocation -- so
		// the model is put back once the columns have landed. Without this the
		// row comes back with no connection and no back pointer, and storing it
		// fails with the error a hand-written literal gets.
		//
		// For a T that does not embed one the model is a value beside the row,
		// nothing overwrites it, and the last line writes back what it read.
		row := instance.Entity
		wiring := *instance
		*row = f.define(fake)
		*instance = wiring

		for _, state := range f.states {
			state(row)
		}
		if n := len(f.sequence); n > 0 {
			f.sequence[i%n](row)
		}
		for _, after := range f.afterMaking {
			after(row)
		}
		out = append(out, instance)
	}
	return out, nil
}

// MakeOne returns one row, whatever Count says.
func (f *Factory[T]) MakeOne() (*T, error) {
	rows, err := f.Count(1).Make()
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

// Create stores the rows and returns them.
//
// It takes the Grant that every write in this collection takes, and the tenant
// comes off it: a factory is not a way around the policy that guards the table.
func (f *Factory[T]) Create(ctx context.Context, g auth.Grant) ([]*T, error) {
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

	built, err := resolved.make()
	if err != nil {
		return nil, err
	}
	out := make([]*T, 0, len(built))

	for _, instance := range built {
		if _, err := instance.Save(ctx, g); err != nil {
			return nil, err
		}
		for _, after := range resolved.afterCreating {
			if err := after(ctx, g, instance.Entity); err != nil {
				return nil, err
			}
		}
		out = append(out, instance.Entity)
	}
	return out, nil
}

// CreateOne stores one row, whatever Count says.
func (f *Factory[T]) CreateOne(ctx context.Context, g auth.Grant) (*T, error) {
	created, err := f.Count(1).Create(ctx, g)
	if err != nil {
		return nil, err
	}
	return created[0], nil
}

// Has returns a factory that creates children for every row it creates.
//
// It is a function rather than a method because Go has no generic method, and
// the child is of another type:
//
//	users, err := factories.Has(
//		userFactory.Count(50),
//		postFactory.Count(5),
//		func(u *User, p *Post) { p.UserID = u.ID },
//	).Create(ctx, g)
//
// link is the caller's, and it is what keeps this typed. Inferring the foreign
// key would mean naming it in a string and setting it by reflection, which is
// the thing this factory exists not to do -- a field renamed in the struct would
// still compile and quietly stop being set.
//
// The children are created after the parent, once per parent, because the row
// they name does not have its identifier until the statement that inserts it
// has run.
func Has[T, C any](parent *Factory[T], child *Factory[C], link func(*T, *C)) *Factory[T] {
	return parent.AfterCreating(func(ctx context.Context, g auth.Grant, created *T) error {
		_, err := child.State(func(c *C) { link(created, c) }).Create(ctx, g)
		return err
	})
}

// ForParent returns a factory whose rows belong to a parent it creates first.
//
// It is the inverse of Has, and the inverse matters: a post needs a user before
// it can name one, so the parent is created once, before any child row is built,
// and every child names that one.
//
//	posts, err := factories.ForParent(
//		postFactory.Count(3),
//		userFactory,
//		func(p *Post, u *User) { p.UserID = u.ID },
//	).Create(ctx, g)
//
// It is not called For because that name is the constructor's, and one package
// with two Fors that mean different things is the kind of thing a reader has to
// look up every time.
func ForParent[C, P any](child *Factory[C], parent *Factory[P], link func(*C, *P)) *Factory[C] {
	out := child.clone()
	out.resolvers = append(out.resolvers, func(ctx context.Context, g auth.Grant) (func(*C), error) {
		created, err := parent.CreateOne(ctx, g)
		if err != nil {
			return nil, err
		}
		return func(c *C) { link(c, created) }, nil
	})
	return out
}
