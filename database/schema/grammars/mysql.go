package grammars

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/schema"
)

// MySQLGrammar answers Illuminate\Database\Schema\Grammars\MySqlGrammar.
//
// It serves MariaDB too, which is what Illuminate's MariaDbGrammar subclass is
// for: that subclass overrides two type methods and nothing else, and both
// overrides are reachable here from Connection.IsMaria.
type MySQLGrammar struct{ *BaseGrammar }

// NewMySQLGrammar answers `new MySqlGrammar($connection)`.
func NewMySQLGrammar(connection schema.Connection) *MySQLGrammar {
	g := &MySQLGrammar{&BaseGrammar{
		conn: connection,
		modifiers: []string{
			"Unsigned", "Charset", "Collate", "VirtualAs", "StoredAs", "Nullable",
			"Default", "OnUpdate", "Invisible", "Increment", "Comment", "After", "First",
		},
		serials:        []string{"bigInteger", "integer", "mediumInteger", "smallInteger", "tinyInteger"},
		fluentCommands: []string{"autoIncrementStartingValues"},
	}}
	g.self = g
	return g
}

// CompileCreateDatabase answers MySqlGrammar::compileCreateDatabase.
func (g *MySQLGrammar) CompileCreateDatabase(name string) (string, error) {
	sql, err := g.BaseGrammar.CompileCreateDatabase(name)
	if err != nil {
		return "", err
	}
	if charset := g.conn.GetConfig("charset"); charset != "" {
		sql += " default character set " + g.wrapValue(charset)
	}
	if collation := g.conn.GetConfig("collation"); collation != "" {
		sql += " default collate " + g.wrapValue(collation)
	}
	return sql, nil
}

// CompileSchemas answers MySqlGrammar::compileSchemas.
func (g *MySQLGrammar) CompileSchemas() (string, error) {
	return "select schema_name as name, schema_name = schema() as `default` from information_schema.schemata where " +
		g.compileSchemaWhereClause(nil, "schema_name") +
		" order by schema_name", nil
}

// CompileTableExists answers MySqlGrammar::compileTableExists.
func (g *MySQLGrammar) CompileTableExists(schemaName, table string) string {
	from := "schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select exists (select 1 from information_schema.tables where "+
			"table_schema = %s and table_name = %s and table_type in ('BASE TABLE', 'SYSTEM VERSIONED')) as `exists`",
		from, g.QuoteString(table))
}

// CompileTables answers MySqlGrammar::compileTables.
func (g *MySQLGrammar) CompileTables(schemas []string, withSize ...bool) (string, error) {
	return "select table_name as `name`, table_schema as `schema`, (data_length + index_length) as `size`, " +
		"table_comment as `comment`, engine as `engine`, table_collation as `collation` " +
		"from information_schema.tables where table_type in ('BASE TABLE', 'SYSTEM VERSIONED') and " +
		g.compileSchemaWhereClause(schemas, "table_schema") +
		" order by table_schema, table_name", nil
}

// CompileViews answers MySqlGrammar::compileViews.
func (g *MySQLGrammar) CompileViews(schemas []string) (string, error) {
	return "select table_name as `name`, table_schema as `schema`, view_definition as `definition` " +
		"from information_schema.views where " +
		g.compileSchemaWhereClause(schemas, "table_schema") +
		" order by table_schema, table_name", nil
}

// compileSchemaWhereClause answers MySqlGrammar::compileSchemaWhereClause.
func (g *MySQLGrammar) compileSchemaWhereClause(schemas []string, column string) string {
	if len(schemas) > 0 {
		return column + " in (" + g.QuoteString(schemas) + ")"
	}
	return column + " not in ('information_schema', 'mysql', 'performance_schema', 'sys')"
}

// CompileColumns answers MySqlGrammar::compileColumns.
func (g *MySQLGrammar) CompileColumns(schemaName, table string) (string, error) {
	from := "schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select column_name as `name`, data_type as `type_name`, column_type as `type`, "+
			"collation_name as `collation`, is_nullable as `nullable`, "+
			"column_default as `default`, column_comment as `comment`, "+
			"generation_expression as `expression`, extra as `extra` "+
			"from information_schema.columns where table_schema = %s and table_name = %s "+
			"order by ordinal_position asc",
		from, g.QuoteString(table)), nil
}

