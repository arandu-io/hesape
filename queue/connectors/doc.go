// Package connectors mirrors Illuminate\Queue\Connectors.
//
// In Laravel a connector turns a block of config into a queue instance, because
// the container resolves drivers by name at runtime. Here the application
// constructs its queues in bootstrap/app.go and hands them to queue.Manager
// (ADR 0001), so what is left in this directory is the drivers that cannot live
// in the collection's own module.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Connectors:
//
//	BackgroundConnector.php
//	BeanstalkdConnector.php
//	ConnectorInterface.php
//	DatabaseConnector.php   -> queue.NewDatabaseQueue
//	DeferredConnector.php
//	FailoverConnector.php
//	NullConnector.php       -> queue.NullQueue
//	RedisConnector.php      -> connectors/redis, its own module
//	SqsConnector.php
//	SyncConnector.php       -> queue.NewSyncQueue
//
// Only redis has a package here, and it has one because it depends on a RESP
// client: in Go there is no optional dependency, so a driver with a third-party
// client is a module of its own or it is in everybody's go.sum (ADR 0048). The
// three drivers that need nothing installed are in the queue package itself,
// next to the contract they implement.
//
// Beanstalkd and SQS are not coming: RULE 11 names the stores this collection
// speaks to, and neither is one of them.
package connectors
