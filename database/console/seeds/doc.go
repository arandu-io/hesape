// Package seeds mirrors Illuminate\Database\Console\Seeds.
//
// db:seed runs the application's seeders through the Seed function on Deps,
// which is the project's own database.Seed closed over its registry -- what a
// seeder is allowed to touch is the application's answer, not the framework's.
//
// The seeder name is positional. Laravel spells it --class=, and refusing that
// spelling with the word to use instead is RULE 9: a name that is sometimes a
// flag and sometimes a word is two ways to say one thing.
//
// make:seeder prints the seeder instead of writing it. A Laravel seeder is
// found by class name, so its generator has to put the file where the
// autoloader will look; a seeder here is a value in a registry the application
// declares, so where the file goes is the application's business and the shape
// is the only thing worth generating.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Console/Seeds:
//
//	SeedCommand.php
//	SeederMakeCommand.php
//	WithoutModelEvents.php
//
// # What is not ported, and why
//
// WithoutModelEvents::withoutModelEvents -- reason 2 of ADR 0044, a method that
// only serves the container. This paragraph used to say the trait had no
// counterpart because there is no Eloquent, and that was wrong twice over:
// hesape/database/eloquent is here, and the reason the trait cannot be is a
// different one.
//
// Its body is `fn () => Model::withoutEvents($callback)`, and
// Model::withoutEvents swaps the model's STATIC event dispatcher -- the one the
// container binds once, that every model of every type reads -- for a
// NullDispatcher and back. There is no such dispatcher here (ADR 0001): a
// callback is registered on the model that will fire it, with
// Model.RegisterModelEvent, and NewInstance carries the registrations onto the
// instances made from it.
//
// So the mute is written on the model instead of on the process:
// Model.WithoutEvents, in hesape/database/eloquent, is Model::withoutEvents
// under Go's spelling, and a seeder that must not fire events calls it on the
// model it is seeding through. A package-level mute would be the second way to
// say the same thing (RULE 9), and it would be process-wide state that two
// seeders running in the same test binary would fight over.
//
// SeedCommand::handle and SeederMakeCommand::handle are answered the way every
// command in this collection answers it, and hesape/database/console says it
// once for all of them: the body PHP writes in handle() is the Run field of a
// console.Command value, and console.Command.Handle is that field called by its
// Laravel name.
package seeds