// CompileIndexes answers MySqlGrammar::compileIndexes.
func (g *MySQLGrammar) CompileIndexes(schemaName, table string) (string, error) {
	from := "schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select index_name as `name`, group_concat(column_name order by seq_in_index) as `columns`, "+
			"index_type as `type`, not non_unique as `unique` "+
			"from information_schema.statistics where table_schema = %s and table_name = %s "+
			"group by index_name, index_type, non_unique",
		from, g.QuoteString(table)), nil
}

// CompileForeignKeys answers MySqlGrammar::compileForeignKeys.
func (g *MySQLGrammar) CompileForeignKeys(schemaName, table string) (string, error) {
	from := "schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select kc.constraint_name as `name`, "+
			"group_concat(kc.column_name order by kc.ordinal_position) as `columns`, "+
			"kc.referenced_table_schema as `foreign_schema`, "+
			"kc.referenced_table_name as `foreign_table`, "+
			"group_concat(kc.referenced_column_name order by kc.ordinal_position) as `foreign_columns`, "+
			"rc.update_rule as `on_update`, rc.delete_rule as `on_delete` "+
			"from information_schema.key_column_usage kc join information_schema.referential_constraints rc "+
			"on kc.constraint_schema = rc.constraint_schema and kc.constraint_name = rc.constraint_name "+
			"where kc.table_schema = %s and kc.table_name = %s and kc.referenced_table_name is not null "+
			"group by kc.constraint_name, kc.referenced_table_schema, kc.referenced_table_name, "+
			"rc.update_rule, rc.delete_rule",
		from, g.QuoteString(table)), nil
}

// CompileCreate answers MySqlGrammar::compileCreate.
func (g *MySQLGrammar) CompileCreate(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	sql, err := g.compileCreateTable(blueprint)
	if err != nil {
		return nil, err
	}
	return one(g.compileCreateEngine(g.compileCreateEncoding(sql, blueprint), blueprint)), nil
}

// compileCreateTable answers MySqlGrammar::compileCreateTable.
func (g *MySQLGrammar) compileCreateTable(blueprint *schema.Blueprint) (string, error) {
	structure, err := g.getColumns(blueprint)
	if err != nil {
		return "", err
	}

	if primary := g.getCommandByName(blueprint, "primary"); primary != nil {
		using := ""
		if primary.Algorithm != "" {
			using = "using " + primary.Algorithm
		}
		structure = append(structure, fmt.Sprintf("primary key %s(%s)", using, g.Columnize(primary.Columns)))
		primary.ShouldBeSkipped = true
	}

	create := "create"
	if blueprint.GetTemporary() {
		create = "create temporary"
	}
	return fmt.Sprintf("%s table %s (%s)", create, g.WrapTable(blueprint), strings.Join(structure, ", ")), nil
}

// compileCreateEncoding answers MySqlGrammar::compileCreateEncoding.
func (g *MySQLGrammar) compileCreateEncoding(sql string, blueprint *schema.Blueprint) string {
	if charset := blueprint.GetCharset(); charset != "" {
		sql += " default character set " + charset
	} else if charset := g.conn.GetConfig("charset"); charset != "" {
		sql += " default character set " + charset
	}
	if collation := blueprint.GetCollation(); collation != "" {
		sql += " collate '" + collation + "'"
	} else if collation := g.conn.GetConfig("collation"); collation != "" {
		sql += " collate '" + collation + "'"
	}
	return sql
}

// compileCreateEngine answers MySqlGrammar::compileCreateEngine.
func (g *MySQLGrammar) compileCreateEngine(sql string, blueprint *schema.Blueprint) string {
	if engine := blueprint.GetEngine(); engine != "" {
		return sql + " engine = " + engine
	}
	if engine := g.conn.GetConfig("engine"); engine != "" {
		return sql + " engine = " + engine
	}
	return sql
}

