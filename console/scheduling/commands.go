package scheduling

import (
	"context"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/console/events"
)

// interruptKey is where schedule:interrupt leaves its mark, and where the
// worker looks for it.
//
// It is ScheduleInterruptCommand's 'illuminate:schedule:interrupt', renamed to
// the prefix this framework uses everywhere else.
const interruptKey = "framework/schedule-interrupt"

// Interrupter is what schedule:interrupt writes to and the worker reads.
//
// It is declared here rather than imported as a cache repository: what the two
// commands need of a store is a flag that expires, and a flag that expires is a
// lock with a ttl.
type Interrupter interface {
	// Interrupt marks the current schedule run as interrupted, for ttl.
	Interrupt(ctx context.Context, ttl time.Duration) error

	// Interrupted reports whether the mark is there, and clears it.
	Interrupted(ctx context.Context) (bool, error)
}

// Commands is the set of scheduling commands, ready to register.
//
// It is the seven ScheduleCommand classes as values, built over one schedule.
// interrupter and cleaner may be nil, and then the two commands that need them
// say so rather than doing nothing.
type Commands struct {
	// Schedule is what the commands read and run.
	Schedule *Schedule

	// Runner runs the due events. Nil means one is built over the schedule.
	Runner *Runner

	// Interrupter backs schedule:interrupt and the worker's check of it.
	Interrupter Interrupter

	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time
}

// All returns every scheduling command.
//
// The order is the order they are listed in, which is alphabetical by name
// because that is how the console help sorts them anyway.
func (c Commands) All() []console.Command {
	return []console.Command{
		c.ScheduleClearCacheCommand(),
		c.ScheduleFinishCommand(),
		c.ScheduleInterruptCommand(),
		c.ScheduleListCommand(),
		c.ScheduleRunCommand(),
		c.ScheduleTestCommand(),
		c.ScheduleWorkCommand(),
	}
}

// now is the clock, defaulted.
func (c Commands) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// runner is the runner, defaulted.
func (c Commands) runner() *Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return NewRunner(c.Schedule)
}

// ScheduleRunCommand runs the events that are due now.
//
// It answers Illuminate\Console\Scheduling\ScheduleRunCommand: `schedule:run`,
// with --whisper to keep quiet when there was nothing to do.
func (c Commands) ScheduleRunCommand() console.Command {
	return console.Command{
		Signature:   "schedule:run {--whisper : Do not output message indicating that no jobs were ready to run}",
		Description: "run the scheduled commands",
		Run: func(ctx context.Context, o *console.IO) error {
			runner := c.runner()
			runner.Listen = listenerFor(o)

			ran := runner.Run(ctx, c.now())
			if ran == 0 && !o.Option("whisper").Bool() {
				o.OutputComponents().Info("No scheduled commands are ready to run")
			}
			return nil
		},
	}
}

// ScheduleWorkCommand runs the schedule for as long as the process lives.
//
// It answers Illuminate\Console\Scheduling\ScheduleWorkCommand:
// `schedule:work`. In Laravel it shells out to schedule:run once a minute
// because PHP has no resident process; here it calls the same runner in the same
// process, which is one artifact instead of two and no crontab to configure.
//
// It ticks at the top of each minute rather than every sixty seconds from boot,
// because an event specified as "0 3 * * *" has to fire at 3:00 and not at 3:00
// plus however long after a deploy the process happened to start.
func (c Commands) ScheduleWorkCommand() console.Command {
	return console.Command{
		Signature:   "schedule:work {--run-output-file= : The file to direct output to}",
		Description: "start the schedule worker",
		Run: func(ctx context.Context, o *console.IO) error {
			runner := c.runner()
			runner.Listen = listenerFor(o)

			o.OutputComponents().Info("Running scheduled tasks every minute")

			for {
				now := c.now()
				next := now.Truncate(time.Minute).Add(time.Minute)

				select {
				case <-ctx.Done():
					return nil
				case <-time.After(next.Sub(now)):
				}

				if c.Interrupter != nil {
					interrupted, err := c.Interrupter.Interrupted(ctx)
					if err != nil {
						return err
					}
					if interrupted {
						o.OutputComponents().Info("Schedule interrupted")
						return nil
					}
				}

				runner.Run(ctx, c.now())
			}
		},
	}
}

// ScheduleFinishCommand reports the exit code of an event that ran in the
// background.
//
// It answers Illuminate\Console\Scheduling\ScheduleFinishCommand:
// `schedule:finish {id} {code=0}`. It is hidden because nobody runs it by hand
// -- CommandBuilder appends it to the backgrounded command line, and it is what
// releases the mutex and runs the after callbacks in a process that stayed
// around to do it.
func (c Commands) ScheduleFinishCommand() console.Command {
	return console.Command{
		Signature:   "schedule:finish {id : The mutex name of the event that finished} {code=0 : The exit status it ended with}",
		Description: "handle the completion of a scheduled command",
		Hidden:      true,
		Run: func(ctx context.Context, o *console.IO) error {
			id := o.Argument("id").String()

			code, err := o.Argument("code").Int()
			if err != nil {
				return err
			}

			for _, event := range c.Schedule.Events() {
				if event.MutexName() != id {
					continue
				}
				return event.Finish(ctx, code)
			}
			return console.Exit(1, "no scheduled event has the mutex name %s", id)
		},
	}
}

