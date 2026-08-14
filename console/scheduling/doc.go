// Package scheduling is the events an application runs on a clock.
//
// It mirrors Illuminate\Console\Scheduling, and the files it answers to, in the
// clone at laravel_illuminate/console/Scheduling:
//
//	CacheAware.php
//	CacheEventMutex.php
//	CacheSchedulingMutex.php
//	CallbackEvent.php
//	CommandBuilder.php
//	Event.php
//	EventMutex.php
//	ManagesAttributes.php
//	ManagesFrequencies.php
//	PendingEventAttributes.php
//	Schedule.php
//	ScheduleClearCacheCommand.php
//	ScheduleFinishCommand.php
//	ScheduleInterruptCommand.php
//	ScheduleListCommand.php
//	ScheduleRunCommand.php
//	ScheduleTestCommand.php
//	ScheduleWorkCommand.php
//	SchedulingMutex.php
//
// # What moved here
//
// hesape/scheduler used to hold a scheduler of its own, and it is gone: Task is
// Event, its Schedule is CronExpression, its Scheduler loop is Module and
// ScheduleWorkCommand, and its Module is Module. Two schedulers is two ways to
// say the same thing (RULE 9), and the one that stayed is the one a Laravel
// developer recognises.
//
// # Two additions that are not in the PHP
//
// Event.Action and Event.PerTenant. RULE 17 says there is no path to a
// repository that skipped authorization, and a scheduled event is a path: it
// carries an auth.Action, and what it hands a callback is the Grant built from
// that action and the tenant. RULE 14 says the tenant comes from the Grant and
// never from a flag, so an event that reads a customer's rows says PerTenant and
// the runner expands it to one Grant at a time.
//
// # The cron expression
//
// Five fields, not six: no seconds. Sub-minute repetition is EverySecond and its
// companions, which set RepeatSeconds and leave the expression on every minute
// -- exactly as Laravel does, because a schedule that says "@every 90s" is a
// busy loop with a nicer name.
//
// What makes that true is Runner.repeatEvents, which is
// ScheduleRunCommand::repeatEvents: after the due events have run, the ones that
// repeat are run again every RepeatSeconds until the minute is over. Without it
// the frequency methods were a field nobody read, and EveryFifteenSeconds ran
// once a minute.
package scheduling
