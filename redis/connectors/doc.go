// Package connectors mirrors Illuminate\Redis\Connectors.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Connectors:
//
//	PhpRedisConnector.php
//	PredisConnector.php
//
// Nothing is implemented here, and nothing will be. A connector is the choice
// between two client libraries -- phpredis, the C extension, and predis, the
// pure-PHP one -- and there is one driver here. What the two of them do,
// turning a configuration into an open connection, is connections.Connect, and
// a package holding a second spelling of that would be a second way to open a
// connection (RULE 9).
//
// The directory stays because the mirror is the map: somebody looking for
// PhpRedisConnector finds this file, and this file says where the work went.
package connectors
