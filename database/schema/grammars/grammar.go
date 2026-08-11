package grammars

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/schema"
)

// dialect is what BaseGrammar needs back from the driver grammar embedding it.
//
// PHP gets this for free: getColumn calls $this->getType(), and late static
// binding sends it to the subclass. Go embedding has no such dispatch, so the
// driver grammar hands itself to the base at construction and the base calls
// back through this interface. It is unexported because it is the mechanism,
// not the contract -- schema.Grammar is the contract.
type dialect interface {
	// wrapValue answers Grammar::wrapValue: it quotes one identifier segment.
	wrapValue(value string) string

	// typeOf answers Grammar::getType: it dispatches to the driver's type
	// method for the column's type.
	typeOf(column *schema.ColumnDefinition) (string, error)

	// modify answers Grammar::modify{$modifier}. It returns a list because
	// Postgres' generatedAs modifier produces more than one fragment when a
	// column is being changed; every other modifier returns none or one. The
	// error is what Postgres throws for a generated column it cannot modify.
	modify(modifier string, blueprint *schema.Blueprint, column *schema.ColumnDefinition) ([]string, error)
}

// BaseGrammar answers the shared body of
// Illuminate\Database\Schema\Grammars\Grammar and the part of
// Illuminate\Database\Grammar the schema component uses.
//
// A driver grammar embeds it and overrides what its dialect spells differently,
// which is what extending the abstract class does in PHP. Every method the
// abstract class implements by throwing is implemented here by returning an
// error, so a driver that does not override it reports the same refusal.
type BaseGrammar struct {
	conn schema.Connection
	self dialect

	transactions   bool
	modifiers      []string
	serials        []string
	fluentCommands []string
}

// GetConnection returns the connection the grammar was built with.
func (g *BaseGrammar) GetConnection() schema.Connection { return g.conn }

// GetFluentCommands answers Grammar::getFluentCommands.
func (g *BaseGrammar) GetFluentCommands() []string { return g.fluentCommands }

// GetAlterCommands answers the SQLite grammar's list of commands that force a
// table rebuild. Every other driver has none.
func (g *BaseGrammar) GetAlterCommands() []string { return nil }

// SupportsSchemaTransactions answers Grammar::supportsSchemaTransactions.
func (g *BaseGrammar) SupportsSchemaTransactions() bool { return g.transactions }

// Wrap answers Grammar::wrap. A column definition is unwrapped to its name, as
// the PHP unwraps a Fluent, and an expression passes through untouched.
func (g *BaseGrammar) Wrap(value any) string {
	if column, ok := value.(*schema.ColumnDefinition); ok {
		value = column.GetName()
	}
	if query.IsExpression(value) {
		return stringOf(value)
	}
	name := stringOf(value)
	if i := aliasIndex(name); i >= 0 {
		return g.Wrap(strings.TrimSpace(name[:i])) + " as " +
			g.self.wrapValue(strings.TrimSpace(name[i+4:]))
	}
	return g.wrapSegments(name)
}

// WrapTable answers Grammar::wrapTable. A blueprint is unwrapped to its table.
//
// The optional prefix replaces the connection's table prefix, which is how the
// SQLite rebuild names the temporary table it copies rows into. Note that only
// the last dotted segment takes the prefix: schema.table prefixes the table,
// not the schema.
func (g *BaseGrammar) WrapTable(table any, prefix ...string) string {
	if blueprint, ok := table.(*schema.Blueprint); ok {
		table = blueprint.GetTable()
	}
	if query.IsExpression(table) {
		return stringOf(table)
	}

	tablePrefix := g.conn.GetTablePrefix()
	if len(prefix) > 0 {
		tablePrefix = prefix[0]
	}

	name := stringOf(table)
	if i := aliasIndex(name); i >= 0 {
		return g.WrapTable(strings.TrimSpace(name[:i]), tablePrefix) + " as " +
			g.self.wrapValue(tablePrefix+strings.TrimSpace(name[i+4:]))
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		return g.wrapSegments(name[:i] + "." + tablePrefix + name[i+1:])
	}
	return g.self.wrapValue(tablePrefix + name)
}

// WrapArray answers Grammar::wrapArray.
func (g *BaseGrammar) WrapArray(values []any) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = g.Wrap(value)
	}
	return out
}

// Columnize answers Grammar::columnize.
func (g *BaseGrammar) Columnize(columns []any) string {
	return strings.Join(g.WrapArray(columns), ", ")
}

