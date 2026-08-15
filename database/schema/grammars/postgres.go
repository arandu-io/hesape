package grammars

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/schema"
)

// PostgresGrammar turns a Blueprint into Postgres DDL, and compiles the
// catalogue queries that read a Postgres schema back.
//
// What it changes about the base grammar: DDL runs inside a transaction, so a
// failed migration leaves no half-built schema; an auto-incrementing integer is
// a serial type rather than a modifier; a timestamp carries its time zone or
// explicitly does not; an index can name an operator class, which is what makes
// a full-text, spatial or vector index reachable at all; and dropping a table,
// view, type or domain is one statement for the whole list.
//
// The introspection half -- CompileTables, CompileColumns, CompileIndexes,
// CompileForeignKeys -- reads pg_catalog rather than information_schema,
// because information_schema does not describe an index at all: its name, its
// access method and the order of its columns are only in pg_index.
//
// Answers Illuminate\Database\Schema\Grammars\PostgresGrammar.
type PostgresGrammar struct{ *BaseGrammar }

// NewPostgresGrammar answers `new PostgresGrammar($connection)`.
func NewPostgresGrammar(connection schema.Connection) *PostgresGrammar {
	g := &PostgresGrammar{&BaseGrammar{
		conn:           connection,
		transactions:   true,
		modifiers:      []string{"Collate", "Nullable", "Default", "VirtualAs", "StoredAs", "GeneratedAs", "Increment"},
		serials:        []string{"bigInteger", "integer", "mediumInteger", "smallInteger", "tinyInteger"},
		fluentCommands: []string{"autoIncrementStartingValues", "comment"},
	}}
	g.self = g
	return g
}

// CompileCreateDatabase answers PostgresGrammar::compileCreateDatabase.
func (g *PostgresGrammar) CompileCreateDatabase(name string) (string, error) {
	sql, err := g.BaseGrammar.CompileCreateDatabase(name)
	if err != nil {
		return "", err
	}
	if charset := g.conn.GetConfig("charset"); charset != "" {
		sql += " encoding " + g.wrapValue(charset)
	}
	return sql, nil
}

// CompileSchemas answers PostgresGrammar::compileSchemas.
func (g *PostgresGrammar) CompileSchemas() (string, error) {
	return "select nspname as name, nspname = any (current_schemas(false)) as \"default\" " +
		"from pg_namespace where nspname not in ('pg_catalog', 'information_schema') and nspname !~ '^pg_' " +
		"order by nspname", nil
}

// CompileTableExists answers PostgresGrammar::compileTableExists.
func (g *PostgresGrammar) CompileTableExists(schemaName, table string) string {
	from := "current_schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select exists (select 1 from pg_class c, pg_namespace n where "+
			"n.nspname = %s and c.relname = %s and c.relnamespace = n.oid and c.relkind in ('r', 'p'))",
		from, g.QuoteString(table))
}

// CompileTables answers PostgresGrammar::compileTables.
func (g *PostgresGrammar) CompileTables(schemas []string, withSize ...bool) (string, error) {
	return "select c.relname as name, n.nspname as schema, pg_total_relation_size(c.oid) as size, " +
		"obj_description(c.oid, 'pg_class') as comment from pg_class c, pg_namespace n " +
		"where c.relkind in ('r', 'p') and n.oid = c.relnamespace and " +
		g.compileSchemaWhereClause(schemas, "n.nspname") +
		" order by n.nspname, c.relname", nil
}

// CompileTypes answers PostgresGrammar::compileTypes.
func (g *PostgresGrammar) CompileTypes(schemas []string) (string, error) {
	return "select t.typname as name, n.nspname as schema, t.typtype as type, t.typcategory as category, " +
		"((t.typinput = 'array_in'::regproc and t.typoutput = 'array_out'::regproc) or t.typtype = 'm') as implicit " +
		"from pg_type t join pg_namespace n on n.oid = t.typnamespace " +
		"left join pg_class c on c.oid = t.typrelid " +
		"left join pg_type el on el.oid = t.typelem " +
		"left join pg_class ce on ce.oid = el.typrelid " +
		"where ((t.typrelid = 0 and (ce.relkind = 'c' or ce.relkind is null)) or c.relkind = 'c') " +
		"and not exists (select 1 from pg_depend d where d.objid in (t.oid, t.typelem) and d.deptype = 'e') and " +
		g.compileSchemaWhereClause(schemas, "n.nspname"), nil
}

