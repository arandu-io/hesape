// Package concurrency runs work in parallel and keeps a panic from taking the
// process down.
//
// It is Arandu's answer to Illuminate\Concurrency -- ConcurrencyManager,
// ForkDriver, ProcessDriver and SyncDriver -- and it answers all four with the
// goroutine. PHP has to fork a process or shell one out to do two things at
// once, so a driver is chosen per deployment and closures are serialized across
// the boundary. Go does not, so there is no manager, no driver and no closure
// to serialize: [Run] starts goroutines, and [RunWith] is the same call with a
// ceiling for tasks that contend for something finite.
//
// Unlike the rest of the leaf packages, its reason to exist is not ergonomics.
// A panic raised while serving a request is caught by the HTTP pipeline and
// turned into a page. A panic raised in a queue handler, in a scheduled task,
// or in a pass of the event relay is caught by nothing, and it ends the process
// for every tenant being served at the time. [Recover] is the guard those call
// sites wrap around code they did not write.
//
// Results come back in argument order, the first failure cancels the context
// the other tasks are running under, and no goroutine outlives the call. A
// panic anywhere in this package becomes a [PanicError] carrying the recovered
// value and the stack at the point of recovery, so it travels the ordinary
// error path instead of a second one.
//
// The package is a leaf: it imports the standard library and nothing else,
// which is what lets the queue, the scheduler and the event relay depend on it.
package concurrency
