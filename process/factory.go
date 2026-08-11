package process

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/str"
)

// FakeHandler is one entry of the array Factory::fake takes.
//
// PHP passes an associative array from command pattern to result, where the
// value may be a string, a ProcessResult, a FakeProcessDescription, a
// FakeProcessSequence, or a closure returning any of those. Go has no untyped
// map that carries a pattern and a heterogeneous value with the order intact --
// and order is load-bearing here, because fakeFor takes the first pattern that
// matches -- so it is a slice of this.
//
// Command is the pattern, matched with Str::is: "git *" answers every git
// command, "*" answers everything, and empty means the same as "*".
type FakeHandler struct {
	// Command is the pattern. Empty and "*" both match anything.
	Command string

	// Result is what the command answers with: a string, a []string, a
	// *FakeProcessResult, a *FakeProcessDescription, a *FakeProcessSequence, or
	// a func(*PendingProcess) any returning one of those.
	Result any
}

// callback normalises Result into the closure PendingProcess.fakeFor hands
// back, which is what PHP gets for free because a value and a closure returning
// it are both callable there.
func (h FakeHandler) callback() func(*PendingProcess) any {
	if fn, ok := h.Result.(func(*PendingProcess) any); ok {
		return fn
	}
	return func(*PendingProcess) any { return h.Result }
}

// ranProcess is one entry of Factory::$recorded: the process as configured and
// the result it produced.
type ranProcess struct {
	process *PendingProcess
	result  ProcessResult
}

// Factory answers Illuminate\Process\Factory.
//
// It is the entry point of the component: Factory.Run for a command that runs,
// Factory.Fake for a test that must not shell out, and Factory.Pool for a set
// that runs at once.
//
// It is safe for concurrent use. Illuminate's is not, and does not need to be:
// a pool there runs each process in sequence under the hood. Here a pool is
// goroutines, and the recording they all write to is behind a mutex.
type Factory struct {
	mu sync.Mutex

	fakeHandlers []FakeHandler
	recording    bool
	recorded     []ranProcess

	preventStray bool
}

// NewFactory constructs a Factory. Illuminate resolves one from the container,
// which this framework does not have (ADR 0001), so an application holds one
// and hands it to whatever runs a process.
func NewFactory() *Factory { return &Factory{} }

// Fake answers Factory::fake.
//
// It puts the factory in recording mode, so every process made from it answers
// from the handlers instead of running. Calling it with no handlers fakes
// everything with an empty successful result, which is PHP's `fake()` with no
// argument.
//
// Handlers are matched in order and the first match wins, so a "*" registered
// first answers everything after it. That is Illuminate's behaviour and it is
// the mistake people make: put the specific patterns first.
func (f *Factory) Fake(handlers ...FakeHandler) *Factory {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recording = true
	if len(handlers) == 0 {
		f.fakeHandlers = append(f.fakeHandlers, FakeHandler{Command: "*", Result: ""})
		return f
	}
	f.fakeHandlers = append(f.fakeHandlers, handlers...)
	return f
}

// IsRecording answers Factory::isRecording.
func (f *Factory) IsRecording() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recording
}

// Record answers Factory::record.
func (f *Factory) Record(process *PendingProcess, result ProcessResult) *Factory {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, ranProcess{process: process, result: result})
	return f
}

// RecordIfRecording answers Factory::recordIfRecording.
func (f *Factory) RecordIfRecording(process *PendingProcess, result ProcessResult) *Factory {
	if f.IsRecording() {
		f.Record(process, result)
	}
	return f
}

// PreventStrayProcesses answers Factory::preventStrayProcesses.
//
// With it on, a command that no handler matched is an error instead of running
// for real. It is the guard that turns "somebody forgot to fake git" from a
// test that quietly shells out on CI into a test that fails naming the command.
func (f *Factory) PreventStrayProcesses(prevent ...bool) *Factory {
	on := true
	if len(prevent) > 0 {
		on = prevent[0]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.preventStray = on
	return f
}

// PreventingStrayProcesses answers Factory::preventingStrayProcesses.
func (f *Factory) PreventingStrayProcesses() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.preventStray
}

// AllowStrayProcesses is the inverse of PreventStrayProcesses. Illuminate has
// no such method because its argument defaults to true and false turns it off;
// this exists so the intent reads at the call site.
func (f *Factory) AllowStrayProcesses() *Factory { return f.PreventStrayProcesses(false) }

// NewPendingProcess answers Factory::newPendingProcess.
func (f *Factory) NewPendingProcess() *PendingProcess {
	p := NewPendingProcess(f)
	f.mu.Lock()
	handlers := append([]FakeHandler(nil), f.fakeHandlers...)
	f.mu.Unlock()
	return p.WithFakeHandlers(handlers)
}