// CompileViews answers PostgresGrammar::compileViews.
func (g *PostgresGrammar) CompileViews(schemas []string) (string, error) {
	return "select viewname as name, schemaname as schema, definition from pg_views where " +
		g.compileSchemaWhereClause(schemas, "schemaname") +
		" order by schemaname, viewname", nil
}

// compileSchemaWhereClause answers PostgresGrammar::compileSchemaWhereClause.
func (g *PostgresGrammar) compileSchemaWhereClause(schemas []string, column string) string {
	if len(schemas) > 0 {
		return column + " in (" + g.QuoteString(schemas) + ")"
	}
	return column + " <> 'information_schema' and " + column + " not like 'pg\\_%'"
}

// CompileColumns answers PostgresGrammar::compileColumns.
func (g *PostgresGrammar) CompileColumns(schemaName, table string) (string, error) {
	from := "current_schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select a.attname as name, t.typname as type_name, format_type(a.atttypid, a.atttypmod) as type, "+
			"(select tc.collcollate from pg_catalog.pg_collation tc where tc.oid = a.attcollation) as collation, "+
			"not a.attnotnull as nullable, "+
			"(select pg_get_expr(adbin, adrelid) from pg_attrdef where c.oid = pg_attrdef.adrelid "+
			"and pg_attrdef.adnum = a.attnum) as default, "+
			"col_description(c.oid, a.attnum) as comment "+
			"from pg_attribute a, pg_class c, pg_type t, pg_namespace n "+
			"where c.relname = %s and n.nspname = %s and a.attnum > 0 and a.attrelid = c.oid "+
			"and a.atttypid = t.oid and n.oid = c.relnamespace order by a.attnum",
		g.QuoteString(table), from), nil
}

// CompileIndexes answers PostgresGrammar::compileIndexes.
func (g *PostgresGrammar) CompileIndexes(schemaName, table string) (string, error) {
	from := "current_schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select ic.relname as name, string_agg(a.attname, ',' order by indseq.ord) as columns, "+
			"am.amname as \"type\", i.indisunique as \"unique\", i.indisprimary as \"primary\" "+
			"from pg_index i join pg_class tc on tc.oid = i.indrelid "+
			"join pg_namespace tn on tn.oid = tc.relnamespace join pg_class ic on ic.oid = i.indexrelid "+
			"join pg_am am on am.oid = ic.relam "+
			"join lateral unnest(i.indkey) with ordinality as indseq(num, ord) on true "+
			"left join pg_attribute a on a.attrelid = i.indrelid and a.attnum = indseq.num "+
			"where tc.relname = %s and tn.nspname = %s "+
			"group by ic.relname, am.amname, i.indisunique, i.indisprimary",
		g.QuoteString(table), from), nil
}

// CompileForeignKeys answers PostgresGrammar::compileForeignKeys.
func (g *PostgresGrammar) CompileForeignKeys(schemaName, table string) (string, error) {
	from := "current_schema()"
	if schemaName != "" {
		from = g.QuoteString(schemaName)
	}
	return fmt.Sprintf(
		"select c.conname as name, string_agg(la.attname, ',' order by conseq.ord) as columns, "+
			"fn.nspname as foreign_schema, fc.relname as foreign_table, "+
			"string_agg(fa.attname, ',' order by conseq.ord) as foreign_columns, "+
			"c.confupdtype as on_update, c.confdeltype as on_delete "+
			"from pg_constraint c join pg_class tc on c.conrelid = tc.oid "+
			"join pg_namespace tn on tn.oid = tc.relnamespace join pg_class fc on c.confrelid = fc.oid "+
			"join pg_namespace fn on fn.oid = fc.relnamespace "+
			"join lateral unnest(c.conkey) with ordinality as conseq(num, ord) on true "+
			"join pg_attribute la on la.attrelid = c.conrelid and la.attnum = conseq.num "+
			"join pg_attribute fa on fa.attrelid = c.confrelid and fa.attnum = c.confkey[conseq.ord] "+
			"where c.contype = 'f' and tc.relname = %s and tn.nspname = %s "+
			"group by c.conname, fn.nspname, fc.relname, c.confupdtype, c.confdeltype",
		g.QuoteString(table), from), nil
}

