package events

// Connection is what an event needs of the connection it carries.
//
// It answers Illuminate\Database\Connection, narrowed to the one method every
// event in this package calls on it: the constructors all do
// `$this->connectionName = $connection->getName()`.
//
// It is declared here rather than imported from the database package because
// database dispatches these events, so naming the concrete type here would
// close an import cycle -- the same reason query.Connection is declared in the
// query package. PHP has no such constraint and needs no such interface.
type Connection interface {
	// GetName answers Connection::getName.
	GetName() string
}

// RawSQLConnection is what QueryExecuted.ToRawSQL asks of its connection.
//
// The PHP reaches through $this->connection->query()->getGrammar() to the
// grammar's substituteBindingsIntoRawSql. Three hops through concrete classes
// is a cycle here, so the destination is named directly.
type RawSQLConnection interface {
	Connection

	// PrepareBindings answers Connection::prepareBindings.
	PrepareBindings(bindings []any) []any

	// SubstituteBindingsIntoRawSQL answers the grammar method of the same name,
	// reached in the PHP through query()->getGrammar(). The PHP spells the last
	// word Sql.
	SubstituteBindingsIntoRawSQL(sql string, bindings []any) string
}

// ConnectionEvent answers Illuminate\Database\Events\ConnectionEvent.
//
// In PHP it is an abstract class the four connection events extend. Here it is
// a struct they embed, which is what "extends, for the part that is state"
// means in Go: the fields below are reachable on every one of them, spelled the
// same way.
type ConnectionEvent struct {
	// ConnectionName is ConnectionEvent::$connectionName.
	ConnectionName string

	// Connection is ConnectionEvent::$connection.
	Connection Connection
}

// NewConnectionEvent answers ConnectionEvent::__construct: it reads the name
// off the connection rather than taking it, exactly as the PHP does.
func NewConnectionEvent(connection Connection) ConnectionEvent {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return ConnectionEvent{Connection: connection, ConnectionName: name}
}

// ConnectionEstablished is dispatched by the DatabaseManager the first time a
// named connection is built, and again when one is reconnected.
//
// It fires once per connection rather than once per query, which makes it the
// place to configure a connection the moment it exists: set a session variable,
// register a query listener, record that the pool grew. A listener that runs
// per query wants QueryExecuted instead.
//
// Answers Illuminate\Database\Events\ConnectionEstablished.
type ConnectionEstablished struct{ ConnectionEvent }

// NewConnectionEstablished answers `new ConnectionEstablished($connection)`.
func NewConnectionEstablished(connection Connection) *ConnectionEstablished {
	return &ConnectionEstablished{NewConnectionEvent(connection)}
}

// TransactionBeginning is dispatched after a transaction level opens on a
// connection.
//
// Every level fires it, not only the outermost: a nested transaction is a
// savepoint, and the savepoint counts. A listener that only wants the real
// transaction has to read the connection's level itself, because the event
// carries nothing but the connection. It fires after the statement succeeded,
// so a listener seeing it knows the transaction is open.
//
// Answers Illuminate\Database\Events\TransactionBeginning.
type TransactionBeginning struct{ ConnectionEvent }

// NewTransactionBeginning answers `new TransactionBeginning($connection)`.
func NewTransactionBeginning(connection Connection) *TransactionBeginning {
	return &TransactionBeginning{NewConnectionEvent(connection)}
}

// TransactionCommitting answers Illuminate\Database\Events\TransactionCommitting.
//
// It fires before the commit reaches the server, which is the only moment a
// listener can still refuse one.
type TransactionCommitting struct{ ConnectionEvent }

// NewTransactionCommitting answers `new TransactionCommitting($connection)`.
func NewTransactionCommitting(connection Connection) *TransactionCommitting {
	return &TransactionCommitting{NewConnectionEvent(connection)}
}

