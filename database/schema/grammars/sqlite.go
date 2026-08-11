package grammars

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/schema"
)

// SQLiteGrammar answers Illuminate\Database\Schema\Grammars\SQLiteGrammar.
//
// SQLite cannot alter most of a table, so several commands compile to nothing
// here and are carried out by the table rebuild CompileAlter emits instead.
type SQLiteGrammar struct{ *BaseGrammar }

// NewSQLiteGrammar answers `new SQLiteGrammar($connection)`.
func NewSQLiteGrammar(connection schema.Connection) *SQLiteGrammar {
	g := &SQLiteGrammar{&BaseGrammar{
		conn:      connection,
		modifiers: []string{"Increment", "Nullable", "Default", "Collate", "VirtualAs", "StoredAs"},
		serials:   []string{"bigInteger", "integer", "mediumInteger", "smallInteger", "tinyInteger"},
	}}
	g.self = g
	return g
}

// GetAlterCommands answers SQLiteGrammar::getAlterCommands: the commands SQLite
// cannot express as an alter statement, and which therefore force the table to
// be rebuilt.
func (g *SQLiteGrammar) GetAlterCommands() []string {
	commands := []string{"change", "primary", "dropPrimary", "foreign", "dropForeign"}
	if !atLeast(g.conn.GetServerVersion(), "3.35") {
		commands = append(commands, "dropColumn")
	}
	return commands
}

// CompileSQLCreateStatement answers SQLiteGrammar::compileSqlCreateStatement.
func (g *SQLiteGrammar) CompileSQLCreateStatement(schemaName, name string, typ ...string) string {
	kind := "table"
	if len(typ) > 0 && typ[0] != "" {
		kind = typ[0]
	}
	return fmt.Sprintf(`select "sql" from %s.sqlite_master where type = %s and name = %s`,
		g.wrapValue(defaultSchema(schemaName)), g.QuoteString(kind), g.QuoteString(name))
}

// CompileTableExists answers SQLiteGrammar::compileTableExists.
func (g *SQLiteGrammar) CompileTableExists(schemaName, table string) string {
	return fmt.Sprintf(`select exists (select 1 from %s.sqlite_master where name = %s and type = 'table') as "exists"`,
		g.wrapValue(defaultSchema(schemaName)), g.QuoteString(table))
}

// CompileTables answers SQLiteGrammar::compileTables. Asking for the size joins
// against dbstat, which is a virtual table not every build of SQLite carries --
// CompileDbstatExists is how the caller finds out before asking.
func (g *SQLiteGrammar) CompileTables(schemas []string, withSize ...bool) (string, error) {
	where := ""
	switch {
	case len(schemas) > 1:
		where = " tl.schema in (" + g.QuoteString(schemas) + ") and"
	case len(schemas) == 1:
		where = " tl.schema = " + g.QuoteString(schemas[0]) + " and"
	}

	size := ""
	if len(withSize) > 0 && withSize[0] {
		size = ", (select sum(s.pgsize) " +
			"from (select tl.name as name union select il.name as name from pragma_index_list(tl.name, tl.schema) as il) as es " +
			"join dbstat(tl.schema) as s on s.name = es.name) as size"
	}

	return "select tl.name as name, tl.schema as schema" + size +
		" from pragma_table_list as tl where" + where +
		` tl.type in ('table', 'virtual') and tl.name not like 'sqlite\_%' escape '\' ` +
		"order by tl.schema, tl.name", nil
}

// CompileDbstatExists answers SQLiteGrammar::compileDbstatExists: whether this
// build of SQLite has the dbstat virtual table that table sizes come from.
func (g *SQLiteGrammar) CompileDbstatExists() string {
	return "select exists (select 1 from pragma_compile_options where compile_options = 'ENABLE_DBSTAT_VTAB') as enabled"
}

