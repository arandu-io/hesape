package process

import (
	"context"
	"os"
	"strconv"

	"github.com/arandu-io/hesape/collections"
)

// entry is one process of a pool or a pipe, under the key it was added with.
//
// PHP keeps them in one array, where a string key and an appended integer key
// live together and the insertion order is the array's order. Go has no such
// map, so the order is the slice and the key is written down beside the
// process.
type entry struct {
	key     string
	process *PendingProcess
}

// keyed holds the pending processes of a pool or a pipe in the order they were
// added, which is the half Pool and Pipe have in common.
//
// The unkeyed counter answers PHP's `$this->pendingProcesses[] = $process`: an
// appended element takes the next integer key, counted only among the integer
// keys, so a pool with one `as('build')` and two unkeyed processes keys them
// "build", "0" and "1".
type keyed struct {
	entries []entry
	next    int
}

// as registers a fresh pending process under key.
func (k *keyed) as(factory *Factory, key string) *PendingProcess {
	process := factory.NewPendingProcess()
	for i := range k.entries {
		if k.entries[i].key == key {
			// PHP writes into the same array slot, so the position the key
			// already has is the position it keeps.
			k.entries[i].process = process
			return process
		}
	}
	k.entries = append(k.entries, entry{key: key, process: process})
	return process
}

// command registers a fresh pending process under the next integer key.
func (k *keyed) command(factory *Factory, command []string) *PendingProcess {
	process := factory.NewPendingProcess().Command(command...)
	k.entries = append(k.entries, entry{key: strconv.Itoa(k.next), process: process})
	k.next++
	return process
}

// Pool answers Illuminate\Process\Pool: a set of processes that run at once.
//
//	results, err := factory.Concurrently(ctx, func(pool *process.Pool) {
//		pool.As("build").Command("go", "build", "./...")
//		pool.Command("go", "vet", "./...")
//	}, nil)
//
// The callback is not run when the pool is built. It runs at Start, which is
// what PHP does -- `call_user_func($this->callback, $this)` is the first line
// of start() -- so a pool defined once and started twice starts fresh both
// times.
//
// A Pool is not safe for concurrent use, and does not need to be: it is built
// by one callback and started by the goroutine that built it. What runs at once
// is the processes, not the definition.
type Pool struct {
	factory  *Factory
	callback func(*Pool)
	keyed
}

// NewPool answers the Pool constructor. [Factory.Pool] is how a caller reaches
// it, exactly as Process::pool is in PHP.
func NewPool(factory *Factory, callback func(*Pool)) *Pool {
	return &Pool{factory: factory, callback: callback}
}

// As adds a process to the pool under a key. PHP's Pool::as.
//
// The key names the process in the output handler and in the results, which is
// what it is for: without it a process is known by its position.
func (p *Pool) As(key string) *PendingProcess { return p.as(p.factory, key) }

// Command adds a process to the pool under the next integer key.
//
// It is `$pool->command(...)`, which PHP delivers through Pool::__call
// forwarding to the factory and appending the result. Go has no method missing
// hook, so the one form that starts a process is written out; everything else
// the PHP proxies -- path, timeout, env, input -- is a method of the
// [PendingProcess] this returns, and reads the same way chained onto it.
func (p *Pool) Command(command ...string) *PendingProcess {
	return p.command(p.factory, command)
}

// Start runs the callback and starts every process it defined. PHP's
// Pool::start.
//
// The processes are started in the order they were added and none of them is
// waited for, so they run at the same time. The output handler is called with
// the key of the process the chunk came from, which is PHP's
// `function ($type, $buffer, $key)`; pass nil for no handler.
//
// A process that could not be started ends the start and the error names it.
// The ones already started are still running: [InvokedProcessPool.Wait] on what
// the caller has is not reachable, so this signals nothing and leaves them --
// the same thing PHP does, since an exception there abandons the pool too.
func (p *Pool) Start(ctx context.Context, output PoolOutputHandler) (*InvokedProcessPool, error) {
	if p.callback != nil {
		// Defined afresh, so a pool started twice starts the same set twice
		// rather than twice as many. A pool with no callback was filled by
		// hand, and there is nothing to define again.
		p.entries = nil
		p.next = 0
		p.callback(p)
	}

	invoked := &InvokedProcessPool{}
	for _, e := range p.entries {
		var handler OutputHandler
		if output != nil {
			key := e.key
			handler = func(stream Stream, buffer string) { output(stream, buffer, key) }
		}
		started, err := e.process.Start(ctx, nil, handler)
		if err != nil {
			return nil, err
		}
		invoked.processes = append(invoked.processes, invokedEntry{key: e.key, process: started})
	}
	return invoked, nil
}

// Run starts the pool and waits for it. PHP's Pool::run, which is wait().
func (p *Pool) Run(ctx context.Context) (*ProcessPoolResults, error) { return p.Wait(ctx) }

// Wait starts the pool and waits for it. PHP's Pool::wait, which is
// `$this->start()->wait()`.
func (p *Pool) Wait(ctx context.Context) (*ProcessPoolResults, error) {
	started, err := p.Start(ctx, nil)
	if err != nil {
		return nil, err
	}
	return started.Wait()
}

// invokedEntry is one started process of a pool, under its key.
type invokedEntry struct {
	key     string
	process InvokedProcess
}

// InvokedProcessPool answers Illuminate\Process\InvokedProcessPool: the
// processes of a pool, started and still running.
type InvokedProcessPool struct {
	processes []invokedEntry
}

