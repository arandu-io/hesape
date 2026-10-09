// Package faker generates plausible values for factories and tests.
//
// # Why this is written here rather than taken from a library
//
// One property decides it: a failing test has to be reproducible from the seed
// it printed. Every Faker this package hands out is driven by an explicitly
// seeded generator, so faker.New(42) yields the same sequence on every run, on
// every machine, forever. The package-level functions of math/rand and
// math/rand/v2 do not have that property -- they are seeded from the runtime --
// and a library that reaches for them cannot be made to have it from outside.
//
// The second reason is smaller and still real: the core of this collection
// carries one third-party dependency, and a name generator is not the one worth
// making it two.
//
// # What it is not
//
// It is not a locale library and it will not become one. The word lists are
// small and English, chosen so that a generated row reads like a row rather
// than like a hash. A project that needs Portuguese street names writes its own
// Faker and passes it in -- which is what the interface is for.
package faker