// CompileLegacyTables answers SQLiteGrammar::compileLegacyTables, for a server
// old enough to lack pragma_table_list.
func (g *SQLiteGrammar) CompileLegacyTables(schemaName string, withSize ...bool) string {
	name := defaultSchema(schemaName)
	if len(withSize) > 0 && withSize[0] {
		return fmt.Sprintf(
			"select m.tbl_name as name, %s as schema, sum(s.pgsize) as size from %s.sqlite_master as m "+
				"join dbstat(%s) as s on s.name = m.name "+
				`where m.type in ('table', 'index') and m.tbl_name not like 'sqlite\_%%' escape '\' `+
				"group by m.tbl_name order by m.tbl_name",
			g.QuoteString(name), g.wrapValue(name), g.QuoteString(name))
	}
	return fmt.Sprintf(
		"select name, %s as schema from %s.sqlite_master "+
			`where type = 'table' and name not like 'sqlite\_%%' escape '\' order by name`,
		g.QuoteString(name), g.wrapValue(name))
}

// CompileRebuild answers SQLiteGrammar::compileRebuild.
func (g *SQLiteGrammar) CompileRebuild(schemaName ...string) string {
	return "vacuum " + g.wrapValue(defaultSchema(firstOr(schemaName, "main")))
}

// CompileViews answers SQLiteGrammar::compileViews.
func (g *SQLiteGrammar) CompileViews(schemas []string) (string, error) {
	where := ""
	switch {
	case len(schemas) > 1:
		where = "schema in (" + g.QuoteString(schemas) + ") and "
	case len(schemas) == 1:
		where = "schema = " + g.QuoteString(schemas[0]) + " and "
	}
	return "select name, schema, sql as definition from pragma_table_list where " + where +
		"type = 'view' order by schema, name", nil
}

// CompileColumns answers SQLiteGrammar::compileColumns.
func (g *SQLiteGrammar) CompileColumns(schemaName, table string) (string, error) {
	return fmt.Sprintf(
		`select name, type, not "notnull" as "nullable", dflt_value as "default", pk as "primary", hidden as "extra" `+
			"from pragma_table_xinfo(%s, %s) order by cid asc",
		g.QuoteString(table), g.QuoteString(defaultSchema(schemaName))), nil
}

// CompileIndexes answers SQLiteGrammar::compileIndexes.
func (g *SQLiteGrammar) CompileIndexes(schemaName, table string) (string, error) {
	quotedTable := g.QuoteString(table)
	quotedSchema := g.QuoteString(defaultSchema(schemaName))
	return fmt.Sprintf(
		`select 'primary' as name, group_concat(col) as columns, 1 as "unique", 1 as "primary" `+
			"from (select name as col from pragma_table_xinfo(%s, %s) where pk > 0 order by pk, cid) group by name "+
			`union select name, group_concat(col) as columns, "unique", origin = 'pk' as "primary" `+
			"from (select il.*, ii.name as col from pragma_index_list(%s, %s) il, pragma_index_info(il.name, %s) ii order by il.seq, ii.seqno) "+
			`group by name, "unique", "primary"`,
		quotedTable, quotedSchema, quotedTable, quotedSchema, quotedSchema), nil
}

// CompileForeignKeys answers SQLiteGrammar::compileForeignKeys.
func (g *SQLiteGrammar) CompileForeignKeys(schemaName, table string) (string, error) {
	quotedSchema := g.QuoteString(defaultSchema(schemaName))
	return fmt.Sprintf(
		`select group_concat("from") as columns, %s as foreign_schema, "table" as foreign_table, `+
			`group_concat("to") as foreign_columns, on_update, on_delete `+
			"from (select * from pragma_foreign_key_list(%s, %s) order by id desc, seq) "+
			`group by id, "table", on_update, on_delete`,
		quotedSchema, g.QuoteString(table), quotedSchema), nil
}

