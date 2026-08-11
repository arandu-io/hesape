// Package capsule mirrors Illuminate\Database\Capsule.
//
// A capsule is the database usable from a script with no application around it:
// three lines of configuration, and a connection. Laravel built it so its
// database layer could be used outside Laravel, and it is here for the same
// case -- a one-off migration tool, a fixture loader, a test harness.
//
// It is not how an application reaches its data, and the difference matters
// more here than it does there. An application holds a Repository, which holds
// an auth.Grant and filters by auth.Tenant(g); the capsule's static Connection
// and Table hold neither. A capsule call in a request path is a query nobody
// authorized (RULE 17), and it is the sort of thing that gets a module rejected
// in review.
//
// The container the PHP's constructor takes is gone (ADR 0001), and what it was
// actually being used for -- somewhere to keep database.default and
// database.connections -- is the argument in its place.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Capsule:
//
//	Manager.php
//
// Manager::setFetchMode has no counterpart: PDO's fetch mode chooses between a
// stdClass and an array per row, and there is one row shape here. __callStatic
// has none either -- Go has no dynamic dispatch, and Connection(name) then the
// call is one more word and one fewer indirection.
package capsule
