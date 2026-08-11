// Package connections holds the connection everything else in this adapter
// speaks over.
//
// It mirrors Illuminate\Redis\Connections, and it is one connection rather than
// the six classes the PHP has: those six are two client libraries times cluster
// and single, and here there is one driver and one address. Sharding is the
// deployment's business (see Config.Address).
//
// The connection is deliberately thin. It opens the socket, carries the
// application prefix and answers Ping; it does not know what a tenant is, what a
// cache entry is or what a session is. Everything that decides WHICH key a value
// belongs under lives one layer up -- in cache.Repository for entries, in the
// session handler for sessions -- where it is written once instead of once per
// caller.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Connections:
//
//	Connection.php                 -> Connection
//	PacksPhpRedisValues.php        -> not here: the driver encodes
//	PhpRedisClusterConnection.php  -> not here: one address, see Config.Address
//	PhpRedisConnection.php         -> Connection
//	PredisClusterConnection.php    -> not here
//	PredisConnection.php           -> not here: one driver
package connections
