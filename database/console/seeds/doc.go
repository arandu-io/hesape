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
// WithoutModelEvents has no counterpart: it is a trait that mutes Eloquent's
// model events for the duration of a seeder, and there is no Eloquent.
package seeds
