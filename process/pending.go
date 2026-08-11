package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// defaultTimeout is how long a command may run when nobody said. It is PHP's
// sixty seconds, and it is a default rather than no limit because a command
// with no limit is how a deploy hangs.
const defaultTimeout = 60 * time.Second

// PendingProcess is a command being described, before it runs.
//
// It answers to Illuminate\Process\PendingProcess, and it is the fluent builder
// a Laravel developer already knows:
//
//	result, err := factory.NewPendingProcess().
//		Path("/srv/app").
//		Timeout(2 * time.Minute).
//		Run(ctx, []string{"go", "test", "./..."}, nil)
//
// PHP exposes $command, $path, $timeout and the rest as public properties
// beside the setters of the same name. Go cannot have a field and a method
// called the same thing, so the setters keep the Illuminate names (ADR 0044)
// and the state is unexported; String is how a fake handler or an assertion
// reads back the command line.
type PendingProcess struct {
	factory      *Factory
	command      []string
	path         string
	timeout      time.Duration
	idleTimeout  time.Duration
	environment  map[string]string
	input        any
	quietly      bool
	tty          bool
	options      *syscall.SysProcAttr
	fakeHandlers []FakeHandler
}

// NewPendingProcess builds a pending process bound to a factory. It is PHP's
// constructor, which takes the same one argument -- the factory is what holds
// the fakes and the recording, and a nil one is a process that always really
// runs.
func NewPendingProcess(factory *Factory) *PendingProcess {
	return &PendingProcess{factory: factory, timeout: defaultTimeout}
}

// String is the command line, rendered the way a person would type it.
//
// It stands for reading PHP's public $command property, which Go has no room
// for beside the Command method, and it is the exact string fake patterns and
// string assertions are matched against.
func (p *PendingProcess) String() string { return commandLine(p.command) }

// Command sets the program and its arguments. PHP's command.
//
// PHP takes array|string and lets Symfony escape the string form into a shell
// line. This takes only the array form, and there is no string form to add:
// see the package comment.
func (p *PendingProcess) Command(command ...string) *PendingProcess {
	p.command = command
	return p
}

// Path sets the working directory. PHP's path.
func (p *PendingProcess) Path(path string) *PendingProcess {
	p.path = path
	return p
}

// Timeout sets how long the whole run may take. PHP's timeout.
//
// PHP counts seconds because that is what Symfony takes; this takes a
// time.Duration, which is the same number with its unit written down. Zero is
// no limit, which is what Forever sets.
func (p *PendingProcess) Timeout(timeout time.Duration) *PendingProcess {
	p.timeout = timeout
	return p
}

// IdleTimeout sets how long the program may go without writing anything. PHP's
// idleTimeout, in a Duration for the reason Timeout is.
//
// It is the bound that catches what a total timeout does not: a fetch whose
// peer stopped answering, or a compiler waiting on a lock, both of which sit
// quiet for as long as they are given. A program that reports progress is never
// affected by it.
func (p *PendingProcess) IdleTimeout(timeout time.Duration) *PendingProcess {
	p.idleTimeout = timeout
	return p
}

// Forever removes the time limit. PHP's forever, which sets the timeout to
// null.
func (p *PendingProcess) Forever() *PendingProcess {
	p.timeout = 0
	return p
}

// Env sets environment variables for the program. PHP's env.
//
// They are added to the ones this process already has, not substituted for
// them, which is what Symfony does with the same array -- os/exec would replace
// the environment wholesale, and the caller who forgets to copy os.Environ
// first ships a program that cannot find PATH. A name already set is overridden
// by the value here.
func (p *PendingProcess) Env(environment map[string]string) *PendingProcess {
	p.environment = environment
	return p
}

