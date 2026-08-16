// Package concurrency runs a set of tasks at the same time and collects their
// results.
//
// [Run] runs tasks at the same time and answers with their results in argument
// order; [Defer] hands back the callback that starts them once the current
// work is finished; [Manager] picks a driver by name from configuration and
// forwards those two to it.
//
// # Three driver names, one mechanism
//
// [DriverProcess], [DriverFork] and [DriverSync] all resolve to the same
// [Driver], which runs each task in a goroutine. The name selects nothing,
// because there is nothing left to select: a goroutine needs no separate
// process to run in, no extension to be compiled in, and no restriction to the
// console. The three names are kept so an existing configuration still
// resolves.
//
// [Manager.Extend] is how a deployment registers a driver of its own under a
// name of its own.
//
// # A panic is an error
//
// Unlike the rest of the leaf packages, this one's reason to exist is not
// ergonomics. A panic raised while serving a request is caught by the HTTP
// pipeline and turned into a page. A panic raised in a queue handler, in a
// scheduled task, or in a pass of the event relay is caught by nothing, and it
// ends the process for every tenant being served at the time.
//
// A panic anywhere in this package becomes a [PanicError] carrying the
// recovered value and the stack at the point of recovery, so it travels the
// ordinary error path instead of a second one. [Recover] is that same guard
// with no concurrency around it, for the call sites that run code they did not
// write.
//
// # Dependencies
//
// The package is a leaf: it imports the standard library and nothing else,
// which is what lets the queue, the scheduler and the event relay depend on it.
package concurrency