// CompileCreate answers SQLiteGrammar::compileCreate. SQLite takes its foreign
// keys and its primary key inside the create statement, because it cannot add
// either afterwards.
func (g *SQLiteGrammar) CompileCreate(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	columns, err := g.getColumns(blueprint)
	if err != nil {
		return nil, err
	}
	create := "create"
	if blueprint.GetTemporary() {
		create = "create temporary"
	}
	return one(fmt.Sprintf("%s table %s (%s%s%s)",
		create,
		g.WrapTable(blueprint),
		strings.Join(columns, ", "),
		g.addForeignKeys(g.getCommandsByName(blueprint, "foreign")),
		g.addPrimaryKeys(g.getCommandByName(blueprint, "primary")))), nil
}

// addForeignKeys answers SQLiteGrammar::addForeignKeys.
func (g *SQLiteGrammar) addForeignKeys(foreignKeys []*schema.Command) string {
	sql := ""
	for _, foreign := range foreignKeys {
		sql += g.getForeignKey(foreign)
	}
	return sql
}

// getForeignKey answers SQLiteGrammar::getForeignKey.
func (g *SQLiteGrammar) getForeignKey(foreign *schema.Command) string {
	sql := fmt.Sprintf(", foreign key(%s) references %s(%s)",
		g.Columnize(foreign.Columns),
		g.WrapTable(foreign.On),
		g.Columnize(foreign.References))

	if foreign.OnDelete != "" {
		sql += " on delete " + foreign.OnDelete
	}
	if foreign.OnUpdate != "" {
		sql += " on update " + foreign.OnUpdate
	}
	return sql
}

// addPrimaryKeys answers SQLiteGrammar::addPrimaryKeys.
func (g *SQLiteGrammar) addPrimaryKeys(primary *schema.Command) string {
	if primary == nil {
		return ""
	}
	return ", primary key (" + g.Columnize(primary.Columns) + ")"
}

// CompileAdd answers SQLiteGrammar::compileAdd.
func (g *SQLiteGrammar) CompileAdd(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	definition, err := g.getColumn(blueprint, command.Column)
	if err != nil {
		return nil, err
	}
	return one("alter table " + g.WrapTable(blueprint) + " add column " + definition), nil
}

// CompilePrimary answers SQLiteGrammar::compilePrimary: handled on table
// creation or alteration.
func (g *SQLiteGrammar) CompilePrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileChange answers SQLiteGrammar::compileChange: handled on table
// alteration.
func (g *SQLiteGrammar) CompileChange(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileForeign answers SQLiteGrammar::compileForeign: handled on table
// creation or alteration.
func (g *SQLiteGrammar) CompileForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileUnique answers SQLiteGrammar::compileUnique.
func (g *SQLiteGrammar) CompileUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	schemaName, table, err := schema.ParseSchemaAndTable(blueprint.GetTable())
	if err != nil {
		return nil, err
	}
	return one(fmt.Sprintf("create unique index %s%s on %s (%s)",
		g.schemaPrefix(schemaName), g.Wrap(command.Index), g.WrapTable(table), g.Columnize(command.Columns))), nil
}

// CompileIndex answers SQLiteGrammar::compileIndex.
func (g *SQLiteGrammar) CompileIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	schemaName, table, err := schema.ParseSchemaAndTable(blueprint.GetTable())
	if err != nil {
		return nil, err
	}
	return one(fmt.Sprintf("create index %s%s on %s (%s)",
		g.schemaPrefix(schemaName), g.Wrap(command.Index), g.WrapTable(table), g.Columnize(command.Columns))), nil
}

// CompileDrop answers SQLiteGrammar::compileDrop.
func (g *SQLiteGrammar) CompileDrop(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table " + g.WrapTable(blueprint)), nil
}

// CompileDropIfExists answers SQLiteGrammar::compileDropIfExists.
func (g *SQLiteGrammar) CompileDropIfExists(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table if exists " + g.WrapTable(blueprint)), nil
}

