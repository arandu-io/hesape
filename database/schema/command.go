package schema

// Command is one entry of Illuminate\Database\Schema\Blueprint::$commands.
//
// PHP appends an untyped Illuminate\Support\Fluent per command, with a
// different key set for each Name. The union of those keys is spelled out here
// so the grammar can read them, exactly as query.Where does for the query
// builder's clauses.
//
// IndexDefinition and ForeignKeyDefinition are not separate values: in PHP they
// are the very Fluent that was appended, subclassed, so here they are typed
// handles over this struct. Whatever the caller chains onto them is what the
// grammar compiles.
type Command struct {
	// Name is the command, spelled as PHP spells it: create, add, change,
	// dropColumn, renameIndex, tableComment. The grammar is selected by it.
	Name string

	// ShouldBeSkipped answers $command->shouldBeSkipped. The MySQL grammar sets
	// it on the primary key command it folded into the create table statement.
	ShouldBeSkipped bool

	// Column is the column an add, change, comment or
	// autoIncrementStartingValues command carries.
	Column *ColumnDefinition

	// Columns is the column list of an index, foreign key or dropColumn
	// command. It is []any rather than []string because a raw index passes an
	// Expression, as rawIndex does.
	Columns []any

	// From and To carry rename, renameColumn and renameIndex.
	From string
	To   string

	// Index is the index name an index or drop index command works on.
	Index string

	// Algorithm is the index algorithm: using btree, using hash, using gist.
	Algorithm string

	// OperatorClass is the Postgres operator class of a spatial or vector index.
	OperatorClass string

	// Language is the Postgres full text search configuration.
	Language string

	// Comment is the table comment of a tableComment command.
	Comment string

	// References, On, OnDelete and OnUpdate are the foreign key.
	References []any
	On         any
	OnDelete   string
	OnUpdate   string

	// Deferrable, InitiallyImmediate, NotValid and NullsNotDistinct are the
	// Postgres constraint modifiers. They are pointers because the PHP
	// distinguishes "not set" from false: an unset deferrable emits nothing,
	// and a false one emits "not deferrable".
	Deferrable         *bool
	InitiallyImmediate *bool
	NotValid           *bool
	NullsNotDistinct   *bool

	// Online is Postgres' concurrent index build.
	Online bool

	// Lock and Instant are the MySQL DDL lock mode and the instant algorithm.
	Lock    string
	Instant bool

	// isColumnDefinition marks the placeholder a bare ColumnDefinition occupies
	// in the command list while the table is being altered. PHP appends the
	// ColumnDefinition itself and rewrites it into an add or change command in
	// addImpliedCommands; the marker is how the same rewrite finds it here.
	isColumnDefinition bool
}

// NewCommand answers Blueprint::createCommand.
func NewCommand(name string) *Command {
	return &Command{Name: name}
}