// Input sets what is written to the program's standard input. PHP's input,
// whose union of string, resource and Traversable is a string, a []byte or an
// io.Reader here.
//
// Standard input is closed once it has been written. Without an Input the
// program reads an immediately closed input, never the terminal -- a command
// that waits for a person is a command that hangs a deploy.
func (p *PendingProcess) Input(input any) *PendingProcess {
	p.input = input
	return p
}

// Quietly discards the program's output instead of keeping it. PHP's quietly,
// which calls Symfony's disableOutput.
//
// Output and ErrorOutput then answer empty rather than throwing, which is the
// one place this differs from PHP: a result that cannot be read is a result
// nobody can log.
func (p *PendingProcess) Quietly() *PendingProcess {
	p.quietly = true
	return p
}

// Tty hands the program this process's own terminal. PHP's tty, whose default
// argument of true is the empty call here.
//
// Output is not captured in this mode -- it goes straight to the terminal --
// and the run fails if there is no terminal to hand over, which SupportsTty
// answers in advance.
func (p *PendingProcess) Tty(tty ...bool) *PendingProcess {
	p.tty = len(tty) == 0 || tty[0]
	return p
}

// SupportsTty reports whether this machine has a terminal to hand over. It is
// the newer Laravel's supportsTty, which the clone predates, and it is
// Symfony's isTtySupported: whether /dev/tty can be opened.
func (p *PendingProcess) SupportsTty() bool {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = terminal.Close()
	return true
}

// Options sets the attributes the operating system is handed when the process
// is created. PHP's options, which are the ones it passes to proc_open.
//
// It is the same escape hatch under a different name: proc_open's options are
// PHP's way to reach the process creation call, and SysProcAttr is Go's. What
// is in it differs by platform, which is true on both sides.
func (p *PendingProcess) Options(options *syscall.SysProcAttr) *PendingProcess {
	p.options = options
	return p
}

// WithFakeHandlers gives the pending process the fakes it should answer with.
// PHP's withFakeHandlers, which the factory calls on every process it makes.
func (p *PendingProcess) WithFakeHandlers(fakeHandlers []FakeHandler) *PendingProcess {
	p.fakeHandlers = fakeHandlers
	return p
}

// Run runs the command and waits for it. PHP's run.
//
// The command may be given here or with Command; nil here keeps the one already
// set, which is PHP's `$command ?: $this->command`. The output handler is
// PHP's second argument and may be nil.
//
// A command that exits non-zero is not an error: the result comes back with a
// nil error and Failed reports it, exactly as in PHP. The error is for the
// program that could not start, ran out of time, or was stray.
//
// ctx has no counterpart in PHP and comes first because that is where Go puts
// it. Cancelling it kills the program.
func (p *PendingProcess) Run(ctx context.Context, command []string, output OutputHandler) (ProcessResult, error) {
	if len(command) > 0 {
		p.command = command
	}
	line := p.String()

	if fake := p.fakeFor(line); fake != nil {
		result, err := p.resolveSynchronousFake(line, fake)
		if err != nil {
			return nil, err
		}
		p.factory.RecordIfRecording(p, result)
		return result, nil
	}
	if p.factory.IsRecording() && p.factory.PreventingStrayProcesses() {
		return nil, &StrayProcessError{Command: line}
	}

	invoked, err := p.start(ctx, output)
	if err != nil {
		return nil, err
	}
	return invoked.Wait(nil)
}

// Start starts the command and returns while it is still running. PHP's start.
//
// The caller must call Wait on what comes back, or the program is never reaped.
func (p *PendingProcess) Start(ctx context.Context, command []string, output OutputHandler) (InvokedProcess, error) {
	if len(command) > 0 {
		p.command = command
	}
	line := p.String()

	if fake := p.fakeFor(line); fake != nil {
		invoked, err := p.resolveAsynchronousFake(line, output, fake)
		if err != nil {
			return nil, err
		}
		p.factory.RecordIfRecording(p, invoked.PredictProcessResult())
		return invoked, nil
	}
	if p.factory.IsRecording() && p.factory.PreventingStrayProcesses() {
		return nil, &StrayProcessError{Command: line}
	}

	return p.start(ctx, output)
}