// TransactionCommitted is dispatched after a transaction level closes
// successfully.
//
// Every level fires it, so releasing a savepoint fires it as well as the
// outermost commit. Only the outermost one has actually reached the server --
// TransactionCommitting is the event that fires exactly once for that, just
// before it does. Work that must not happen unless the data is durable belongs
// after the outermost commit, which means this event alone is not enough to
// decide it.
//
// Answers Illuminate\Database\Events\TransactionCommitted.
type TransactionCommitted struct{ ConnectionEvent }

// NewTransactionCommitted answers `new TransactionCommitted($connection)`.
func NewTransactionCommitted(connection Connection) *TransactionCommitted {
	return &TransactionCommitted{NewConnectionEvent(connection)}
}

// TransactionRolledBack is dispatched after a rollback has been carried out on
// a connection.
//
// A rollback to a savepoint fires it too, so it does not on its own mean the
// whole transaction was abandoned -- only that the connection is back at some
// earlier level. It is the signal for undoing what was done outside the
// database on the strength of a write that is now gone: a cache entry primed
// early, a counter held in memory.
//
// Answers Illuminate\Database\Events\TransactionRolledBack.
type TransactionRolledBack struct{ ConnectionEvent }

// NewTransactionRolledBack answers `new TransactionRolledBack($connection)`.
func NewTransactionRolledBack(connection Connection) *TransactionRolledBack {
	return &TransactionRolledBack{NewConnectionEvent(connection)}
}

// QueryExecuted answers Illuminate\Database\Events\QueryExecuted.
//
// Time is the duration in milliseconds, which is what the PHP puts there
// (Connection::getElapsedTime rounds to two decimals). It is a float rather
// than a time.Duration because the field is read by listeners that compare it
// against a threshold in milliseconds, and changing the unit would make every
// one of those comparisons silently wrong.
type QueryExecuted struct {
	// SQL is QueryExecuted::$sql. The PHP spells it $sql.
	SQL string

	// Bindings is QueryExecuted::$bindings.
	Bindings []any

	// Time is QueryExecuted::$time, in milliseconds.
	Time float64

	// Connection is QueryExecuted::$connection.
	Connection RawSQLConnection

	// ConnectionName is QueryExecuted::$connectionName.
	ConnectionName string

	// ReadWriteType is QueryExecuted::$readWriteType: "read", "write", or
	// empty where the PHP has null.
	ReadWriteType string
}

// NewQueryExecuted answers QueryExecuted::__construct.
func NewQueryExecuted(sql string, bindings []any, time float64, connection RawSQLConnection, readWriteType string) *QueryExecuted {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return &QueryExecuted{
		SQL:            sql,
		Bindings:       bindings,
		Time:           time,
		Connection:     connection,
		ConnectionName: name,
		ReadWriteType:  readWriteType,
	}
}

// ToRawSQL answers QueryExecuted::toRawSql: the query with its bindings written
// into it, for a log a person reads rather than one a machine parses.
//
// The PHP spells it toRawSql; SQL is an initialism and goes up. A nil
// connection answers the query unchanged rather than panicking, because an
// event constructed in a test has nowhere to ask.
func (e *QueryExecuted) ToRawSQL() string {
	if e.Connection == nil {
		return e.SQL
	}
	return e.Connection.SubstituteBindingsIntoRawSQL(e.SQL, e.Connection.PrepareBindings(e.Bindings))
}

// StatementPrepared answers Illuminate\Database\Events\StatementPrepared.
//
// Statement is the prepared statement the driver handed back. In PHP it is a
// PDOStatement; here it is any, because database/sql hands back a *sql.Stmt and
// naming it would put the standard library's driver types into an event package
// that has no other reason to know about them.
type StatementPrepared struct {
	// Connection is StatementPrepared::$connection.
	Connection Connection

	// Statement is StatementPrepared::$statement.
	Statement any
}

// NewStatementPrepared answers StatementPrepared::__construct.
func NewStatementPrepared(connection Connection, statement any) *StatementPrepared {
	return &StatementPrepared{Connection: connection, Statement: statement}
}

