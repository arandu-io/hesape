package process

import (
	"os"
	"strings"
	"sync"
	"time"
)

// FakeInvokedProcess is a started process that was never started.
//
// It answers to Illuminate\Process\FakeInvokedProcess, and it is what
// PendingProcess.Start hands back when a fake handler matched. It replays a
// FakeProcessDescription: every question asked of it -- ID, Running, Signal --
// hands the output handler one more line, so a test's watching loop sees output
// arrive the way it would from a program.
//
// That is the part worth knowing before writing a test against it: the output
// does not arrive on its own, because nothing is running. It arrives because
// the test asked something.
type FakeInvokedProcess struct {
	mu                     sync.Mutex
	command                string
	process                *FakeProcessDescription
	receivedSignals        []os.Signal
	remainingRunIterations int
	startedIterations      bool
	outputHandler          OutputHandler
	nextOutputIndex        int
	nextErrorOutputIndex   int
}

var _ InvokedProcess = (*FakeInvokedProcess)(nil)

// NewFakeInvokedProcess is PHP's constructor: the command line the fake stands
// for, and the description it replays.
func NewFakeInvokedProcess(command string, process *FakeProcessDescription) *FakeInvokedProcess {
	return &FakeInvokedProcess{command: command, process: process}
}

// ID is the process id the description was given. PHP's id.
//
// Asking hands the output handler one more line, which is PHP's behaviour and
// not an accident of this port: a loop that polls the process is the only clock
// a fake has.
func (p *FakeInvokedProcess) ID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emitNextLine()
	return p.process.processID
}

// Signal records a signal against the fake. PHP's signal.
//
// Nothing is killed, because nothing is running; HasReceivedSignal is how a
// test asks whether the code under test sent it. The error is always nil, and
// exists because the contract's real implementation has one.
func (p *FakeInvokedProcess) Signal(signal os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emitNextLine()
	p.receivedSignals = append(p.receivedSignals, signal)
	return nil
}

// HasReceivedSignal reports whether Signal was called with the given signal.
// PHP's hasReceivedSignal.
func (p *FakeInvokedProcess) HasReceivedSignal(signal os.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, received := range p.receivedSignals {
		if received == signal {
			return true
		}
	}
	return false
}

// Stop records a stop against the fake and finishes it. It is the newer
// Laravel's InvokedProcess::stop, which the clone does not have; the signal is
// recorded the way Signal records one, and both arguments may be zero.
func (p *FakeInvokedProcess) Stop(timeout time.Duration, signal os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if signal != nil {
		p.receivedSignals = append(p.receivedSignals, signal)
	}
	p.remainingRunIterations = 0
	p.startedIterations = true
	return nil
}

// Running reports whether the fake still counts as running. PHP's running.
//
// It answers true as many times as the description's RunsFor, then false
// forever -- and on the answer that turns false it flushes every remaining line
// to the output handler, so a `for Running()` loop ends having seen all of it.
func (p *FakeInvokedProcess) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.emitNextLine()

	if !p.startedIterations {
		p.remainingRunIterations = p.process.runIterations
		p.startedIterations = true
	}
	if p.remainingRunIterations == 0 {
		for p.emitNextLine() {
		}
		return false
	}
	p.remainingRunIterations--
	return true
}

// emitNextLine hands the output handler the next line neither cursor has passed
// yet, and reports whether there was one. It is PHP's
// invokeOutputHandlerWithNextLineOfOutput, cursors and all.
//
// Called with the lock held.
func (p *FakeInvokedProcess) emitNextLine() bool {
	if p.outputHandler == nil {
		return false
	}
	start := min(p.nextOutputIndex, p.nextErrorOutputIndex)
	for i := start; i < len(p.process.output); i++ {
		line := p.process.output[i]
		if line.stream == Out && i >= p.nextOutputIndex {
			p.outputHandler(Out, line.buffer)
			p.nextOutputIndex = i + 1
			return true
		}
		if line.stream == Err && i >= p.nextErrorOutputIndex {
			p.outputHandler(Err, line.buffer)
			p.nextErrorOutputIndex = i + 1
			return true
		}
	}
	return false
}

// Output is the standard output the fake has produced so far. PHP's output.
//
// So far, and not in total: only lines the cursor has reached count, so a test
// that asks before the fake has been polled sees less than the description
// holds. Asking advances the cursor by one line first, which is PHP's
// latestOutput call at the top of the same method.
func (p *FakeInvokedProcess) Output() string {
	p.LatestOutput()

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.collected(Out, p.nextOutputIndex)
}

// ErrorOutput is the error output the fake has produced so far. PHP's
// errorOutput.
func (p *FakeInvokedProcess) ErrorOutput() string {
	p.LatestErrorOutput()

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.collected(Err, p.nextErrorOutputIndex)
}

// collected joins one stream up to a cursor. Called with the lock held.
//
// The ending is PHP's, and PHP's is odd on purpose or by accident: an empty
// stream comes back as a single newline rather than as nothing, because the
// rtrim runs on the join and the newline is concatenated after it.
func (p *FakeInvokedProcess) collected(stream Stream, upTo int) string {
	var joined strings.Builder
	for i := 0; i < upTo && i < len(p.process.output); i++ {
		if p.process.output[i].stream == stream {
			joined.WriteString(p.process.output[i].buffer)
		}
	}
	return strings.TrimRight(joined.String(), "\n") + "\n"
}

// LatestOutput is the next line of standard output, and the empty string once
// there are none left. PHP's latestOutput, which is Symfony's
// getIncrementalOutput.
//
// One line, not everything since the last call: the cursor moves by one
// matching entry, and by every non-matching one it stepped over on the way.
func (p *FakeInvokedProcess) LatestOutput() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest(Out)
}

// LatestErrorOutput is the next line of error output. PHP's latestErrorOutput.
func (p *FakeInvokedProcess) LatestErrorOutput() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest(Err)
}

// latest is called with the lock held.
func (p *FakeInvokedProcess) latest(stream Stream) string {
	cursor := &p.nextOutputIndex
	if stream == Err {
		cursor = &p.nextErrorOutputIndex
	}
	for i := *cursor; i < len(p.process.output); i++ {
		*cursor = i + 1
		if p.process.output[i].stream == stream {
			return p.process.output[i].buffer
		}
	}
	return ""
}

// Wait finishes the fake and returns the result the description ends with.
// PHP's wait.
//
// A handler passed here replaces the one Start was given, as it does in PHP.
// With any handler at all, every line still unread is flushed to it before the
// result comes back -- which is what makes a fake started process end up having
// printed everything, whether the test polled it or not.
func (p *FakeInvokedProcess) Wait(output OutputHandler) (ProcessResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if output != nil {
		p.outputHandler = output
	}
	if p.outputHandler == nil {
		p.remainingRunIterations = 0
		p.startedIterations = true
		return p.process.ToProcessResult(p.command), nil
	}
	for p.emitNextLine() {
	}
	p.remainingRunIterations = 0
	p.startedIterations = true
	return p.process.ToProcessResult(p.command), nil
}

// PredictProcessResult is the result this fake will end with, asked before it
// ends. PHP's predictProcessResult, which the factory uses to record a started
// fake at the moment it starts.
func (p *FakeInvokedProcess) PredictProcessResult() ProcessResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.process.ToProcessResult(p.command)
}

// WithOutputHandler sets the handler every question will feed. PHP's
// withOutputHandler, which takes the callable Start was given.
func (p *FakeInvokedProcess) WithOutputHandler(output OutputHandler) *FakeInvokedProcess {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outputHandler = output
	return p
}
