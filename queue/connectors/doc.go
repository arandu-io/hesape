// Package connectors opens queue connections.
//
// It mirrors Illuminate\Queue\Connectors. In Laravel a connector turns a block
// of config into a queue instance, because the container resolves drivers by
// name at runtime. Here the application constructs its queues in
// bootstrap/app.go and hands them to queue.QueueManager (ADR 0001), so a
// connector is what is left of that: the thing that knows how to open a
// connection, handed to QueueManager.AddConnector so it is not opened until
// something asks for it.
//
//	m.AddConnector("database", connectors.Database{DB: db}.Connect)
//
// That laziness is the one thing the indirection buys, and it is worth having
// for exactly one reason: a binary that runs `aru migrate` should not open a
// RESP socket on the way past.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Connectors:
//
//	BackgroundConnector.php -> Background
//	BeanstalkdConnector.php -- not coming, RULE 11
//	ConnectorInterface.php  -> Connector
//	DatabaseConnector.php   -> Database
//	DeferredConnector.php   -> Deferred
//	FailoverConnector.php   -> Failover
//	NullConnector.php       -> Null
//	RedisConnector.php      -> connectors/redis, its own module
//	SqsConnector.php        -- not coming, RULE 11
//	SyncConnector.php       -> Sync
//
// Only redis has a package of its own, and it has one because it depends on a
// RESP client: in Go there is no optional dependency, so a driver with a
// third-party client is a module of its own or it is in everybody's go.sum
// (ADR 0048). The drivers that need nothing installed are in the queue package
// itself, next to the contract they implement, and these connectors are how
// they are wired lazily.
//
// Beanstalkd and SQS are not coming: RULE 11 names the stores this collection
// speaks to, and neither is one of them.
package connectors
