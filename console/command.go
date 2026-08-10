package console

import "context"

// Command is one command a binary answers to.
//
// It is a value, not an interface and not a class discovered by scanning a
// directory. What that buys is that the console listing and the compiler read
// the same slice: a command missing from it does not exist, and one in it with
// a broken Run does not build.
//
// The collaborators a command needs -- a repository, a mailer, a queue -- are
// captured by the closure that Run is, built where the application is wired. A
// command that reaches for a global is a command no test can pin.
type Command struct {
	// Name is what the person types. Lowercase, with a colon for the group:
	// "invoice:close". It is the key of the registry, so it is unique.
	Name string

	// Description is the one line the listing prints next to the name. One
	// sentence, no full stop, in the imperative: "close the open invoices".
	Description string

	// Hidden keeps the command out of the listing and out of Names. It still
	// runs when it is asked for by name. It is for a command that exists to be
	// called by another program -- a health probe, an internal hook -- and that
	// would only be noise to a person reading the list.
	Hidden bool

	// Isolated names the lock this command takes before it runs, and empty
	// means it may run as many times at once as it is started.
	//
	// It is a name rather than a bool so two commands that must not overlap
	// each other can share one -- a nightly reconciliation and the manual
	// command that does the same work name the same lock, and neither starts
	// while the other holds it.
	//
	// A registry without an issuer refuses to run an isolated command rather
	// than running it unprotected: see Registry.WithLocks.
	Isolated string

	// Run does the work.
	//
	// It receives the context of the process and the terminal it was started
	// from. The arguments that followed the command name are on the IO, so
	// that a command that takes none has nothing to ignore.
	Run func(ctx context.Context, o *IO) error
}
