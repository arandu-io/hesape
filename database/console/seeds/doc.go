// Package seeds holds db:seed and make:seeder.
//
// db:seed runs the application's seeders through the Seed function on Deps,
// which is the project's own database.Seed closed over its registry -- what a
// seeder is allowed to touch is the application's answer, not the framework's.
//
// The seeder name is positional. It is never also a flag, because a name that is
// sometimes a flag and sometimes a word is two ways to say one thing.
//
// make:seeder prints the seeder instead of writing it. A seeder here is a value
// in a registry the application declares, so where the file goes is the
// application's business and the shape is the only thing worth generating.
//
// # Muting model events is done on the model
//
// There is no process-wide event dispatcher to swap out. A callback is
// registered on the model that will fire it, with Model.RegisterModelEvent, and
// NewInstance carries the registrations onto the instances made from it. A
// seeder that must not fire events calls Model.WithoutEvents on the model it is
// seeding through. A package-level mute would be process-wide state that two
// seeders running in the same test binary would fight over.
package seeds