// Run answers Factory::run, which PHP reaches through __call forwarding to a
// new PendingProcess.
func (f *Factory) Run(ctx context.Context, command []string, output OutputHandler) (ProcessResult, error) {
	return f.NewPendingProcess().Run(ctx, command, output)
}

// Start answers Factory::start, by the same forwarding.
func (f *Factory) Start(ctx context.Context, command []string, output OutputHandler) (InvokedProcess, error) {
	return f.NewPendingProcess().Start(ctx, command, output)
}

// Result answers Factory::result: a fake result to register with Fake.
func (f *Factory) Result(output any, errorOutput any, exitCode int) *FakeProcessResult {
	return NewFakeProcessResult("", exitCode, output, errorOutput)
}

// Describe answers Factory::describe: a fake description, for a process that is
// started rather than run and whose output arrives over time.
func (f *Factory) Describe() *FakeProcessDescription {
	return NewFakeProcessDescription()
}

// Sequence answers Factory::sequence: a series of results, one per call, for a
// command that is run more than once and answers differently each time.
func (f *Factory) Sequence(processes ...any) *FakeProcessSequence {
	return NewFakeProcessSequence(processes...)
}

// Recorded answers what Factory::$recorded holds: the command line of every
// process that ran, in order.
//
// Illuminate exposes the array itself and its assertions read it. The command
// lines are what an assertion failure has to print, so this returns them rather
// than the pending processes, and the assertions below use it for their message.
//
// The line is PendingProcess.String, which is what a fake pattern is matched
// against -- for the reason matchesCommand is one function: rendering it any
// other way here would be a command a fake answered and an assertion then said
// never ran.
func (f *Factory) Recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.recorded))
	for _, ran := range f.recorded {
		out = append(out, ran.process.String())
	}
	return out
}

// AssertRan answers Factory::assertRan.
//
// Illuminate throws a PHPUnit assertion; Go takes *testing.T, calls t.Helper()
// so the failure points at the caller, and prints every command that did run --
// "assertRan: nothing matched" is a message that sends somebody to a debugger,
// and the list is the answer they would have gone looking for.
func (f *Factory) AssertRan(t *testing.T, pattern string) {
	t.Helper()
	if n := f.countRan(pattern); n == 0 {
		t.Errorf("AssertRan(%q): no process matched it.\n%s", pattern, f.ranReport())
	}
}

// AssertRanTimes answers Factory::assertRanTimes.
func (f *Factory) AssertRanTimes(t *testing.T, pattern string, times int) {
	t.Helper()
	if n := f.countRan(pattern); n != times {
		t.Errorf("AssertRanTimes(%q, %d): %d matched.\n%s", pattern, times, n, f.ranReport())
	}
}

// AssertNotRan answers Factory::assertNotRan.
func (f *Factory) AssertNotRan(t *testing.T, pattern string) {
	t.Helper()
	if n := f.countRan(pattern); n != 0 {
		t.Errorf("AssertNotRan(%q): %d matched it.\n%s", pattern, n, f.ranReport())
	}
}

// AssertDidntRun answers Factory::assertDidntRun, the alias of assertNotRan.
func (f *Factory) AssertDidntRun(t *testing.T, pattern string) {
	t.Helper()
	f.AssertNotRan(t, pattern)
}

// AssertNothingRan answers Factory::assertNothingRan.
func (f *Factory) AssertNothingRan(t *testing.T) {
	t.Helper()
	if ran := f.Recorded(); len(ran) > 0 {
		t.Errorf("AssertNothingRan: %d processes ran.\n%s", len(ran), f.ranReport())
	}
}

// countRan reports how many recorded commands match the pattern, with the same
// matcher fakeFor uses so that a pattern asserted on and a pattern faked on
// behave the same.
func (f *Factory) countRan(pattern string) int {
	n := 0
	for _, command := range f.Recorded() {
		if matchesCommand(pattern, command) {
			n++
		}
	}
	return n
}

// matchesCommand reports whether a fake or assertion pattern answers a command
// line, and is the single implementation of that question.
//
// PHP asks it with Str::is, and it is asked in two places -- fakeFor when the
// process runs, and the assertions when the test checks. Two implementations
// would be a fake that answers a command an assertion then says never ran.
//
// An empty pattern and "*" both match anything, which is what an entry with no
// Command means.
func matchesCommand(pattern, command string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	return str.Is([]string{pattern}, command, false)
}

// ranReport lists the commands that ran, for an assertion failure.
func (f *Factory) ranReport() string {
	ran := f.Recorded()
	if len(ran) == 0 {
		return "no process ran"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d ran:", len(ran)))
	for _, command := range ran {
		b.WriteString("\n  " + command)
	}
	return b.String()
}
