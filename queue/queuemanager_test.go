package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/events"
)

// recordingDispatcher is the two methods queue.Dispatcher asks for, keeping
// what went through so a test can read it.
type recordingDispatcher struct {
	dispatched []any
	until      any
}

func (d *recordingDispatcher) Dispatch(event any, _ ...any) []any {
	d.dispatched = append(d.dispatched, event)
	return nil
}

func (d *recordingDispatcher) Until(event any, _ ...any) any {
	d.dispatched = append(d.dispatched, event)
	return d.until
}

func (d *recordingDispatcher) sawEvent(want any) bool {
	for _, event := range d.dispatched {
		if event == want {
			return true
		}
	}
	return false
}

func TestConnectorIsNotOpenedUntilItIsAskedFor(t *testing.T) {
	m := queue.NewQueueManager()
	opened := 0
	m.AddConnector("database", func() (queue.Queue, error) {
		opened++
		return queue.NullQueue{}, nil
	})

	if opened != 0 {
		t.Fatalf("the connector ran %d times before anybody asked", opened)
	}
	if m.Connected("database") {
		t.Error("Connected says yes about a connection nobody opened")
	}

	if _, err := m.Connection("database"); err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if _, err := m.Connection("database"); err != nil {
		t.Fatalf("Connection: %v", err)
	}
	// Once, not twice: a connector that reopens the socket on every push is a
	// connection pool nobody asked for.
	if opened != 1 {
		t.Errorf("the connector ran %d times for two calls", opened)
	}
	if !m.Connected("database") {
		t.Error("Connected says no about a connection it just handed out")
	}
}

func TestExtendNamesTheConnection(t *testing.T) {
	q := queue.NewDatabaseQueue(nil)
	queue.NewQueueManager().Extend("primary", q)

	// The name is what a popped job reports and what a pause is recorded
	// under, so a queue registered under one name must not answer to another.
	if q.GetConnectionName() != "primary" {
		t.Errorf("the connection is named %q after being registered as primary", q.GetConnectionName())
	}
}

// TestPauseAndResumeAreRecordedWhereTheWorkerLooks is the switch an incident
// reaches for: the worker reads the same key the manager writes.
func TestPauseAndResumeAreRecordedWhereTheWorkerLooks(t *testing.T) {
	ctx := context.Background()
	m := queue.NewQueueManager().SetCache(cache.NewArrayStore())
	m.Extend("database", queue.NullQueue{})
	dispatcher := &recordingDispatcher{}
	m.SetEvents(dispatcher)

	if paused, err := m.IsPaused(ctx, "database", "default"); err != nil || paused {
		t.Fatalf("a fresh queue reports paused = %v (%v)", paused, err)
	}

	if err := m.Pause(ctx, "database", "default"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused, err := m.IsPaused(ctx, "database", "default"); err != nil || !paused {
		t.Fatalf("a paused queue reports paused = %v (%v)", paused, err)
	}
	if !dispatcher.sawEvent(events.QueuePaused{ConnectionName: "database", Queue: "default"}) {
		t.Errorf("nothing announced the pause: %v", dispatcher.dispatched)
	}

	if err := m.Resume(ctx, "database", "default"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if paused, err := m.IsPaused(ctx, "database", "default"); err != nil || paused {
		t.Errorf("a resumed queue reports paused = %v (%v)", paused, err)
	}
	if !dispatcher.sawEvent(events.QueueResumed{ConnectionName: "database", Queue: "default"}) {
		t.Errorf("nothing announced the resume: %v", dispatcher.dispatched)
	}
}

// TestPauseWithoutACacheSaysSo: a queue somebody believes is paused and is not
// is worse than an error.
func TestPauseWithoutACacheSaysSo(t *testing.T) {
	m := queue.NewQueueManager()
	if err := m.Pause(context.Background(), "database", "default"); err == nil {
		t.Error("Pause with nowhere to record it returned nil")
	}
	if err := m.Restart(context.Background()); err == nil {
		t.Error("Restart with nowhere to record it returned nil")
	}
}

// TestPauseForRefusesAPauseWithNoDeadline: PauseFor exists to be the safe half,
// and a zero duration would make it the unsafe one under a different name.
func TestPauseForRefusesAPauseWithNoDeadline(t *testing.T) {
	m := queue.NewQueueManager().SetCache(cache.NewArrayStore())
	if err := m.PauseFor(context.Background(), "database", "default", 0); err == nil {
		t.Error("PauseFor(0) was accepted")
	}
}

// TestRestartStopsAWorkerThatWasAlreadyRunning is the deploy story: the
// timestamp moves, and the worker that read the old one exits after its batch.
func TestRestartStopsAWorkerThatWasAlreadyRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := cache.NewArrayStore()
	m := queue.NewQueueManager().SetCache(store)
	w := queue.NewWorker(&fakeQueue{}, queue.WorkerOptions{Sleep: time.Millisecond}).SetCache(store)

	done := make(chan int, 1)
	go func() {
		status, _ := w.Daemon(ctx)
		done <- status
	}()

	// Let the worker read the absent signal before it is written, so what it
	// notices is the change and not the value.
	time.Sleep(20 * time.Millisecond)
	if err := m.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	select {
	case status := <-done:
		if status != queue.ExitSuccess {
			t.Errorf("the worker exited with %d after a restart signal", status)
		}
	case <-ctx.Done():
		t.Fatal("the worker did not stop after the restart signal")
	}
}

func TestQueueRoutesSendAJobToItsConnectionAndQueue(t *testing.T) {
	routes := queue.NewQueueRoutes()
	routes.Set("report.monthly", "reports", "")
	routes.Set("invoice.send", "", "redis")

	if got := routes.GetQueue("report.monthly"); got != "reports" {
		t.Errorf("report.monthly goes on %q", got)
	}
	if got := routes.GetConnection("invoice.send"); got != "redis" {
		t.Errorf("invoice.send goes to %q", got)
	}
	if _, routed := routes.GetRoute("nothing.here"); routed {
		t.Error("an unrouted name came back routed")
	}
	if got := len(routes.All()); got != 2 {
		t.Errorf("All returned %d routes", got)
	}
	// All hands out a copy: the table is read concurrently by whatever
	// dispatches jobs, and handing out the map would be a data race.
	routes.All()["report.monthly"] = queue.Route{Queue: "elsewhere"}
	if got := routes.GetQueue("report.monthly"); got != "reports" {
		t.Errorf("the table was changed through All: %q", got)
	}
}
