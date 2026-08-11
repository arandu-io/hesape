// Package console mirrors Illuminate\Concurrency\Console.
//
// The files it answers to, in the clone at
// laravel_illuminate/concurrency/Console:
//
//	InvokeSerializedClosureCommand.php
//
// Nothing is implemented here, and nothing will be. The command is the far end
// of ProcessDriver's pipe: the parent serializes a closure into
// LARAVEL_INVOKABLE_CLOSURE, shells out to `artisan invoke-serialized-closure`,
// and this command unserializes it, runs it, and prints the result -- or the
// exception's class, message and constructor arguments -- as JSON on stdout.
//
// Serializing a closure is a PHP language facility Go does not have, and
// hesape/concurrency runs tasks in goroutines rather than in a second process,
// so there is no pipe and no far end. The package comment of hesape/concurrency
// says why the three drivers are one.
package console