// Signal sends a signal to every process still running, and answers with the
// ones it signalled. PHP's InvokedProcessPool::signal.
//
// PHP takes the signal number and Go takes an os.Signal, which is what
// [InvokedProcess.Signal] takes for the same reason. Where the PHP throws, this
// returns the error, and it returns the processes it had already signalled with
// it -- a signal that failed on the third of five is worth seeing along with
// the two that took it.
func (p *InvokedProcessPool) Signal(signal os.Signal) (collections.Collection[InvokedProcess], error) {
	signalled := collections.Collection[InvokedProcess]{}
	for _, e := range p.processes {
		if !e.process.Running() {
			continue
		}
		if err := e.process.Signal(signal); err != nil {
			return signalled, err
		}
		signalled = append(signalled, e.process)
	}
	return signalled, nil
}

// Running answers the processes of the pool that are still going. PHP's
// InvokedProcessPool::running.
func (p *InvokedProcessPool) Running() collections.Collection[InvokedProcess] {
	out := collections.Collection[InvokedProcess]{}
	for _, e := range p.processes {
		if e.process.Running() {
			out = append(out, e.process)
		}
	}
	return out
}

// Wait waits for every process of the pool. PHP's InvokedProcessPool::wait.
//
// Every process is waited for even after one of them fails to be waited for,
// because the alternative is a pool that leaves children running and a caller
// that has no handle left to stop them. The first error is what comes back, and
// the results of the ones that did finish come back with it.
func (p *InvokedProcessPool) Wait() (*ProcessPoolResults, error) {
	results := &ProcessPoolResults{}
	var first error
	for _, e := range p.processes {
		result, err := e.process.Wait(nil)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		results.results = append(results.results, resultEntry{key: e.key, result: result})
	}
	return results, first
}

// Count answers how many processes the pool holds. PHP's
// InvokedProcessPool::count, which is the Countable interface -- a Go caller
// calls the method, and there is no len() to overload.
func (p *InvokedProcessPool) Count() int { return len(p.processes) }

// resultEntry is one finished process of a pool, under its key.
type resultEntry struct {
	key    string
	result ProcessResult
}

// ProcessPoolResults answers Illuminate\Process\ProcessPoolResults: what a pool
// of processes finished with.
//
// The results are in the order the processes were added to the pool, which is
// the order PHP's array keeps them in.
type ProcessPoolResults struct {
	results []resultEntry
}

// Collect answers the results as a collection. PHP's
// ProcessPoolResults::collect.
//
// PHP hands back a Collection keyed by the pool's keys, because a PHP array
// carries the key and the order at once. A [collections.Collection] is an
// ordered list, as its own documentation says, so this is the results in the
// order the pool was built and the key is the position.
func (r *ProcessPoolResults) Collect() collections.Collection[ProcessResult] {
	out := make(collections.Collection[ProcessResult], 0, len(r.results))
	for _, e := range r.results {
		out = append(out, e.result)
	}
	return out
}

// Pipe answers Illuminate\Process\Pipe: processes run one after another, each
// one reading what the one before it wrote.
//
//	result, err := factory.Pipe(ctx, func(pipe *process.Pipe) {
//		pipe.Command("cat", "notes.txt")
//		pipe.Command("grep", "-i", "todo")
//	}, nil)
//
// The first process that fails ends the pipe and its result is what comes back,
// which is PHP's `if ($previousProcessResult && $previousProcessResult->failed())`.
type Pipe struct {
	factory  *Factory
	callback func(*Pipe)
	keyed
}

// NewPipe answers the Pipe constructor. [Factory.Pipe] is how a caller reaches
// it, exactly as Process::pipe is in PHP.
func NewPipe(factory *Factory, callback func(*Pipe)) *Pipe {
	return &Pipe{factory: factory, callback: callback}
}

// As adds a process to the pipe under a key. PHP's Pipe::as.
func (p *Pipe) As(key string) *PendingProcess { return p.as(p.factory, key) }

// Command adds a process to the pipe under the next integer key.
//
// It is `$pipe->command(...)`, which PHP delivers through Pipe::__call. What
// [Pool.Command] says about that applies here.
func (p *Pipe) Command(command ...string) *PendingProcess {
	return p.command(p.factory, command)
}

// Run runs the callback and then the processes, in order. PHP's Pipe::run.
//
// Each process after the first is given the output of the one before it as its
// standard input, and a process that exits non-zero stops the pipe -- its
// result is returned, and nothing after it runs. The output handler is called
// with the key of the process the chunk came from; pass nil for no handler.
//
// A pipe with no processes answers a nil result and a nil error, which is what
// PHP's reduce over an empty array does.
func (p *Pipe) Run(ctx context.Context, output PoolOutputHandler) (ProcessResult, error) {
	if p.callback != nil {
		p.entries = nil
		p.next = 0
		p.callback(p)
	}

	var previous ProcessResult
	for _, e := range p.entries {
		if previous != nil {
			if previous.Failed() {
				return previous, nil
			}
			e.process.Input(previous.Output())
		}

		var handler OutputHandler
		if output != nil {
			key := e.key
			handler = func(stream Stream, buffer string) { output(stream, buffer, key) }
		}

		result, err := e.process.Run(ctx, nil, handler)
		if err != nil {
			return nil, err
		}
		previous = result
	}
	return previous, nil
}
