// Package console mirrors Illuminate\Cache\Console.
//
// Four commands, each a value with a Handle and a Command that puts it in a
// console.Registry. They are here and not in the parent package because a
// command imports the console, the console imports the cache, and a cache that
// imported its own commands would be a cycle.
//
// Every command that touches entries takes a --tenant and holds a system grant
// for it. Laravel's do not, because a Laravel cache has one tenant; here Flush
// empties one tenant's slice of one namespace, and a cache:clear that emptied
// the store would clear every other customer on the way past (RULE 14).
//
// The files it answers to, in the clone at
// laravel_illuminate/cache/Console:
//
//	CacheTableCommand.php      -> CacheTableCommand
//	ClearCommand.php           -> ClearCommand
//	ForgetCommand.php          -> ForgetCommand
//	PruneStaleTagsCommand.php  -> PruneStaleTagsCommand
//
// ClearCommand::flushFacades does not arrive. It deletes the real-time facade
// classes Laravel writes into storage/framework/cache, and there are no facades
// here (ADR 0002), so there is nothing for it to delete.
package console