// CompileAdd answers MySqlGrammar::compileAdd.
func (g *MySQLGrammar) CompileAdd(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	definition, err := g.getColumn(blueprint, command.Column)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("alter table %s add %s", g.WrapTable(blueprint), definition)
	if command.Column.GetInstant() {
		sql += ", algorithm=instant"
	}
	if lock := command.Column.GetLock(); lock != "" {
		sql += ", lock=" + lock
	}
	return one(sql), nil
}

// CompileAutoIncrementStartingValues answers
// MySqlGrammar::compileAutoIncrementStartingValues.
func (g *MySQLGrammar) CompileAutoIncrementStartingValues(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	value := startingValue(command.Column)
	if !command.Column.GetAutoIncrement() || value == 0 {
		return nil, nil
	}
	return one("alter table " + g.WrapTable(blueprint) + " auto_increment = " + strconv.Itoa(value)), nil
}

// CompileRenameColumn answers MySqlGrammar::compileRenameColumn.
//
// The PHP falls back to compileLegacyRenameColumn on MySQL before 8.0.3 and
// MariaDB before 10.5.2, where "rename column" does not exist and the whole
// column definition has to be repeated -- which means reading it back off the
// server in the middle of compiling. Compilation here does not talk to a
// database, so those servers are refused with the reason rather than served a
// statement they will reject.
func (g *MySQLGrammar) CompileRenameColumn(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	version := g.conn.GetServerVersion()
	if g.conn.IsMaria() {
		if !atLeast(version, "10.5.2") {
			return nil, fmt.Errorf("%w: renaming a column on MariaDB %s needs the column's full definition read back from the server, which this grammar does not do while compiling", schema.ErrUnsupported, version)
		}
	} else if !atLeast(version, "8.0.3") {
		return nil, fmt.Errorf("%w: renaming a column on MySQL %s needs the column's full definition read back from the server, which this grammar does not do while compiling", schema.ErrUnsupported, version)
	}
	return g.BaseGrammar.CompileRenameColumn(blueprint, command)
}

// CompileChange answers MySqlGrammar::compileChange.
func (g *MySQLGrammar) CompileChange(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	column := command.Column

	verb, renamed := "modify", ""
	if column.GetRenameTo() != "" {
		verb, renamed = "change", " "+g.Wrap(column.GetRenameTo())
	}

	typ, err := g.typeOf(column)
	if err != nil {
		return nil, err
	}

	sql := fmt.Sprintf("alter table %s %s %s%s %s",
		g.WrapTable(blueprint), verb, g.Wrap(column), renamed, typ)
	sql, err = g.addModifiers(sql, blueprint, column)
	if err != nil {
		return nil, err
	}

	if column.GetInstant() {
		sql += ", algorithm=instant"
	}
	if lock := column.GetLock(); lock != "" {
		sql += ", lock=" + lock
	}
	return one(sql), nil
}

// CompilePrimary answers MySqlGrammar::compilePrimary.
func (g *MySQLGrammar) CompilePrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	using := ""
	if command.Algorithm != "" {
		using = "using " + command.Algorithm
	}
	sql := fmt.Sprintf("alter table %s add primary key %s(%s)",
		g.WrapTable(blueprint), using, g.Columnize(command.Columns))
	if command.Lock != "" {
		sql += ", lock=" + command.Lock
	}
	return one(sql), nil
}

// CompileUnique answers MySqlGrammar::compileUnique.
func (g *MySQLGrammar) CompileUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(g.compileKey(blueprint, command, "unique")), nil
}

// CompileIndex answers MySqlGrammar::compileIndex.
func (g *MySQLGrammar) CompileIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(g.compileKey(blueprint, command, "index")), nil
}

// CompileFullText answers MySqlGrammar::compileFullText.
func (g *MySQLGrammar) CompileFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(g.compileKey(blueprint, command, "fulltext")), nil
}

// CompileSpatialIndex answers MySqlGrammar::compileSpatialIndex.
func (g *MySQLGrammar) CompileSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(g.compileKey(blueprint, command, "spatial index")), nil
}