// CompileCreate answers PostgresGrammar::compileCreate.
func (g *PostgresGrammar) CompileCreate(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	columns, err := g.getColumns(blueprint)
	if err != nil {
		return nil, err
	}
	create := "create"
	if blueprint.GetTemporary() {
		create = "create temporary"
	}
	return one(fmt.Sprintf("%s table %s (%s)", create, g.WrapTable(blueprint), strings.Join(columns, ", "))), nil
}

// CompileAdd answers PostgresGrammar::compileAdd.
func (g *PostgresGrammar) CompileAdd(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	definition, err := g.getColumn(blueprint, command.Column)
	if err != nil {
		return nil, err
	}
	return one("alter table " + g.WrapTable(blueprint) + " add column " + definition), nil
}

// CompileAutoIncrementStartingValues answers
// PostgresGrammar::compileAutoIncrementStartingValues.
func (g *PostgresGrammar) CompileAutoIncrementStartingValues(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	value := startingValue(command.Column)
	if !command.Column.GetAutoIncrement() || value == 0 {
		return nil, nil
	}
	return one(fmt.Sprintf("select setval(pg_get_serial_sequence(%s, %s), %d, false)",
		g.QuoteString(g.WrapTable(blueprint)),
		g.QuoteString(command.Column.GetName()),
		value)), nil
}

// CompileChange answers PostgresGrammar::compileChange.
func (g *PostgresGrammar) CompileChange(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	column := command.Column

	typ, err := g.typeOf(column)
	if err != nil {
		return nil, err
	}

	collate, err := g.modify("Collate", blueprint, column)
	if err != nil {
		return nil, err
	}
	changes := []string{"type " + typ + strings.Join(collate, "")}

	for _, modifier := range g.modifiers {
		if modifier == "Collate" {
			continue
		}
		fragments, err := g.modify(modifier, blueprint, column)
		if err != nil {
			return nil, err
		}
		changes = append(changes, fragments...)
	}

	return one(fmt.Sprintf("alter table %s %s",
		g.WrapTable(blueprint),
		strings.Join(g.PrefixArray("alter column "+g.Wrap(column), changes), ", "))), nil
}

// CompilePrimary answers PostgresGrammar::compilePrimary.
func (g *PostgresGrammar) CompilePrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " add primary key (" + g.Columnize(command.Columns) + ")"), nil
}

// CompileUnique answers PostgresGrammar::compileUnique.
func (g *PostgresGrammar) CompileUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	unique := "unique"
	if command.NullsNotDistinct != nil {
		if *command.NullsNotDistinct {
			unique += " nulls not distinct"
		} else {
			unique += " nulls distinct"
		}
	}

	var statements []string
	var sql string

	if command.Online || command.Algorithm != "" {
		concurrently := ""
		if command.Online {
			concurrently = "concurrently "
		}
		using := ""
		if command.Algorithm != "" {
			using = " using " + command.Algorithm
		}
		statements = append(statements, fmt.Sprintf("create unique index %s%s on %s%s (%s)",
			concurrently, g.Wrap(command.Index), g.WrapTable(blueprint), using, g.Columnize(command.Columns)))

		sql = fmt.Sprintf("alter table %s add constraint %s unique using index %s",
			g.WrapTable(blueprint), g.Wrap(command.Index), g.Wrap(command.Index))
	} else {
		sql = fmt.Sprintf("alter table %s add constraint %s %s (%s)",
			g.WrapTable(blueprint), g.Wrap(command.Index), unique, g.Columnize(command.Columns))
	}

	sql += deferrability(command)
	return append(statements, sql), nil
}

