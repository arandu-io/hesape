package database

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// DefaultConnectionName is the name [NewSingleConnectionResolver] registers its
// one connection under when it is given none.
//
// A migration that names no connection runs on the default one, which is this,
// so the name is only ever written down by a migration that sets
// BaseMigration.Connection to reach somewhere else.
const DefaultConnectionName = "default"

// NewSingleConnectionResolver returns a resolver holding db as its only
// connection, registered under name and made the default. An empty name is
// [DefaultConnectionName].
//
// It is the resolver of an application that opens exactly one database, which
// is what the migrator, the database commands and the wipe all reach their
// connection through. The connection is built from db itself -- its pool, its
// dialect -- so the driver every command reports is the engine the application
// is actually talking to.
//
// Each call builds a fresh resolver over the same pool. Nothing it holds is
// worth sharing: the pool is db's, and a resolver is a name and a pointer.
func NewSingleConnectionResolver(db *DB, name string) *ConnectionResolver {
	if name == "" {
		name = DefaultConnectionName
	}

	connection := NewConnection(db.Unwrap(), "", "", map[string]any{
		"driver": string(db.Dialect()),
		"name":   name,
	})

	resolver := NewConnectionResolver(map[string]ConnectionInterface{name: connection})
	resolver.SetDefaultConnection(name)
	return resolver
}

// NewMigrator returns the migrator of an application that opens one database:
// a repository recording applied migrations in table, over a resolver holding
// db under [DefaultConnectionName].
//
// An empty table is migrations.DefaultTable. A project whose databases were
// migrated under another name passes that name, and must keep passing it:
// pointing a binary at a table that is empty on a fully migrated database makes
// it apply every migration a second time. The value is the compatibility, and
// changing it is a data migration of the tracking table rather than a rename.
//
// The resolver adapts each connection with [ForMigrations] on the way out,
// which is what gives every migration its own transaction where the engine can
// roll DDL back and what lets --pretend capture the statements instead of
// running them.
//
// It writes no progress until somebody calls SetOutput: the migration commands
// point it at their own output when they run. The registry is left as it is --
// the migrations a module declares are registered by whoever collected them,
// under the group they should be listed in.
func NewMigrator(db *DB, table string) *migrations.Migrator {
	if table == "" {
		table = migrations.DefaultTable
	}

	resolver := MigrationResolver{Resolver: NewSingleConnectionResolver(db, DefaultConnectionName)}
	repository := migrations.NewDatabaseMigrationRepository(resolver, table)

	migrator := migrations.NewMigrator(repository, resolver, nil)
	migrator.SetConnection(DefaultConnectionName)
	return migrator
}

// Wiper returns the function that drops every table of a named connection,
// which is what migrate:fresh and db:wipe are handed. An empty connection name
// is the resolver's default.
//
// It reads and drops through the schema builder over that connection, the same
// door every migration writes DDL through, so what a wipe removes and what a
// migration created cannot be two descriptions of the schema. The builder drops
// only what the connection searches -- see schema.Builder.DropAllTables.
//
// It is handed over as a function, and the commands refuse to run without one,
// because "drop every table" is a capability an application gives a command
// deliberately rather than one the framework assumes it has. Where it may run is
// still the command's question: the destructive ones refuse outside development
// before they call it.
func Wiper(resolver ConnectionResolverInterface) func(ctx context.Context, connection string) error {
	return func(ctx context.Context, connection string) error {
		resolved, err := resolver.Connection(connection)
		if err != nil {
			return err
		}
		concrete, ok := resolved.(*Connection)
		if !ok {
			return fmt.Errorf("database: dropping every table needs a *database.Connection to reach the schema builder, and %q resolved to %T",
				connection, resolved)
		}
		return schema.NewBuilder(ForSchema(concrete)).DropAllTables(ctx)
	}
}
