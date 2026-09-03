package console

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/events"
	"github.com/arandu-io/hesape/queue/failed"
	"github.com/arandu-io/hesape/queue/jobs"
)

// tenantFlag is the flag every failed job command takes.
//
// It is not optional and it has no default. A failed job carries a customer's
// payload, so listing them is a read like any other and it is scoped by
// tenant; a command that defaulted the tenant would be a command that prints
// whichever customer happened to sort first.
func tenantFlag(o *console.IO) *string {
	return o.Flags().String("tenant", "", "the tenant whose failed jobs to work with")
}

func grantFor(tenant string) (auth.Grant, error) {
	if tenant == "" {
		return auth.Grant{}, fmt.Errorf("queue: --tenant is required. A failed job list is one customer's, and there is no default")
	}
	return auth.SystemGrant(failed.Action, tenant), nil
}

// requeue puts one recorded failure back in line on the connection it came off.
//
// The driver is asked first, because a driver that still holds the parked job
// holds the job itself: the row it un-parks already carries the id, the action,
// the tenant and the arguments, and none of them is rebuilt from anything.
// Pushing a job instead would leave that copy parked for good -- a dead letter
// list reporting the failure gone beside a driver still reporting it -- and on a
// store keyed by the job's id the push would not be accepted at all.
//
// A driver with no such parked job is the case the record exists for: the store
// was flushed, or it never kept the job. The record is then the only copy left,
// and the job is rebuilt from it.
//
// Either way the work goes back under the id it failed with and the action it
// was pushed with. tenant comes from the Grant the list was read under, which is
// the one the record was found by.
func requeue(ctx context.Context, target queue.Queue, tenant string, record failed.FailedJob) error {
	if err := target.Retry(ctx, record.UUID); !errors.Is(err, queue.ErrNotParked) {
		return err
	}
	if record.Action == "" {
		return fmt.Errorf("queue: %s was recorded before the list kept the permission a job was pushed under, and the connection no longer holds the job. Rebuilding it would mean choosing an action nobody granted", record.UUID)
	}

	// The job's own action, never failed.Action: the worker reissues the Grant
	// from what the record says, so this is the permission the work runs with.
	g := auth.SystemGrant(auth.Action(record.Action), tenant)
	return target.Push(ctx, g, jobs.Job{
		UUID:         record.UUID,
		Queue:        record.Queue,
		Name:         record.Name,
		TenantID:     tenant,
		Payload:      record.Payload,
		Action:       record.Action,
		AuthorizedBy: g.Subject().ID,
	})
}

// ListFailedCommand prints the jobs that gave up. It is `queue:failed`.
type ListFailedCommand struct {
	failed failed.FailedJobProvider
}

// NewListFailedCommand returns the command.
func NewListFailedCommand(p failed.FailedJobProvider) *ListFailedCommand {
	return &ListFailedCommand{failed: p}
}

// Command is the registry entry for queue:failed.
func (c *ListFailedCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:failed",
		Description: "list the queue jobs that gave up",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *ListFailedCommand) Handle(ctx context.Context, o *console.IO) error {
	tenant := tenantFlag(o)
	if err := o.Flags().Parse(o.Args()); err != nil {
		return err
	}
	g, err := grantFor(*tenant)
	if err != nil {
		return err
	}

	all, err := c.failed.All(ctx, g)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		o.Comment("no failed jobs")
		return nil
	}

	rows := make([][]string, 0, len(all))
	for _, job := range all {
		rows = append(rows, []string{
			job.ID,
			job.Connection + ":" + job.Queue,
			job.Name,
			job.FailedAt.Format(time.RFC3339),
			job.Exception,
		})
	}
	o.Table([]string{"id", "connection:queue", "job", "failed at", "why"}, rows)
	return nil
}

// RetryCommand puts a failed job back in line.
//
// It is `queue:retry`, with the ids as arguments and --queue to take every
// failure off one queue. Without it the only way out of a dead letter list is
// SQL by hand, which is how it becomes a table nobody touches.
type RetryCommand struct {
	failed  failed.FailedJobProvider
	manager *queue.QueueManager
	events  queue.Dispatcher
}

// NewRetryCommand returns the command.
func NewRetryCommand(p failed.FailedJobProvider, m *queue.QueueManager, d queue.Dispatcher) *RetryCommand {
	return &RetryCommand{failed: p, manager: m, events: d}
}

// Command is the registry entry for queue:retry.
func (c *RetryCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:retry",
		Description: "put failed queue jobs back in line, by id or by queue",
		Run:         c.Handle,
	}
}

