// Package console is what an application writes its own commands against.
//
// It is the command side of a project, and it is deliberately small: a Command
// is a value with a name, a sentence of help and a function, a Application is the
// slice of them a binary answers to, and an IO is the terminal that function
// was handed. Nothing here scans a directory, instantiates a class it found or
// parses a signature string at run time -- a command that is not in the
// application does not exist, and one that is in it with a broken signature does
// not build.
//
// This package is not the aru CLI. aru is a separate binary that generates
// files and forwards the rest to the project, because only the project knows
// which modules are wired. What lives here is what the project itself runs.
//
// The type used to be declared in the skeleton, in routes/console.go, which
// meant every project had a different nominal Command type and nothing could be
// written against two of them: neither the framework nor a library could ship a
// command. It is here now, and it is one type.
//
// # Isolation
//
// A command that must not run twice at once names a lock in Command.Isolated,
// and a Application given an issuer with WithLocks takes it before the command
// runs. The lock is not prefixed by tenant, and that is deliberate: a lock per
// tenant would let N replicas each run the task for a different tenant at the
// same time, which is the problem and not the solution. See cache.Locks and
// docs/15.
//
// # Illuminate
//
// It mirrors Illuminate\Console, and the files it answers to, in the clone at
// laravel_illuminate/console:
//
//	Application.php
//	BufferedConsoleOutput.php
//	CacheCommandMutex.php
//	Command.php
//	CommandMutex.php
//	ConfirmableTrait.php
//	ContainerCommandLoader.php
//	GeneratorCommand.php
//	ManuallyFailedException.php
//	MigrationGeneratorCommand.php
//	OutputStyle.php
//	Parser.php
//	Prohibitable.php
//	PromptValidationException.php
//	QuestionHelper.php
//	Signals.php
//
// This paragraph used to say that Parser.php had no equivalent and never would,
// that GeneratorCommand.php was aru's and not this package's, and that
// Console\Scheduling lived at the top level as hesape/scheduler. All three are
// out of date, and the corrections are the point of the package:
//
//   - Parse reads a signature. A command declares "{user} {--queue=}" and gets
//     Argument and Option, which is what makes this the package an application
//     writes commands against rather than a slice of closures. The flag set the
//     paragraph described is still on IO, and it is the older path.
//   - GeneratorCommand is here, because the framework's own packages generate
//     files too -- a migration for the cache table is written by the cache
//     package, not by the CLI.
//   - Console\Scheduling is the scheduling package below this one, and
//     hesape/scheduler is gone. Two schedulers is two ways to say the same
//     thing (RULE 9).
//
// ContainerCommandLoader.php has no equivalent, and its two public methods go
// with it -- reason 2 of ADR 0044, a method that only serves the container. The
// class is Symfony's lazy command loader over Laravel's container: it holds a
// map of name to class name so that a command is built only when it is asked
// for. A Command here is a value that was already built (ADR 0001), so there is
// nothing left to defer.
//
//   - ContainerCommandLoader::get makes the container build the class
//     registered under a name. [Application.Find] returns the registered value,
//     with false where PHP throws CommandNotFoundException.
//   - ContainerCommandLoader::getNames lists the names it could build.
//     [Application.Names] is that list without the hidden commands, which is
//     what a person is offered, and [Application.All] is the commands
//     themselves.
//   - Application::setContainerCommandLoader installs the loader.
//     [Application.Add] takes the built command instead.
//
// PromptValidationException.php has none either: it carries a laravel/prompts
// validation failure, and the prompts on IO return an error.
//
// Application::getLaravel(), Command::getLaravel() and Command::setLaravel()
// are the container under its other name: they hand a command the application
// instance so it can resolve services mid-run. A Command here is built with
// what it needs, and the run gets a context.Context rather than a service
// locator (ADR 0045).
//
// # Concerns
//
// The seven traits in Concerns/ are not a package. A PHP trait is methods mixed
// into a class, and in Go they are methods on the type that had them:
// InteractsWithIO, ConfiguresPrompts, InteractsWithSignals and
// PromptsForMissingInput are on IO; CallsCommands is on Application, as Call and
// CallSilent; HasParameters is Command.Definition, over the signature; and
// CreatesMatchingTest is on GeneratorCommand.
package console
