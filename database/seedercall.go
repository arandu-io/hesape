package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// called is the list of seeders already run, one per process.
//
// It is a package-level variable, and the mutex is what makes it safe to read
// and write from more than one goroutine.
var (
	calledMu sync.Mutex
	called   []string
)

// Call runs the named seeders, in the order they are named, and remembers
// that they ran.
//
// It is a function rather than a method on a base type, because Seeder is
// already the interface a seeder implements and a Go type cannot be both an
// interface and a base type. What a seeder writes is:
//
//	func (DatabaseSeeder) Run(ctx context.Context, d Deps) error {
//	    _, err := database.Call(ctx, registry, d, "UserSeeder", "PostSeeder")
//	    return err
//	}
//
// It returns the names that ran, which is what the console prints.
func Call[D any](ctx context.Context, registry []Seeder[D], deps D, names ...string) ([]string, error) {
	var ran []string

	for _, name := range names {
		seeder, err := lookup(registry, name)
		if err != nil {
			return ran, err
		}

		if err := seeder.Run(ctx, deps); err != nil {
			return ran, fmt.Errorf("%s: %w", seeder.Name(), err)
		}

		ran = append(ran, seeder.Name())

		calledMu.Lock()
		called = append(called, seeder.Name())
		calledMu.Unlock()
	}

	return ran, nil
}

// CallWith is Call, with arguments for the seeders.
//
// The arguments are whatever the project's Deps carries, so CallWith takes
// a function that builds them -- the same shape Seed takes, and for the
// same reason: this package does not know what a project's Deps has on it.
func CallWith[D any](ctx context.Context, registry []Seeder[D], deps func(args []string) D, args []string, names ...string) ([]string, error) {
	return Call(ctx, registry, deps(args), names...)
}

// CallSilent is Call.
//
// Nothing here writes to a console -- a library that prints is a library a test
// cannot read, which is why Seed prints nothing either -- so there is no output
// to suppress and the two names do the same thing.
func CallSilent[D any](ctx context.Context, registry []Seeder[D], deps D, names ...string) ([]string, error) {
	return Call(ctx, registry, deps, names...)
}

// CallOnce runs the named seeders unless they have already run in this
// process.
//
// It is what keeps a shared seeder -- the one that inserts the currencies --
// from running four times because four other seeders each depend on it.
func CallOnce[D any](ctx context.Context, registry []Seeder[D], deps D, names ...string) ([]string, error) {
	var pending []string

	calledMu.Lock()
	for _, name := range names {
		already := false
		for _, done := range called {
			if strings.EqualFold(done, name) {
				already = true
				break
			}
		}
		if !already {
			pending = append(pending, name)
		}
	}
	calledMu.Unlock()

	return Call(ctx, registry, deps, pending...)
}

// CalledSeeders returns the names of every seeder Call has run in this
// process. There is no shared field to read directly, so the list is read
// through a function.
func CalledSeeders() []string {
	calledMu.Lock()
	defer calledMu.Unlock()
	return append([]string(nil), called...)
}

// ForgetCalledSeeders empties that list.
//
// A Go test binary runs every test in one process, so a package-level list
// that nothing clears makes the second test's CallOnce do nothing.
func ForgetCalledSeeders() {
	calledMu.Lock()
	defer calledMu.Unlock()
	called = nil
}
