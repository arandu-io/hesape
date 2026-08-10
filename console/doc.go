// Package console is what an application writes its own commands against.
//
// It is the command side of a project, and it is deliberately small: a Command
// is a value with a name, a sentence of help and a function, a Registry is the
// slice of them a binary answers to, and an IO is the terminal that function
// was handed. Nothing here scans a directory, instantiates a class it found or
// parses a signature string at run time -- a command that is not in the
// registry does not exist, and one that is in it with a broken signature does
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
// and a Registry given an issuer with WithLocks takes it before the command
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
// Parser.php has no equivalent and never will: it exists to read a signature
// string into arguments and options at run time, and here the flag set is
// declared in Go and checked by the compiler. GeneratorCommand.php is aru, not
// this package. Console\Scheduling is hesape/scheduler, at the top level, for
// the reason docs/31 gives.
package console
