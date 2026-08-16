package console

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/console/concerns"
	"github.com/arandu-io/hesape/queue/events"
	"github.com/arandu-io/hesape/queue/jobs"
)

// WorkCommand drains a queue.
//
// It is `queue:work`, with the connection as an argument and the worker's
// options as flags: the process a deployment runs beside the web one, from the
// same image.
//
// The worker it runs is the one the application built, with its handlers
// already registered -- nothing resolves a job name to code on its own, so the
// registry is the thing that has to be passed in.
type WorkCommand struct {
	worker  *queue.Worker
	manager *queue.QueueManager
}

// NewWorkCommand returns the command.
//
// The manager may be nil, and then the worker drains the queue it was built
// with and the connection argument is refused rather than ignored.
func NewWorkCommand(w *queue.Worker, m *queue.QueueManager) *WorkCommand {
	return &WorkCommand{worker: w, manager: m}
}

// Command is the registry entry for queue:work.
func (c *WorkCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:work",
		Description: "start processing jobs on the queue as a daemon",
		Run:         c.Handle,
	}
}

// Handle runs the command.
//
// The exit status is the worker's, and it is what a supervisor reads: see
// queue.WorkerStopReason.
func (c *WorkCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	queueName := flags.String("queue", "", "the queue to drain")
	once := flags.Bool("once", false, "run the next job and stop")
	stopWhenEmpty := flags.Bool("stop-when-empty", false, "stop when the queue is empty")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	// The flags override what the worker was built with: the worker holds its
	// options, so the command replaces them.
	c.worker.FlushState()
	options := c.worker.Options()
	if *queueName != "" {
		options.Queue = *queueName
	}
	options.StopWhenEmpty = options.StopWhenEmpty || *stopWhenEmpty
	c.worker.SetOptions(options)

	if *once {
		return c.worker.RunNextJob(ctx)
	}

	status, err := c.worker.Daemon(ctx)
	if err != nil {
		return err
	}
	if status != queue.ExitSuccess {
		return console.Exit(status, "the worker stopped with status %d", status)
	}
	return nil
}

// ListenCommand runs a worker in a child process and restarts it when it exits.
//
// It is `queue:listen`, and it is for development, where the point is that a
// rebuilt binary is picked up without anybody restarting anything -- see
// queue.Listener for why it is not the way to run a queue in production.
type ListenCommand struct {
	listener *queue.Listener
	options  queue.ListenerOptions
}

// NewListenCommand returns the command.
func NewListenCommand(l *queue.Listener, options queue.ListenerOptions) *ListenCommand {
	return &ListenCommand{listener: l, options: options}
}

// Command is the registry entry for queue:listen.
func (c *ListenCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:listen",
		Description: "listen to a given queue, restarting the worker after each job",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *ListenCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	queueName := flags.String("queue", jobs.DefaultQueue, "the queue to drain")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	connectionName := ""
	if rest := flags.Args(); len(rest) > 0 {
		connectionName = rest[0]
	}

	c.listener.SetOutputHandler(func(line string) { o.Line("%s", strings.TrimRight(line, "\n")) })
	return c.listener.Listen(ctx, connectionName, *queueName, c.options)
}

// RestartCommand asks every running worker to stop after its current job.
//
// It is `queue:restart`, and it is how a deploy replaces the workers -- the new
// binary starts, the old processes notice and exit cleanly, and whatever
// supervises them starts the new image.
type RestartCommand struct {
	manager *queue.QueueManager
}

// NewRestartCommand returns the command.
func NewRestartCommand(m *queue.QueueManager) *RestartCommand { return &RestartCommand{manager: m} }

// Command is the registry entry for queue:restart.
func (c *RestartCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:restart",
		Description: "restart queue worker daemons after their current job",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *RestartCommand) Handle(ctx context.Context, o *console.IO) error {
	if err := c.manager.Restart(ctx); err != nil {
		return err
	}
	o.Info("broadcasting the restart signal")
	return nil
}

// PauseCommand stops workers taking new jobs off a queue.
//
// It is `queue:pause`, the switch to reach for when a downstream system is
// failing and retrying into it is making things worse.
type PauseCommand struct {
	manager *queue.QueueManager
}

// NewPauseCommand returns the command.
func NewPauseCommand(m *queue.QueueManager) *PauseCommand { return &PauseCommand{manager: m} }

