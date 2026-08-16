// Package events holds the event a connection fires.
//
// One event, CommandExecuted, fired by connections.Connection.Command on every
// command that goes through the wrapper, carrying the command, its arguments,
// how long it took and which connection ran it.
// connections.Connection.Listen is where a listener registers.
//
// It is off by default: a connection with no dispatcher fires nothing, and
// RedisManager.EnableEvents is what turns it on. An event per command is an
// event per cache read, which is not what most applications want to pay for.
package events
