// Package factories builds rows of a table for tests and for seeding.
//
// A factory has one job: to answer "a valid row of this kind, please" so that a
// test can say only the part it cares about. The default state lives in the
// definition; everything a caller wants different is a state on top of it.
//
// Factory is one type for every table, compiled once. An application calls the
// typed factory generated beside each entity, which holds one of these and
// converts at the boundary:
//
//	users := factories.New(userTable.Query(db), func(f faker.Faker, row model.Entity) {
//		*row.(*User) = User{Name: f.Name(), Email: f.Unique().Email(), Active: true}
//	})
//
//	suspended, err := users.Count(3).
//		State(func(row model.Entity) { row.(*User).Active = false }).
//		Create(ctx, g)
//
// # A made row is a row
//
// The rows are built on the table and the connection of the query the factory
// was given, made and created alike, which is what lets a made row be saved:
//
//	row, err := users.MakeOne()
//	row.(*User).Name = "Ada"
//	_, err = row.(*User).Save(ctx, g)
//
// A definition may assign the whole struct, and the struct carries the row's
// embedded model -- so building the row and then assigning over it would leave
// the model with no connection to save through. The factory puts the model back
// once the definition has run, and Create stores exactly the rows Make would
// have handed over.
//
// # Make does not take a Grant, and Create does
//
// Make touches nothing, so a Grant on it would authorize nothing -- a parameter
// that looks like enforcement and enforces nothing teaches the opposite of what
// the Grant means everywhere else. Create writes, so it takes one, and the
// tenant comes off it like every other write. A factory is not a way around the
// policy that guards the table.
package factories
