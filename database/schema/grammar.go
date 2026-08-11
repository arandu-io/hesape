package schema

// Grammar answers Illuminate\Database\Schema\Grammars\Grammar together with the
// base it extends, Illuminate\Database\Grammar.
//
// In PHP the schema grammar is an abstract class the driver grammars extend,
// and the blueprint holds one. Here it is an interface, declared in the package
// that consumes it, because the concrete grammars live in schema/grammars and a
// class in Go would close an import cycle: grammars imports schema for
// *Blueprint, so schema cannot import grammars for the type. It is the same
// arrangement query makes for its own grammar.
//
// Every command compiler has one signature, ([]string, error), where the PHP
// returns string|array|null and throws. A nil slice is the PHP null -- the
// command produced no statement, which is how SQLite says "handled on table
// creation". An error is what the PHP throws, and a grammar that does not
// support a command returns one rather than being absent, because a missing
// method that silently skips the command is how a migration ends up half
// applied with nothing said.
type Grammar interface {
	// CompileCreateDatabase answers Grammar::compileCreateDatabase.
	CompileCreateDatabase(name string) (string, error)

	// CompileDropDatabaseIfExists answers Grammar::compileDropDatabaseIfExists.
	CompileDropDatabaseIfExists(name string) (string, error)

	// CompileSchemas answers Grammar::compileSchemas.
	CompileSchemas() (string, error)

	// CompileTableExists answers Grammar::compileTableExists. An empty result is
	// the PHP null: the driver has no cheap existence query and Builder falls
	// back to listing the tables.
	CompileTableExists(schema, table string) string

	// CompileTables answers Grammar::compileTables. The optional withSize is
	// the second parameter SQLiteGrammar widens the base method with: table
	// sizes cost a join against dbstat there, so they are asked for rather than
	// always computed. The other two drivers report the size either way and
	// ignore it.
	CompileTables(schemas []string, withSize ...bool) (string, error)

	// CompileViews answers Grammar::compileViews.
	CompileViews(schemas []string) (string, error)

	// CompileTypes answers Grammar::compileTypes.
	CompileTypes(schemas []string) (string, error)

	// CompileColumns answers Grammar::compileColumns.
	CompileColumns(schema, table string) (string, error)

	// CompileIndexes answers Grammar::compileIndexes.
	CompileIndexes(schema, table string) (string, error)

	// CompileForeignKeys answers Grammar::compileForeignKeys.
	CompileForeignKeys(schema, table string) (string, error)

	// CompileCreate answers Grammar::compileCreate.
	CompileCreate(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileAdd answers Grammar::compileAdd.
	CompileAdd(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileChange answers Grammar::compileChange.
	CompileChange(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileAlter answers SQLiteGrammar::compileAlter, the table rebuild the
	// other two drivers never need. It is on the interface rather than on the
	// SQLite grammar alone because the blueprint dispatches by command name and
	// has no way to ask a Go interface whether a method exists.
	CompileAlter(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileRenameColumn answers Grammar::compileRenameColumn.
	CompileRenameColumn(blueprint *Blueprint, command *Command) ([]string, error)

	// CompilePrimary answers Grammar::compilePrimary.
	CompilePrimary(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileUnique answers Grammar::compileUnique.
	CompileUnique(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileIndex answers Grammar::compileIndex.
	CompileIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileFullText answers Grammar::compileFulltext.
	CompileFullText(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileSpatialIndex answers Grammar::compileSpatialIndex.
	CompileSpatialIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileVectorIndex answers Grammar::compileVectorIndex.
	CompileVectorIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileForeign answers Grammar::compileForeign.
	CompileForeign(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDrop answers Grammar::compileDrop.
	CompileDrop(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropIfExists answers Grammar::compileDropIfExists.
	CompileDropIfExists(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropColumn answers Grammar::compileDropColumn.
	CompileDropColumn(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropPrimary answers Grammar::compileDropPrimary.
	CompileDropPrimary(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropUnique answers Grammar::compileDropUnique.
	CompileDropUnique(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropIndex answers Grammar::compileDropIndex.
	CompileDropIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropFullText answers Grammar::compileDropFullText.
	CompileDropFullText(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropSpatialIndex answers Grammar::compileDropSpatialIndex.
	CompileDropSpatialIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropForeign answers Grammar::compileDropForeign.
	CompileDropForeign(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileRename answers Grammar::compileRename.
	CompileRename(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileRenameIndex answers Grammar::compileRenameIndex.
	CompileRenameIndex(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileTableComment answers Grammar::compileTableComment.
	CompileTableComment(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileComment answers PostgresGrammar::compileComment, the fluent command
	// that puts a column comment in a statement of its own.
	CompileComment(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileAutoIncrementStartingValues answers
	// Grammar::compileAutoIncrementStartingValues.
	CompileAutoIncrementStartingValues(blueprint *Blueprint, command *Command) ([]string, error)

	// CompileDropAllTables answers Grammar::compileDropAllTables.
	CompileDropAllTables(tables []string) (string, error)

	// CompileDropAllViews answers Grammar::compileDropAllViews.
	CompileDropAllViews(views []string) (string, error)

	// CompileDropAllTypes answers Grammar::compileDropAllTypes.
	CompileDropAllTypes(types []string) (string, error)

	// CompileEnableForeignKeyConstraints answers
	// Grammar::compileEnableForeignKeyConstraints.
	CompileEnableForeignKeyConstraints() (string, error)

	// CompileDisableForeignKeyConstraints answers
	// Grammar::compileDisableForeignKeyConstraints.
	CompileDisableForeignKeyConstraints() (string, error)

	// GetFluentCommands answers Grammar::getFluentCommands: the commands a
	// driver runs outside the create or alter statement, one per column.
	GetFluentCommands() []string

	// GetAlterCommands answers SQLiteGrammar::getAlterCommands: the commands
	// that force a table rebuild. Every other driver answers with none.
	GetAlterCommands() []string

	// SupportsSchemaTransactions answers Grammar::supportsSchemaTransactions.
	SupportsSchemaTransactions() bool

	// Wrap answers Grammar::wrap.
	Wrap(value any) string

	// WrapTable answers Grammar::wrapTable. The optional prefix replaces the
	// connection's table prefix, which is how the SQLite rebuild names its
	// temporary table.
	WrapTable(table any, prefix ...string) string
}

// compileCommand hands one command to the grammar.
//
// PHP builds the method name from the command ('compile'.ucfirst($name)) and
// asks method_exists, so a command no grammar declares is dropped in silence.
// Go cannot look a method up by name, so the dispatch is written out. An
// unknown command is an error rather than a silent skip: the PHP's silence is
// only safe because every command name in the file is also a method there.
func compileCommand(g Grammar, blueprint *Blueprint, command *Command) ([]string, error) {
	switch command.Name {
	case "create":
		return g.CompileCreate(blueprint, command)
	case "add":
		return g.CompileAdd(blueprint, command)
	case "change":
		return g.CompileChange(blueprint, command)
	case "alter":
		return g.CompileAlter(blueprint, command)
	case "renameColumn":
		return g.CompileRenameColumn(blueprint, command)
	case "primary":
		return g.CompilePrimary(blueprint, command)
	case "unique":
		return g.CompileUnique(blueprint, command)
	case "index":
		return g.CompileIndex(blueprint, command)
	case "fulltext":
		return g.CompileFullText(blueprint, command)
	case "spatialIndex":
		return g.CompileSpatialIndex(blueprint, command)
	case "vectorIndex":
		return g.CompileVectorIndex(blueprint, command)
	case "foreign":
		return g.CompileForeign(blueprint, command)
	case "drop":
		return g.CompileDrop(blueprint, command)
	case "dropIfExists":
		return g.CompileDropIfExists(blueprint, command)
	case "dropColumn":
		return g.CompileDropColumn(blueprint, command)
	case "dropPrimary":
		return g.CompileDropPrimary(blueprint, command)
	case "dropUnique":
		return g.CompileDropUnique(blueprint, command)
	case "dropIndex":
		return g.CompileDropIndex(blueprint, command)
	case "dropFullText":
		return g.CompileDropFullText(blueprint, command)
	case "dropSpatialIndex":
		return g.CompileDropSpatialIndex(blueprint, command)
	case "dropForeign":
		return g.CompileDropForeign(blueprint, command)
	case "rename":
		return g.CompileRename(blueprint, command)
	case "renameIndex":
		return g.CompileRenameIndex(blueprint, command)
	case "tableComment":
		return g.CompileTableComment(blueprint, command)
	case "comment":
		return g.CompileComment(blueprint, command)
	case "autoIncrementStartingValues":
		return g.CompileAutoIncrementStartingValues(blueprint, command)
	default:
		return nil, &UnknownCommandError{Name: command.Name}
	}
}

// UnknownCommandError is returned when a blueprint carries a command no grammar
// knows how to compile.
type UnknownCommandError struct{ Name string }

func (e *UnknownCommandError) Error() string {
	return "schema: no grammar compiles the blueprint command " + e.Name
}