// CompileIndex answers PostgresGrammar::compileIndex.
func (g *PostgresGrammar) CompileIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	concurrently := ""
	if command.Online {
		concurrently = "concurrently "
	}
	using := ""
	if command.Algorithm != "" {
		using = " using " + command.Algorithm
	}
	return one(fmt.Sprintf("create index %s%s on %s%s (%s)",
		concurrently, g.Wrap(command.Index), g.WrapTable(blueprint), using, g.Columnize(command.Columns))), nil
}

// CompileFullText answers PostgresGrammar::compileFulltext.
func (g *PostgresGrammar) CompileFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	language := command.Language
	if language == "" {
		language = "english"
	}

	columns := make([]string, len(command.Columns))
	for i, column := range command.Columns {
		columns[i] = "to_tsvector(" + g.QuoteString(language) + ", " + g.Wrap(column) + ")"
	}

	concurrently := ""
	if command.Online {
		concurrently = "concurrently "
	}

	return one(fmt.Sprintf("create index %s%s on %s using gin ((%s))",
		concurrently, g.Wrap(command.Index), g.WrapTable(blueprint), strings.Join(columns, " || "))), nil
}

// CompileSpatialIndex answers PostgresGrammar::compileSpatialIndex.
func (g *PostgresGrammar) CompileSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	command.Algorithm = "gist"
	if command.OperatorClass != "" {
		return g.compileIndexWithOperatorClass(blueprint, command), nil
	}
	return g.CompileIndex(blueprint, command)
}

// CompileVectorIndex answers PostgresGrammar::compileVectorIndex.
func (g *PostgresGrammar) CompileVectorIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.compileIndexWithOperatorClass(blueprint, command), nil
}

// compileIndexWithOperatorClass answers
// PostgresGrammar::compileIndexWithOperatorClass.
func (g *PostgresGrammar) compileIndexWithOperatorClass(blueprint *schema.Blueprint, command *schema.Command) []string {
	columns := make([]string, len(command.Columns))
	for i, column := range command.Columns {
		columns[i] = g.Wrap(column)
		if command.OperatorClass != "" {
			columns[i] += " " + command.OperatorClass
		}
	}

	concurrently := ""
	if command.Online {
		concurrently = "concurrently "
	}
	using := ""
	if command.Algorithm != "" {
		using = " using " + command.Algorithm
	}

	return one(fmt.Sprintf("create index %s%s on %s%s (%s)",
		concurrently, g.Wrap(command.Index), g.WrapTable(blueprint), using, strings.Join(columns, ", ")))
}

// CompileForeign answers PostgresGrammar::compileForeign.
func (g *PostgresGrammar) CompileForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	statements, err := g.BaseGrammar.CompileForeign(blueprint, command)
	if err != nil {
		return nil, err
	}
	statements[0] += deferrability(command)
	if command.NotValid != nil && *command.NotValid {
		statements[0] += " not valid"
	}
	return statements, nil
}

// CompileDrop answers PostgresGrammar::compileDrop.
func (g *PostgresGrammar) CompileDrop(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table " + g.WrapTable(blueprint)), nil
}

// CompileDropIfExists answers PostgresGrammar::compileDropIfExists.
func (g *PostgresGrammar) CompileDropIfExists(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop table if exists " + g.WrapTable(blueprint)), nil
}

// CompileDropAllTables answers PostgresGrammar::compileDropAllTables.
func (g *PostgresGrammar) CompileDropAllTables(tables []string) (string, error) {
	return "drop table " + strings.Join(g.EscapeNames(tables), ", ") + " cascade", nil
}

// CompileDropAllViews answers PostgresGrammar::compileDropAllViews.
func (g *PostgresGrammar) CompileDropAllViews(views []string) (string, error) {
	return "drop view " + strings.Join(g.EscapeNames(views), ", ") + " cascade", nil
}

// CompileDropAllTypes answers PostgresGrammar::compileDropAllTypes.
func (g *PostgresGrammar) CompileDropAllTypes(types []string) (string, error) {
	return "drop type " + strings.Join(g.EscapeNames(types), ", ") + " cascade", nil
}

// CompileDropAllDomains answers PostgresGrammar::compileDropAllDomains.
func (g *PostgresGrammar) CompileDropAllDomains(domains []string) (string, error) {
	return "drop domain " + strings.Join(g.EscapeNames(domains), ", ") + " cascade", nil
}

