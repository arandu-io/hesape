package process

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// excerpt is how much of what the program said goes into an error message.
//
// Enough for a compiler's first complaint or a connection refused; short enough
// that a wrapped error stays one thing a person reads. The whole text is on
// ExitError.Stderr and on Result.
const excerpt = 512

// ExitError is a program that ran and returned a non-zero status.
//
// It exists for its message. os/exec reports the same event as "exit status 1"
// and discards what the program wrote, so the one line that explains the
// failure has to be printed by whoever remembered to capture stderr -- and the
// caller who did not remember ships a build failure that names the exit status
// of a program the person did not know was running.
type ExitError struct {
	// Name and Args are the program that failed.
	Name string
	Args []string
	// ExitCode is the status it returned, always non-zero.
	ExitCode int
	// Stderr is everything the program wrote to standard error, uncut. The
	// message carries only the beginning of it.
	Stderr string
}

func (e *ExitError) Error() string {
	line := fmt.Sprintf("%s: exit status %d", Command{Name: e.Name, Args: e.Args}, e.ExitCode)
	said := strings.TrimSpace(e.Stderr)
	if said == "" {
		return line
	}
	if len(said) > excerpt {
		said = said[:excerpt] + "..."
	}
	return line + ": " + said
}

// TimeoutError is a program that was killed for taking too long, or -- when
// Idle is set -- for having said nothing for too long.
//
// It unwraps to context.DeadlineExceeded, so a caller that only wants to know
// whether it was time that ran out does not have to name this type.
type TimeoutError struct {
	// Name and Args are the program that was killed.
	Name string
	Args []string
	// After is the limit that was reached: Command.Timeout, or
	// Command.IdleTimeout when Idle is set.
	After time.Duration
	// Idle distinguishes the two, because they mean different things. A total
	// timeout is work that did not fit; an idle timeout is work that stopped
	// happening, and raising the limit will not help it.
	Idle bool
}

func (e *TimeoutError) Error() string {
	if e.Idle {
		return fmt.Sprintf("%s: killed after %s with no output", Command{Name: e.Name, Args: e.Args}, e.After)
	}
	return fmt.Sprintf("%s: killed after %s", Command{Name: e.Name, Args: e.Args}, e.After)
}

// Unwrap reports the timeout as the standard one.
func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }
