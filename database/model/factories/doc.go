// Package factories builds rows of an entity for tests and for seeding.
//
// A factory has one job: to answer "a valid row of this kind, please" so that a
// test can say only the part it cares about. The default state lives in the
// definition; everything a caller wants different is a state on top of it.
//
//	users := factories.For(Users, func(f faker.Faker) User {
//		return User{Name: f.Name(), Email: f.Unique().Email(), Active: true}
//	})
//
//	suspended, err := users.Count(3).
//		State(func(u *User) { u.Active = false }).
//		Create(ctx, g)
//
// # It is typed, and that is the difference worth naming
//
// The definition returns a T. A state takes a *T. What comes back is the row --
// a *T, the same value a query terminal hands back. A column the entity does not
// declare does not compile, where the same mistake against a map of strings is a
// key that is silently dropped and a row that is quietly wrong.
//
// The row is built through the model, made and created alike, which is what lets
// an entity that embeds model.Model[T] be saved after it was made:
//
//	user, err := users.MakeOne()
//	user.Name = "Ada"
//	_, err = user.Save(ctx, g)
//
// A definition answers a T, and a T that embeds the model carries a zero one in
// it -- so building the row and then assigning the definition over it would
// leave the model with no connection to save through. What comes back here has
// its model, and Create stores exactly the rows Make would have handed over.
//
// This package used to hold the other shape -- a factory over map[string]any,
// with relationships resolved by a runtime type switch over a caller-supplied
// closure. It was a faithful port and it had no caller, no test, and a contract
// the model could not satisfy. It is in the history, and what replaced it is
// here.
//
// # Make does not take a Grant, and Create does
//
// Make touches nothing, so a Grant on it would authorize nothing -- a parameter
// that looks like enforcement and enforces nothing teaches the opposite of what
// the Grant means everywhere else. Create writes, so it takes one, and the
// tenant comes off it like every other write. A factory is not a way around the
// policy that guards the table.
package factories
