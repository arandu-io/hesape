// Package events holds the events the console and scheduler dispatch.
//
// Every one of them is a value with public fields and nothing else.
//
// The five scheduling events carry a ScheduledTask rather than the scheduling
// package's Event: the scheduler reports through these, so importing it here
// would be a cycle. The interface is the part of an event a listener reads.
package events

import (
	"io"
	"time"
)

// Application is the console application an ArtisanStarting listener is handed.
//
// It is an interface for the same reason ScheduledTask is: the console package
// dispatches this event, and importing it back would be a cycle. What a
// bootstrapper does with the application is add commands to it.
type Application interface {
	// Names lists the commands a person can be told about.
	Names() []string
}

// ArtisanStarting is fired when the console application has been built and
// before it has run anything.
//
// It is where a package adds its commands.
type ArtisanStarting struct {
	// Artisan is the application that is starting.
	Artisan Application
}

// CommandStarting is fired before a command runs.
type CommandStarting struct {
	// Command is the command name.
	Command string

	// Arguments is what followed the name on the command line, unparsed. It
	// carries what the command line was built from, because a listener that
	// reaches into a half-parsed input is a listener that breaks when the
	// signature changes.
	Arguments []string

	// Output is the stream the command writes to.
	Output io.Writer
}

// CommandFinished is fired after a command has run, whatever it returned.
type CommandFinished struct {
	// Command is the command name.
	Command string

	// Arguments is what followed the name on the command line, unparsed.
	Arguments []string

	// Output is the stream the command wrote to.
	Output io.Writer

	// ExitCode is the status the command ended with. Zero is success.
	ExitCode int
}

// ScheduledTask is the part of a scheduled event a listener reads.
//
// It is declared here rather than imported because the scheduler dispatches
// these events, and a scheduling package that imported this one while this one
// imported it would not compile. It is the minimum the five events below need.
type ScheduledTask interface {
	// GetSummaryForDisplay is the task as a listing prints it: its description
	// when it has one, and the command line it runs otherwise.
	GetSummaryForDisplay() string

	// MutexName is the key the task's overlap lock is stored under, which is
	// what identifies one task across replicas.
	MutexName() string

	// GetExpression is the cron expression the task fires on.
	GetExpression() string
}

// ScheduledTaskStarting is fired before a scheduled task runs.
type ScheduledTaskStarting struct {
	// Task is the scheduled event being run.
	Task ScheduledTask
}

// ScheduledTaskFinished is fired after a scheduled task has run.
type ScheduledTaskFinished struct {
	// Task is the scheduled event that ran.
	Task ScheduledTask

	// Runtime is how long it took, as a duration rather than a bare number of
	// seconds.
	Runtime time.Duration
}

// ScheduledTaskFailed is fired when a scheduled task ended badly.
type ScheduledTaskFailed struct {
	// Task is the scheduled event that failed.
	Task ScheduledTask

	// Exception is what went wrong.
	Exception error
}

// ScheduledTaskSkipped is fired when a task was due and a filter turned it away.
//
// It is fired for a task that was skipped by a when, a skip or an overlap
// lock -- not for one that was simply not due, which is every task in most
// minutes.
type ScheduledTaskSkipped struct {
	// Task is the scheduled event that did not run.
	Task ScheduledTask
}

// ScheduledBackgroundTaskFinished is fired when a task that was sent to the
// background reports back.
type ScheduledBackgroundTaskFinished struct {
	// Task is the scheduled event that ran.
	Task ScheduledTask
}