// start is the real thing: no fake matched, so a program is launched.
//
// It stands for PHP's toSymfonyProcess followed by Symfony's start, and it is
// where every setting on the pending process turns into something os/exec
// understands.
func (p *PendingProcess) start(ctx context.Context, output OutputHandler) (*invokedProcess, error) {
	if ctx == nil {
		return nil, errors.New("process: a nil context")
	}
	if len(p.command) == 0 || strings.TrimSpace(p.command[0]) == "" {
		return nil, errors.New("process: a command with no name")
	}
	environment, err := p.environ()
	if err != nil {
		return nil, err
	}
	stdin, err := p.stdin()
	if err != nil {
		return nil, err
	}
	if p.tty && !p.SupportsTty() {
		return nil, errors.New("process: TTY mode requires a terminal, and there is none")
	}

	// One cancellable context serves both limits: the deadline fires on its own
	// and the idle watchdog cancels by hand. Which one it was is decided in
	// classify, from the flag and from the cause -- an exit status cannot tell
	// them apart, because a killed process looks the same either way.
	run, cancel := ctx, context.CancelFunc(nil)
	switch {
	case p.timeout > 0:
		run, cancel = context.WithTimeout(ctx, p.timeout)
	case p.idleTimeout > 0:
		run, cancel = context.WithCancel(ctx)
	}

	shared := &sink{handler: output}
	if p.idleTimeout > 0 {
		shared.bump = make(chan struct{}, 1)
	}

	cmd := exec.CommandContext(run, p.command[0], p.command[1:]...)
	cmd.Dir = p.path
	cmd.Env = environment
	cmd.SysProcAttr = p.options
	cmd.WaitDelay = waitDelay

	invoked := &invokedProcess{
		command:     commandLine(p.command),
		cmd:         cmd,
		stdout:      &buffer{},
		stderr:      &buffer{},
		sink:        shared,
		timeout:     p.timeout,
		idleTimeout: p.idleTimeout,
		quietly:     p.quietly,
		parent:      ctx,
		cancel:      cancel,
		run:         run,
		watching:    make(chan struct{}),
		settled:     make(chan struct{}),
	}

	if p.tty {
		// Symfony's TTY mode hands the child this process's own descriptors,
		// so there is nothing left to capture and nothing to hand a handler.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	} else {
		cmd.Stdin = stdin
		cmd.Stdout = &collector{stream: Out, buf: invoked.stdout, sink: shared, quietly: p.quietly}
		cmd.Stderr = &collector{stream: Err, buf: invoked.stderr, sink: shared, quietly: p.quietly}
	}

	invoked.started = time.Now()
	if err := cmd.Start(); err != nil {
		if cancel != nil {
			cancel()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("process: %q was not found in PATH: %w", p.command[0], err)
		}
		return nil, fmt.Errorf("process: starting [%s]: %w", invoked.command, err)
	}

	if p.idleTimeout > 0 {
		go invoked.watchIdle(p.idleTimeout, shared.bump)
	}
	// Reaped by this goroutine and not by Wait, so that Running becomes false
	// on its own: `for invoked.Running()` is the loop PHP writes, and it never
	// ends if only Wait can settle the process.
	go func() {
		waitErr := cmd.Wait()
		close(invoked.watching)
		if cancel != nil {
			cancel()
		}
		invoked.waitErr = waitErr
		close(invoked.settled)
	}()
	return invoked, nil
}

