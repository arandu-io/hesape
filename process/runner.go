package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Runner runs commands.
//
// It is the one seam of this package: everything that shells out takes a Runner
// rather than calling System directly, and a test supplies its own two methods
// instead of the machine's. There is no registry and no default instance to
// swap -- what runs a command is what was passed in.
type Runner interface {
	// Run starts the command and waits for it.
	Run(ctx context.Context, c Command) (Result, error)
	// Start starts the command and returns while it is still running. The
	// caller must call Wait, or the process is never reaped.
	Start(ctx context.Context, c Command) (*Invoked, error)
}

// System is the Runner that runs programs on this machine.
//
// The zero value is ready: it holds nothing, because everything a run needs is
// on the Command.
type System struct{}

var _ Runner = System{}

// waitDelay bounds how long Wait keeps reading after the program itself has
// exited.
//
// A child that spawned children of its own hands them the same pipes, and those
// stay open after it dies -- so a wait with no bound is a command that finished
// and a caller that never returns. Five seconds is long enough that a normal
// exit never notices and short enough that a stuck one is a hiccup rather than
// a hang.
const waitDelay = 5 * time.Second

// Run starts the command and waits for it to finish.
//
// The Result is filled even when the error is not nil: output and exit code
// describe what happened, and the error says what was wrong with it.
func (s System) Run(ctx context.Context, c Command) (Result, error) {
	invoked, err := s.Start(ctx, c)
	if err != nil {
		return Result{Name: c.Name, Args: c.Args, ExitCode: -1}, err
	}
	return invoked.Wait()
}

