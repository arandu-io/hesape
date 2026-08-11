package console_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/queue"
	queueconsole "github.com/arandu-io/hesape/queue/console"
	"github.com/arandu-io/hesape/queue/events"
	"github.com/arandu-io/hesape/queue/failed"
)

// run drives one command and returns what it printed.
func run(t *testing.T, c console.Command, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	o := console.NewIO(c.Name, args, &out, &errOut, strings.NewReader(""))
	err := c.Run(context.Background(), o)
	return out.String() + errOut.String(), err
}

func TestPauseAndResumeRecordTheState(t *testing.T) {
	m := queue.NewQueueManager().SetCache(cache.NewArrayStore())
	m.Extend("database", queue.NullQueue{})

	if _, err := run(t, queueconsole.NewPauseCommand(m).Command(), "database:reports"); err != nil {
		t.Fatalf("queue:pause: %v", err)
	}
	paused, err := m.IsPaused(context.Background(), "database", "reports")
	if err != nil || !paused {
		t.Fatalf("the queue reports paused = %v (%v)", paused, err)
	}

	if _, err := run(t, queueconsole.NewResumeCommand(m).Command(), "database:reports"); err != nil {
		t.Fatalf("queue:resume: %v", err)
	}
	if paused, _ := m.IsPaused(context.Background(), "database", "reports"); paused {
		t.Error("the queue is still paused after queue:resume")
	}
}

// runWithInput drives one command with something on its standard input, for the
// commands that ask before they act.
func runWithInput(t *testing.T, c console.Command, input string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	o := console.NewIO(c.Name, args, &out, &errOut, strings.NewReader(input))
	err := c.Run(context.Background(), o)
	return out.String() + errOut.String(), err
}

// busyQueue reports a backlog, so the monitor has something to be loud about.
type busyQueue struct {
	queue.NullQueue
	waiting int
}

func (q busyQueue) PendingSize(context.Context, string) (int, error) { return q.waiting, nil }

// recordingDispatcher keeps what the monitor announced.
type recordingDispatcher struct{ dispatched []any }

func (d *recordingDispatcher) Dispatch(event any, _ ...any) []any {
	d.dispatched = append(d.dispatched, event)
	return nil
}

func (d *recordingDispatcher) Until(any, ...any) any { return nil }

func TestMonitorAnnouncesAQueueThatIsBehind(t *testing.T) {
	m := queue.NewQueueManager().Extend("database", busyQueue{waiting: 5000})
	dispatcher := &recordingDispatcher{}

	printed, err := run(t, queueconsole.NewMonitorCommand(m, dispatcher).Command(),
		"-max=1000", "database:default")
	if err != nil {
		t.Fatalf("queue:monitor: %v", err)
	}
	if !strings.Contains(printed, "busy") {
		t.Errorf("the table does not say the queue is busy:\n%s", printed)
	}

	want := events.QueueBusy{ConnectionName: "database", Queue: "default", Size: 5000}
	if len(dispatcher.dispatched) != 1 || dispatcher.dispatched[0] != want {
		t.Errorf("the monitor announced %v", dispatcher.dispatched)
	}
}

func TestMonitorSaysNothingAboutAQueueThatIsKeepingUp(t *testing.T) {
	m := queue.NewQueueManager().Extend("database", busyQueue{waiting: 3})
	dispatcher := &recordingDispatcher{}

	if _, err := run(t, queueconsole.NewMonitorCommand(m, dispatcher).Command(),
		"-max=1000", "database:default"); err != nil {
		t.Fatalf("queue:monitor: %v", err)
	}
	if len(dispatcher.dispatched) != 0 {
		t.Errorf("the monitor announced %v about a queue that is fine", dispatcher.dispatched)
	}
}

// TestMonitorNeedsAQueueToWatch: a monitor with no arguments that silently
// watches nothing is a monitor somebody believes is running.
func TestMonitorNeedsAQueueToWatch(t *testing.T) {
	m := queue.NewQueueManager().Extend("database", queue.NullQueue{})
	if _, err := run(t, queueconsole.NewMonitorCommand(m, nil).Command()); err == nil {
		t.Error("queue:monitor with no queues returned nil")
	}
}

// TestClearAsksBeforeItEmptiesAQueue: a queue with jobs on it is somebody's
// work, in whichever environment it is.
func TestClearAsksBeforeItEmptiesAQueue(t *testing.T) {
	m := queue.NewQueueManager().Extend("database", queue.NullQueue{})

	printed, err := runWithInput(t, queueconsole.NewClearCommand(m).Command(), "no\n")
	if err != nil {
		t.Fatalf("queue:clear: %v", err)
	}
	if !strings.Contains(printed, "nothing was cleared") {
		t.Errorf("an unconfirmed clear went ahead:\n%s", printed)
	}

	printed, err = run(t, queueconsole.NewClearCommand(m).Command(), "-force")
	if err != nil {
		t.Fatalf("queue:clear -force: %v", err)
	}
	if strings.Contains(printed, "nothing was cleared") {
		t.Errorf("a forced clear asked anyway:\n%s", printed)
	}
}

// TestTheFailedJobCommandsRefuseWithoutATenant is RULE 17 at the console: a
// listing that defaulted would print whichever customer sorted first.
func TestTheFailedJobCommandsRefuseWithoutATenant(t *testing.T) {
	var provider nullProvider
	for _, c := range []console.Command{
		queueconsole.NewListFailedCommand(provider).Command(),
		queueconsole.NewForgetFailedCommand(provider).Command(),
		queueconsole.NewFlushFailedCommand(provider).Command(),
	} {
		if _, err := run(t, c, "j-1"); err == nil {
			t.Errorf("%s ran without a tenant", c.Name)
		}
	}
}

// nullProvider is the failed job provider that keeps nothing, which is all
// these commands need to be refused for the right reason.
type nullProvider = failed.NullFailedJobProvider