// Command is the registry entry for queue:pause.
func (c *PauseCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:pause",
		Description: "pause job processing for a queue, as connection:queue",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *PauseCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	forDuration := flags.Duration("for", 0, "resume on its own after this long")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	arg := ""
	if rest := flags.Args(); len(rest) > 0 {
		arg = rest[0]
	}
	connectionName, queueName := concerns.ParsesQueue{}.ParseQueue(arg)

	if *forDuration > 0 {
		if err := c.manager.PauseFor(ctx, connectionName, queueName, *forDuration); err != nil {
			return err
		}
		o.Info("%s is paused for %s", queueName, *forDuration)
		return nil
	}
	if err := c.manager.Pause(ctx, connectionName, queueName); err != nil {
		return err
	}
	o.Info("%s is paused. Resume it with `aru queue:resume %s`", queueName, arg)
	return nil
}

// ResumeCommand lets workers take jobs off a paused queue again. It is
// `queue:resume`.
type ResumeCommand struct {
	manager *queue.QueueManager
}

// NewResumeCommand returns the command.
func NewResumeCommand(m *queue.QueueManager) *ResumeCommand { return &ResumeCommand{manager: m} }

// Command is the registry entry for queue:resume.
func (c *ResumeCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:resume",
		Description: "resume job processing for a paused queue, as connection:queue",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *ResumeCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	arg := ""
	if rest := flags.Args(); len(rest) > 0 {
		arg = rest[0]
	}
	connectionName, queueName := concerns.ParsesQueue{}.ParseQueue(arg)

	if err := c.manager.Resume(ctx, connectionName, queueName); err != nil {
		return err
	}
	o.Info("%s is processing again", queueName)
	return nil
}

// ClearCommand deletes every job waiting on a queue.
//
// It is `queue:clear`. Parked jobs are not cleared -- a job that gave up is in
// the dead letter list, not on a queue, and `aru queue:flush` is what empties
// that.
type ClearCommand struct {
	manager *queue.QueueManager
}

// NewClearCommand returns the command.
func NewClearCommand(m *queue.QueueManager) *ClearCommand { return &ClearCommand{manager: m} }

// Command is the registry entry for queue:clear.
func (c *ClearCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:clear",
		Description: "delete all of the jobs waiting on a queue",
		Run:         c.Handle,
	}
}

// Handle runs the command.
//
// It asks first, unless --force, and it asks in every environment: a queue with
// jobs on it is somebody's work whichever one it is.
func (c *ClearCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	queueName := flags.String("queue", jobs.DefaultQueue, "the queue to empty")
	force := flags.Bool("force", false, "do not ask for confirmation")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	connectionName := ""
	if rest := flags.Args(); len(rest) > 0 {
		connectionName = rest[0]
	}
	q, err := c.manager.Connection(connectionName)
	if err != nil {
		return err
	}

	if !*force {
		confirmed, err := o.Confirm(fmt.Sprintf("delete every job waiting on %s?", *queueName), false)
		if err != nil {
			return err
		}
		if !confirmed {
			o.Comment("nothing was cleared")
			return nil
		}
	}

	removed, err := q.Clear(ctx, *queueName)
	if err != nil {
		return err
	}
	o.Info("%d job(s) deleted from %s", removed, *queueName)
	return nil
}

// MonitorCommand reports how much work is waiting, and says so loudly when it
// is too much.
//
// It is `queue:monitor`, meant to run on a schedule: the table is for a person,
// and the QueueBusy event is for whatever pages one.
type MonitorCommand struct {
	manager *queue.QueueManager
	events  queue.Dispatcher
}

// NewMonitorCommand returns the command.
//
// The dispatcher may be nil, and then the command prints the table and
// announces nothing.
func NewMonitorCommand(m *queue.QueueManager, d queue.Dispatcher) *MonitorCommand {
	return &MonitorCommand{manager: m, events: d}
}

// Command is the registry entry for queue:monitor.
func (c *MonitorCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:monitor",
		Description: "report the size of the given queues, as connection:queue",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *MonitorCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	max := flags.Int("max", 1000, "how many waiting jobs count as busy")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	names := flags.Args()
	if len(names) == 0 {
		return fmt.Errorf("queue: name at least one queue to monitor, as connection:queue")
	}

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		connectionName, queueName := concerns.ParsesQueue{}.ParseQueue(name)
		q, err := c.manager.Connection(connectionName)
		if err != nil {
			return err
		}

		size, err := q.PendingSize(ctx, queueName)
		if err != nil {
			return err
		}
		status := "ok"
		if size >= *max {
			status = "busy"
			if c.events != nil {
				c.events.Dispatch(events.QueueBusy{
					ConnectionName: c.manager.GetName(connectionName),
					Queue:          queueName,
					Size:           size,
				})
			}
		}
		rows = append(rows, []string{
			c.manager.GetName(connectionName) + ":" + queueName,
			strconv.Itoa(size),
			status,
		})
	}

	o.Table([]string{"queue", "waiting", "status"}, rows)
	return nil
}
