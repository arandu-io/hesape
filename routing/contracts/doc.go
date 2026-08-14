// Package contracts mirrors Illuminate\Routing\Contracts.
//
// The files it answers to, in the clone at
// laravel_illuminate/routing/Contracts:
//
//	CallableDispatcher.php
//	ControllerDispatcher.php
//
// Nothing is implemented here, and nothing will be. An interface lives in the
// package that CONSUMES it, not in one of its own -- docs/13 fixed that, and
// ADR 0045 is where it is argued.
//
// A contracts package is a PHP necessity: the implementation is resolved out of
// the container by interface name, so the name has to exist somewhere both sides
// can see. Nothing is resolved by name here.
package contracts
