package console

import (
	"context"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/failed"
)

// batchAction is the action the batch commands hold a system grant for.
const batchAction auth.Action = "queue:batches"

// RetryBatchCommand puts the failed jobs of a batch back in line.
//
// It answers Illuminate\Queue\Console\RetryBatchCommand: `queue:retry-batch`.
// It is `queue:retry` narrowed to one batch, which is the shape the question
// usually arrives in: the nightly import half-failed, and what has to be retried
// is the half that belongs to it.
type RetryBatchCommand struct {
	batches bus.BatchRepository
	failed  failed.FailedJobProvider
	manager *queue.QueueManager
}

// NewRetryBatchCommand returns the command.
func NewRetryBatchCommand(b bus.BatchRepository, p failed.FailedJobProvider, m *queue.QueueManager) *RetryBatchCommand {
	return &RetryBatchCommand{batches: b, failed: p, manager: m}
}

// Command is the registry entry for queue:retry-batch.
func (c *RetryBatchCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:retry-batch",
		Description: "put the failed jobs of a batch back in line",
		Run:         c.Handle,
	}
}

// IsolatableID is the lock this command takes, which is per batch.
//
// It answers isolatableId(), which in PHP is what makes `--isolated` mean "one
// at a time per batch" rather than "one at a time". Two people retrying two
// different batches must not block each other; two people retrying the same one
// must, or the jobs are pushed twice.
func (c *RetryBatchCommand) IsolatableID(batchID string) string {
	return "queue:retry-batch:" + batchID
}

// Handle runs the command. It answers RetryBatchCommand::handle().
func (c *RetryBatchCommand) Handle(ctx context.Context, o *console.IO) error {
	tenant := tenantFlag(o)
	if err := o.Flags().Parse(o.Args()); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("queue: --tenant is required. A batch belongs to one customer")
	}
	rest := o.Flags().Args()
	if len(rest) == 0 {
		return fmt.Errorf("queue: name the batch to retry")
	}
	batchID := rest[0]
	g := auth.SystemGrant(batchAction, *tenant)

	batch, err := c.batches.Find(ctx, g, batchID)
	if err != nil {
		return err
	}
	if len(batch.FailedJobIDs) == 0 {
		o.Comment("no failed jobs in %s", batchID)
		return nil
	}

	retried := 0
	for _, id := range batch.FailedJobIDs {
		job, err := c.failed.Find(ctx, g, id)
		if err != nil {
			o.Error("%s: %v", id, err)
			continue
		}
		target, err := c.manager.Connection(job.Connection)
		if err != nil {
			return err
		}
		pusher, can := target.(interface {
			PushRaw(context.Context, auth.Grant, string, []byte, string) error
		})
		if !can {
			return fmt.Errorf("queue: the %s connection cannot take a job back", job.Connection)
		}
		if err := pusher.PushRaw(ctx, g, job.Name, job.Payload, job.Queue); err != nil {
			o.Error("%s: %v", id, err)
			continue
		}
		if _, err := c.failed.Forget(ctx, g, id); err != nil {
			o.Error("%s was queued again but not forgotten: %v", id, err)
			continue
		}
		retried++
	}

	o.Info("%d job(s) from %s were pushed back", retried, batchID)
	return nil
}

// PruneBatchesCommand deletes the batch rows that are old enough not to matter.
//
// It answers Illuminate\Queue\Console\PruneBatchesCommand:
// `queue:prune-batches`, with the three retentions Laravel has -- finished,
// unfinished and cancelled -- because they are three different decisions. A
// finished batch is history; an unfinished one that is a week old is a bug
// somebody should have seen.
type PruneBatchesCommand struct {
	batches bus.PrunableBatchRepository
}

// NewPruneBatchesCommand returns the command.
//
// It takes the prunable repository rather than the plain one, which is the Go
// form of the `instanceof PrunableBatchRepository` check the PHP does at the top
// of handle(): a store that cannot prune cannot be wired here, instead of being
// wired and then refusing at three in the morning.
func NewPruneBatchesCommand(b bus.PrunableBatchRepository) *PruneBatchesCommand {
	return &PruneBatchesCommand{batches: b}
}

// Command is the registry entry for queue:prune-batches.
func (c *PruneBatchesCommand) Command() console.Command {
	return console.Command{
		Name:        "queue:prune-batches",
		Description: "delete the stale entries from the batches table",
		// Two of these at once would each count the rows the other is
		// deleting, and both would report a number that was never true.
		Isolated: "queue:prune-batches",
		Run:      c.Handle,
	}
}

// Handle runs the command. It answers PruneBatchesCommand::handle().
func (c *PruneBatchesCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := tenantFlag(o)
	hours := flags.Int("hours", 24, "delete the finished batches older than this many hours")
	unfinished := flags.Int("unfinished", 0, "also delete the unfinished batches older than this many hours")
	cancelled := flags.Int("cancelled", 0, "also delete the cancelled batches older than this many hours")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("queue: --tenant is required. A batch belongs to one customer")
	}
	g := auth.SystemGrant(batchAction, *tenant)

	total, err := c.batches.Prune(ctx, g, cut(*hours))
	if err != nil {
		return err
	}
	if *unfinished > 0 {
		removed, err := c.batches.PruneUnfinished(ctx, g, cut(*unfinished))
		if err != nil {
			return err
		}
		total += removed
	}
	if *cancelled > 0 {
		removed, err := c.batches.PruneCancelled(ctx, g, cut(*cancelled))
		if err != nil {
			return err
		}
		total += removed
	}

	o.Info("%d batch(es) deleted", total)
	return nil
}

// cut is the instant a retention of n hours reaches back to.
func cut(hours int) time.Time {
	return time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
}
