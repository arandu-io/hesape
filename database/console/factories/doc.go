// Package factories mirrors Illuminate\Database\Console\Factories.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Console/Factories:
//
//	FactoryMakeCommand.php
//
// Nothing is implemented here. FactoryMakeCommand generates an Eloquent model
// factory -- a class extending Illuminate\Database\Eloquent\Factories\Factory,
// resolved back to its model by naming convention. The generator cannot be
// written before the thing it generates, so it belongs with
// database/eloquent/factories rather than here, and it lands when that does.
package factories
