package console

import (
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/config"
)

// ExitError is an error that also says what the process should exit with.
//
// A command returns an error and the binary decides the status: os.Exit inside
// a command is a command no test can call, and a command that only returns an
// error can never say more than "1". This carries both, and ExitCode is how the
// entry point reads it back.
type ExitError struct {
	// Code is the process status. It is never zero: an ExitError that exited
	// successfully is a contradiction, and Exit refuses to build one.
	Code int

	// Message is what to print. It may be empty, for the command that already
	// said everything it had to say and only wants the status changed.
	Message string
}

// Error implements error.
func (e *ExitError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Message
}

// Exit returns an error that ends the command with a status of its own.
//
// The status is what a caller scripts against -- a pipeline that tells a
// failure it can retry from one it cannot -- so it is worth choosing. Use 1
// when there is nothing to distinguish.
func Exit(code int, format string, a ...any) error {
	if code == 0 {
		// Zero means success, and an error that reports success would end the
		// command while telling the shell everything went well.
		code = 1
	}
	message := format
	if len(a) > 0 {
		message = fmt.Sprintf(format, a...)
	}
	return &ExitError{Code: code, Message: message}
}

// ExitCode is the status a binary should exit with, given what the command
// returned.
//
// Nil is zero, an ExitError anywhere in the chain is its code, and anything
// else is 1. It is the whole of what an entry point needs:
//
//	if err := registry.Handle(ctx, os.Args[1:]); err != nil {
//		fmt.Fprintln(os.Stderr, err)
//		os.Exit(console.ExitCode(err))
//	}
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}

// GuardEnv refuses an action outside the environment it belongs to.
//
// It is the check that stands between `migrate:fresh` and a production
// database. Laravel asks for confirmation; this refuses, because a prompt is
// answered by whoever is at the keyboard and a deploy pipeline has nobody
// there.
//
// action is what would have happened, in the imperative, so the message reads
// as a sentence: GuardEnv(cfg.App.Env, config.EnvDev, "dropping every table").
func GuardEnv(env config.Env, want config.Env, action string) error {
	if env.Is(want) {
		return nil
	}
	return Exit(1, "%s is refused in the %s environment: it is allowed in %s only, and the environment comes from APP_ENV",
		action, env, want)
}
