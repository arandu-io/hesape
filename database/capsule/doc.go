// Package capsule is the database usable from a script with no application
// around it: three lines of configuration, and a connection. It is for a one-off
// migration tool, a fixture loader or a test harness.
//
// It is not how an application reaches its data. An application holds a
// Repository, which holds an auth.Grant and filters by auth.Tenant(g); the
// capsule's Connection and Table hold neither. A capsule call in a request path
// is a query nobody authorized, and it is the sort of thing that gets a module
// rejected in review.
//
// Where database.default and database.connections come from is the argument the
// constructor takes; there is nothing else to configure and no registry to reach
// into. There is no fetch mode either, because there is one row shape here.
package capsule