// compileKey answers MySqlGrammar::compileKey.
func (g *MySQLGrammar) compileKey(blueprint *schema.Blueprint, command *schema.Command, typ string) string {
	using := ""
	if command.Algorithm != "" {
		using = " using " + command.Algorithm
	}
	sql := fmt.Sprintf("alter table %s add %s %s%s(%s)",
		g.WrapTable(blueprint), typ, g.Wrap(command.Index), using, g.Columnize(command.Columns))
	if command.Lock != "" {
		sql += ", lock=" + command.Lock
	}
	return sql
}

// CompileDrop answers MySqlGrammar::compileDrop.
func (g *MySQLGrammar) CompileDrop(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table " + g.WrapTable(blueprint)), nil
}

// CompileDropIfExists answers MySqlGrammar::compileDropIfExists.
func (g *MySQLGrammar) CompileDropIfExists(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table if exists " + g.WrapTable(blueprint)), nil
}

// CompileDropColumn answers MySqlGrammar::compileDropColumn.
func (g *MySQLGrammar) CompileDropColumn(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	columns := g.PrefixArray("drop", g.WrapArray(command.Columns))
	sql := "alter table " + g.WrapTable(blueprint) + " " + strings.Join(columns, ", ")
	if command.Instant {
		sql += ", algorithm=instant"
	}
	if command.Lock != "" {
		sql += ", lock=" + command.Lock
	}
	return one(sql), nil
}

// CompileDropPrimary answers MySqlGrammar::compileDropPrimary.
func (g *MySQLGrammar) CompileDropPrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " drop primary key"), nil
}

// CompileDropUnique answers MySqlGrammar::compileDropUnique.
func (g *MySQLGrammar) CompileDropUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileDropIndex answers MySqlGrammar::compileDropIndex.
func (g *MySQLGrammar) CompileDropIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " drop index " + g.Wrap(command.Index)), nil
}

// CompileDropFullText answers MySqlGrammar::compileDropFullText.
func (g *MySQLGrammar) CompileDropFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileDropSpatialIndex answers MySqlGrammar::compileDropSpatialIndex.
func (g *MySQLGrammar) CompileDropSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileForeign answers MySqlGrammar::compileForeign.
func (g *MySQLGrammar) CompileForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	statements, err := g.BaseGrammar.CompileForeign(blueprint, command)
	if err != nil {
		return nil, err
	}
	if command.Lock != "" {
		statements[0] += ", lock=" + command.Lock
	}
	return statements, nil
}

// CompileDropForeign answers MySqlGrammar::compileDropForeign.
func (g *MySQLGrammar) CompileDropForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " drop foreign key " + g.Wrap(command.Index)), nil
}

// CompileRename answers MySqlGrammar::compileRename.
func (g *MySQLGrammar) CompileRename(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("rename table " + g.WrapTable(blueprint) + " to " + g.WrapTable(command.To)), nil
}

// CompileRenameIndex answers MySqlGrammar::compileRenameIndex.
func (g *MySQLGrammar) CompileRenameIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(fmt.Sprintf("alter table %s rename index %s to %s",
		g.WrapTable(blueprint), g.Wrap(command.From), g.Wrap(command.To))), nil
}

// CompileDropAllTables answers MySqlGrammar::compileDropAllTables.
func (g *MySQLGrammar) CompileDropAllTables(tables []string) (string, error) {
	return "drop table " + strings.Join(g.EscapeNames(tables), ", "), nil
}

// CompileDropAllViews answers MySqlGrammar::compileDropAllViews.
func (g *MySQLGrammar) CompileDropAllViews(views []string) (string, error) {
	return "drop view " + strings.Join(g.EscapeNames(views), ", "), nil
}

// CompileEnableForeignKeyConstraints answers
// MySqlGrammar::compileEnableForeignKeyConstraints.
func (g *MySQLGrammar) CompileEnableForeignKeyConstraints() (string, error) {
	return "SET FOREIGN_KEY_CHECKS=1;", nil
}