// environ is the environment the program is started with, or nil to inherit
// this one's unchanged.
//
// The additions go after os.Environ, which is what makes them win: os/exec
// keeps the last value for a repeated name. Sorted, so two runs of the same
// command hand the program the same slice and a test can say what it expects.
func (p *PendingProcess) environ() ([]string, error) {
	if len(p.environment) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(p.environment))
	for name := range p.environment {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return nil, fmt.Errorf("process: %q is not an environment variable name", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	environment := os.Environ()
	for _, name := range names {
		environment = append(environment, name+"="+p.environment[name])
	}
	return environment, nil
}

// stdin is what the program reads, and never the terminal: exec leaves a nil
// Stdin connected to the null device, and a command that could read from the
// console is a command that can wait for somebody who is not there.
func (p *PendingProcess) stdin() (io.Reader, error) {
	switch input := p.input.(type) {
	case nil:
		return strings.NewReader(""), nil
	case string:
		return strings.NewReader(input), nil
	case []byte:
		return bytes.NewReader(input), nil
	case io.Reader:
		return input, nil
	default:
		return nil, fmt.Errorf("process: input of type %T is not a string, a []byte or an io.Reader", p.input)
	}
}

// fakeFor is the handler for a command line, or nil when none matched. PHP's
// protected fakeFor, which takes the first pattern that is "*" or that Str::is
// matches -- so order of registration decides, and a "*" registered first wins
// over everything after it.
func (p *PendingProcess) fakeFor(command string) func(*PendingProcess) any {
	for _, handler := range p.fakeHandlers {
		if matchesCommand(handler.Command, command) {
			return handler.callback()
		}
	}
	return nil
}

// resolveSynchronousFake turns what a handler returned into a result. PHP's
// protected method of the same name, including the order of the cases: a
// sequence is resolved by taking its next entry and starting again.
func (p *PendingProcess) resolveSynchronousFake(command string, fake func(*PendingProcess) any) (ProcessResult, error) {
	switch result := fake(p).(type) {
	case string:
		return NewFakeProcessResult(command, 0, result, ""), nil
	case []string:
		return NewFakeProcessResult(command, 0, result, ""), nil
	case *FakeProcessResult:
		return result.WithCommand(command), nil
	case *FakeProcessDescription:
		return result.ToProcessResult(command), nil
	case *FakeProcessSequence:
		next, err := result.next()
		if err != nil {
			return nil, err
		}
		return p.resolveSynchronousFake(command, func(*PendingProcess) any { return next })
	case ProcessResult:
		return result, nil
	default:
		return nil, fmt.Errorf("%w: synchronous", ErrUnsupportedFakeResult)
	}
}

// resolveAsynchronousFake turns what a handler returned into a started fake.
// PHP's protected method of the same name.
//
// A plain result becomes a description that prints it all at once and is
// finished on the first question, which is what PHP builds from the same input.
func (p *PendingProcess) resolveAsynchronousFake(command string, output OutputHandler, fake func(*PendingProcess) any) (*FakeInvokedProcess, error) {
	switch result := fake(p).(type) {
	case string:
		return p.fakeInvokedFrom(command, output, NewFakeProcessResult(command, 0, result, "")), nil
	case []string:
		return p.fakeInvokedFrom(command, output, NewFakeProcessResult(command, 0, result, "")), nil
	case *FakeProcessDescription:
		return NewFakeInvokedProcess(command, result).WithOutputHandler(output), nil
	case *FakeProcessSequence:
		next, err := result.next()
		if err != nil {
			return nil, err
		}
		return p.resolveAsynchronousFake(command, output, func(*PendingProcess) any { return next })
	case ProcessResult:
		return p.fakeInvokedFrom(command, output, result), nil
	default:
		return nil, fmt.Errorf("%w: asynchronous", ErrUnsupportedFakeResult)
	}
}

func (p *PendingProcess) fakeInvokedFrom(command string, output OutputHandler, result ProcessResult) *FakeInvokedProcess {
	description := NewFakeProcessDescription().
		ReplaceOutput(result.Output()).
		ReplaceErrorOutput(result.ErrorOutput()).
		RunsFor(0).
		ExitCode(result.ExitCode())

	return NewFakeInvokedProcess(command, description).WithOutputHandler(output)
}
