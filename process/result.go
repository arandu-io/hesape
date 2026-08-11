package process

import "strings"

// ProcessResult is what a command left behind.
//
// It answers to Illuminate\Contracts\Process\ProcessResult, the contract both
// Illuminate\Process\ProcessResult and Illuminate\Process\FakeProcessResult
// implement -- and it is an interface here for the same reason it is a contract
// there: Run hands back a real result or a faked one, and the caller is not
// supposed to be able to tell.
//
// A command that exited non-zero is not an error. Run returns the Result with a
// nil error and Failed reports the exit, exactly as PHP does; Throw is what
// turns it into one. The error Run returns is for the process that never ran,
// timed out, or was stray.
type ProcessResult interface {
	// Command is the command line that was run. PHP's ProcessResult::command.
	Command() string
	// Successful reports an exit code of zero. PHP's successful.
	Successful() bool
	// Failed is the other half of Successful. PHP's failed.
	Failed() bool
	// ExitCode is what the program returned, and -1 when it never got far
	// enough to return anything -- PHP's exitCode, which is null there.
	ExitCode() int
	// Output is everything the program wrote on standard output. PHP's output.
	Output() string
	// SeeInOutput reports whether Output contains the given string. PHP's
	// seeInOutput.
	SeeInOutput(output string) bool
	// ErrorOutput is everything the program wrote on standard error. PHP's
	// errorOutput.
	ErrorOutput() string
	// SeeInErrorOutput reports whether ErrorOutput contains the given string.
	// PHP's seeInErrorOutput.
	SeeInErrorOutput(output string) bool
	// Throw returns a ProcessFailedException when the command failed. PHP's
	// throw, which throws where this returns (T, error).
	Throw(callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error)
	// ThrowIf is Throw when the condition holds. PHP's throwIf.
	ThrowIf(condition bool, callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error)
}

// processResult is the ProcessResult of a program that really ran.
//
// PHP's Illuminate\Process\ProcessResult keeps the Symfony Process and asks it
// every question; this keeps the four answers, because by the time a result
// exists there is nothing left to ask an os/exec.Cmd that is not already read
// out of it. It is unexported for that reason: there is no state on it a caller
// could want that the contract does not already expose.
type processResult struct {
	command     string
	exitCode    int
	output      string
	errorOutput string
}

var _ ProcessResult = (*processResult)(nil)

// Command is the command line that was run. PHP's command.
func (r *processResult) Command() string { return r.command }

// Successful reports an exit code of zero. PHP's successful, which asks Symfony
// isSuccessful, which is the same question.
func (r *processResult) Successful() bool { return r.exitCode == 0 }

// Failed reports that the command did not succeed. PHP's failed.
func (r *processResult) Failed() bool { return !r.Successful() }

// ExitCode is the status the program returned. PHP's exitCode returns null for
// a process that never terminated; this returns -1, which is what os/exec calls
// the same thing.
func (r *processResult) ExitCode() int { return r.exitCode }

// Output is standard output. PHP's output.
func (r *processResult) Output() string { return r.output }

// SeeInOutput reports whether standard output contains the given string. PHP's
// seeInOutput.
func (r *processResult) SeeInOutput(output string) bool {
	return strings.Contains(r.Output(), output)
}

// ErrorOutput is standard error. PHP's errorOutput.
func (r *processResult) ErrorOutput() string { return r.errorOutput }

// SeeInErrorOutput reports whether standard error contains the given string.
// PHP's seeInErrorOutput.
func (r *processResult) SeeInErrorOutput(output string) bool {
	return strings.Contains(r.ErrorOutput(), output)
}

// Throw reports a failed command as an error. PHP's throw, which throws
// ProcessFailedException where this returns it; the callback is handed the same
// two arguments PHP hands it, and runs before the error is returned.
func (r *processResult) Throw(callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	return throw(r, callback)
}

// ThrowIf is Throw when the condition holds, and nothing when it does not.
// PHP's throwIf.
func (r *processResult) ThrowIf(condition bool, callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	return throwIf(r, condition, callback)
}

// throw is the body both ProcessResult implementations share, because PHP
// copies it into each of them.
func throw(r ProcessResult, callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	if r.Successful() {
		return r, nil
	}
	err := NewProcessFailedException(r)
	if callback != nil {
		callback(r, err)
	}
	return r, err
}

func throwIf(r ProcessResult, condition bool, callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	if condition {
		return throw(r, callback)
	}
	return r, nil
}
