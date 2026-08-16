package console

import (
	"os"
	"sort"
	"strings"
)

// bootstrappers is what Starting registered.
//
// It is package level so a package can register a bootstrapper in its own
// wiring, before any application exists, and have every application built
// afterwards run it.
var bootstrappers []func(*Application)

// Starting registers a callback run against every application that is built
// after it.
//
// It is where a package that ships commands adds them without the entry point
// having to name it.
func Starting(callback func(*Application)) {
	if callback != nil {
		bootstrappers = append(bootstrappers, callback)
	}
}

// ForgetBootstrappers drops every callback Starting registered.
//
// It exists for the test that must not inherit what an earlier one
// registered.
func ForgetBootstrappers() { bootstrappers = nil }

// Bootstrap runs the registered callbacks against this application.
//
// It is a separate call from NewApplication: a constructor that runs
// arbitrary callbacks is a constructor no test can build quietly.
func (r *Application) Bootstrap() *Application {
	for _, bootstrapper := range bootstrappers {
		bootstrapper(r)
	}
	return r
}

// AddCommand registers one command.
//
// Add is the same thing for many.
func (r *Application) AddCommand(command Command) *Application { return r.Add(command) }

// Resolve registers a command and returns the application.
//
// The command given is already the value that will run: there is no class
// name to look up or construct.
func (r *Application) Resolve(command Command) *Application { return r.Add(command) }

// ResolveCommands registers many.
func (r *Application) ResolveCommands(commands ...Command) *Application { return r.Add(commands...) }

// Has reports whether a command with that name is registered.
func (r *Application) Has(name string) bool {
	_, found := r.commands[name]
	return found
}

// Find returns one registered command.
//
// The second return is false when there is none.
func (r *Application) Find(name string) (Command, bool) {
	command, found := r.commands[name]
	return command, found
}

// All returns every registered command, hidden ones included, sorted by name.
//
// Names is the same list with the hidden ones left out, which is what a
// person is offered.
func (r *Application) All() []Command {
	out := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PhpBinary returns the path to the binary a command runs under.
//
// Go compiles, so there is no interpreter separate from the program: the
// binary is both, and this is the whole of what FormatCommandString needs to
// build a command line that runs this binary again.
func PhpBinary() string {
	if executable, err := os.Executable(); err == nil {
		return executable
	}
	return os.Args[0]
}

// ArtisanBinary is the console entry point.
//
// A compiled binary is both the interpreter and the script, so there is no
// second file to name and this is empty -- FormatCommandString drops it.
func ArtisanBinary() string { return "" }

// FormatCommandString turns a command name into a line a shell can run.
//
// The binary path, the script path and the command name are joined with
// spaces; the script path here is always empty, and an empty part is dropped,
// so what comes out is "/path/to/app schedule:finish" rather than a line with
// a hole in the middle.
func FormatCommandString(command string) string {
	parts := []string{}
	for _, part := range []string{PhpBinary(), ArtisanBinary(), command} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}
