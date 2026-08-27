package queue_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/process"
	"github.com/arandu-io/hesape/queue"
)

// countingFake answers every command and stops the listener's loop after the
// given number of children, so a `for` with no end can be tested.
//
// PreventStrayProcesses is on: a listener that built a command the fake does not
// answer fails naming it rather than starting a program that is not there.
func countingFake(stop context.CancelFunc, after int, result any) *process.Factory {
	runs := 0
	factory := process.NewFactory()
	return factory.Fake(process.FakeHandler{
		Command: "*",
		Result: func(*process.PendingProcess) any {
			runs++
			if runs >= after {
				stop()
			}
			return result
		},
	}).PreventStrayProcesses()
}

// TestTheListenerStartsTheWorkerWithItsFlagsAsArguments is the round trip that
// makes a listener and a worker configurable in one place, and the thing that
// keeps it honest: a queue named with a semicolon and a space is one argument,
// because nothing between here and the child parses a line.
func TestTheListenerStartsTheWorkerWithItsFlagsAsArguments(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	factory := countingFake(cancel, 1, "")
	listener := queue.NewListener("/srv/app", "/usr/local/bin/app", "work").UseProcessFactory(factory)

	options := queue.ListenerOptions{}
	options.Name = "default"
	options.MaxTries = 3
	options.Memory = 128

	if err := listener.Listen(ctx, "redis", "mail; touch /tmp/pwned", options); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ran := factory.Recorded()
	if len(ran) == 0 {
		t.Fatal("the listener started no worker at all")
	}

	// The queue name renders quoted because it is one word with spaces in it.
	// That is the whole assertion: a shell would have seen two commands.
	if want := `"--queue=mail; touch /tmp/pwned"`; !strings.Contains(ran[0], want) {
		t.Errorf("the worker was started as %s\nand the queue name is not the single argument %s", ran[0], want)
	}
	for _, want := range []string{"/usr/local/bin/app work redis --once", "--tries=3", "--memory=128"} {
		if !strings.Contains(ran[0], want) {
			t.Errorf("the worker was started as %s, which is missing %q", ran[0], want)
		}
	}
}

// TestTheListenerStartsAnotherWorkerWhenOneExitsBadly is what the listener is
// for. A worker that ran and failed is a result, not an error, and the loop
// starts the next one.
func TestTheListenerStartsAnotherWorkerWhenOneExitsBadly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	factory := process.NewFactory()
	failed := factory.Result("", "the worker gave up", queue.ExitMemoryLimit)
	listener := queue.NewListener(".", "app", "work").
		UseProcessFactory(countingFake(cancel, 3, failed))

	if err := listener.Listen(ctx, "redis", "default", queue.ListenerOptions{}); err != nil {
		t.Fatalf("Listen returned %v; a worker that exited non-zero is what the listener restarts", err)
	}
}

// TestTheListenerStopsWhenTheWorkerCannotBeStarted is the other half: retrying a
// binary that is not there is a loop that fills a disk with the same line.
func TestTheListenerStopsWhenTheWorkerCannotBeStarted(t *testing.T) {
	factory := process.NewFactory().
		Fake(process.FakeHandler{Command: "some:other:command", Result: ""}).
		PreventStrayProcesses()

	listener := queue.NewListener(".", "app", "work").UseProcessFactory(factory)

	err := listener.Listen(t.Context(), "redis", "default", queue.ListenerOptions{})
	if err == nil {
		t.Fatal("Listen returned nil for a worker that never ran, and the loop would spin forever")
	}
	if !strings.Contains(err.Error(), "starting the worker") {
		t.Errorf("Listen reported %v, want the failure to start", err)
	}
}

// TestAListenerWithNoCommandSaysSo: a listener built with nothing to run must
// not loop starting nothing.
func TestAListenerWithNoCommandSaysSo(t *testing.T) {
	if err := queue.NewListener(".").Listen(t.Context(), "redis", "default", queue.ListenerOptions{}); err == nil {
		t.Fatal("a listener with no command ran anyway")
	}
}