// Handle runs the command.
//
// The order matters: the job is pushed back before it is forgotten, so a push
// that fails leaves the failure where it was. Forgetting first and then failing
// to push loses the job.
//
// Two Grants, and they are not interchangeable. The list is read under
// [failed.Action], which is an administrator's permission over other people's
// failures; the job goes back under the action it was pushed with, which is
// what the worker will reissue its Grant from. Using one for both is the whole
// defect: the read Grant would put the work back as the administrator.
func (c *RetryCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := tenantFlag(o)
	queueName := flags.String("queue", "", "retry every failure from this queue")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}
	g, err := grantFor(*tenant)
	if err != nil {
		return err
	}

	ids := flags.Args()
	if *queueName != "" {
		fromQueue, err := c.failed.IDs(ctx, g, *queueName)
		if err != nil {
			return err
		}
		ids = append(ids, fromQueue...)
	}
	if len(ids) == 0 {
		return fmt.Errorf("queue: name at least one failed job id, or a --queue to retry all of")
	}

	for _, id := range ids {
		job, err := c.failed.Find(ctx, g, id)
		if err != nil {
			o.Error("%s: %v", id, err)
			continue
		}

		target, err := c.manager.Connection(job.Connection)
		if err != nil {
			return err
		}
		if err := requeue(ctx, target, auth.Tenant(g), job); err != nil {
			o.Error("%s: %v", id, err)
			continue
		}
		if _, err := c.failed.Forget(ctx, g, id); err != nil {
			o.Error("%s was queued again but not forgotten: %v", id, err)
			continue
		}
		if c.events != nil {
			c.events.Dispatch(events.JobRetryRequested{})
		}
		o.Info("%s was pushed back onto %s", id, job.Queue)
	}
	return nil
}

// ForgetFailedCommand deletes one failed job. It is `queue:forget`.
type ForgetFailedCommand struct {
	failed failed.FailedJobProvider
}

// NewForgetFailedCommand returns the command.
func NewForgetFailedCommand(p failed.FailedJobProvider) *ForgetFailedCommand {
	return &ForgetFailedCommand{failed: p}
}

// Command is the registry entry for queue:forget.
func (c *ForgetFailedCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:forget",
		Description: "delete one failed queue job",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *ForgetFailedCommand) Handle(ctx context.Context, o *console.IO) error {
	tenant := tenantFlag(o)
	if err := o.Flags().Parse(o.Args()); err != nil {
		return err
	}
	g, err := grantFor(*tenant)
	if err != nil {
		return err
	}

	rest := o.Flags().Args()
	if len(rest) == 0 {
		return fmt.Errorf("queue: name the failed job to delete")
	}

	forgotten, err := c.failed.Forget(ctx, g, rest[0])
	if err != nil {
		return err
	}
	if !forgotten {
		o.Comment("no failed job with that id")
		return nil
	}
	o.Info("%s was deleted", rest[0])
	return nil
}

// FlushFailedCommand deletes the failed jobs.
//
// It is `queue:flush`, with --hours to keep the recent ones.
type FlushFailedCommand struct {
	failed failed.FailedJobProvider
}

// NewFlushFailedCommand returns the command.
func NewFlushFailedCommand(p failed.FailedJobProvider) *FlushFailedCommand {
	return &FlushFailedCommand{failed: p}
}

// Command is the registry entry for queue:flush.
func (c *FlushFailedCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:flush",
		Description: "delete the failed queue jobs",
		Run:         c.Handle,
	}
}

// Handle runs the command.
func (c *FlushFailedCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := tenantFlag(o)
	hours := flags.Int("hours", 0, "keep the failures from the last this many hours")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}
	g, err := grantFor(*tenant)
	if err != nil {
		return err
	}

	if err := c.failed.Flush(ctx, g, time.Duration(*hours)*time.Hour); err != nil {
		return err
	}
	o.Info("the failed jobs were deleted")
	return nil
}

// PruneFailedJobsCommand deletes the failed jobs that are old enough not to
// matter.
//
// It is `queue:prune-failed`, meant to run on a schedule, which is the
// difference from `queue:flush`: that one is a person deciding, this one is the
// retention policy.
type PruneFailedJobsCommand struct {
	failed failed.PrunableFailedJobProvider
}

// NewPruneFailedJobsCommand returns the command.
func NewPruneFailedJobsCommand(p failed.PrunableFailedJobProvider) *PruneFailedJobsCommand {
	return &PruneFailedJobsCommand{failed: p}
}

// Command is the registry entry for queue:prune-failed.
func (c *PruneFailedJobsCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:prune-failed",
		Description: "delete the failed queue jobs older than the retention",
		// Two of these at once would each count the rows the other is
		// deleting, and both would report a number that was never true.
		Isolated: "queue:prune-failed",
		Run:      c.Handle,
	}
}

// Handle runs the command.
func (c *PruneFailedJobsCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := tenantFlag(o)
	hours := flags.Int("hours", 24, "delete the failures older than this many hours")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}
	g, err := grantFor(*tenant)
	if err != nil {
		return err
	}

	removed, err := c.failed.Prune(ctx, g, time.Now().UTC().Add(-time.Duration(*hours)*time.Hour))
	if err != nil {
		return err
	}
	o.Info("%d failed job(s) deleted", removed)
	return nil
}