// CompileDropColumn answers PostgresGrammar::compileDropColumn.
func (g *PostgresGrammar) CompileDropColumn(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	columns := g.PrefixArray("drop column", g.WrapArray(command.Columns))
	return one("alter table " + g.WrapTable(blueprint) + " " + strings.Join(columns, ", ")), nil
}

// CompileDropPrimary answers PostgresGrammar::compileDropPrimary.
func (g *PostgresGrammar) CompileDropPrimary(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	_, table, err := schema.ParseSchemaAndTable(blueprint.GetTable())
	if err != nil {
		return nil, err
	}
	index := g.Wrap(g.conn.GetTablePrefix() + table + "_pkey")
	return one("alter table " + g.WrapTable(blueprint) + " drop constraint " + index), nil
}

// CompileDropUnique answers PostgresGrammar::compileDropUnique.
func (g *PostgresGrammar) CompileDropUnique(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " drop constraint " + g.Wrap(command.Index)), nil
}

// CompileDropIndex answers PostgresGrammar::compileDropIndex.
func (g *PostgresGrammar) CompileDropIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("drop index " + g.Wrap(command.Index)), nil
}

// CompileDropFullText answers PostgresGrammar::compileDropFullText.
func (g *PostgresGrammar) CompileDropFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileDropSpatialIndex answers PostgresGrammar::compileDropSpatialIndex.
func (g *PostgresGrammar) CompileDropSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return g.CompileDropIndex(blueprint, command)
}

// CompileDropForeign answers PostgresGrammar::compileDropForeign.
func (g *PostgresGrammar) CompileDropForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " drop constraint " + g.Wrap(command.Index)), nil
}

// CompileRename answers PostgresGrammar::compileRename.
func (g *PostgresGrammar) CompileRename(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter table " + g.WrapTable(blueprint) + " rename to " + g.WrapTable(command.To)), nil
}

// CompileRenameIndex answers PostgresGrammar::compileRenameIndex.
func (g *PostgresGrammar) CompileRenameIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one("alter index " + g.Wrap(command.From) + " rename to " + g.Wrap(command.To)), nil
}

// CompileEnableForeignKeyConstraints answers
// PostgresGrammar::compileEnableForeignKeyConstraints.
func (g *PostgresGrammar) CompileEnableForeignKeyConstraints() (string, error) {
	return "SET CONSTRAINTS ALL IMMEDIATE;", nil
}

// CompileDisableForeignKeyConstraints answers
// PostgresGrammar::compileDisableForeignKeyConstraints.
func (g *PostgresGrammar) CompileDisableForeignKeyConstraints() (string, error) {
	return "SET CONSTRAINTS ALL DEFERRED;", nil
}

// CompileComment answers PostgresGrammar::compileComment.
func (g *PostgresGrammar) CompileComment(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	comment := command.Column.GetComment()
	if comment == nil && !command.Column.GetChange() {
		return nil, nil
	}
	value := "NULL"
	if comment != nil {
		value = quote(*comment)
	}
	return one(fmt.Sprintf("comment on column %s.%s is %s",
		g.WrapTable(blueprint), g.Wrap(command.Column.GetName()), value)), nil
}

// CompileTableComment answers PostgresGrammar::compileTableComment.
func (g *PostgresGrammar) CompileTableComment(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(fmt.Sprintf("comment on table %s is %s",
		g.WrapTable(blueprint), quote(command.Comment))), nil
}

