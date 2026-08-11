// Package contracts mirrors Illuminate\Contracts, and is deliberately empty.
//
// # There is no package here
//
// In Go an interface belongs to the package that consumes it, not to a tree of
// its own. Illuminate\Contracts is a namespace of 150 contracts that every
// other component type-hints against; the Go equivalent of each one is declared
// where it is used -- cache.Store in package cache, queue.Queue in package
// queue, session.Handler in package session -- so a second copy declared here
// would be a second way to say the same thing (RULE 9).
//
// The directory exists so the mirror of the 42 Illuminate components is
// complete, and so that somebody looking for Illuminate\Contracts finds the
// reason rather than silence.
//
// # The map
//
// Illuminate\Contracts is not only a tree of interfaces: it is the inventory of
// the framework's surface. That inventory is worth more than the package would
// have been, so it was measured and written down instead.
//
// docs/32-mapa-de-contratos.md crosses all 150 contracts against this module,
// one row each, and reports for every one of them whether the hesape has the
// whole method set (EXISTS), part of it (PARTIAL, with the missing PHP method
// names listed), or nothing at all (MISSING). It is ordered MISSING first,
// because that is the list somebody is looking for.
//
// Measured on 10/08/2026 against the clone in laravel_illuminate/contracts:
// 81 MISSING, 39 PARTIAL, 30 EXISTS. The count moves while the other packages
// are being written -- cookie went from two MISSING to two EXISTS on the day
// this was measured -- so the map carries its date and the reader should check
// it before reusing the numbers.
package contracts
