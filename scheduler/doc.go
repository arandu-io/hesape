// Package scheduler is Arandu's Console\Scheduling: the tasks a module
// declares, and the loop that fires them.
//
// A scheduler built on a system cron exists because the runtime has no resident
// process: cron calls a command every minute and the command decides what to
// run. That is two artifacts and a dependency on the operating system.
//
// Go has a resident process. The scheduler is a goroutine in the same binary,
// which is also what keeps the deploy story of doc 17 true: one image, no
// crontab to configure, nothing to forget when a machine is replaced.
//
// What it does not do is retry. A task that fails is logged and diagnosed, and
// the next window runs it again; work that needs its own retry budget enqueues a
// job, and the queue owns the retry. Scheduler fires, queue persists.
//
// The pieces are Task, which a module declares; Schedule, the parsed cron
// expression; Scheduler, the loop; and Module, which runs the loop in the
// application process.
package scheduler
