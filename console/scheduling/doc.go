// Package scheduling is the events an application runs on a clock.
//
// # What moved here
//
// hesape/scheduler used to hold a scheduler of its own, and it is gone: Task
// is Event, its Schedule is CronExpression, its Scheduler loop is Module and
// ScheduleWorkCommand, and its Module is Module. Two schedulers is two ways
// to say the same thing, and only one stayed.
//
// # The tasks modules declare
//
// A module declares its scheduled work as foundation.Task values, and the
// kernel collects them. TaskScheduler runs them on this package's Schedule and
// Runner, adding what a task carries and an Event has no field for: a Timeout,
// a Recorder and a durable claim on each occurrence, so a Singleton task runs
// once per minute per tenant however many replicas fire. TaskModule is that
// scheduler as a foundation.Module, and TaskCommands answers schedule:list and
// schedule:run <id> [--tenant=] over it.
//
// # Authorization and tenancy
//
// Every scheduled event carries an auth.Action, and what it hands a callback
// is the Grant built from that action and the tenant. An event that reads a
// customer's rows calls PerTenant, and the runner expands it to one Grant
// per tenant instead of reading a tenant off a flag.
//
// # The cron expression
//
// Five fields, not six: no seconds. Sub-minute repetition is EverySecond and
// its companions, which set RepeatSeconds and leave the expression on every
// minute, because a schedule that says "@every 90s" is a busy loop with a
// nicer name.
//
// What makes that true is Runner.repeatEvents: after the due events have
// run, the ones that repeat are run again every RepeatSeconds until the
// minute is over. Without it the frequency methods would be a field nobody
// reads, and EveryFifteenSeconds would run once a minute instead of four
// times.
package scheduling
