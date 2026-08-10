// Package process runs external programs.
//
// It is Arandu's Process -- Illuminate\Process, whose Factory, PendingProcess,
// InvokedProcess and ProcessResult are Runner, Command, Invoked and Result
// here. What it adds to os/exec is the handful of things every caller of
// os/exec writes again and each one writes differently:
//
//   - an error that repeats what the program said. os/exec reports a failed
//     command as "exit status 1" and throws the explanation away, so a build
//     that stopped because a module could not be fetched answers with the exit
//     status of a program the person did not know was running.
//   - a deadline that also covers the program that never finishes and never
//     prints. A context deadline bounds the total; IdleTimeout bounds the
//     silence, which is the shape a hung download or a stalled compiler has.
//   - a bound on how much output is held in memory, so a program that decides
//     to print forever costs a known number of bytes and not the process.
//   - one seam. Runner is the interface; System is the only implementation
//     that touches the operating system, and a test substitutes for it by
//     implementing two methods.
//
// # One way to ask whether it failed
//
// Run returns (Result, error). The Result is always filled -- output, exit code
// and duration are there whether the program succeeded or not -- and the error
// is the only place failure is reported: there is no Successful method beside
// it (RULE 9). A caller that expects a non-zero exit and wants to carry on
// reads it back off the error:
//
//	res, err := process.System{}.Run(ctx, process.Command{Name: "git", Args: []string{"diff", "--quiet"}})
//	var exit *process.ExitError
//	if errors.As(err, &exit) && exit.ExitCode == 1 {
//		// a difference, not a failure
//	}
//
// # What is not here
//
// No shell. A Command names a program and its arguments, and nothing in this
// package hands a string to sh -- the quoting rules of a shell are how an
// argument becomes a command.
//
// No pool and no pipe, so Pool, Pipe, InvokedProcessPool and ProcessPoolResults
// answer to nothing: a process is started with Start and waited on where the
// caller wants it, running several at once is what a goroutine already is, and
// what Pipe composes in PHP is one program's Stdin fed from another's Result.
//
// No fake either, and the five Fake* classes have no counterpart: Runner is an
// interface, so a test writes the two methods it needs instead of scripting a
// sequence of pretend processes.
package process