// QuoteString answers Grammar::quoteString.
func (g *BaseGrammar) QuoteString(value any) string {
	switch v := value.(type) {
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = quote(item)
		}
		return strings.Join(out, ", ")
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = quote(stringOf(item))
		}
		return strings.Join(out, ", ")
	default:
		return quote(stringOf(value))
	}
}

// PrefixArray answers Grammar::prefixArray.
func (g *BaseGrammar) PrefixArray(prefix string, values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = prefix + " " + value
	}
	return out
}

// GetDefaultValue answers Grammar::getDefaultValue: a value formatted so it can
// stand in a "default" clause.
func (g *BaseGrammar) GetDefaultValue(value any) string {
	if query.IsExpression(value) {
		return stringOf(value)
	}
	if b, ok := value.(bool); ok {
		if b {
			return "'1'"
		}
		return "'0'"
	}
	return quote(stringOf(value))
}

// getColumns answers Grammar::getColumns: the blueprint's added columns, each
// compiled to its definition.
func (g *BaseGrammar) getColumns(blueprint *schema.Blueprint) ([]string, error) {
	added := blueprint.GetAddedColumns()
	columns := make([]string, 0, len(added))
	for _, column := range added {
		sql, err := g.getColumn(blueprint, column)
		if err != nil {
			return nil, err
		}
		columns = append(columns, sql)
	}
	return columns, nil
}

// getColumn answers Grammar::getColumn.
func (g *BaseGrammar) getColumn(blueprint *schema.Blueprint, column *schema.ColumnDefinition) (string, error) {
	typ, err := g.self.typeOf(column)
	if err != nil {
		return "", err
	}
	return g.addModifiers(g.Wrap(column)+" "+typ, blueprint, column)
}

// addModifiers answers Grammar::addModifiers.
func (g *BaseGrammar) addModifiers(sql string, blueprint *schema.Blueprint, column *schema.ColumnDefinition) (string, error) {
	for _, modifier := range g.modifiers {
		fragments, err := g.self.modify(modifier, blueprint, column)
		if err != nil {
			return "", err
		}
		for _, fragment := range fragments {
			sql += fragment
		}
	}
	return sql, nil
}

// getCommandByName answers Grammar::getCommandByName.
func (g *BaseGrammar) getCommandByName(blueprint *schema.Blueprint, name string) *schema.Command {
	for _, command := range blueprint.GetCommands() {
		if command.Name == name {
			return command
		}
	}
	return nil
}

// getCommandsByName answers Grammar::getCommandsByName.
func (g *BaseGrammar) getCommandsByName(blueprint *schema.Blueprint, name string) []*schema.Command {
	var commands []*schema.Command
	for _, command := range blueprint.GetCommands() {
		if command.Name == name {
			commands = append(commands, command)
		}
	}
	return commands
}

// hasCommand answers Grammar::hasCommand.
func (g *BaseGrammar) hasCommand(blueprint *schema.Blueprint, name string) bool {
	return g.getCommandByName(blueprint, name) != nil
}

// isSerial reports whether the column's type is one the driver can make
// auto-incrementing, which is Grammar::$serials.
func (g *BaseGrammar) isSerial(column *schema.ColumnDefinition) bool {
	for _, serial := range g.serials {
		if serial == column.GetType() {
			return true
		}
	}
	return false
}

// CompileCreateDatabase answers Grammar::compileCreateDatabase.
func (g *BaseGrammar) CompileCreateDatabase(name string) (string, error) {
	return "create database " + g.self.wrapValue(name), nil
}

// CompileDropDatabaseIfExists answers Grammar::compileDropDatabaseIfExists.
func (g *BaseGrammar) CompileDropDatabaseIfExists(name string) (string, error) {
	return "drop database if exists " + g.self.wrapValue(name), nil
}

// CompileSchemas answers Grammar::compileSchemas.
func (g *BaseGrammar) CompileSchemas() (string, error) {
	return "", unsupported("retrieving schemas")
}

// CompileTableExists answers Grammar::compileTableExists. The base answers with
// nothing, which is the PHP's null.
func (g *BaseGrammar) CompileTableExists(schemaName, table string) string { return "" }

// CompileTables answers Grammar::compileTables.
func (g *BaseGrammar) CompileTables(schemas []string, withSize ...bool) (string, error) {
	return "", unsupported("retrieving tables")
}