// CompileDropAllTables answers SQLiteGrammar::compileDropAllTables. The PHP
// takes a schema name where every other driver takes the tables, because SQLite
// clears the catalogue rather than naming them.
func (g *SQLiteGrammar) CompileDropAllTables(schemas []string) (string, error) {
	return fmt.Sprintf("delete from %s.sqlite_master where type in ('table', 'index', 'trigger')",
		g.wrapValue(firstOr(schemas, "main"))), nil
}

// CompileDropAllViews answers SQLiteGrammar::compileDropAllViews.
func (g *SQLiteGrammar) CompileDropAllViews(schemas []string) (string, error) {
	return fmt.Sprintf("delete from %s.sqlite_master where type in ('view')",
		g.wrapValue(firstOr(schemas, "main"))), nil
}

// CompileDropColumn answers SQLiteGrammar::compileDropColumn. Before SQLite
// 3.35 there is no "drop column", and the rebuild does it instead.
func (g *SQLiteGrammar) CompileDropColumn(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	if !atLeast(g.conn.GetServerVersion(), "3.35") {
		return nil, nil
	}
	table := g.WrapTable(blueprint)
	var statements []string
	for _, column := range g.PrefixArray("drop column", g.WrapArray(command.Columns)) {
		statements = append(statements, "alter table "+table+" "+column)
	}
	return statements, nil
}

// CompileDropPrimary answers SQLiteGrammar::compileDropPrimary: handled on
// table alteration.
func (g *SQLiteGrammar) CompileDropPrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileDropUnique answers SQLiteGrammar::compileDropUnique.
func (g *SQLiteGrammar) CompileDropUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileDropIndex answers SQLiteGrammar::compileDropIndex.
func (g *SQLiteGrammar) CompileDropIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	schemaName, _, err := schema.ParseSchemaAndTable(blueprint.GetTable())
	if err != nil {
		return nil, err
	}
	return one("drop index " + g.schemaPrefix(schemaName) + g.Wrap(command.Index)), nil
}

// CompileDropForeign answers SQLiteGrammar::compileDropForeign: handled on
// table alteration, and refused when the caller named the key rather than its
// columns, because SQLite's foreign keys have no names to drop by.
func (g *SQLiteGrammar) CompileDropForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	if len(command.Columns) == 0 {
		return nil, unsupported("dropping foreign keys by name")
	}
	return nil, nil
}

// CompileRename answers SQLiteGrammar::compileRename.
func (g *SQLiteGrammar) CompileRename(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " rename to " + g.WrapTable(command.To)), nil
}

// CompileRenameIndex answers SQLiteGrammar::compileRenameIndex.
//
// SQLite has no rename for an index: the PHP reads the index back off the
// server, drops it and creates it again under the new name. That read happens
// in the middle of compiling, which this grammar does without a connection, so
// the operation is refused with the reason instead of emitting a statement
// SQLite would reject.
func (g *SQLiteGrammar) CompileRenameIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, fmt.Errorf("%w: renaming an index on SQLite means dropping and recreating it, which needs the index read back from the server; drop and create it in the migration instead", schema.ErrUnsupported)
}

// CompileEnableForeignKeyConstraints answers
// SQLiteGrammar::compileEnableForeignKeyConstraints.
func (g *SQLiteGrammar) CompileEnableForeignKeyConstraints() (string, error) {
	return g.Pragma("foreign_keys", "1"), nil
}

// CompileDisableForeignKeyConstraints answers
// SQLiteGrammar::compileDisableForeignKeyConstraints.
func (g *SQLiteGrammar) CompileDisableForeignKeyConstraints() (string, error) {
	return g.Pragma("foreign_keys", "0"), nil
}

// Pragma answers SQLiteGrammar::pragma. An empty value reads the pragma rather
// than setting it, which is the PHP's null.
func (g *SQLiteGrammar) Pragma(key, value string) string {
	if value == "" {
		return "pragma " + key
	}
	return "pragma " + key + " = " + value
}

