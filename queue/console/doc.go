// Package console is the queue's commands.
//
// It mirrors Illuminate\Queue\Console. Each command is a struct built with the
// collaborators it needs and turned into a console.Command by its Command
// method, which is how every command in the collection is written: the registry
// and the compiler read the same slice, so a command missing from it does not
// exist and one in it with a broken Run does not build.
//
//	reg.Add(
//		console.NewWorkCommand(worker, manager).Command(),
//		console.NewRestartCommand(manager).Command(),
//	)
//
// # Every failed job command takes a tenant
//
// A failed job carries a customer's payload, so `aru queue:failed` is a read
// like any other and it is scoped: --tenant is required and has no default
// (RULE 17). Laravel's commands have no equivalent because Laravel has no
// tenant, and a listing that defaulted would print whichever customer happened
// to sort first.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Console:
//
//	BatchesTableCommand.php    -- see below
//	ClearCommand.php           -> ClearCommand
//	FailedTableCommand.php     -- see below
//	FlushFailedCommand.php     -> FlushFailedCommand
//	ForgetFailedCommand.php    -> ForgetFailedCommand
//	ListFailedCommand.php      -> ListFailedCommand
//	ListenCommand.php          -> ListenCommand
//	MonitorCommand.php         -> MonitorCommand
//	PauseCommand.php           -> PauseCommand
//	PruneBatchesCommand.php    -> PruneBatchesCommand
//	PruneFailedJobsCommand.php -> PruneFailedJobsCommand
//	RestartCommand.php         -> RestartCommand
//	ResumeCommand.php          -> ResumeCommand
//	RetryBatchCommand.php      -> RetryBatchCommand
//	RetryCommand.php           -> RetryCommand
//	TableCommand.php           -- see below
//	WorkCommand.php            -> WorkCommand
//
// The three table commands -- make:queue-table, make:queue-failed-table,
// make:queue-batches-table -- have no counterpart, and that is not an omission.
// In Laravel they write a migration file into the application because the
// schema belongs to the application; here it belongs to whoever owns the table
// and travels with them: queue.DatabaseQueue.Migrations is the jobs table and
// failed.DatabaseFailedJobProvider.Migrations is the failed jobs table, both
// collected by the module. A generator that copied them into the project would
// be a second copy that drifts (RULE 9).
package console
