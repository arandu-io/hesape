// Package console is the publication command: what the registered modules offer
// a project, and writing it into the tree.
//
// The engine underneath, package publish, is deliberately ignorant of what it
// writes. This package is the one place that knows the vocabulary on top of it
// -- a module's [foundation.Publication], its tag, the lock a project commits --
// and turns it into the `vendor:publish` command an application answers.
//
// The command is a struct built with what it reads and turned into a
// console.Command by its Command method, the shape every command in the
// collection has:
//
//	app.Add(console.NewPublishCommand(".", kernel.Publications).Command())
//
// The publications are asked for when the command runs rather than handed over
// when it is built, because collecting them walks the registered modules and a
// binary that answers some other command has no reason to do that.
//
// [foundation.Publication]: https://pkg.go.dev/github.com/arandu-io/hesape/foundation#Publication
package console
