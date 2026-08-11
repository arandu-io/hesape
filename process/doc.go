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
// # What is not here, and why
//
// Pool, Pipe, InvokedProcessPool and ProcessPoolResults are the concurrency
// surface. Running several programs at once is what a goroutine already is, and
// feeding one program's output into the next is its Stdin fed from the other's
// Output. They answer to nothing here, and that is a gap rather than a
// decision: a caller who wants Illuminate's concurrently() writes the errgroup
// themselves today.
//
// The output held in memory is not bounded. Illuminate does not bound it
// either, and a bound that Laravel has no counterpart for was one this package
// used to carry -- it was removed along with the Truncated field, because a
// surface that is ours rather than Illuminate's is the thing ADR 0044 exists to
// keep out.
package process
