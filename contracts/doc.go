// Package contracts exports nothing.
//
// In Go an interface belongs to the package that consumes it, not to a tree of
// its own. Every interface this module needs is declared where it is used --
// cache.Store in package cache, queue.Queue in package queue, session.Handler
// in package session -- so a copy gathered here would be a second way to say
// the same thing.
package contracts
