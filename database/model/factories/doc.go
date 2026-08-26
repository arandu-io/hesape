// Package factories builds model instances for tests and seeds: Factory,
// Sequence and the relationship factories.
//
// # A factory registers itself; it is not found by name
//
// Go has no type reachable from a name at run time and no file that becomes a
// type by being read, so a factory registers under its model's name when it is
// constructed, and FactoryForModel reads that registry.
//
// The consequence a caller sees is that the factory's package has to be
// imported. That is the same import a migration needs, at link time rather than
// run time, and the error from FactoryForModel says so by name. The count and
// state to apply are Count and State on the factory it answers.
//
// # Every create takes the Grant
//
// A factory writes rows, and in tests it writes a great many of them. Create,
// CreateMany, CreateOne and Insert all take a context and an auth.Grant, and the
// rows land in auth.Tenant(g) like any other write. A seeded fixture that
// belonged to no tenant would be invisible to every scoped read that came
// looking for it, which is a failing test whose cause is three layers from the
// assertion.
package factories
