package console

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrManuallyFailed is what Fail throws when it is given no reason.
//
// The command decided it was done and wrong, and there is nothing more to say
// than that.
var ErrManuallyFailed = errors.New("command failed manually")

// Definition parses the command's signature.
//
// A command with no signature has no arguments and no options, and its name
// is Name.
func (c Command) Definition() (name string, arguments []Argument, options []Option, err error) {
	if strings.TrimSpace(c.Signature) == "" {
		return c.Name, nil, nil, nil
	}
	return Parse(c.Signature)
}

// SetName names the command.
//
// It returns the command so the setters chain.
func (c *Command) SetName(name string) *Command { c.Name = name; return c }

// SetDescription sets the line the listing prints beside the name.
func (c *Command) SetDescription(description string) *Command {
	c.Description = description
	return c
}

// SetHelp sets the long explanation.
func (c *Command) SetHelp(help string) *Command { c.Help = help; return c }

// SetAliases sets the other names the command answers to.
func (c *Command) SetAliases(aliases ...string) *Command { c.Aliases = aliases; return c }

// SetHidden keeps the command out of the listing, or puts it back.
//
// Setting it with no value hides.
func (c *Command) SetHidden(hidden ...bool) *Command {
	c.Hidden = true
	if len(hidden) > 0 {
		c.Hidden = hidden[0]
	}
	return c
}

// IsHidden reports whether the command stays out of the listing.
func (c Command) IsHidden() bool { return c.Hidden }

// Handle does the command's own work.
//
// What Execute adds around it is the isolation mutex.
func (c Command) Handle(ctx context.Context, o *IO) error {
	if c.Run == nil {
		return fmt.Errorf("console: the command %q has no Run", c.Name)
	}
	return c.Run(ctx, o)
}

// Execute runs the command, holding its isolation mutex when it has one.
//
// The order is the isolated check first, the work second, and the mutex
// released whatever happened. A command whose mutex is already held says so
// and reports success, because the work is being done -- which is what keeps
// a cron entry from paging somebody every minute.
//
// mutex may be nil, and then an isolated command is refused rather than run
// unprotected: a command that says it must not overlap and then does is worse
// than one that does not start.
func (c Command) Execute(ctx context.Context, o *IO, mutex CommandMutex) error {
	if c.Isolated == "" {
		return c.Handle(ctx, o)
	}
	if mutex == nil {
		return Exit(1, "the command %s is isolated on the lock %q and this binary has no command mutex wired: "+
			"pass one, or the command would run as many times at once as it is started", c.Name, c.Isolated)
	}

	created, err := mutex.Create(ctx, c)
	if err != nil {
		return err
	}
	if !created {
		o.Comment("the %s command is already running", c.Name)
		return nil
	}
	defer func() { _, _ = mutex.Forget(ctx, c) }()

	return c.Handle(ctx, o)
}

// Fail ends the command with a reason of its own.
//
// Passing nothing is ErrManuallyFailed; passing an error wraps it, so
// errors.Is finds both.
func (c Command) Fail(cause error) error {
	if cause == nil {
		return fmt.Errorf("%s: %w", c.Name, ErrManuallyFailed)
	}
	return fmt.Errorf("%s: %w", c.Name, cause)
}