// CompileAlter answers SQLiteGrammar::compileAlter: the table rebuild.
//
// SQLite can add a column and little else, so anything further is done by
// building the table the blueprint describes under a temporary name, copying
// the rows across, dropping the original and renaming. The shape being built
// comes from the BlueprintState, which read the table before the first command
// was compiled and has been kept current since.
func (g *SQLiteGrammar) CompileAlter(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	state := blueprint.GetState()
	if state == nil {
		return nil, nil
	}

	var columnNames []string
	autoIncrementColumn := ""
	var columns []string

	for _, column := range state.GetColumns() {
		if column.GetAutoIncrement() {
			autoIncrementColumn = column.GetName()
		}
		if !isGenerated(column) {
			columnNames = append(columnNames, g.Wrap(column))
		}

		definition := column.GetFullTypeDefinition()
		if definition == "" {
			typ, err := g.typeOf(column)
			if err != nil {
				return nil, err
			}
			definition = typ
		}
		modified, err := g.addModifiers(g.Wrap(column)+" "+definition, blueprint, column)
		if err != nil {
			return nil, err
		}
		columns = append(columns, modified)
	}

	var indexes []string
	for _, index := range state.GetIndexes() {
		var statements []string
		var err error
		switch index.GetCommand().Name {
		case "unique":
			statements, err = g.CompileUnique(blueprint, index.GetCommand())
		default:
			statements, err = g.CompileIndex(blueprint, index.GetCommand())
		}
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, statements...)
	}

	_, tableName, err := schema.ParseSchemaAndTable(blueprint.GetTable())
	if err != nil {
		return nil, err
	}

	tempTable := g.WrapTable(blueprint, "__temp__"+g.conn.GetTablePrefix())
	table := g.WrapTable(blueprint)
	names := strings.Join(columnNames, ", ")

	foreignKeys := make([]*schema.Command, 0, len(state.GetForeignKeys()))
	for _, foreign := range state.GetForeignKeys() {
		foreignKeys = append(foreignKeys, foreign.GetCommand())
	}

	primaryKey := ""
	if autoIncrementColumn == "" && state.GetPrimaryKey() != nil {
		primaryKey = g.addPrimaryKeys(state.GetPrimaryKey().GetCommand())
	}

	enabled := g.conn.ForeignKeyConstraintsEnabled()

	var statements []string
	if enabled {
		disable, err := g.CompileDisableForeignKeyConstraints()
		if err != nil {
			return nil, err
		}
		statements = append(statements, disable)
	}

	statements = append(statements,
		fmt.Sprintf("create table %s (%s%s%s)", tempTable, strings.Join(columns, ", "), g.addForeignKeys(foreignKeys), primaryKey),
		fmt.Sprintf("insert into %s (%s) select %s from %s", tempTable, names, names, table),
		"drop table "+table,
		fmt.Sprintf("alter table %s rename to %s", tempTable, g.WrapTable(tableName)),
	)
	statements = append(statements, indexes...)

	if enabled {
		enable, err := g.CompileEnableForeignKeyConstraints()
		if err != nil {
			return nil, err
		}
		statements = append(statements, enable)
	}

	return statements, nil
}

// schemaPrefix renders the "schema." an index statement carries when the table
// was named with one.
func (g *SQLiteGrammar) schemaPrefix(schemaName string) string {
	if schemaName == "" {
		return ""
	}
	return g.wrapValue(schemaName) + "."
}