// DatabaseBusy answers Illuminate\Database\Events\DatabaseBusy: the event
// `db:monitor` dispatches when a connection is over its threshold.
type DatabaseBusy struct {
	// ConnectionName is DatabaseBusy::$connectionName.
	ConnectionName string

	// Connections is DatabaseBusy::$connections: how many are open.
	Connections int
}

// NewDatabaseBusy answers DatabaseBusy::__construct.
func NewDatabaseBusy(connectionName string, connections int) *DatabaseBusy {
	return &DatabaseBusy{ConnectionName: connectionName, Connections: connections}
}

// DatabaseRefreshed answers Illuminate\Database\Events\DatabaseRefreshed: what
// `migrate:fresh` and `migrate:refresh` dispatch once the schema is back.
type DatabaseRefreshed struct {
	// Database is DatabaseRefreshed::$database, empty where the PHP has null.
	Database string

	// Seeding is DatabaseRefreshed::$seeding.
	Seeding bool
}

// NewDatabaseRefreshed answers DatabaseRefreshed::__construct.
func NewDatabaseRefreshed(database string, seeding bool) *DatabaseRefreshed {
	return &DatabaseRefreshed{Database: database, Seeding: seeding}
}

// Migration is what a migration event carries.
//
// It answers Illuminate\Database\Migrations\Migration, narrowed to what the
// events read. The migrations package cannot be imported here: the Migrator
// dispatches these, so it imports this package.
type Migration interface {
	// GetConnection answers Migration::getConnection.
	GetConnection() string
}

// MigrationEvent is the pair of fields every single-migration event carries:
// which migration, and which direction it is being run in.
//
// It is embedded by MigrationStarted and MigrationEnded rather than inherited,
// so both read the same two fields under the same names. Nothing dispatches
// this on its own -- it is only ever the embedded half of one of those.
//
// Answers Illuminate\Database\Events\MigrationEvent, the abstract base
// MigrationStarted and MigrationEnded extend.
type MigrationEvent struct {
	// Migration is MigrationEvent::$migration.
	Migration Migration

	// Method is MigrationEvent::$method: "up" or "down".
	Method string
}

// NewMigrationEvent answers MigrationEvent::__construct.
func NewMigrationEvent(migration Migration, method string) MigrationEvent {
	return MigrationEvent{Migration: migration, Method: method}
}

// MigrationStarted is dispatched by the Migrator immediately before one
// migration's Up or Down is called.
//
// It fires inside the transaction the migration runs in, where the engine has
// transactional DDL, so a listener writing to the same connection is writing
// inside that transaction. A migration skipped by ShouldRun does not fire it;
// MigrationSkipped does.
//
// Answers Illuminate\Database\Events\MigrationStarted.
type MigrationStarted struct{ MigrationEvent }

// NewMigrationStarted answers `new MigrationStarted($migration, $method)`.
func NewMigrationStarted(migration Migration, method string) *MigrationStarted {
	return &MigrationStarted{NewMigrationEvent(migration, method)}
}

// MigrationEnded is dispatched after one migration's Up or Down returned
// without an error.
//
// A migration that failed does not fire it -- the error stops the run before
// this point -- so seeing MigrationStarted with no MigrationEnded for the same
// name is exactly the signature of a migration that broke. It fires before the
// transaction commits, so the schema change is not durable yet.
//
// Answers Illuminate\Database\Events\MigrationEnded.
type MigrationEnded struct{ MigrationEvent }

// NewMigrationEnded answers `new MigrationEnded($migration, $method)`.
func NewMigrationEnded(migration Migration, method string) *MigrationEnded {
	return &MigrationEnded{NewMigrationEvent(migration, method)}
}

// MigrationSkipped answers Illuminate\Database\Events\MigrationSkipped: what
// the Migrator dispatches when a migration's ShouldRun answers false.
type MigrationSkipped struct {
	// MigrationName is MigrationSkipped::$migrationName.
	MigrationName string
}

