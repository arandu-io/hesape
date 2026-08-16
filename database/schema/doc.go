// Package schema declares tables and indexes: the Blueprint a migration writes
// against, the Builder that executes it, and the dump-and-load schema state.
//
// # Where the SQL comes from
//
// A Blueprint is a list of columns and commands. It builds no SQL of its own:
// ToSQL walks the commands and hands each one to the Grammar, which is where the
// three dialects differ. The dispatch is an explicit switch in compileCommand,
// so a grammar that does not support a command returns an error rather than
// being silently absent. A fluent command a driver does not declare is skipped
// by GetFluentCommands, which only adds the commands the grammar asked for.
//
// There is one Builder rather than one per driver, because what differs between
// drivers is the SQL and the SQL is the Grammar's. The exception is
// RefreshDatabaseFile, which touches a file rather than the database and so
// checks the driver itself.
//
// # The Grant
//
// Blueprint and Grammar build strings and decide nothing: they take no
// auth.Grant, exactly as query.Builder takes none. Builder is the half that
// executes, and every method on it that reaches the database takes an
// auth.Grant and checks it against ActionMigrate before a statement is sent.
//
// DDL is the one place in this component where auth.Tenant(g) does not appear in
// the statement: a create table names a table, not rows, so there is no tenant
// column to filter on. The Grant is still mandatory, because what it closes is a
// path that reaches the database with nobody having decided anything, and a
// migration runner is such a path. A pipeline gets its Grant from
// auth.SystemGrant, which refuses to issue one without a tenant:
//
//	g := auth.SystemGrant(schema.ActionMigrate, tenant)
//	if err := builder.Create(ctx, g, "users", func(t *schema.Blueprint) {
//	    t.ID("")
//	    t.String("email", 255).Unique()
//	    t.Timestamps()
//	}); err != nil { ... }
//
// # Naming
//
// An initialism is upper case: ID, UUID, ULID, JSON, JSONB, SRID, IPAddress.
// Optional arguments of one type are variadic -- String(column, length...),
// Index(columns, name...) -- and where they are not, the parameter is required
// and the empty string selects the default: ULID("") names its column 'ulid'
// and SoftDeletes("") names 'deleted_at'. A value that is both readable and
// settable keeps the setter as the plain verb and prefixes the reader with Get:
// GetTable, GetEngine, GetCharset, GetCollation, GetTemporary, GetAfter,
// GetColumns, GetCommands, GetState, and on ColumnDefinition GetName, GetType,
// GetNullable, GetDefault, GetComment.
//
// MariaDB and MySQL share one schema state: mysqldump is the same program, and
// the one difference -- omitting --set-gtid-purged -- is a branch
// MySqlSchemaState takes on Connection.IsMaria.
package schema