// ScheduleListCommand lists the scheduled events and when they run next.
//
// It answers Illuminate\Console\Scheduling\ScheduleListCommand:
// `schedule:list`, with --timezone to read the times somewhere else.
func (c Commands) ScheduleListCommand() console.Command {
	return console.Command{
		Signature:   "schedule:list {--timezone= : The timezone that times should be displayed in}",
		Description: "list all scheduled tasks",
		Run: func(_ context.Context, o *console.IO) error {
			if len(c.Schedule.Events()) == 0 {
				o.OutputComponents().Info("No scheduled tasks have been defined")
				return nil
			}

			location := time.Local
			if name := o.Option("timezone").String(); name != "" {
				loaded, err := time.LoadLocation(name)
				if err != nil {
					return console.Exit(1, "unknown timezone: %s", name)
				}
				location = loaded
			}

			now := c.now()
			for _, event := range c.Schedule.Events() {
				next := event.NextRunDate(now)
				when := "never"
				if !next.IsZero() {
					when = next.In(location).Format(time.RFC3339)
				}
				o.OutputComponents().TwoColumnDetail(
					fmt.Sprintf("%s  %s", event.GetExpression(), event.GetSummaryForDisplay()),
					"next: "+when,
				)
			}
			return nil
		},
	}
}

// ScheduleTestCommand runs one scheduled event by hand.
//
// It answers Illuminate\Console\Scheduling\ScheduleTestCommand:
// `schedule:test {--name=}`. It runs the event through the same path the runner
// does -- same mutex, same Grant, same callbacks -- which is what makes the
// manual run auditable rather than a back door.
func (c Commands) ScheduleTestCommand() console.Command {
	return console.Command{
		Signature:   "schedule:test {--name= : The name of the scheduled command to run}",
		Description: "run a scheduled command",
		Run: func(ctx context.Context, o *console.IO) error {
			all := c.Schedule.Events()
			if len(all) == 0 {
				o.OutputComponents().Info("No scheduled commands have been defined")
				return nil
			}

			name := o.Option("name").String()
			if name == "" {
				summaries := make([]string, 0, len(all))
				for _, event := range all {
					summaries = append(summaries, event.GetSummaryForDisplay())
				}
				chosen, err := o.OutputComponents().Choice("Which command would you like to run?", summaries, "")
				if err != nil {
					return err
				}
				name = chosen
			}

			for _, event := range all {
				if event.GetSummaryForDisplay() != name {
					continue
				}
				return o.OutputComponents().Task(
					"Running ["+event.GetSummaryForDisplay()+"]",
					func() (console.TaskResult, error) {
						if err := event.Run(ctx); err != nil {
							return console.TaskFailure, err
						}
						return console.TaskSuccess, nil
					},
				)
			}
			return console.Exit(1, "no matching scheduled command found: %s", name)
		},
	}
}

// ScheduleClearCacheCommand deletes the overlap mutexes left behind.
//
// It answers Illuminate\Console\Scheduling\ScheduleClearCacheCommand:
// `schedule:clear-cache`. It is for the case a process died holding a lock and
// the operator does not want to wait out the ttl.
func (c Commands) ScheduleClearCacheCommand() console.Command {
	return console.Command{
		Signature:   "schedule:clear-cache",
		Description: "delete the cached mutex files created by scheduler",
		Run: func(ctx context.Context, o *console.IO) error {
			cleared := 0
			for _, event := range c.Schedule.Events() {
				if event.Mutex == nil {
					continue
				}
				exists, err := event.Mutex.Exists(ctx, event)
				if err != nil {
					return err
				}
				if !exists {
					continue
				}
				if err := event.Mutex.Forget(ctx, event); err != nil {
					return err
				}
				o.OutputComponents().Info("Deleted mutex for [" + event.GetSummaryForDisplay() + "]")
				cleared++
			}
			if cleared == 0 {
				o.OutputComponents().Info("No mutex files were found")
			}
			return nil
		},
	}
}

// ScheduleInterruptCommand stops the running schedule worker at its next tick.
//
// It answers Illuminate\Console\Scheduling\ScheduleInterruptCommand:
// `schedule:interrupt`. It leaves a mark that expires, so a worker started
// afterwards is not stopped by an interrupt meant for the one before it.
func (c Commands) ScheduleInterruptCommand() console.Command {
	return console.Command{
		Signature:   "schedule:interrupt",
		Description: "interrupt the current schedule run",
		Run: func(ctx context.Context, o *console.IO) error {
			if c.Interrupter == nil {
				return console.Exit(1, "schedule:interrupt has no store wired, and the worker would never see the mark")
			}
			if err := c.Interrupter.Interrupt(ctx, time.Hour); err != nil {
				return err
			}
			o.OutputComponents().Info("Broadcasting schedule interrupt signal")
			return nil
		},
	}
}

// listenerFor writes what the runner reports onto the terminal.
//
// It is what ScheduleRunCommand does with the events it dispatches: the task
// line, the failure and the skip, in the shape the components render.
func listenerFor(o *console.IO) Listener {
	return func(event any) {
		switch e := event.(type) {
		case events.ScheduledTaskFinished:
			o.OutputComponents().TwoColumnDetail(e.Task.GetSummaryForDisplay(), durationForHumans(e.Runtime)+" DONE")
		case events.ScheduledTaskFailed:
			o.OutputComponents().Error(e.Task.GetSummaryForDisplay() + ": " + e.Exception.Error())
		case events.ScheduledTaskSkipped:
			o.OutputComponents().TwoColumnDetail(e.Task.GetSummaryForDisplay(), "SKIPPED")
		}
	}
}

// durationForHumans renders a runtime the way the task line does.
func durationForHumans(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
