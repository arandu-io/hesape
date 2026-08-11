// Package redis is the job queue over RESP.
//
// It answers Illuminate\Queue\RedisQueue, and it is its own Go module: in Go
// there is no optional dependency, so a driver that carried a RESP client in
// the collection's own go.mod would put one in every project's go.sum --
// including the projects that queue over their own database (ADR 0048).
//
// Same contract as [github.com/arandu-io/hesape/queue.Queue], same Worker, same
// handlers -- one line different in bootstrap/app.go. Use it when the volume
// outgrows what a table handles comfortably, and accept what it costs: a job
// pushed here is not committed by the transaction that produced it.
//
// That trade is the whole difference between the two drivers. The DatabaseQueue
// cannot lose a job that its transaction committed, and cannot keep one whose
// transaction rolled back. This one is faster and offers neither.
//
// # Plain RESP, and why
//
// The implementation stays inside plain RESP: a sorted set for what is
// scheduled, a second for what is leased, and a hash for the payloads. No
// RedisJSON, no RediSearch, no Lua -- which is what keeps Dragonfly a drop-in
// replacement (RULE 11), and why Laravel's LuaScripts.php has no answer here.
//
// The claim is ZRem: exactly one worker removes the member and the others get
// zero back, which is the compare-and-set the DatabaseQueue spells with
// reserved_until in a WHERE.
//
// # What it answers to in Laravel
//
// The files, in the clone at laravel_illuminate/queue:
//
//	RedisQueue.php  -> RedisQueue
//	LuaScripts.php  -> nothing, on purpose (see above)
//
// and in laravel_illuminate/queue/Connectors:
//
//	RedisConnector.php -> New and Wrap
package redis
