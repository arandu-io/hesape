// Package scheduler has moved to console/scheduling.
//
// It used to hold a scheduler of its own -- Task, Schedule, Scheduler and
// Module -- built before this ecosystem mirrored Illuminate. Illuminate puts
// the same thing in Console\Scheduling, and two schedulers is two ways to say
// the same thing (RULE 9), so the second one is gone rather than kept beside
// the first.
//
// What each piece became, in github.com/arandu-io/hesape/console/scheduling:
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
//
// Nothing is declared here. The package is kept so an import that has not been
// updated fails with this file open rather than with a path that no longer
// resolves.
package scheduler
