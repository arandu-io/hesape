// Package factories mirrors Illuminate\Database\Eloquent\Factories.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Eloquent/Factories:
//
//	Factory.php                    -> factory.go
//	Sequence.php                   -> sequence.go
//	CrossJoinSequence.php          -> sequence.go
//	Relationship.php               -> relationship.go
//	BelongsToRelationship.php      -> belongstorelationship.go
//	BelongsToManyRelationship.php  -> belongstomanyrelationship.go
//	Attributes/UseModel.php        -> attributes/
//
// # A factory registers itself; it is not found by name
//
// This is the one shape that had to change, and it changes for the reason the
// migrations package changes discovery. PHP resolves App\Models\User to
// Database\Factories\UserFactory by string, loads the file and calls its static
// new(). Go has no class name at run time and no file that becomes a type by
// being read, so a factory registers under its model's name when it is
// constructed, and FactoryForModel reads that registry.
//
// The consequence a caller sees is that the factory's package has to be
// imported. That is the same import a migration needs, moved from run time to
// link time, and the error from FactoryForModel says so by name.
//
// # Every create takes the Grant
//
// A factory writes rows, and in tests it writes a great many of them. Create,
// CreateMany, CreateOne and Insert all take a context and an auth.Grant, and
// the rows land in auth.Tenant(g) like any other write. A seeded fixture that
// belonged to no tenant would be invisible to every scoped read that came
// looking for it, which is a failing test whose cause is three layers from the
// assertion.
//
// # What was skipped, and why
//
//   - HasFactory::factory. It is a static method a model gains from a trait,
//     and it resolves the model's own factory through static::class and a
//     ReflectionClass attribute scan. Go has neither: no static methods on
//     types, no class name at run time, no attributes to reflect over. What it
//     is for is reachable under the PHP's other name for the same thing --
//     FactoryForModel, which is Factory::factoryForModel -- and the count and
//     state it would then apply are Count and State on the factory it answers.
//     Its two protected halves, newFactory and getUseFactoryAttribute, are the
//     reflection itself and go with it.
//   - Macroable on Factory: __call, a PHP language interface Go does not have.
package factories
