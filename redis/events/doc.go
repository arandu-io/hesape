// Package events mirrors Illuminate\Redis\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Events:
//
//	CommandExecuted.php
//
// Nothing is implemented here. CommandExecuted is dispatched by RedisManager on
// every command so that Redis::listen() can see them, and there is no manager
// here to dispatch it: ADR 0001 rejected the container, and the connection is
// constructed and passed rather than resolved by name.
//
// What the event is used for -- how long each command took, and which ones --
// is telemetry, and telemetry in this collection is the collector's. A second
// event bus carrying the same facts would be a second place to look when a page
// is slow (RULE 9).
package events
