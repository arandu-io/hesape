// Package console mirrors Illuminate\Auth\Console.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Console:
//
//	ClearResetsCommand.php -> [ClearResetsCommand]
//
// One command: delete the password reset tokens that have expired. It is a
// scheduled sweep, not a boot step -- a table that only grows is a table that
// eventually holds every reset anybody ever asked for.
//
// # What is skipped, and why
//
// Console/stubs/make/views/layouts/app.stub, the Blade layout the removed
// make:auth scaffolding published. There is no make:auth in Illuminate either
// since Laravel 6 -- the stub is a leftover -- and the Arandu equivalent is
// arandu-io/ui, which publishes the nine auth views into the project (ADR 0026).
// The aru CLI deliberately has no make:auth, for the reason the artisan does
// not: two ways to install the same scaffolding diverge (RULE 3).
//
// The Illuminate command reaches the broker through
// $this->laravel['auth.password'], the container. There is no container
// (ADR 0001), so [NewClearResetsCommand] takes the broker factory, and the
// interfaces it needs are declared here rather than imported from
// hesape/auth/passwords: this package needs three methods, and an interface is
// what lets the command be built and tested before that package exists.
package console
