// Package fakes holds the test doubles a test installs so that nothing leaves
// the process, and so that every message, job, event and notification can be
// asserted on afterwards.
//
// [MailFake], [QueueFake], [EventFake], [BusFake], [NotificationFake] and
// [ExceptionHandlerFake] are the doubles; [BatchFake], [BatchRepositoryFake],
// [PendingBatchFake], [PendingChainFake] and [PendingMailFake] are the smaller
// values they hand back.
//
// # Conventions
//
//   - An assertion takes the test as its first argument. There is no ambient
//     running test to find, so the caller hands its own in. See [TestingT].
//   - An argument that accepts more than one shape is typed any, and the forms
//     it accepts are listed on the method. A form that is not one of them is
//     reported as a failure naming the ones that are, rather than panicking.
//   - Where a fake stands in for a type another package owns -- a job, an
//     event dispatcher, a notification -- the interface it needs is declared
//     here, small enough to be implemented without importing this package's
//     opinions.
//
// # Concurrency
//
// Every fake is safe to use from a test that calls t.Parallel: records are
// written and read under a mutex, and a truth test always runs on a copy, so a
// callback that asks the fake another question does not deadlock.
//
// # Installing a double
//
// These fakes are not wired into anything, and there is no call that swaps one
// in: a package-level slot a test could replace would be shared mutable state,
// which two parallel tests would fight over. A double is built and passed, and
// each package says in its own documentation what to build -- the dispatcher a
// test owns in events, transport.Array in mail, Capture in notifications, the
// sync connection in queue, a dispatcher with no queue in bus, a local adapter
// over t.TempDir in filesystem, and a Reportable that stops in exception.
package fakes
