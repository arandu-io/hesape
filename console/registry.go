package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/arandu-io/hesape/cache"
)

// Registry is everything one binary answers to on the command line.
//
// It replaces the switch of a dozen cases that every project copied into its
// own bootstrap: the built-in commands and the application's own are the same
// kind of value in the same map, so neither can shadow the other by accident
// and both are listed by the same help.
//
// A Registry is built at wiring time and read afterwards. It is not safe to Add
// to one while another goroutine is dispatching against it.
type Registry struct {
	commands map[string]Command

	out io.Writer
	err io.Writer
	in  io.Reader

	locks   *cache.Locks
	lockTTL time.Duration

	observers []Observer
}

// Observer is told about a command that has finished.
//
// It is where a Collector, a metric or an audit line hangs. It runs after the
// command returns, on the same goroutine, so an observer that blocks holds the
// process: keep it to a record and a write.
type Observer func(Run)

// Run is one finished command, as an observer sees it.
type Run struct {
	// Name is the command that ran.
	Name string
	// Args is what followed it, unparsed.
	Args []string
	// Duration is how long Run took, including the wait for a lock.
	Duration time.Duration
	// Err is what the command returned. It is cache.ErrLocked when the command
	// was isolated and another process held the lock -- that is the one error
	// the registry does not pass on to the caller, and the observer is where it
	// stays visible.
	Err error
}

// New returns an empty registry over three streams.
//
// The streams are given rather than taken from the process, because a registry
// that reaches for os.Stdout is a registry no test can read the output of. The
// entry point passes os.Stdout, os.Stderr and os.Stdin; a test passes buffers.
func New(out, errOut io.Writer, in io.Reader) *Registry {
	return &Registry{
		commands: map[string]Command{},
		out:      out,
		err:      errOut,
		in:       in,
	}
}

// Add registers commands and returns the registry, so wiring reads as one
// expression.
//
// A command with no name, no Run, or a name that is already taken is a mistake
// made at wiring time and it panics, the way a duplicate route pattern does:
// the alternative is a binary that starts and silently answers the wrong thing.
func (r *Registry) Add(commands ...Command) *Registry {
	for _, c := range commands {
		switch {
		case strings.TrimSpace(c.Name) == "":
			panic("console: a command needs a name")
		case c.Run == nil:
			panic(fmt.Sprintf("console: the command %q has no Run", c.Name))
		}
		if _, taken := r.commands[c.Name]; taken {
			panic(fmt.Sprintf("console: the command %q is registered twice, and only one of them would ever run", c.Name))
		}
		r.commands[c.Name] = c
	}
	return r
}

// WithLocks gives the registry the issuer that isolated commands take a lock
// from, and the time to live of that lock.
//
// One ttl covers every isolated command, and it is sized above the longest of
// them: it is the deadlock protection, and a process that dies holding a lock
// releases it only when the ttl runs out.
//
// Without this, an isolated command is refused rather than run unprotected --
// a command that says it must not overlap and then does is worse than one that
// does not start.
func (r *Registry) WithLocks(locks *cache.Locks, ttl time.Duration) *Registry {
	r.locks = locks
	r.lockTTL = ttl
	return r
}

// Observe adds an observer. They run in the order they were added.
func (r *Registry) Observe(o Observer) *Registry {
	if o != nil {
		r.observers = append(r.observers, o)
	}
	return r
}

// Names lists the commands a person can be told about, sorted.
//
// Hidden commands are not in it: it is what an error message offers instead of
// the name that was not found.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for name, c := range r.commands {
		if c.Hidden {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Help renders the listing.
//
// It is sorted by name and not by registration order, so the group prefix does
// the grouping -- every make: together, every migrate: together -- and adding a
// command cannot move an unrelated one.
func (r *Registry) Help() string {
	names := r.Names()
	if len(names) == 0 {
		return "no commands registered\n"
	}

	var b strings.Builder
	b.WriteString("commands:\n")
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, name := range names {
		fmt.Fprintf(w, "  %s\t%s\n", name, r.commands[name].Description)
	}
	_ = w.Flush()
	return b.String()
}

// Handle dispatches one command line: the name, then its arguments.
//
// No arguments, or help, prints the listing. A name that is not registered is
// an ExitError that lists what was, because an error that only says the command
// is unknown costs a search and this one ends it.
func (r *Registry) Handle(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(r.out, r.Help())
		return nil
	}

	name := args[0]
	switch name {
	case "help", "-h", "--help":
		fmt.Fprint(r.out, r.Help())
		return nil
	}

	command, found := r.commands[name]
	if !found {
		return Exit(1, "unknown command: %s\n\n%s", name, r.Help())
	}
	return r.run(ctx, command, args[1:], r.out, r.err, r.in)
}

// Call runs a registered command from inside another one.
//
// It is how a composite command is written -- `migrate:fresh` calling
// `migrate` -- without either of them being a function that the listing does
// not know about. The output goes where the caller's does.
func (r *Registry) Call(ctx context.Context, name string, args ...string) error {
	command, found := r.commands[name]
	if !found {
		return Exit(1, "unknown command: %s\n\n%s", name, r.Help())
	}
	return r.run(ctx, command, args, r.out, r.err, r.in)
}

// CallSilent is Call with the output thrown away.
//
// The error is not: what it silences is the chatter of a command being used as
// a step, never the reason it failed.
func (r *Registry) CallSilent(ctx context.Context, name string, args ...string) error {
	command, found := r.commands[name]
	if !found {
		return Exit(1, "unknown command: %s\n\n%s", name, r.Help())
	}
	return r.run(ctx, command, args, io.Discard, io.Discard, strings.NewReader(""))
}

// run is the one path a command runs on: isolation, then the command, then the
// observers. Call, CallSilent and Handle differ only in where the output goes.
func (r *Registry) run(ctx context.Context, c Command, args []string, out, errOut io.Writer, in io.Reader) error {
	started := time.Now()
	o := NewIO(c.Name, args, out, errOut, in)

	err := r.guarded(ctx, c, o)

	run := Run{Name: c.Name, Args: args, Duration: time.Since(started), Err: err}
	for _, observe := range r.observers {
		observe(run)
	}

	// A lock another process holds is the answer, not a failure: it means the
	// work is being done. The command says so and the status stays zero, which
	// is what keeps a cron entry from paging somebody every minute.
	if errors.Is(err, cache.ErrLocked) {
		o.Comment("%s is already running somewhere else", c.Name)
		return nil
	}
	return err
}

// guarded runs the command, holding its lock when it has one.
func (r *Registry) guarded(ctx context.Context, c Command, o *IO) error {
	if c.Isolated == "" {
		return c.Run(ctx, o)
	}
	if r.locks == nil {
		return Exit(1, "the command %s is isolated on the lock %q and this binary has no lock issuer wired: "+
			"pass one to Registry.WithLocks, or the command would run as many times at once as it is started",
			c.Name, c.Isolated)
	}
	return r.locks.Lock(c.Isolated, r.lockTTL).Run(ctx, func(ctx context.Context) error {
		return c.Run(ctx, o)
	})
}