// NewMigrationSkipped answers MigrationSkipped::__construct.
func NewMigrationSkipped(migrationName string) *MigrationSkipped {
	return &MigrationSkipped{MigrationName: migrationName}
}

// MigrationsEvent is the pair of fields the whole-run events carry: the
// direction, and the options the run was given.
//
// It is embedded by MigrationsStarted and MigrationsEnded, which bracket a
// whole `aru migrate` or `aru migrate:rollback` -- the plural to
// MigrationEvent's singular. Nothing dispatches this on its own.
//
// Answers Illuminate\Database\Events\MigrationsEvent, the abstract base
// MigrationsStarted and MigrationsEnded extend.
type MigrationsEvent struct {
	// Method is MigrationsEvent::$method: "up" or "down".
	Method string

	// Options is MigrationsEvent::$options, the options the run was given.
	Options map[string]any
}

// NewMigrationsEvent answers MigrationsEvent::__construct.
func NewMigrationsEvent(method string, options map[string]any) MigrationsEvent {
	return MigrationsEvent{Method: method, Options: options}
}

// MigrationsStarted is dispatched once at the top of a run that has something
// to do, before the first migration is touched.
//
// A run with nothing pending fires NoPendingMigrations instead and never fires
// this, so it means "the schema is about to change". It is the hook for the
// things that bracket a whole deploy step: put the application into
// maintenance, open a span, take a note of the time.
//
// Answers Illuminate\Database\Events\MigrationsStarted.
type MigrationsStarted struct{ MigrationsEvent }

// NewMigrationsStarted answers `new MigrationsStarted($method, $options)`.
func NewMigrationsStarted(method string, options map[string]any) *MigrationsStarted {
	return &MigrationsStarted{NewMigrationsEvent(method, options)}
}

// MigrationsEnded is dispatched once at the end of a run in which every
// migration succeeded.
//
// The run stops at the first failure, so a failed run fires MigrationsStarted
// and never this: the pair is the signal that the schema reached the state the
// binary expects. It is where the bracket MigrationsStarted opened is closed.
//
// Answers Illuminate\Database\Events\MigrationsEnded.
type MigrationsEnded struct{ MigrationsEvent }

// NewMigrationsEnded answers `new MigrationsEnded($method, $options)`.
func NewMigrationsEnded(method string, options map[string]any) *MigrationsEnded {
	return &MigrationsEnded{NewMigrationsEvent(method, options)}
}

// NoPendingMigrations is dispatched when a run found nothing to do: `aru
// migrate` with every migration already applied, or `aru migrate:rollback` with
// nothing left to undo.
//
// It takes the place of the Started/Ended pair rather than joining it, so a
// listener that brackets a run has to treat this as the third possible shape.
// It is not a failure -- a deploy that changes no schema is the normal case.
//
// Answers Illuminate\Database\Events\NoPendingMigrations.
type NoPendingMigrations struct {
	// Method is NoPendingMigrations::$method: "up" or "down".
	Method string
}

// NewNoPendingMigrations answers NoPendingMigrations::__construct.
func NewNoPendingMigrations(method string) *NoPendingMigrations {
	return &NoPendingMigrations{Method: method}
}

// MigrationsPruned answers Illuminate\Database\Events\MigrationsPruned: the
// squashed migration files were deleted after a schema dump.
type MigrationsPruned struct {
	// Connection is MigrationsPruned::$connection.
	Connection Connection

	// ConnectionName is MigrationsPruned::$connectionName.
	ConnectionName string

	// Path is MigrationsPruned::$path.
	Path string
}

// NewMigrationsPruned answers MigrationsPruned::__construct.
func NewMigrationsPruned(connection Connection, path string) *MigrationsPruned {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return &MigrationsPruned{Connection: connection, ConnectionName: name, Path: path}
}

