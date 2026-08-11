package console

import (
	"os"
	"sort"
	"strings"
)

// bootstrappers is what Starting registered.
//
// It is package level because Application::$bootstrappers is static: a package
// registers a bootstrapper in its own wiring, before any application exists,
// and every application built afterwards runs it.
var bootstrappers []func(*Application)

// Starting registers a callback run against every application that is built
// after it.
//
// It answers Application::starting. It is where a package that ships commands
// adds them without the entry point having to name it.
func Starting(callback func(*Application)) {
	if callback != nil {
		bootstrappers = append(bootstrappers, callback)
	}
}

// ForgetBootstrappers drops every callback Starting registered.
//
// It answers Application::forgetBootstrappers, and it is there for the test that
// must not inherit what an earlier one registered.
func ForgetBootstrappers() { bootstrappers = nil }

// Bootstrap runs the registered callbacks against this application.
//
// It answers Application::bootstrap, which the PHP constructor calls. It is
// separate here because the constructor takes no container to resolve them
// from, and a constructor that runs arbitrary callbacks is a constructor no test
// can build quietly.
func (r *Application) Bootstrap() *Application {
	for _, bootstrapper := range bootstrappers {
		bootstrapper(r)
	}
	return r
}

// AddCommand registers one command.
//
// It answers Application::addCommand. Add is the same thing for many, which is
// the pair the PHP has in add() and addCommand().
func (r *Application) AddCommand(command Command) *Application { return r.Add(command) }

// Resolve registers a command and returns the application.
//
// It answers Application::resolve, minus the container: PHP takes a class name
// and makes it, and here the command is already the value it will be run as.
func (r *Application) Resolve(command Command) *Application { return r.Add(command) }

// ResolveCommands registers many.
//
// It answers Application::resolveCommands.
func (r *Application) ResolveCommands(commands ...Command) *Application { return r.Add(commands...) }

// Has reports whether a command with that name is registered.
//
// It answers Symfony's Application::has, which Application::call checks before
// it runs anything.
func (r *Application) Has(name string) bool {
	_, found := r.commands[name]
	return found
}

// Find returns one registered command.
//
// It answers Application::find. The second return is false when there is none,
// where PHP throws CommandNotFoundException.
func (r *Application) Find(name string) (Command, bool) {
	command, found := r.commands[name]
	return command, found
}

// All returns every registered command, hidden ones included, sorted by name.
//
// It answers Application::all. Names is the same list with the hidden ones left
// out, which is what a person is offered.
func (r *Application) All() []Command {
	out := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PhpBinary is the interpreter a command runs under.
//
// It answers Application::phpBinary. Go compiles, so there is no interpreter
// separate from the program: this is the running executable, and it is the
// whole of what FormatCommandString needs to build a command line that runs
// this binary again.
//
// The name is the PHP one because Event::normalizeCommand and CommandBuilder
// both name it, and a scheduler that says "binary" in one file and "php" in the
// other is a scheduler whose output nobody can grep for.
func PhpBinary() string {
	if executable, err := os.Executable(); err == nil {
		return executable
	}
	return os.Args[0]
}

// ArtisanBinary is the console entry point.
//
// It answers Application::artisanBinary. A PHP project runs `php artisan`: two
// files, the interpreter and the script. A compiled binary is both, so there is
// no second file to name and this is empty -- FormatCommandString drops it.
func ArtisanBinary() string { return "" }

// FormatCommandString turns a command name into a line a shell can run.
//
// It answers Application::formatCommandString. PHP joins three parts, the
// interpreter, the script and the name; here the script is empty and the empty
// part is dropped, so what comes out is "/path/to/app schedule:finish" rather
// than a line with a hole in the middle.
func FormatCommandString(command string) string {
	parts := []string{}
	for _, part := range []string{PhpBinary(), ArtisanBinary(), command} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}