// CompileViews answers Grammar::compileViews.
func (g *BaseGrammar) CompileViews(schemas []string) (string, error) {
	return "", unsupported("retrieving views")
}

// CompileTypes answers Grammar::compileTypes.
func (g *BaseGrammar) CompileTypes(schemas []string) (string, error) {
	return "", unsupported("retrieving user-defined types")
}

// CompileColumns answers Grammar::compileColumns.
func (g *BaseGrammar) CompileColumns(schemaName, table string) (string, error) {
	return "", unsupported("retrieving columns")
}

// CompileIndexes answers Grammar::compileIndexes.
func (g *BaseGrammar) CompileIndexes(schemaName, table string) (string, error) {
	return "", unsupported("retrieving indexes")
}

// CompileForeignKeys answers Grammar::compileForeignKeys.
func (g *BaseGrammar) CompileForeignKeys(schemaName, table string) (string, error) {
	return "", unsupported("retrieving foreign keys")
}

// CompileRenameColumn answers Grammar::compileRenameColumn.
func (g *BaseGrammar) CompileRenameColumn(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return one(fmt.Sprintf("alter table %s rename column %s to %s",
		g.WrapTable(blueprint), g.Wrap(command.From), g.Wrap(command.To))), nil
}

// CompileChange answers Grammar::compileChange.
func (g *BaseGrammar) CompileChange(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("modifying columns")
}

// CompileAlter answers the SQLite table rebuild. No other driver emits an alter
// command, so the base compiles nothing rather than refusing.
func (g *BaseGrammar) CompileAlter(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileFullText answers Grammar::compileFulltext.
func (g *BaseGrammar) CompileFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("fulltext index creation")
}

// CompileDropFullText answers Grammar::compileDropFullText.
func (g *BaseGrammar) CompileDropFullText(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("fulltext index removal")
}

// CompileVectorIndex answers Grammar::compileVectorIndex.
func (g *BaseGrammar) CompileVectorIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("vector indexes")
}

// CompileForeign answers Grammar::compileForeign.
func (g *BaseGrammar) CompileForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	sql := fmt.Sprintf("alter table %s add constraint %s ",
		g.WrapTable(blueprint), g.Wrap(command.Index))

	sql += fmt.Sprintf("foreign key (%s) references %s (%s)",
		g.Columnize(command.Columns),
		g.WrapTable(command.On),
		g.Columnize(command.References))

	if command.OnDelete != "" {
		sql += " on delete " + command.OnDelete
	}
	if command.OnUpdate != "" {
		sql += " on update " + command.OnUpdate
	}

	return one(sql), nil
}

// CompileDropForeign answers Grammar::compileDropForeign.
func (g *BaseGrammar) CompileDropForeign(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("dropping foreign keys")
}

// CompileComment answers the fluent column comment command. Only Postgres
// declares it, so the base compiles nothing.
func (g *BaseGrammar) CompileComment(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileAutoIncrementStartingValues answers
// Grammar::compileAutoIncrementStartingValues. Only MySQL and Postgres declare
// it as a fluent command, so the base compiles nothing.
func (g *BaseGrammar) CompileAutoIncrementStartingValues(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, nil
}

// CompileDropAllTables answers Grammar::compileDropAllTables.
func (g *BaseGrammar) CompileDropAllTables(tables []string) (string, error) {
	return "", unsupported("dropping all tables")
}

// CompileDropAllViews answers Grammar::compileDropAllViews.
func (g *BaseGrammar) CompileDropAllViews(views []string) (string, error) {
	return "", unsupported("dropping all views")
}

// CompileDropAllTypes answers Grammar::compileDropAllTypes.
func (g *BaseGrammar) CompileDropAllTypes(types []string) (string, error) {
	return "", unsupported("dropping all types")
}

// CompileSpatialIndex answers Grammar::compileSpatialIndex.
func (g *BaseGrammar) CompileSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("spatial indexes")
}

// CompileDropSpatialIndex answers Grammar::compileDropSpatialIndex.
func (g *BaseGrammar) CompileDropSpatialIndex(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("spatial indexes")
}

// CompileTableComment answers Grammar::compileTableComment.
func (g *BaseGrammar) CompileTableComment(blueprint *schema.Blueprint, command *schema.Command) ([]string, error) {
	return nil, unsupported("table comments")
}

