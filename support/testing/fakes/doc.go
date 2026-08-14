// Package fakes mirrors Illuminate\Support\Testing\Fakes.
//
// It is where Mail::fake(), Queue::fake(), Event::fake(), Bus::fake() and
// Notification::fake() come from: the doubles a test installs so that nothing
// leaves the process, and every message, job, event and notification can be
// asserted on afterwards.
//
// The files it answers to, in the clone at
// laravel_illuminate/support/Testing/Fakes:
//
//	BatchFake.php
//	BatchRepositoryFake.php
//	BusFake.php
//	ChainedBatchTruthTest.php
//	EventFake.php
//	ExceptionHandlerFake.php
//	Fake.php
//	MailFake.php
//	NotificationFake.php
//	PendingBatchFake.php
//	PendingChainFake.php
//	PendingMailFake.php
//	QueueFake.php
//
// The clone is the source. The current Laravel, in
// reference_laravel/framework/src/Illuminate/Support/Testing/Fakes, was read
// beside it and differs in three ways worth naming: assertPushedTimes is public
// there and protected in the clone (public here, as in the current one);
// assertDispatchedOnce and assertPushedOnce exist only there (AssertDispatchedOnce
// is here, and says so); and QueueFake grew a set of inspection methods --
// pendingJobs, delayedJobs, reservedJobs, reserve, clearReserved, beforePushing --
// that belong to a queue with a real store behind it, which a fake does not have.
//
// Three shapes of the PHP do not cross over, and every one of them is written
// where it happens:
//
//   - An assertion takes the test as its first argument. PHPUnit's assertions
//     are static calls that find the running test through global state; Go has
//     no such thing. See TestingT.
//   - Where the PHP overloads one argument -- a count, a truth test with one
//     parameter, or a truth test with three -- the argument is typed any and
//     the accepted forms are listed on the method. A form that is not one of
//     them is reported as a failure that names the ones that are.
//   - Where the fake stands in for a type another package owns -- a job, an
//     event dispatcher, a notification -- the interface it needs is declared
//     here, small enough to be implemented without importing this package's
//     opinions.
//
// Every fake is safe to use from a test that calls t.Parallel: records are
// written and read under a mutex, and a truth test always runs on a copy, so a
// callback that asks the fake another question does not deadlock.
//
// What this package is not, and where the double a test installs comes from.
//
// These are the Illuminate types with the Illuminate signatures: the Queue a
// QueueFake stands in for is push($job, $data, $queue), not the one
// hesape/queue declares, which carries a context and an auth.Grant. So a fake
// here does not satisfy a collection contract and is not meant to -- it is
// what the component looks like, for somebody reading across from the PHP.
//
// There is also no way to install one. In Laravel the shortcut is the facade
// swapping a container binding, and there is neither facade nor container
// (ADR 0001, ADR 0002); a package-level slot a test could swap would be shared
// mutable state, which two tests calling t.Parallel would fight over. A double
// is built and passed, and each package says what to build in its own doc: the
// dispatcher a test owns in events, transport.Array in mail, Capture in
// notifications, the sync connection in queue, a dispatcher with no queue in
// bus, a local adapter over t.TempDir in filesystem, and a Reportable that
// stops in exception.
package fakes
