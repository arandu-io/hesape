// Package schema mirrors Illuminate\Database\Schema.
//
// The source of truth is the clone at laravel_illuminate/database/Schema,
// commit d59e7abde7bc62bb2a52b4fb0651327acf8d0d0a ("Merge branch '13.x'",
// 2026-03-02). The second clone, reference_laravel, was not used.
//
// The files it answers to:
//
//	Blueprint.php
//	BlueprintState.php
//	Builder.php
//	ColumnDefinition.php
//	ForeignIdColumnDefinition.php
//	ForeignKeyDefinition.php
//	IndexDefinition.php
//	SchemaState.php
//	MySqlSchemaState.php
//	PostgresSchemaState.php
//	SqliteSchemaState.php
//
// # The names
//
// Every name is the Illuminate name with the initial letter capitalised, and
// nothing else -- Create, Increments, ForeignID, Timestamps, SoftDeletes,
// RememberToken, Morphs, DropConstrainedForeignID. Four mechanical changes are
// permitted and each one is said at the method that takes it:
//
//   - (T, error) where the PHP throws. Nothing here panics for a condition the
//     PHP reports as an exception.
//   - An initialism is upper case: id becomes ID, uuid becomes UUID, ulid
//     becomes ULID, json becomes JSON, jsonb becomes JSONB, srid becomes SRID,
//     ipAddress becomes IPAddress.
//   - A PHP default argument becomes a Go variadic when the optional arguments
//     are all of one type: String(column, length...), Index(columns, name...).
//     When they are not, the parameter is required and the empty string selects
//     the PHP default -- ULID("") is the PHP ulid() with its 'ulid' column, and
//     SoftDeletes("") is deleted_at.
//   - PHP has a property and a method with the same name ($temporary and
//     temporary(), $engine and engine()). Go has one namespace, so the field is
//     unexported and a getter carries the Get prefix: GetTable, GetEngine,
//     GetCharset, GetCollation, GetTemporary, GetAfter, GetColumns,
//     GetCommands, GetState. The same rule runs through ColumnDefinition, where
//     nearly every attribute is also a fluent setter: GetName, GetType,
//     GetNullable, GetDefault, GetComment.
//
// # Where the SQL comes from
//
// A Blueprint is a list of columns and commands. It builds no SQL of its own:
// ToSQL walks the commands and hands each one to the Grammar, which is where
// the three dialects differ. PHP dispatches by building the method name from
// the command ('compile'.ucfirst($command->name)) and asking method_exists.
// Go has no method lookup by name, so the dispatch is an explicit switch in
// compileCommand, and a grammar that does not support a command answers with
// an error rather than being silently absent. The one case the PHP skips on
// purpose -- a fluent command the driver does not declare -- is preserved by
// GetFluentCommands, which only adds the commands the grammar asked for.
//
// # The Grant
//
// Blueprint and Grammar build strings and decide nothing: they take no
// auth.Grant, exactly as query.Builder takes none. Builder is the half that
// executes, and every method on it that reaches the database takes an
// auth.Grant and checks it against ActionMigrate before a statement is sent.
//
// DDL is the one place in this component where auth.Tenant(g) does not appear
// in the statement, and the reason is not an exception to RULE 17: a create
// table names a table, not rows, so there is no tenant column to filter on. The
// Grant is still mandatory, because the hole RULE 17 closes is a path that
// reaches the database with nobody having decided anything, and a migration
// runner is such a path. A pipeline gets its Grant from auth.SystemGrant, which
// refuses to issue one without a tenant:
//
//	g := auth.SystemGrant(schema.ActionMigrate, tenant)
//	if err := builder.Create(ctx, g, "users", func(t *schema.Blueprint) {
//	    t.ID("")
//	    t.String("email", 255).Unique()
//	    t.Timestamps()
//	}); err != nil { ... }
//
// # What was skipped, and why
//
//   - Macroable on Blueprint and Builder: __call, a PHP language interface Go
//     does not have.
//   - Fluent's offsetGet, offsetSet, jsonSerialize, toArray and __get: the same.
//     The attribute bag is a typed struct here, and Command spells out the union
//     of keys the way query.Where does for the query builder.
//   - SqlServerBuilder, SqlServerGrammar and the SQL Server halves of every
//     method: a driver this ecosystem does not carry. Three method names exist
//     only there and so exist nowhere here -- compileDefault,
//     compileDropAllForeignKeys and compileDropDefaultConstraint, all of them
//     about the named default constraints that are SQL Server's own way of
//     spelling a column default.
//   - The per-driver Builder subclasses (MySqlBuilder, PostgresBuilder,
//     SQLiteBuilder, MariaDbBuilder) are one Builder here, because what differs
//     between them is the SQL, and the SQL is the Grammar's. Their methods are
//     on Builder under the PHP's names; the one that could not be, because it
//     touches a file rather than the database, is RefreshDatabaseFile, which
//     checks the driver where PHP relies on the class.
//   - MariaDbSchemaState: MariaDB and MySQL share mysqldump, and the one place
//     the PHP's subclass differs -- omitting --set-gtid-purged -- is a branch
//     MySqlSchemaState already takes on Connection.IsMaria. A second type would
//     be a second way to dump the same database, which RULE 9 refuses.
package schema