// typeOf answers SQLiteGrammar's type methods. SQLite has five storage classes,
// so most of the Illuminate types collapse onto varchar, integer or text.
func (g *SQLiteGrammar) typeOf(column *schema.ColumnDefinition) (string, error) {
	switch column.GetType() {
	case "char", "string", "uuid", "ipAddress", "macAddress":
		return "varchar", nil
	case "tinyText", "text", "mediumText", "longText":
		return "text", nil
	case "integer", "bigInteger", "mediumInteger", "tinyInteger", "smallInteger":
		return "integer", nil
	case "float":
		return "float", nil
	case "double":
		return "double", nil
	case "decimal":
		return "numeric", nil
	case "boolean":
		return "tinyint(1)", nil
	case "enum":
		return fmt.Sprintf(`varchar check ("%s" in (%s))`,
			column.GetName(), g.QuoteString(column.GetAllowed())), nil
	case "json":
		if g.conn.GetConfig("use_native_json") != "" {
			return "json", nil
		}
		return "text", nil
	case "jsonb":
		if g.conn.GetConfig("use_native_jsonb") != "" {
			return "jsonb", nil
		}
		return "text", nil
	case "date":
		if column.GetUseCurrent() {
			column.Default(query.Raw("CURRENT_DATE"))
		}
		return "date", nil
	case "dateTime", "dateTimeTz", "timestamp", "timestampTz":
		if column.GetUseCurrent() {
			column.Default(query.Raw("CURRENT_TIMESTAMP"))
		}
		return "datetime", nil
	case "time", "timeTz":
		return "time", nil
	case "year":
		if column.GetUseCurrent() {
			column.Default(query.Raw("(CAST(strftime('%Y', 'now') AS INTEGER))"))
		}
		return "integer", nil
	case "binary":
		return "blob", nil
	case "geometry", "geography":
		return "geometry", nil
	case "computed":
		return "", fmt.Errorf("%w: SQLite requires a type, see the VirtualAs and StoredAs modifiers", schema.ErrUnsupported)
	case "raw":
		return typeRaw(column), nil
	default:
		return "", fmt.Errorf("%w: the column type %q", schema.ErrUnsupported, column.GetType())
	}
}

// modify answers SQLiteGrammar's modifier methods.
func (g *SQLiteGrammar) modify(modifier string, blueprint *schema.Blueprint, column *schema.ColumnDefinition) ([]string, error) {
	switch modifier {
	case "Increment":
		if g.isSerial(column) && column.GetAutoIncrement() {
			return one(" primary key autoincrement"), nil
		}
	case "Nullable":
		if !isGenerated(column) {
			if isNullable(column) {
				return nil, nil
			}
			return one(" not null"), nil
		}
		if n := column.GetNullable(); n != nil && !*n {
			return one(" not null"), nil
		}
	case "Default":
		if column.GetDefault() != nil && column.GetVirtualAs() == nil &&
			column.GetVirtualAsJSON() == "" && column.GetStoredAs() == nil {
			return one(" default " + g.GetDefaultValue(column.GetDefault())), nil
		}
	case "Collate":
		if column.GetCollation() != "" {
			return one(" collate '" + column.GetCollation() + "'"), nil
		}
	case "VirtualAs":
		if json := column.GetVirtualAsJSON(); json != "" {
			return one(" as (" + g.wrapJSONSelector(json) + ")"), nil
		}
		if column.GetVirtualAs() != nil {
			return one(" as (" + stringOf(column.GetVirtualAs()) + ")"), nil
		}
	case "StoredAs":
		if json := column.GetStoredAsJSON(); json != "" {
			return one(" as (" + g.wrapJSONSelector(json) + ") stored"), nil
		}
		if column.GetStoredAs() != nil {
			return one(" as (" + stringOf(column.GetStoredAs()) + ") stored"), nil
		}
	}
	return nil, nil
}

// wrapJSONSelector answers SQLiteGrammar::wrapJsonSelector.
func (g *SQLiteGrammar) wrapJSONSelector(value string) string {
	field, path := wrapJSONFieldAndPath(value, g.wrapValue)
	return "json_extract(" + field + path + ")"
}

// defaultSchema answers the PHP's `$schema ?? 'main'`.
func defaultSchema(schemaName string) string {
	if schemaName == "" {
		return "main"
	}
	return schemaName
}

func firstOr(values []string, fallback string) string {
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return fallback
}
