package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

// ListenerOptions configures a [Listener].
//
// It answers Illuminate\Queue\ListenerOptions, which extends WorkerOptions with
// one field. Go has no inheritance, so it embeds instead -- which is the same
// arrangement with the same fields under the same names.
type ListenerOptions struct {
	// WorkerOptions is what each child worker runs with. The listener turns
	// them back into the flags it starts the child with.
	WorkerOptions

	// Environment is the environment the child workers run in, and empty means
	// they inherit this process's. It answers $environment.
	Environment string
}

// Listener runs a worker in a child process and restarts it when it exits.
//
// It answers Illuminate\Queue\Listener, which is what `queue:listen` is. In PHP
// it exists for one reason: a long-lived worker holds a copy of the application
// in memory, so code deployed after it started is code it will never run, and
// the only fix is to start a new process per job.
//
// Go has that problem in a much smaller way -- the binary is the binary -- so
// this is not the way to run a queue in production. [Worker] is. What is left
// for the listener is development: `aru queue:listen` picks up a rebuilt binary
// without anybody remembering to restart anything.
//
// The child is started with Command, which is the path of the binary and the
// arguments before the ones this adds. An application whose worker command is
// not `aru work` says so there.
type Listener struct {
	// commandPath is the working directory the child is started in. It answers
	// $commandPath.
	commandPath string
	// command is the binary and the leading arguments.
	command []string
	// outputHandler receives each line the child writes.
	outputHandler func(line string)
	// stop ends the loop. It is a function so [Listener.Stop] can be called
	// from a signal handler and from a test.
	stop context.CancelFunc
}

// NewListener returns a listener that starts command in dir.
//
// The PHP constructor takes the path and works the command out from the PHP
// binary and the artisan script. Here the binary is the thing being run, so
// there is nothing to work out and the caller says what it is:
//
//	queue.NewListener(".", os.Args[0], "work")
func NewListener(dir string, command ...string) *Listener {
	return &Listener{commandPath: dir, command: command}
}

// SetOutputHandler sets what to do with each line the child writes.
//
// It answers setOutputHandler(). Without one the child's output goes nowhere,
// which is what a test wants; `aru queue:listen` passes one that writes to the
// terminal.
func (l *Listener) SetOutputHandler(handler func(line string)) {
	l.outputHandler = handler
}

// ErrNoListenerCommand is returned when a listener was built with no command to
// run.
var ErrNoListenerCommand = errors.New("queue: this listener has no command to run. Pass one to NewListener")

// Listen starts a worker for connection and queue, and restarts it whenever it
// exits.
//
// It answers listen(). It returns when the context is cancelled or
// [Listener.Stop] is called -- the PHP loops forever and is stopped by a signal,
// which is the same thing said by the language that has signals built in.
//
// A child that exits with [ExitMemoryLimit] is started again, because that is
// what it stopped for. A child that cannot be started at all is an error: a
// loop that retries a binary that does not exist is a loop that fills a disk
// with the same line.
func (l *Listener) Listen(ctx context.Context, connectionName, queue string, options ListenerOptions) error {
	if len(l.command) == 0 {
		return ErrNoListenerCommand
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l.stop = cancel

	for {
		if ctx.Err() != nil {
			return nil
		}

		process := l.MakeProcess(ctx, connectionName, queue, options)
		if err := l.RunProcess(process, options.Memory); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				// Not the worker failing: the worker not starting. Retrying
				// cannot fix a path that is wrong.
				return fmt.Errorf("queue: starting the worker: %w", err)
			}
		}

		if options.Rest > 0 {
			timer := time.NewTimer(options.Rest)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
}

// MakeProcess builds the child worker's command.
//
// It answers makeProcess(). The flags are the worker's options turned back into
// the arguments `aru work` parses, which is the round trip that makes a listener
// and a worker configurable in one place.
func (l *Listener) MakeProcess(ctx context.Context, connectionName, queue string, options ListenerOptions) *exec.Cmd {
	args := append([]string(nil), l.command[1:]...)
	args = append(args,
		connectionName,
		"--once",
		"--name="+options.Name,
		"--queue="+queue,
		"--backoff="+seconds(options.Backoff),
		"--memory="+strconv.Itoa(options.Memory),
		"--sleep="+seconds(func(int) time.Duration { return options.Sleep }),
		"--tries="+strconv.Itoa(options.MaxTries),
	)
	if options.Force {
		args = append(args, "--force")
	}
	if options.Environment != "" {
		args = append(args, "--env="+options.Environment)
	}

	cmd := exec.CommandContext(ctx, l.command[0], args...)
	cmd.Dir = l.commandPath
	return cmd
}

// seconds renders a backoff schedule as the whole seconds a flag carries.
//
// The first attempt's wait is the one a child worker needs: it runs one job and
// exits, so it never sees a second.
func seconds(backoff func(int) time.Duration) string {
	if backoff == nil {
		return "0"
	}
	return strconv.Itoa(int(backoff(1) / time.Second))
}

// RunProcess runs one child worker to completion.
//
// It answers runProcess(). The memory check afterwards is the PHP's, and it
// means the same thing: this process, the listener, has grown, and something
// outside it should start it again with a clean heap.
func (l *Listener) RunProcess(process *exec.Cmd, memory int) error {
	if l.outputHandler != nil {
		out, err := process.StdoutPipe()
		if err != nil {
			return err
		}
		process.Stderr = process.Stdout
		if err := process.Start(); err != nil {
			return err
		}
		l.handleWorkerOutput(out)
		if err := process.Wait(); err != nil {
			return err
		}
	} else if err := process.Run(); err != nil {
		return err
	}

	if l.MemoryExceeded(memory) {
		l.Stop()
	}
	return nil
}

// handleWorkerOutput hands each line the child wrote to the output handler.
func (l *Listener) handleWorkerOutput(out io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := out.Read(buf)
		if n > 0 && l.outputHandler != nil {
			l.outputHandler(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// MemoryExceeded reports whether this process is holding more than limit
// megabytes.
//
// It answers memoryExceeded(). It reads MemStats.Sys, for the reason
// [Worker.MemoryExceeded] gives.
func (l *Listener) MemoryExceeded(limitMB int) bool {
	if limitMB <= 0 {
		return false
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Sys/1024/1024 >= uint64(limitMB)
}

// Stop ends the loop after the child that is running.
//
// It answers stop(), which calls exit() in PHP. It cancels instead: a listener
// that killed the process would take the child with it, and the child is
// holding a job.
func (l *Listener) Stop() {
	if l.stop != nil {
		l.stop()
	}
}

// Stderr is where a listener with no output handler sends the child's output.
// It is here so a caller can say `l.SetOutputHandler(queue.Stderr)`.
func Stderr(line string) { fmt.Fprint(os.Stderr, line) }
