// Package console mirrors Illuminate\Routing\Console.
//
// The files it answers to, in the clone at
// laravel_illuminate/routing/Console:
//
//	ControllerMakeCommand.php
//	MiddlewareMakeCommand.php
//
// Nothing is implemented here, and nothing will be. These are console commands,
// and ADR 0050 settled where those live: the project's own binary answers them
// through the switch in bootstrap/console.go, which `aru` reaches by running
// the project.
//
// Illuminate keeps them in the component because ArtisanServiceProvider
// registers each one into the container by name. There is no container
// (ADR 0001), so there is nothing here to register into.
package console
