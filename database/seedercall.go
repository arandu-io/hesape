package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// called answers Illuminate\Database\Seeder::$called.
//
// It is static in PHP, which means one list per process, which is what a
// package-level variable is here. The mutex is what static gives away for free
// in a language with no goroutines.
var (
	calledMu sync.Mutex
	called   []string
)

// Call answers Seeder::call: run the named seeders, in the order they are
// named, and remember that they ran.
//
// The PHP is a method on the Seeder base class, so a seeder calls
// $this->call(PostSeeder::class). Here it is a function, because Seeder is
// already the interface a seeder implements and a Go type cannot be both the
// interface and the base class. What a seeder writes is:
//
//	func (DatabaseSeeder) Run(ctx context.Context, d Deps) error {
//	    _, err := database.Call(ctx, registry, d, "UserSeeder", "PostSeeder")
//	    return err
//	}
//
// It answers the names that ran, which is what the console prints.
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

// CallWith answers Seeder::callWith: Call, with arguments for the seeders.
//
// The PHP's $parameters are the arguments its container passes to run(). Here
// the arguments are whatever the project's Deps carries, so CallWith takes a
// function that builds them -- the same shape Seed takes, and for the same
// reason: this package does not know what a project's Deps has on it.
func CallWith[D any](ctx context.Context, registry []Seeder[D], deps func(args []string) D, args []string, names ...string) ([]string, error) {
	return Call(ctx, registry, deps(args), names...)
}

// CallSilent answers Seeder::callSilent.
//
// The PHP's $silent suppresses the console output the base class writes.
// Nothing here writes to a console -- a library that prints is a library a test
// cannot read, which is why Seed prints nothing either -- so this is Call, and
// it exists so a seeder ported from Laravel keeps compiling.
func CallSilent[D any](ctx context.Context, registry []Seeder[D], deps D, names ...string) ([]string, error) {
	return Call(ctx, registry, deps, names...)
}

// CallOnce answers Seeder::callOnce: run the named seeders unless they have
// already run in this process.
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

// CalledSeeders answers Seeder::$called, which the PHP reads directly because
// its property is protected-static and its subclasses are in the same class
// hierarchy. Go has neither, so the list is read through a function.
func CalledSeeders() []string {
	calledMu.Lock()
	defer calledMu.Unlock()
	return append([]string(nil), called...)
}

// ForgetCalledSeeders empties that list.
//
// It has no PHP counterpart because a PHP process handles one command and then
// exits. A Go test binary runs every test in one process, so a static list that
// nothing clears makes the second test's CallOnce do nothing.
func ForgetCalledSeeders() {
	calledMu.Lock()
	defer calledMu.Unlock()
	called = nil
}
