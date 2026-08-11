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

// ConnectionEstablished answers Illuminate\Database\Events\ConnectionEstablished.
type ConnectionEstablished struct{ ConnectionEvent }

// NewConnectionEstablished answers `new ConnectionEstablished($connection)`.
func NewConnectionEstablished(connection Connection) *ConnectionEstablished {
	return &ConnectionEstablished{NewConnectionEvent(connection)}
}

// TransactionBeginning answers Illuminate\Database\Events\TransactionBeginning.
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

// TransactionCommitted answers Illuminate\Database\Events\TransactionCommitted.
type TransactionCommitted struct{ ConnectionEvent }

// NewTransactionCommitted answers `new TransactionCommitted($connection)`.
func NewTransactionCommitted(connection Connection) *TransactionCommitted {
	return &TransactionCommitted{NewConnectionEvent(connection)}
}

// TransactionRolledBack answers Illuminate\Database\Events\TransactionRolledBack.
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

// MigrationEvent answers Illuminate\Database\Events\MigrationEvent, the
// abstract base MigrationStarted and MigrationEnded extend.
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

// MigrationStarted answers Illuminate\Database\Events\MigrationStarted.
type MigrationStarted struct{ MigrationEvent }

// NewMigrationStarted answers `new MigrationStarted($migration, $method)`.
func NewMigrationStarted(migration Migration, method string) *MigrationStarted {
	return &MigrationStarted{NewMigrationEvent(migration, method)}
}

// MigrationEnded answers Illuminate\Database\Events\MigrationEnded.
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

// MigrationsEvent answers Illuminate\Database\Events\MigrationsEvent, the
// abstract base MigrationsStarted and MigrationsEnded extend.
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

// MigrationsStarted answers Illuminate\Database\Events\MigrationsStarted.
type MigrationsStarted struct{ MigrationsEvent }

// NewMigrationsStarted answers `new MigrationsStarted($method, $options)`.
func NewMigrationsStarted(method string, options map[string]any) *MigrationsStarted {
	return &MigrationsStarted{NewMigrationsEvent(method, options)}
}

// MigrationsEnded answers Illuminate\Database\Events\MigrationsEnded.
type MigrationsEnded struct{ MigrationsEvent }

// NewMigrationsEnded answers `new MigrationsEnded($method, $options)`.
func NewMigrationsEnded(method string, options map[string]any) *MigrationsEnded {
	return &MigrationsEnded{NewMigrationsEvent(method, options)}
}

// NoPendingMigrations answers Illuminate\Database\Events\NoPendingMigrations.
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

// SchemaDumped answers Illuminate\Database\Events\SchemaDumped.
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

// SchemaLoaded answers Illuminate\Database\Events\SchemaLoaded.
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

// ModelPruningStarting answers Illuminate\Database\Events\ModelPruningStarting.
type ModelPruningStarting struct {
	// Models is ModelPruningStarting::$models.
	Models []string
}

// NewModelPruningStarting answers ModelPruningStarting::__construct.
func NewModelPruningStarting(models []string) *ModelPruningStarting {
	return &ModelPruningStarting{Models: models}
}

// ModelPruningFinished answers Illuminate\Database\Events\ModelPruningFinished.
type ModelPruningFinished struct {
	// Models is ModelPruningFinished::$models.
	Models []string
}

// NewModelPruningFinished answers ModelPruningFinished::__construct.
func NewModelPruningFinished(models []string) *ModelPruningFinished {
	return &ModelPruningFinished{Models: models}
}
