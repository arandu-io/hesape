// Package connectors opens queue connections.
//
// The application constructs its queues in bootstrap/app.go and hands them to
// queue.QueueManager. A connector is the lazy form of that: the thing that
// knows how to open a connection, handed to QueueManager.AddConnector so it is
// not opened until something asks for it.
//
//	m.AddConnector("database", connectors.Database{DB: db}.Connect)
//
// That laziness is the one thing the indirection buys, and it is worth having
// for exactly one reason: a binary that runs `aru migrate` should not open a
// RESP socket on the way past.
//
// Only redis has a package of its own, and it has one because it depends on a
// RESP client: in Go there is no optional dependency, so a driver with a
// third-party client is a module of its own or it is in everybody's go.sum. The
// drivers that need nothing installed are in the queue package itself, next to
// the contract they implement, and these connectors are how they are wired
// lazily.
package connectors
