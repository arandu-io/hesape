// Package console is what an application writes its own commands against.
//
// It is the command side of a project, and it is deliberately small: a Command
// is a value with a name, a sentence of help and a function, a Application is the
// slice of them a binary answers to, and an IO is the terminal that function
// was handed. Nothing here scans a directory, instantiates a class it found or
// parses a signature string at run time -- a command that is not in the
// application does not exist, and one that is in it with a broken signature does
// not build. What lives here is what the project itself runs; generating new
// files for a project is a separate concern.
//
// The type used to be declared in the skeleton, in routes/console.go, which
// meant every project had a different nominal Command type and nothing could be
// written against two of them: neither the framework nor a library could ship a
// command. It is here now, and it is one type.
//
// # Isolation
//
// A command that must not run twice at once names a lock in Command.Isolated,
// and a Application given an issuer with WithLocks takes it before the command
// runs. The lock is not prefixed by tenant, and that is deliberate: a lock per
// tenant would let N replicas each run the task for a different tenant at the
// same time, which is the problem and not the solution. See cache.Locks and
// docs/15.
//
// Application holds already-built commands rather than a map of names to
// construct on demand: a Command here is a value, so there is nothing left to
// defer.
package console