// Start launches the command and returns with it still running.
//
// Wait has to be called on the result, once: it is what reaps the process,
// releases the timers this sets up and produces the Result. Start is for the
// caller that needs the pid, or needs to do something else while the program
// runs; a caller that only wants the output wants Run.
func (s System) Start(ctx context.Context, c Command) (*Invoked, error) {
	if ctx == nil {
		return nil, errors.New("process: a nil context")
	}
	if strings.TrimSpace(c.Name) == "" {
		return nil, errors.New("process: a command with no name")
	}
	env, err := c.environ()
	if err != nil {
		return nil, err
	}

	// One cancellable context serves both limits: the deadline fires on its own
	// and the idle watchdog cancels by hand. Which one it was is decided in
	// classify, from the flag and from the cause -- an exit status cannot tell
	// them apart, because a killed process looks the same either way.
	run, cancel := ctx, context.CancelFunc(nil)
	switch {
	case c.Timeout > 0:
		run, cancel = context.WithTimeout(ctx, c.Timeout)
	case c.IdleTimeout > 0:
		run, cancel = context.WithCancel(ctx)
	}

	shared := &sink{onOutput: c.OnOutput}
	if c.IdleTimeout > 0 {
		shared.bump = make(chan struct{}, 1)
	}

	cmd := exec.CommandContext(run, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = env
	cmd.WaitDelay = waitDelay
	// Standard input is a reader that is at its end straight away when Stdin is
	// empty, and never the terminal: exec leaves a nil Stdin connected to
	// /dev/null, and a command that could read from the console is a command
	// that can wait for somebody who is not there.
	cmd.Stdin = strings.NewReader(c.Stdin)

	stdout := &collector{stream: Stdout, sink: shared, max: c.MaxOutput}
	stderr := &collector{stream: Stderr, sink: shared, max: c.MaxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	invoked := &Invoked{
		command:  c,
		cmd:      cmd,
		parent:   ctx,
		run:      run,
		cancel:   cancel,
		stdout:   stdout,
		stderr:   stderr,
		settled:  make(chan struct{}),
		watching: make(chan struct{}),
	}

	invoked.started = time.Now()
	if err := cmd.Start(); err != nil {
		if cancel != nil {
			cancel()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("process: %q was not found in PATH: %w", c.Name, err)
		}
		return nil, fmt.Errorf("process: starting %s: %w", c, err)
	}

	if c.IdleTimeout > 0 {
		go invoked.watchIdle(c.IdleTimeout, shared.bump)
	}
	return invoked, nil
}

// Invoked is a command that was started and has not been waited on yet.
type Invoked struct {
	command Command
	cmd     *exec.Cmd
	// parent is the context the caller passed; run is the one the process is
	// tied to. Telling them apart is what says whether it was the caller who
	// gave up or one of this package's own limits.
	parent context.Context
	run    context.Context
	cancel context.CancelFunc

	stdout  *collector
	stderr  *collector
	started time.Time

	idled    atomic.Bool
	watching chan struct{}

	once    sync.Once
	settled chan struct{}
	result  Result
	err     error
}

// PID is the process identifier the operating system gave the program.
func (i *Invoked) PID() int {
	if i.cmd.Process == nil {
		return 0
	}
	return i.cmd.Process.Pid
}

// Running reports whether the program has still not been reaped.
//
// It answers about the moment it was asked: a program can exit between the
// answer and the next line, which is why nothing here decides anything on it --
// Wait is what settles the question.
func (i *Invoked) Running() bool {
	select {
	case <-i.settled:
		return false
	default:
		return true
	}
}

// Signal sends a signal to the program.
//
// Cancelling the context is the portable way to stop a process and the one this
// package uses itself; Signal is for the caller who needs a particular signal,
// and it carries that platform's rules -- on Windows only Kill is delivered.
func (i *Invoked) Signal(sig os.Signal) error {
	if i.cmd.Process == nil {
		return errors.New("process: signalling a command that was never started")
	}
	if !i.Running() {
		return fmt.Errorf("process: signalling %s, which has already exited", i.command)
	}
	return i.cmd.Process.Signal(sig)
}

// Wait waits for the program to finish and reports what happened.
//
// It can be called more than once and from more than one goroutine: the first
// call does the waiting and every call gets the same answer.
func (i *Invoked) Wait() (Result, error) {
	i.once.Do(func() {
		err := i.cmd.Wait()
		close(i.watching)
		if i.cancel != nil {
			i.cancel()
		}

		out, outCut := i.stdout.text()
		errOut, errCut := i.stderr.text()
		i.result = Result{
			Name:      i.command.Name,
			Args:      i.command.Args,
			ExitCode:  exitCode(i.cmd),
			Stdout:    out,
			Stderr:    errOut,
			Duration:  time.Since(i.started),
			Truncated: outCut || errCut,
		}
		i.err = i.classify(err)
		close(i.settled)
	})
	<-i.settled
	return i.result, i.err
}

// classify turns what os/exec reported into what actually happened.
//
// The order is the whole point. A killed process comes back as an exit status
// whatever killed it, so every reason this package had to kill one has to be
// examined before the exit status is believed -- otherwise a command that hit
// its deadline is reported as a program that failed, and the person goes
// looking for a bug in it.
func (i *Invoked) classify(err error) error {
	if err == nil {
		return nil
	}
	// The caller gave up first: their cancellation, not our limit, and the
	// error they get back is the one they can test with errors.Is.
	if cause := i.parent.Err(); cause != nil {
		return fmt.Errorf("process: %s: %w", i.command, cause)
	}
	if i.idled.Load() {
		return &TimeoutError{
			Name:  i.command.Name,
			Args:  i.command.Args,
			After: i.command.IdleTimeout,
			Idle:  true,
		}
	}
	if errors.Is(context.Cause(i.run), context.DeadlineExceeded) {
		return &TimeoutError{
			Name:  i.command.Name,
			Args:  i.command.Args,
			After: i.command.Timeout,
		}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{
			Name:     i.command.Name,
			Args:     i.command.Args,
			ExitCode: i.result.ExitCode,
			Stderr:   i.result.Stderr,
		}
	}
	return fmt.Errorf("process: %s: %w", i.command, err)
}

// watchIdle kills the program when it has gone quiet for too long.
//
// The timer is reset on every chunk either stream produces, so a program that
// prints anything at all is never touched. Reset needs no drain: since Go 1.23 a
// stopped timer cannot deliver a value that was already in flight.
func (i *Invoked) watchIdle(silence time.Duration, bump chan struct{}) {
	timer := time.NewTimer(silence)
	defer timer.Stop()
	for {
		select {
		case <-bump:
			timer.Stop()
			timer.Reset(silence)
		case <-timer.C:
			// Flagged before the kill, because the kill is what Wait sees and
			// by then the reason has to be readable.
			i.idled.Store(true)
			i.cancel()
			return
		case <-i.watching:
			return
		}
	}
}

// exitCode is what the program returned, and -1 for every way of leaving that
// is not a returned status.
func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

// sink is what the two collectors share: the caller's callback and the signal
// that keeps the idle watchdog awake.
//
// One mutex for both streams is what lets OnOutput be written without a lock of
// its own -- stdout and stderr are copied by two goroutines, and a callback that
// writes to a terminal from both at once interleaves a line into nonsense.
type sink struct {
	mu       sync.Mutex
	onOutput func(Stream, []byte)
	bump     chan struct{}
}

// wrote is called with mu held.
func (s *sink) wrote(stream Stream, chunk []byte) {
	if s.onOutput != nil {
		s.onOutput(stream, chunk)
	}
	if s.bump != nil {
		// Non-blocking: the watchdog only needs to know that something arrived,
		// and a full buffer already says so. Blocking here would let a quiet
		// watchdog stall the program's own output.
		select {
		case s.bump <- struct{}{}:
		default:
		}
	}
}

// collector keeps one stream, up to a cap, and passes every byte on.
type collector struct {
	stream Stream
	sink   *sink
	max    int
	kept   []byte
	cut    bool
}

func (c *collector) Write(p []byte) (int, error) {
	c.sink.mu.Lock()
	defer c.sink.mu.Unlock()

	room := len(p)
	if c.max > 0 {
		if free := c.max - len(c.kept); free < room {
			room = max(free, 0)
		}
	}
	c.kept = append(c.kept, p[:room]...)
	if room < len(p) {
		c.cut = true
	}

	c.sink.wrote(c.stream, p)
	// Always the full length: a short write stops exec's copy and turns "this
	// program printed a lot" into "this program failed".
	return len(p), nil
}

func (c *collector) text() (string, bool) {
	c.sink.mu.Lock()
	defer c.sink.mu.Unlock()
	return string(c.kept), c.cut
}
