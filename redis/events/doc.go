// Package events mirrors Illuminate\Redis\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Events:
//
//	CommandExecuted.php -> CommandExecuted
//
// One event, fired by Connection.Command on every command that goes through
// the wrapper, carrying the command, its arguments, how long it took and which
// connection ran it. It is what Redis::listen() sees in Laravel, and
// connections.Connection.Listen is the same registration here.
//
// It is off by default: a connection with no dispatcher fires nothing, and
// RedisManager.EnableEvents is what turns it on. That is the Laravel default
// too, and the reason is the same -- an event per command is an event per cache
// read.
package events
