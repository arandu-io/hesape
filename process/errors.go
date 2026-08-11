package process

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ProcessFailedException is a command that ran and exited non-zero.
//
// It answers to Illuminate\Process\Exceptions\ProcessFailedException, and it
// keeps that name rather than becoming a ProcessFailedError: the name is what a
// Laravel developer greps for, and ADR 0044 spends the Go convention on that
// instead. It lives here and not in the exceptions directory because a Go error
// type belongs in the package that returns it -- see exceptions/doc.go.
//
// Nothing produces one on its own. It is what ProcessResult.Throw returns,
// which is the only place PHP raises it too.
type ProcessFailedException struct {
	// Result is the result that failed, the public $result property PHP
	// attaches to the exception.
	Result ProcessResult
	// Code is the exit status, which PHP passes as the exception code.
	Code int
}

// NewProcessFailedException builds the exception for a result. It is PHP's
// constructor, which takes the same one argument.
func NewProcessFailedException(result ProcessResult) *ProcessFailedException {
	code := result.ExitCode()
	if code == 0 {
		// PHP's `$result->exitCode() ?? 1`: a failure with no status is still
		// a failure, and zero would say the opposite.
		code = 1
	}
	return &ProcessFailedException{Result: result, Code: code}
}

// Error is the message PHP builds, line for line: the command, the exit code,
// and each of the two output streams under a ruled heading when it has
// anything in it.
func (e *ProcessFailedException) Error() string {
	message := fmt.Sprintf("The command \"%s\" failed.\n\nExit Code: %d", e.Result.Command(), e.Result.ExitCode())
	if e.Result.Output() != "" {
		message += fmt.Sprintf("\n\nOutput:\n================\n%s", e.Result.Output())
	}
	if e.Result.ErrorOutput() != "" {
		message += fmt.Sprintf("\n\nError Output:\n================\n%s", e.Result.ErrorOutput())
	}
	return message
}

// ProcessTimedOutException is a command that was killed for taking too long, or
// -- when Idle is set -- for having gone too long without printing anything.
//
// It answers to Illuminate\Process\Exceptions\ProcessTimedOutException, which
// in PHP wraps the Symfony exception of the same name and repeats its message.
// The message here is Symfony's, so the sentence a Laravel developer knows is
// the sentence this prints.
//
// It unwraps to context.DeadlineExceeded, so a caller that only wants to know
// whether time ran out never has to name this type.
type ProcessTimedOutException struct {
	// Result is what the program managed to say before it was killed. PHP
	// attaches the same public $result property.
	Result ProcessResult
	// Command is the command line that was killed.
	Command string
	// Timeout is the limit that was reached.
	Timeout time.Duration
	// Idle tells the two limits apart, the way Symfony's TYPE_IDLE does. They
	// mean different things: a general timeout is work that did not fit, an
	// idle timeout is work that stopped happening, and raising the limit does
	// not help the second one.
	Idle bool
}

func (e *ProcessTimedOutException) Error() string {
	if e.Idle {
		return fmt.Sprintf("The process \"%s\" exceeded the idle timeout of %v seconds.", e.Command, e.Timeout.Seconds())
	}
	return fmt.Sprintf("The process \"%s\" exceeded the timeout of %v seconds.", e.Command, e.Timeout.Seconds())
}

// Unwrap reports the timeout as the standard one, so errors.Is against
// context.DeadlineExceeded answers yes.
func (e *ProcessTimedOutException) Unwrap() error { return context.DeadlineExceeded }

// StrayProcessError is a command that was about to really run inside a test
// that had asked for that not to happen.
//
// PHP raises a bare RuntimeException with this message; a named type is what
// lets errors.As find it, and the message is copied word for word because it is
// the string somebody will search for. The name comes from Laravel's own
// preventStrayProcesses, which is the switch that produces it.
type StrayProcessError struct {
	// Command is the command line that had no matching fake.
	Command string
}

func (e *StrayProcessError) Error() string {
	return fmt.Sprintf("Attempted process [%s] without a matching fake.", e.Command)
}

// ErrEmptyProcessSequence is a FakeProcessSequence that was asked for one more
// result than it was given.
//
// PHP throws OutOfBoundsException with this sentence. Call
// FakeProcessSequence.DontFailWhenEmpty or WhenEmpty to make the sequence
// answer instead of running out.
var ErrEmptyProcessSequence = errors.New("A process was invoked, but the process result sequence is empty.")

// ErrUnsupportedFakeResult is a fake handler that returned something no fake can
// be built from.
//
// PHP throws LogicException with one of two sentences, one for the synchronous
// path and one for the asynchronous one; this is the one error, wrapped with
// which path it came from. What a handler may return is listed on Factory.Fake.
var ErrUnsupportedFakeResult = errors.New("unsupported process fake result provided")
