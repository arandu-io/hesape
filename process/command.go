package process

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stream names which of a process's two output streams a chunk came from.
type Stream string

const (
	// Stdout is the program's standard output.
	Stdout Stream = "stdout"
	// Stderr is the program's standard error.
	Stderr Stream = "stderr"
)

// Command describes one program to run.
//
// It is a plain value with no open resource in it, so the same Command can be
// run twice, kept in a slice, or written down in a test as a literal.
type Command struct {
	// Name is the program. A name with no separator in it is looked up in PATH,
	// exactly as os/exec does; a path is used as given.
	Name string
	// Args are the arguments, without the program name. They reach the program
	// as written: nothing here expands a glob, a variable or a tilde, because
	// nothing here runs a shell.
	Args []string
	// Dir is the working directory. Empty means the calling process's own,
	// which is almost never what a command that acts on a project wants.
	Dir string
	// Env are variables added to the ones this process already has, keyed by
	// name.
	//
	// Added, not substituted: os/exec replaces the environment wholesale, so
	// every caller has to remember to copy os.Environ() first and the one who
	// forgets ships a program that cannot find PATH. A name that is already set
	// is overridden by the value here.
	Env map[string]string
	// Stdin is fed to the program on its standard input, and standard input is
	// closed once it has been written. Empty means the program reads an
	// immediately closed input, never the terminal -- a command that waits for
	// a person is a command that hangs a deploy.
	Stdin string
	// Timeout is how long the whole run may take. Zero means no limit of its
	// own, and the context still applies.
	Timeout time.Duration
	// IdleTimeout is how long the program may go without writing anything.
	// Zero means silence is allowed forever.
	//
	// It is the bound that catches what a total timeout does not: a fetch whose
	// peer stopped answering, or a compiler waiting on a lock, both of which sit
	// quiet for as long as they are given. A program that reports progress is
	// never affected by it.
	IdleTimeout time.Duration
	// MaxOutput is the most bytes kept from each of the two streams. Zero or
	// less keeps everything.
	//
	// Only what is kept is capped: the program is not killed for being loud,
	// its output keeps being read so it never blocks on a full pipe, and
	// OnOutput still sees every byte. Result.Truncated says whether anything was
	// dropped.
	MaxOutput int
	// OnOutput, when set, is called with each chunk as it arrives -- the way
	// output reaches a terminal while the command is still running.
	//
	// Calls are serialized, so the two streams never overlap and the function
	// needs no lock of its own. It must not retain the slice: the bytes are
	// reused as soon as it returns, the same contract io.Writer states.
	OnOutput func(stream Stream, chunk []byte)
}

// String renders the command the way a person would type it, for an error
// message or a log line.
//
// It is a rendering and not a quoting: an argument that needs quotes gets Go's,
// which no shell has to agree with. Nothing in this package feeds a string to a
// shell, so nothing depends on the answer being executable.
func (c Command) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	for _, word := range append([]string{c.Name}, c.Args...) {
		if word == "" || strings.ContainsAny(word, " \t\n\"'\\$`") {
			word = strconv.Quote(word)
		}
		parts = append(parts, word)
	}
	return strings.Join(parts, " ")
}

// environ is the environment the program is started with, or nil to inherit
// this process's own unchanged.
//
// The additions go after os.Environ(), which is what makes them win: os/exec
// keeps the last value for a repeated name. Sorted, so two runs of the same
// Command hand the program the same slice and a test can say what it expects.
func (c Command) environ() ([]string, error) {
	if len(c.Env) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(c.Env))
	for name := range c.Env {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return nil, fmt.Errorf("process: %q is not an environment variable name", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	env := os.Environ()
	for _, name := range names {
		env = append(env, name+"="+c.Env[name])
	}
	return env, nil
}