// SchemaDumped reports that a connection's whole schema was written to a file,
// and where.
//
// The dump is what replaces years of migration files: a single SQL file the
// engine's own tool produced, loaded in one step by a fresh database instead of
// replayed migration by migration. Nothing in this package dispatches the
// event -- SchemaState.Dump does the work and the command that drives it lives
// in the application -- so it exists so that a project wiring `schema:dump`
// fires the event a Laravel developer already listens for, and so that a
// listener can add the file to a release artifact.
//
// Answers Illuminate\Database\Events\SchemaDumped.
type SchemaDumped struct {
	// Connection is SchemaDumped::$connection.
	Connection Connection

	// ConnectionName is SchemaDumped::$connectionName.
	ConnectionName string

	// Path is SchemaDumped::$path.
	Path string
}

// NewSchemaDumped answers SchemaDumped::__construct.
func NewSchemaDumped(connection Connection, path string) *SchemaDumped {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return &SchemaDumped{Connection: connection, ConnectionName: name, Path: path}
}

// SchemaLoaded reports that a schema file was run against a connection, and
// which file.
//
// It is the other half of SchemaDumped: the migrator loads the dump into an
// empty database and then applies only the migrations written since. Nothing
// here dispatches it, for the same reason -- SchemaState.Load does the work,
// and the event is the vocabulary an application uses when it wires the
// command.
//
// Answers Illuminate\Database\Events\SchemaLoaded.
type SchemaLoaded struct {
	// Connection is SchemaLoaded::$connection.
	Connection Connection

	// ConnectionName is SchemaLoaded::$connectionName.
	ConnectionName string

	// Path is SchemaLoaded::$path.
	Path string
}

// NewSchemaLoaded answers SchemaLoaded::__construct.
func NewSchemaLoaded(connection Connection, path string) *SchemaLoaded {
	name := ""
	if connection != nil {
		name = connection.GetName()
	}
	return &SchemaLoaded{Connection: connection, ConnectionName: name, Path: path}
}

// ModelsPruned answers Illuminate\Database\Events\ModelsPruned.
//
// Eloquent has no counterpart in this framework and never will, so nothing here
// dispatches this. It exists because `model:prune` is a command a project may
// write against its own repositories, and the event it fires should be the one
// a Laravel developer already listens for.
type ModelsPruned struct {
	// Model is ModelsPruned::$model: the type whose rows were pruned.
	Model string

	// Count is ModelsPruned::$count.
	Count int
}

// NewModelsPruned answers ModelsPruned::__construct.
func NewModelsPruned(model string, count int) *ModelsPruned {
	return &ModelsPruned{Model: model, Count: count}
}

// ModelPruningStarting is dispatched by `aru model:prune` before it prunes
// anything, carrying the names of everything it is about to prune.
//
// It fires once for the whole command, in sorted order, and only when there is
// at least one registered prunable. Because the list is complete before the
// first deletion, a listener can log or refuse on the whole set rather than
// discovering it one ModelsPruned at a time. --pretend still fires it: the
// names are what pretending is for.
//
// Answers Illuminate\Database\Events\ModelPruningStarting.
type ModelPruningStarting struct {
	// Models is ModelPruningStarting::$models.
	Models []string
}

// NewModelPruningStarting answers ModelPruningStarting::__construct.
func NewModelPruningStarting(models []string) *ModelPruningStarting {
	return &ModelPruningStarting{Models: models}
}

// ModelPruningFinished is dispatched by `aru model:prune` once every registered
// prunable has run, carrying the same list ModelPruningStarting carried.
//
// The command stops at the first prunable that errors, so this not arriving
// after a ModelPruningStarting means one of them failed. How many rows each one
// removed is in the ModelsPruned events between the two, not here.
//
// Answers Illuminate\Database\Events\ModelPruningFinished.
type ModelPruningFinished struct {
	// Models is ModelPruningFinished::$models.
	Models []string
}

// NewModelPruningFinished answers ModelPruningFinished::__construct.
func NewModelPruningFinished(models []string) *ModelPruningFinished {
	return &ModelPruningFinished{Models: models}
}