// EscapeNames answers PostgresGrammar::escapeNames: each dotted segment of a
// name quoted on its own.
func (g *PostgresGrammar) EscapeNames(names []string) []string {
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

// typeOf answers PostgresGrammar's type methods.
func (g *PostgresGrammar) typeOf(column *schema.ColumnDefinition) (string, error) {
	switch column.GetType() {
	case "char":
		if length := intOr(column.GetLength(), 0); length != 0 {
			return fmt.Sprintf("char(%d)", length), nil
		}
		return "char", nil
	case "string":
		if length := intOr(column.GetLength(), 0); length != 0 {
			return fmt.Sprintf("varchar(%d)", length), nil
		}
		return "varchar", nil
	case "tinyText":
		return "varchar(255)", nil
	case "text", "mediumText", "longText":
		return "text", nil
	case "integer", "mediumInteger":
		return g.typeSerial(column, "serial", "integer"), nil
	case "bigInteger":
		return g.typeSerial(column, "bigserial", "bigint"), nil
	case "smallInteger", "tinyInteger":
		return g.typeSerial(column, "smallserial", "smallint"), nil
	case "float":
		if p := intOr(column.GetPrecision(), 0); p != 0 {
			return fmt.Sprintf("float(%d)", p), nil
		}
		return "float", nil
	case "double":
		return "double precision", nil
	case "real":
		return "real", nil
	case "decimal":
		return fmt.Sprintf("decimal(%d, %d)", column.GetTotal(), column.GetPlaces()), nil
	case "boolean":
		return "boolean", nil
	case "enum":
		return fmt.Sprintf(`varchar(255) check ("%s" in (%s))`,
			column.GetName(), g.QuoteString(column.GetAllowed())), nil
	case "set":
		return "", fmt.Errorf("%w: the set type", schema.ErrUnsupported)
	case "json":
		return "json", nil
	case "jsonb":
		return "jsonb", nil
	case "date":
		if column.GetUseCurrent() {
			column.Default(query.Raw("CURRENT_DATE"))
		}
		return "date", nil
	case "dateTime", "timestamp":
		return g.typeTimestamp(column, " without time zone"), nil
	case "dateTimeTz", "timestampTz":
		return g.typeTimestamp(column, " with time zone"), nil
	case "time":
		return "time" + precisionSuffix(column) + " without time zone", nil
	case "timeTz":
		return "time" + precisionSuffix(column) + " with time zone", nil
	case "year":
		if column.GetUseCurrent() {
			column.Default(query.Raw("EXTRACT(YEAR FROM CURRENT_DATE)"))
		}
		return g.typeSerial(column, "serial", "integer"), nil
	case "binary":
		return "bytea", nil
	case "uuid":
		return "uuid", nil
	case "ipAddress":
		return "inet", nil
	case "macAddress":
		return "macaddr", nil
	case "geometry":
		return g.typeSpatial("geometry", column), nil
	case "geography":
		return g.typeSpatial("geography", column), nil
	case "vector":
		if d := intOr(column.GetDimensions(), 0); d != 0 {
			return fmt.Sprintf("vector(%d)", d), nil
		}
		return "vector", nil
	case "computed":
		return "", fmt.Errorf("%w: the computed type", schema.ErrUnsupported)
	case "raw":
		return typeRaw(column), nil
	default:
		return "", fmt.Errorf("%w: the column type %q", schema.ErrUnsupported, column.GetType())
	}
}

// typeSerial answers the PHP's `$column->autoIncrement && is_null(generatedAs)
// && ! $column->change ? 'serial' : 'integer'` over the three integer widths.
func (g *PostgresGrammar) typeSerial(column *schema.ColumnDefinition, serial, plain string) string {
	if column.GetAutoIncrement() && column.GetGeneratedAs() == nil && !column.GetChange() {
		return serial
	}
	return plain
}

// typeTimestamp answers PostgresGrammar::typeTimestamp and typeTimestampTz.
func (g *PostgresGrammar) typeTimestamp(column *schema.ColumnDefinition, zone string) string {
	if column.GetUseCurrent() {
		column.Default(query.Raw("CURRENT_TIMESTAMP"))
	}
	return "timestamp" + precisionSuffix(column) + zone
}

// typeSpatial answers PostgresGrammar::typeGeometry and typeGeography.
func (g *PostgresGrammar) typeSpatial(keyword string, column *schema.ColumnDefinition) string {
	if column.GetSubtype() == "" {
		return keyword
	}
	srid := ""
	if column.GetSRID() != 0 {
		srid = "," + strconv.Itoa(column.GetSRID())
	}
	return fmt.Sprintf("%s(%s%s)", keyword, strings.ToLower(column.GetSubtype()), srid)
}

// modify answers PostgresGrammar's modifier methods.
func (g *PostgresGrammar) modify(modifier string, blueprint *schema.Blueprint, column *schema.ColumnDefinition) ([]string, error) {
	switch modifier {
	case "Collate":
		if column.GetCollation() != "" {
			return one(" collate " + g.wrapValue(column.GetCollation())), nil
		}
	case "Nullable":
		if column.GetChange() {
			if isNullable(column) {
				return one("drop not null"), nil
			}
			return one("set not null"), nil
		}
		if isNullable(column) {
			return one(" null"), nil
		}
		return one(" not null"), nil
	case "Default":
		if column.GetChange() {
			if column.GetAutoIncrement() && column.GetGeneratedAs() == nil {
				return nil, nil
			}
			if column.GetDefault() == nil {
				return one("drop default"), nil
			}
			return one("set default " + g.GetDefaultValue(column.GetDefault())), nil
		}
		if column.GetDefault() != nil {
			return one(" default " + g.GetDefaultValue(column.GetDefault())), nil
		}
	case "VirtualAs":
		return g.modifyGenerated(column, column.HasVirtualAs(), column.GetVirtualAs(), " virtual")
	case "StoredAs":
		return g.modifyGenerated(column, column.HasStoredAs(), column.GetStoredAs(), " stored")
	case "GeneratedAs":
		return g.modifyGeneratedAs(column), nil
	case "Increment":
		if !column.GetChange() &&
			!g.hasCommand(blueprint, "primary") &&
			(g.isSerial(column) || column.GetGeneratedAs() != nil) &&
			column.GetAutoIncrement() {
			return one(" primary key"), nil
		}
	}
	return nil, nil
}

// modifyGenerated answers PostgresGrammar::modifyVirtualAs and modifyStoredAs,
// which are the same body over a different keyword.
//
// On a changed column PHP looks at whether the attribute is present at all:
// absent means the generation expression is left alone, present and null means
// it is dropped, and present with an expression is a LogicException, because
// Postgres cannot rewrite one. The error is that refusal.
func (g *PostgresGrammar) modifyGenerated(column *schema.ColumnDefinition, wasSet bool, expression any, keyword string) ([]string, error) {
	if column.GetChange() {
		if !wasSet {
			return nil, nil
		}
		if expression == nil {
			return one("drop expression if exists"), nil
		}
		return nil, fmt.Errorf("%w: modifying generated columns", schema.ErrUnsupported)
	}
	if expression != nil {
		return one(" generated always as (" + stringOf(expression) + ")" + keyword), nil
	}
	return nil, nil
}

// modifyGeneratedAs answers PostgresGrammar::modifyGeneratedAs.
func (g *PostgresGrammar) modifyGeneratedAs(column *schema.ColumnDefinition) []string {
	sql := ""
	if generated := column.GetGeneratedAs(); generated != nil {
		kind := "by default"
		if column.GetAlways() {
			kind = "always"
		}
		options := ""
		if text, ok := generated.(string); ok && text != "" {
			options = " (" + text + ")"
		}
		sql = fmt.Sprintf(" generated %s as identity%s", kind, options)
	}

	if !column.GetChange() {
		if sql == "" {
			return nil
		}
		return one(sql)
	}

	var changes []string
	if !(column.GetAutoIncrement() && sql == "") {
		changes = append(changes, "drop identity if exists")
	}
	if sql != "" {
		changes = append(changes, "add "+sql)
	}
	return changes
}

// deferrability answers the shared tail of PostgresGrammar::compileUnique and
// compileForeign.
func deferrability(command *schema.Command) string {
	sql := ""
	if command.Deferrable != nil {
		if *command.Deferrable {
			sql += " deferrable"
		} else {
			sql += " not deferrable"
		}
	}
	if command.Deferrable != nil && *command.Deferrable && command.InitiallyImmediate != nil {
		if *command.InitiallyImmediate {
			sql += " initially immediate"
		} else {
			sql += " initially deferred"
		}
	}
	return sql
}

// precisionSuffix answers the "($precision)" the time types carry when one was
// asked for. PHP tests is_null, so a precision of zero still prints.
func precisionSuffix(column *schema.ColumnDefinition) string {
	if precision := column.GetPrecision(); precision != nil {
		return "(" + strconv.Itoa(*precision) + ")"
	}
	return ""
}
