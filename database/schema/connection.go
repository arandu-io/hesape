package schema

import "context"

// Record is one row as the connection hands it back: column name to value.
// It is the same alias query uses, for the same reason.
type Record = map[string]any

// Connection is what this component needs from Illuminate\Database\Connection.
//
// PHP passes the concrete Connection to Blueprint, Builder and every schema
// grammar. Here it is an interface declared in the package that consumes it,
// because the concrete connection lives in the parent package and a Go import
// the other way would close a cycle. The method set is only what the schema
// component actually calls on a connection -- nothing here is a general purpose
// database handle.
//
// The Process* methods answer Connection::getPostProcessor()->process*. They
// are on the connection rather than on a Processor value because the post
// processor is per driver, and this package must not know which driver it is
// talking to.
type Connection interface {
	// GetSchemaGrammar answers Connection::getSchemaGrammar.
	GetSchemaGrammar() Grammar

	// GetConfig answers Connection::getConfig. An option that was not set is
	// the empty string, which is the PHP null for every option this component
	// reads (charset, collation, engine, prefix_indexes, use_native_json,
	// use_native_jsonb).
	GetConfig(option string) string

	// GetTablePrefix answers Connection::getTablePrefix.
	GetTablePrefix() string

	// GetDriverName answers Connection::getDriverName: mysql, mariadb, pgsql or
	// sqlite. Blueprint reads it where the PHP asks whether the grammar is an
	// instance of a particular class, which Go cannot do through an interface.
	GetDriverName() string

	// GetServerVersion answers Connection::getServerVersion.
	GetServerVersion() string

	// IsMaria answers MySqlConnection::isMaria. It is false on every connection
	// that is not MySQL, as the MySQL grammar is the only caller.
	IsMaria() bool

	// ForeignKeyConstraintsEnabled reports whether the connection is currently
	// enforcing foreign keys.
	//
	// PHP reads it with `pragma foreign_keys` in the middle of compiling
	// SQLite's table rebuild, which has to switch them off and then put them
	// back exactly as it found them -- switching them on unconditionally would
	// start enforcing constraints a caller had deliberately suspended. The read
	// is on the connection here so that no pragma of one driver's is written
	// into a package that must not know which driver it is talking to.
	ForeignKeyConstraintsEnabled() bool

	// Statement answers Connection::statement: it runs a statement that returns
	// no rows.
	Statement(ctx context.Context, query string) error

	// Select answers Connection::selectFromWriteConnection. Schema reads always
	// go to the write connection in PHP, because a table created a moment ago
	// may not have reached a replica yet.
	Select(ctx context.Context, query string) ([]Record, error)

	// Scalar answers Connection::scalar: the first column of the first row.
	Scalar(ctx context.Context, query string) (any, error)

	// ProcessTables answers Processor::processTables.
	ProcessTables(rows []Record) []TableInfo

	// ProcessViews answers Processor::processViews.
	ProcessViews(rows []Record) []ViewInfo

	// ProcessColumns answers Processor::processColumns.
	ProcessColumns(rows []Record) []ColumnInfo

	// ProcessIndexes answers Processor::processIndexes.
	ProcessIndexes(rows []Record) []IndexInfo

	// ProcessForeignKeys answers Processor::processForeignKeys.
	ProcessForeignKeys(rows []Record) []ForeignKeyInfo

	// ProcessSchemas answers Processor::processSchemas.
	ProcessSchemas(rows []Record) []SchemaInfo
}

// SchemaInfo is one entry of Builder::getSchemas.
type SchemaInfo struct {
	Name    string
	Path    string
	Default bool
}

// TableInfo is one entry of Builder::getTables.
type TableInfo struct {
	Name                string
	Schema              string
	SchemaQualifiedName string
	Size                int64
	Comment             string
	Collation           string
	Engine              string
}

// ViewInfo is one entry of Builder::getViews.
type ViewInfo struct {
	Name                string
	Schema              string
	SchemaQualifiedName string
	Definition          string
}

// ColumnInfo is one entry of Builder::getColumns.
//
// TypeName is the PHP type_name -- the bare type, 'varchar' -- and Type is the
// full definition, 'varchar(255)'. GetColumnType returns one or the other, and
// which one is the whole point of its fullDefinition argument.
type ColumnInfo struct {
	Name          string
	Type          string
	TypeName      string
	Nullable      bool
	Default       any
	AutoIncrement bool
	Comment       string
	Collation     string
	Generation    *Generation
}

// Generation is the generation key of a ColumnInfo: how a computed column is
// computed, or nil when the column is not computed.
type Generation struct {
	Type       string
	Expression string
}

// IndexInfo is one entry of Builder::getIndexes.
type IndexInfo struct {
	Name    string
	Columns []string
	Type    string
	Unique  bool
	Primary bool
}

// ForeignKeyInfo is one entry of Builder::getForeignKeys.
type ForeignKeyInfo struct {
	Name           string
	Columns        []string
	ForeignSchema  string
	ForeignTable   string
	ForeignColumns []string
	OnUpdate       string
	OnDelete       string
}

// Model is what ForeignIDFor needs from Illuminate\Database\Eloquent\Model.
//
// PHP takes the model itself, or its class name and news it up. Go has no
// class name to instantiate, so the caller passes the model, and the interface
// names only the four methods Blueprint calls on it.
type Model interface {
	// GetTable answers Model::getTable.
	GetTable() string

	// GetForeignKey answers Model::getForeignKey.
	GetForeignKey() string

	// GetKeyName answers Model::getKeyName.
	GetKeyName() string

	// GetKeyType answers Model::getKeyType: "int" or "string".
	GetKeyType() string
}

// ULIDModel is how a model says it keys on ULIDs rather than UUIDs.
//
// PHP asks class_uses_recursive whether the model uses the HasUlids trait. Go
// has no traits, so the model answers for itself, and a model that does not
// implement this is treated as a UUID model exactly as one without the trait is
// there.
type ULIDModel interface {
	// UsesULIDs reports whether the model's key is a ULID.
	UsesULIDs() bool
}
