// Package console is the queue's commands.
//
// Each command is a struct built with the collaborators it needs and turned
// into a console.Command by its Command method, which is how every command in
// the collection is written: the registry and the compiler read the same slice,
// so a command missing from it does not exist and one in it with a broken Run
// does not build.
//
//	reg.Add(
//		console.NewWorkCommand(worker, manager).Command(),
//		console.NewRestartCommand(manager).Command(),
//	)
//
// # Every failed job command takes a tenant
//
// A failed job carries a customer's payload, so `aru queue:failed` is a read
// like any other and it is scoped: --tenant is required and has no default. A
// listing that defaulted would print whichever customer happened to sort first.
//
// # There is no table generator
//
// A migration for the jobs table, the failed jobs table or the batches table is
// not written into the application, because the schema belongs to whoever owns
// the table and travels with them: queue.DatabaseQueue.Migrations is the jobs
// table and failed.DatabaseFailedJobProvider.Migrations is the failed jobs
// table, both collected by the module. A generator that copied them into the
// project would be a second copy that drifts.
package console
