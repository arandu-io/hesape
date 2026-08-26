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
// is a T. Nothing here takes a map of strings, and a column that does not exist
// on the entity does not compile rather than being dropped at run time.
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
	afterCreating []func(context.Context, auth.Grant, *model.Model[T]) error
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
// statement, and a statement here is a statement like any other.
func (f *Factory[T]) AfterCreating(fn func(context.Context, auth.Grant, *model.Model[T]) error) *Factory[T] {
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
func (f *Factory[T]) Make() []T {
	fake := faker.New(f.seed)
	out := make([]T, 0, f.count)
	for i := range f.count {
		row := f.define(fake)
		for _, state := range f.states {
			state(&row)
		}
		if n := len(f.sequence); n > 0 {
			f.sequence[i%n](&row)
		}
		for _, after := range f.afterMaking {
			after(&row)
		}
		out = append(out, row)
	}
	return out
}

// MakeOne returns one row, whatever Count says.
func (f *Factory[T]) MakeOne() T {
	rows := f.Count(1).Make()
	return rows[0]
}

// Create stores the rows and returns them as models.
//
// It takes the Grant that every write in this collection takes, and the tenant
// comes off it: a factory is not a way around the policy that guards the table.
func (f *Factory[T]) Create(ctx context.Context, g auth.Grant) ([]*model.Model[T], error) {
	rows := f.Make()
	out := make([]*model.Model[T], 0, len(rows))

	for i := range rows {
		instance, err := f.model.NewInstance(nil, false)
		if err != nil {
			return nil, err
		}
		*instance.Entity = rows[i]

		if _, err := instance.Save(ctx, g); err != nil {
			return nil, err
		}
		for _, after := range f.afterCreating {
			if err := after(ctx, g, instance); err != nil {
				return nil, err
			}
		}
		out = append(out, instance)
	}
	return out, nil
}

// CreateOne stores one row, whatever Count says.
func (f *Factory[T]) CreateOne(ctx context.Context, g auth.Grant) (*model.Model[T], error) {
	created, err := f.Count(1).Create(ctx, g)
	if err != nil {
		return nil, err
	}
	return created[0], nil
}
