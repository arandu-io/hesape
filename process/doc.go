// Package process runs external programs.
//
// It answers Illuminate\Process, name for name: Factory, PendingProcess,
// InvokedProcess, ProcessResult, Pool, Pipe, and the five Fake* types. What a
// Laravel developer types here is what they type there.
//
//	factory := process.NewFactory()
//	result, err := factory.Run(ctx, []string{"go", "build", "./..."}, nil)
//	if result.Failed() {
//		log.Print(result.ErrorOutput())
//	}
//
// # A failed exit is a result, not an error
//
// This is the one thing to know before writing anything else with it, and it is
// Illuminate's rule rather than ours. A program that exits non-zero comes back
// with a nil error and Failed reporting it:
//
//	result, err := factory.Run(ctx, []string{"git", "diff", "--quiet"}, nil)
//	// err is nil. A difference is exit code 1, and it is not a failure.
//	if result.ExitCode() == 1 {
//		// there are changes
//	}
//
// The error is for the program that never ran, ran out of time, or was stray.
// Throw is what turns a failed exit into one, when the caller wants that:
//
//	result, err := factory.Run(ctx, []string{"go", "test", "./..."}, nil)
//	if _, err = result.Throw(nil); err != nil {
//		return err // a *ProcessFailedException, carrying both streams
//	}
//
// # What os/exec does not do
//
//   - An error that repeats what the program said. os/exec reports a failed
//     command as "exit status 1" and throws the explanation away, so a build
//     that stopped because a module could not be fetched answers with the exit
//     status of a program the person did not know was running.
//   - A deadline that also covers the program that never finishes and never
//     prints. A context deadline bounds the total; IdleTimeout bounds the
//     silence, which is the shape a hung download or a stalled compiler has.
//   - A fake. Factory.Fake answers a command pattern with a canned result, so a
//     test that shells out stops shelling out, and PreventStrayProcesses turns
//     "somebody forgot to fake git" from a test that quietly runs git on CI
//     into a test that fails naming the command.
//
// # No shell, and no string form to add
//
// A command is a program and its arguments, and nothing here hands a string to
// sh. PHP accepts the string form because Symfony escapes it; accepting it in
// Go would be exec.Command("sh", "-c", line) with an interpolated line, which
// is command injection with a familiar signature.
//
// This is the one place the surface is deliberately narrower than Illuminate's,
// and it is narrower by removing a footgun rather than by leaving work undone.
//
// # Several at once, and one after another
//
// Pool, InvokedProcessPool, ProcessPoolResults and Pipe are here under those
// names, and so are the three methods of the factory that reach them:
//
//	results, err := factory.Concurrently(ctx, func(pool *process.Pool) {
//		pool.As("build").Command("go", "build", "./...")
//		pool.Command("go", "vet", "./...")
//	}, nil)
//
//	result, err := factory.Pipe(ctx, func(pipe *process.Pipe) {
//		pipe.Command("cat", "notes.txt")
//		pipe.Command("grep", "-i", "todo")
//	}, nil)
//
// Concurrently starts them all and waits; Factory.Pool is the same set started
// and not waited for, which is what Signal, Running and Wait on the
// InvokedProcessPool are for. A pipe feeds each process the output of the one
// before it and stops at the first that exits non-zero, whose result is what
// comes back.
//
// This used to say the four types were a gap rather than a decision, and that a
// caller who wanted concurrently() wrote the errgroup themselves. They are
// written now, and the gap is closed: a pool is goroutines under the same names
// PHP gives it, so what a Laravel developer types is what runs.
//
// The key is the string a process is added under -- As names one, and a process
// added without a name takes the next integer key, counted only among the
// unnamed, exactly as PHP's array append numbers them. It is what the pool's
// output handler is given alongside the chunk, so output from four programs at
// once can be told apart.
//
// # What is not here, and why
//
// Five public methods of the component have no counterpart.
//
// ProcessPoolResults::offsetExists, offsetGet, offsetSet and offsetUnset are
// ArrayAccess -- the language interface behind $results[0] and
// $results['build'] -- which is reason 1 of the porting rule: Go has no
// operator to overload. Each is one line over the results array, and what it
// does is index the collection [ProcessPoolResults.Collect] returns, which
// holds them in the order the pool was built.
//
// PendingProcess::toSymfonyProcess builds and hands back a
// Symfony\Component\Process\Process. It is reason 3: a library this ecosystem
// does not carry. Illuminate\Process is a wrapper over Symfony's component and
// that method is the way out of the wrapper; this package wraps os/exec, and
// what the method would hand back does not exist here. Everything it configures
// -- the working directory, the environment, the timeouts, the input, the tty
// -- is set on the [PendingProcess] and applied by Run and Start.
//
// The output held in memory is not bounded. Illuminate does not bound it
// either, and a bound that Laravel has no counterpart for was one this package
// used to carry -- it was removed along with the Truncated field, because a
// surface that is ours rather than Illuminate's is the thing ADR 0044 exists to
// keep out.
package process
