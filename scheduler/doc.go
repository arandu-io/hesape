// Package scheduler declares nothing. The scheduler lives in
// github.com/arandu-io/hesape/console/scheduling, and this package is kept so
// that an import which has not been updated fails with this file open rather
// than with a path that no longer resolves.
//
// What each name became there:
//
//	Task            -> Event, declared through Schedule.Call or Schedule.Command
//	Task.Spec       -> Event.Cron, and the frequency methods that write it
//	Task.Scope      -> Event.PerTenant
//	Task.Singleton  -> Event.OnOneServer
//	Task.Action     -> Event.Action
//	Schedule (cron) -> CronExpression
//	Scheduler       -> Runner, and the Module that ticks it
//	Module          -> Module
//	Schedulable     -> a module returning []console.Command, or a Schedule
package scheduler