// CompileDisableForeignKeyConstraints answers
// MySqlGrammar::compileDisableForeignKeyConstraints.
func (g *MySQLGrammar) CompileDisableForeignKeyConstraints() (string, error) {
	return "SET FOREIGN_KEY_CHECKS=0;", nil
}

// CompileTableComment answers MySqlGrammar::compileTableComment.
func (g *MySQLGrammar) CompileTableComment(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(fmt.Sprintf("alter table %s comment = %s",
		g.WrapTable(blueprint), quote(command.Comment))), nil
}

// EscapeNames answers MySqlGrammar::escapeNames: each dotted segment of a name
// quoted on its own.
func (g *MySQLGrammar) EscapeNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		segments := strings.Split(name, ".")
		for j, segment := range segments {
			segments[j] = g.wrapValue(segment)
		}
		out[i] = strings.Join(segments, ".")
	}
	return out
}

// wrapValue answers MySqlGrammar::wrapValue: MySQL quotes with a backtick.
func (g *MySQLGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

// typeOf answers MySqlGrammar's type methods.
func (g *MySQLGrammar) typeOf(column *schema.ColumnDefinition) (string, error) {
	switch column.GetType() {
	case "char":
		return fmt.Sprintf("char(%d)", intOr(column.GetLength(), 0)), nil
	case "string":
		return fmt.Sprintf("varchar(%d)", intOr(column.GetLength(), 0)), nil
	case "tinyText":
		return "tinytext", nil
	case "text":
		return "text", nil
	case "mediumText":
		return "mediumtext", nil
	case "longText":
		return "longtext", nil
	case "bigInteger":
		return "bigint", nil
	case "integer":
		return "int", nil
	case "mediumInteger":
		return "mediumint", nil
	case "tinyInteger":
		return "tinyint", nil
	case "smallInteger":
		return "smallint", nil
	case "float":
		if p := intOr(column.GetPrecision(), 0); p != 0 {
			return fmt.Sprintf("float(%d)", p), nil
		}
		return "float", nil
	case "double":
		return "double", nil
	case "decimal":
		return fmt.Sprintf("decimal(%d, %d)", column.GetTotal(), column.GetPlaces()), nil
	case "boolean":
		return "tinyint(1)", nil
	case "enum":
		return fmt.Sprintf("enum(%s)", g.QuoteString(column.GetAllowed())), nil
	case "set":
		return fmt.Sprintf("set(%s)", g.QuoteString(column.GetAllowed())), nil
	case "json", "jsonb":
		// MySQL has one JSON type; jsonb is Postgres' spelling and lands here.
		return "json", nil
	case "date":
		if g.supportsFunctionalDefaults() && column.GetUseCurrent() {
			column.Default(query.Raw("(CURDATE())"))
		}
		return "date", nil
	case "dateTime", "dateTimeTz":
		return g.typeDateTime(column, "datetime"), nil
	case "time", "timeTz":
		if p := intOr(column.GetPrecision(), 0); p != 0 {
			return fmt.Sprintf("time(%d)", p), nil
		}
		return "time", nil
	case "timestamp", "timestampTz":
		return g.typeDateTime(column, "timestamp"), nil
	case "year":
		if g.supportsFunctionalDefaults() && column.GetUseCurrent() {
			column.Default(query.Raw("(YEAR(CURDATE()))"))
		}
		return "year", nil
	case "binary":
		if length := intOr(column.GetLength(), 0); length != 0 {
			if column.GetFixed() {
				return fmt.Sprintf("binary(%d)", length), nil
			}
			return fmt.Sprintf("varbinary(%d)", length), nil
		}
		return "blob", nil
	case "uuid":
		return "char(36)", nil
	case "ipAddress":
		return "varchar(45)", nil
	case "macAddress":
		return "varchar(17)", nil
	case "geometry", "geography":
		return g.typeGeometry(column), nil
	case "computed":
		return "", fmt.Errorf("%w: MySQL requires a type, see the VirtualAs and StoredAs modifiers", schema.ErrUnsupported)
	case "vector":
		if d := intOr(column.GetDimensions(), 0); d != 0 {
			return fmt.Sprintf("vector(%d)", d), nil
		}
		return "vector", nil
	case "raw":
		return typeRaw(column), nil
	default:
		return "", fmt.Errorf("%w: the column type %q", schema.ErrUnsupported, column.GetType())
	}
}

// typeDateTime answers MySqlGrammar::typeDateTime and typeTimestamp, which are
// the same body over a different keyword.
func (g *MySQLGrammar) typeDateTime(column *schema.ColumnDefinition, keyword string) string {
	precision := intOr(column.GetPrecision(), 0)

	current := "CURRENT_TIMESTAMP"
	if precision != 0 {
		current = fmt.Sprintf("CURRENT_TIMESTAMP(%d)", precision)
	}
	if column.GetUseCurrent() {
		column.Default(query.Raw(current))
	}
	if column.GetUseCurrentOnUpdate() {
		column.OnUpdate(query.Raw(current))
	}

	if precision != 0 {
		return fmt.Sprintf("%s(%d)", keyword, precision)
	}
	return keyword
}

// typeGeometry answers MySqlGrammar::typeGeometry.
func (g *MySQLGrammar) typeGeometry(column *schema.ColumnDefinition) string {
	subtype := strings.ToLower(column.GetSubtype())
	switch subtype {
	case "point", "linestring", "polygon", "geometrycollection", "multipoint", "multilinestring", "multipolygon":
	default:
		subtype = "geometry"
	}

	srid := column.GetSRID()
	switch {
	case srid != 0 && g.conn.IsMaria():
		return subtype + " ref_system_id=" + strconv.Itoa(srid)
	case srid != 0:
		return subtype + " srid " + strconv.Itoa(srid)
	default:
		return subtype
	}
}

// supportsFunctionalDefaults reports whether the server accepts an expression
// as a column default, which MariaDB always has and MySQL has since 8.0.13.
func (g *MySQLGrammar) supportsFunctionalDefaults() bool {
	return g.conn.IsMaria() || atLeast(g.conn.GetServerVersion(), "8.0.13")
}

// modify answers MySqlGrammar's modifier methods.
func (g *MySQLGrammar) modify(modifier string, blueprint *schema.Blueprint, column *schema.ColumnDefinition) ([]string, error) {
	switch modifier {
	case "Unsigned":
		if column.GetUnsigned() {
			return one(" unsigned"), nil
		}
	case "Charset":
		if column.GetCharset() != "" {
			return one(" character set " + column.GetCharset()), nil
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
	case "Nullable":
		if !isGenerated(column) {
			if isNullable(column) {
				return one(" null"), nil
			}
			return one(" not null"), nil
		}
		if n := column.GetNullable(); n != nil && !*n {
			return one(" not null"), nil
		}
	case "Invisible":
		if column.GetInvisible() != nil {
			return one(" invisible"), nil
		}
	case "Default":
		if column.GetDefault() != nil {
			return one(" default " + g.GetDefaultValue(column.GetDefault())), nil
		}
	case "OnUpdate":
		if column.GetOnUpdate() != nil {
			return one(" on update " + stringOf(column.GetOnUpdate())), nil
		}
	case "Increment":
		if g.isSerial(column) && column.GetAutoIncrement() {
			if g.hasCommand(blueprint, "primary") || (column.GetChange() && column.GetPrimary() == nil) {
				return one(" auto_increment"), nil
			}
			return one(" auto_increment primary key"), nil
		}
	case "First":
		if column.GetFirst() != nil {
			return one(" first"), nil
		}
	case "After":
		if after := column.GetAfter(); after != nil {
			return one(" after " + g.Wrap(*after)), nil
		}
	case "Comment":
		if comment := column.GetComment(); comment != nil {
			return one(" comment '" + addSlashes(*comment) + "'"), nil
		}
	}
	return nil, nil
}

// wrapJSONSelector answers MySqlGrammar::wrapJsonSelector.
func (g *MySQLGrammar) wrapJSONSelector(value string) string {
	field, path := wrapJSONFieldAndPath(value, g.wrapValue)
	return "json_unquote(json_extract(" + field + path + "))"
}