// wrapValue answers Grammar::wrapValue with the SQL standard double quote. A
// quote inside the identifier is doubled rather than stripped, because
// stripping it would silently rename the column.
func (g *BaseGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func (g *BaseGrammar) wrapSegments(name string) string {
	segments := strings.Split(name, ".")
	out := make([]string, len(segments))
	for i, segment := range segments {
		out[i] = g.self.wrapValue(segment)
	}
	return strings.Join(out, ".")
}

// typeRaw answers Grammar::typeRaw.
func typeRaw(column *schema.ColumnDefinition) string { return column.GetDefinition() }

// one wraps a single statement in the list every compiler returns.
func one(sql string) []string { return []string{sql} }

// unsupported is the error a driver answers where PHP throws RuntimeException.
func unsupported(what string) error {
	return fmt.Errorf("%w: %s", schema.ErrUnsupported, what)
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// addSlashes answers PHP's addslashes, which MySQL's comment modifier uses in
// place of doubling the quote. It escapes the backslash first, so that an
// escape it added is not escaped again.
func addSlashes(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "'", `\'`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return strings.ReplaceAll(value, "\x00", `\0`)
}

// stringOf renders the string, expression or number a grammar is handed.
func stringOf(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case query.Expression:
		return v.String()
	case *query.Expression:
		return v.String()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}

func aliasIndex(name string) int {
	return strings.Index(strings.ToLower(name), " as ")
}

// atLeast reports whether the connection's server version is at or past want,
// comparing the way PHP's version_compare does for the dotted forms these
// servers report.
func atLeast(version, want string) bool {
	return compareVersions(version, want) >= 0
}

func compareVersions(a, b string) int {
	as, bs := strings.Split(numericPrefix(a), "."), strings.Split(numericPrefix(b), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			fmt.Sscanf(as[i], "%d", &x)
		}
		if i < len(bs) {
			fmt.Sscanf(bs[i], "%d", &y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// numericPrefix keeps the leading digits and dots of a version string, so that
// "10.5.2-MariaDB" compares as 10.5.2.
func numericPrefix(version string) string {
	for i := 0; i < len(version); i++ {
		if (version[i] < '0' || version[i] > '9') && version[i] != '.' {
			return version[:i]
		}
	}
	return version
}

// intOr reads an optional int attribute, answering fallback where PHP would see
// null.
func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// isNullable reads $column->nullable as PHP's truth test does: unset is false.
func isNullable(column *schema.ColumnDefinition) bool {
	nullable := column.GetNullable()
	return nullable != nil && *nullable
}

// isGenerated reports whether the column is computed, in which case the
// nullable modifier is written differently or not at all.
func isGenerated(column *schema.ColumnDefinition) bool {
	return column.GetVirtualAs() != nil || column.GetVirtualAsJSON() != "" ||
		column.GetStoredAs() != nil || column.GetStoredAsJSON() != ""
}

// startingValue answers $column->get('startingValue', $column->get('from')).
func startingValue(column *schema.ColumnDefinition) int {
	if value := column.GetStartingValue(); value != nil {
		return *value
	}
	if value := column.GetFrom(); value != nil {
		return *value
	}
	return 0
}

// wrapJSONFieldAndPath answers CompilesJsonPaths::wrapJsonFieldAndPath.
func wrapJSONFieldAndPath(column string, wrap func(string) string) (field, path string) {
	parts := strings.SplitN(column, "->", 2)
	field = wrap(parts[0])
	if len(parts) > 1 {
		path = ", " + wrapJSONPath(parts[1])
	}
	return field, path
}

// wrapJSONPath answers CompilesJsonPaths::wrapJsonPath.
func wrapJSONPath(value string) string {
	value = strings.ReplaceAll(value, `\'`, "''")
	value = strings.ReplaceAll(value, "'", "''")

	segments := strings.Split(value, "->")
	for i, segment := range segments {
		segments[i] = wrapJSONPathSegment(segment)
	}
	jsonPath := strings.Join(segments, ".")

	if strings.HasPrefix(jsonPath, "[") {
		return "'$" + jsonPath + "'"
	}
	return "'$." + jsonPath + "'"
}

// wrapJSONPathSegment answers CompilesJsonPaths::wrapJsonPathSegment.
func wrapJSONPathSegment(segment string) string {
	if i := strings.Index(segment, "["); i >= 0 && strings.HasSuffix(segment, "]") {
		key, brackets := segment[:i], segment[i:]
		if key != "" {
			return `"` + key + `"` + brackets
		}
		return brackets
	}
	return `"` + segment + `"`
}
