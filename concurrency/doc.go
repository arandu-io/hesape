// Package concurrency mirrors Illuminate\Concurrency.
//
// The files it answers to, in the clone at laravel_illuminate/concurrency:
//
//	ConcurrencyManager.php
//	ForkDriver.php
//	ProcessDriver.php
//	SyncDriver.php
//	ConcurrencyServiceProvider.php
//	Console/InvokeSerializedClosureCommand.php
//
// [Run] runs tasks at the same time and answers with their results in argument
// order; [Defer] hands back the callback that starts them once the current work
// is finished; [Manager] is the ConcurrencyManager, picking a driver by name
// and forwarding those two to it. The vocabulary is Illuminate's: run, defer,
// driver, extend.
//
// # Three drivers, one mechanism
//
// ForkDriver, ProcessDriver and SyncDriver are one [Driver] here, and
// [Manager.Driver] answers with it for all three names.
//
// The three are not three designs. A PHP process does one thing at a time, so
// to do two the fork driver calls pcntl_fork, the process driver shells out to
// `artisan invoke-serialized-closure` with the closure serialized into an
// environment variable, and the sync driver gives up and runs them in a row.
// Which one a deployment can use depends on whether pcntl is compiled in and
// whether the code is running in the console -- createForkDriver throws inside a
// web request for that reason. That is a language limit being routed around,
// which is the first of the three reasons a method may be skipped, wearing the
// costume of an architectural choice. The goroutine is the mechanism, it is
// available everywhere, and there is nothing left for a driver name to select.
//
// So the name of the operation does not move: run is Run and defer is Defer,
// with the arguments and the answers Driver::run and Driver::defer have. What
// goes is the choice underneath them.
//
// [DriverProcess], [DriverFork] and [DriverSync] are kept as the strings a
// ported configuration already contains, and they resolve to the same driver.
// So are the three factories that build it: [Manager.CreateProcessDriver],
// [Manager.CreateForkDriver] and [Manager.CreateSyncDriver] are the methods
// MultipleInstanceManager::resolve reaches by name -- "fork" reaches
// createForkDriver -- and [Manager.Driver] dispatches through them for the same
// reason. Three names answering with one value is what the collapse looks like
// from the outside, and each comment says what went with the mechanism it was
// named after: the ProcessFactory the PHP constructor resolves, and the two
// RuntimeExceptions createForkDriver throws. [Manager.Extend] is the seam the
// PHP keeps for putting something else behind a name.
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
// ordinary error path instead of a second one -- which is where PHP already
// puts it, since a Throwable crossing a fork boundary comes back as an
// exception ProcessDriver::run rethrows. [Recover] is that same guard with no
// concurrency around it, for the call sites that run code they did not write.
//
// # What is not here, and why
//
//   - InvokeSerializedClosureCommand (handle) is the far end of the process
//     driver's pipe: it unserializes a closure out of an environment variable
//     and prints the result as JSON. Closure serialization is a PHP language
//     facility Go does not have, and with no process to send anything to there
//     is nothing at the far end.
//   - ConcurrencyServiceProvider (register, provides) binds the manager into
//     the container, which ADR 0001 and ADR 0002 rejected. [NewManager] takes
//     the configuration repository instead.
//
// # Sources
//
// The clone is the source. It is identical to
// reference_laravel/framework/src/Illuminate/Concurrency for all five files.
//
// The package is a leaf: it imports the standard library and nothing else,
// which is what lets the queue, the scheduler and the event relay depend on it.
package concurrency
