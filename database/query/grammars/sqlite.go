package grammars

import (
	"strings"

	"github.com/arandu-io/hesape/database/query"
)

// SQLiteGrammar answers Illuminate\Database\Query\Grammars\SQLiteGrammar.
//
// What it changes about standard SQL: there is no row lock at all, so the lock
// clause compiles to nothing; a union subquery has to be wrapped in a select
// because SQLite will not parenthesise one; the date parts come out of
// strftime; an upsert is ON CONFLICT DO UPDATE; the JSON operators are the
// json_* functions; and an update or delete narrowed by a limit goes through
// rowid.
//
// The right join is not on that list. SQLite has had one since 3.39, and the
// Illuminate grammar has no branch for it either way.
type SQLiteGrammar struct {
	*Grammar
}

var _ query.Grammar = (*SQLiteGrammar)(nil)

// NewSQLiteGrammar answers `new SQLiteGrammar`.
func NewSQLiteGrammar() *SQLiteGrammar {
	g := &SQLiteGrammar{Grammar: NewGrammar()}
	g.Grammar.self = g
	return g
}

// sqliteOperators answers SQLiteGrammar::$operators.
var sqliteOperators = []string{
	"=", "<", ">", "<=", ">=", "<>", "!=",
	"like", "not like", "ilike",
	"&", "|", "<<", ">>",
}

// GetOperators answers SQLiteGrammar::$operators.
func (g *SQLiteGrammar) GetOperators() []string {
	return append(g.Grammar.GetOperators(), sqliteOperators...)
}

// CompileLock answers SQLiteGrammar::compileLock: SQLite locks the file, not
// the row, so there is nothing to say.
func (g *SQLiteGrammar) CompileLock(q *query.Builder, value any) string { return "" }

// WrapUnion answers SQLiteGrammar::wrapUnion: SQLite does not accept a
// parenthesised select as a union operand, so it gets a select around it
// instead.
func (g *SQLiteGrammar) WrapUnion(sql string) string {
	return "select * from (" + sql + ")"
}

// WhereBasic answers SQLiteGrammar::whereBasic: the null safe equality is
// spelled IS.
func (g *SQLiteGrammar) WhereBasic(q *query.Builder, where query.Where) string {
	if where.Operator == "<=>" {
		d := g.self
		return d.Wrap(where.Column) + " IS " + d.Parameter(where.Value)
	}
	return g.Grammar.WhereBasic(q, where)
}

// WhereLike answers SQLiteGrammar::whereLike.
//
// SQLite's like is case insensitive and has no case sensitive form, so the
// case sensitive question is answered with glob -- which is case sensitive and
// uses different wildcards. PrepareWhereLikeBinding translates the pattern.
func (g *SQLiteGrammar) WhereLike(q *query.Builder, where query.Where) string {
	if !where.CaseSensitive {
		return g.Grammar.WhereLike(q, where)
	}

	where.Operator = "glob"
	if where.Not {
		where.Operator = "not glob"
	}

	return g.self.WhereBasic(q, where)
}

// PrepareWhereLikeBinding answers SQLiteGrammar::prepareWhereLikeBinding: a
// like pattern rewritten as a glob pattern, since the two spell their wildcards
// the other way round.
func (g *SQLiteGrammar) PrepareWhereLikeBinding(value string, caseSensitive bool) string {
	if !caseSensitive {
		return value
	}
	return strings.NewReplacer("*", "[*]", "?", "[?]", "%", "*", "_", "?").Replace(value)
}

// WhereDate answers SQLiteGrammar::whereDate.
func (g *SQLiteGrammar) WhereDate(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("%Y-%m-%d", q, where)
}

// WhereDay answers SQLiteGrammar::whereDay.
func (g *SQLiteGrammar) WhereDay(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("%d", q, where)
}

// WhereMonth answers SQLiteGrammar::whereMonth.
func (g *SQLiteGrammar) WhereMonth(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("%m", q, where)
}

// WhereYear answers SQLiteGrammar::whereYear.
func (g *SQLiteGrammar) WhereYear(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("%Y", q, where)
}

// WhereTime answers SQLiteGrammar::whereTime.
func (g *SQLiteGrammar) WhereTime(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("%H:%M:%S", q, where)
}

// DateBasedWhere answers SQLiteGrammar::dateBasedWhere. SQLite has no date
// type, so both sides are compared as text.
func (g *SQLiteGrammar) DateBasedWhere(typ string, q *query.Builder, where query.Where) string {
	d := g.self
	return "strftime('" + typ + "', " + d.Wrap(where.Column) + ") " + where.Operator +
		" cast(" + d.Parameter(where.Value) + " as text)"
}

// CompileIndexHint answers SQLiteGrammar::compileIndexHint: SQLite has one form
// of hint and it is a demand, so anything but a forced index compiles to
// nothing.
//
// An index name that is not a bare identifier is dropped rather than
// interpolated; see MySQLGrammar.CompileIndexHint for why that is a string and
// not an error.
func (g *SQLiteGrammar) CompileIndexHint(q *query.Builder, indexHint *query.IndexHint) string {
	if indexHint.Type != "force" {
		return ""
	}
	if !indexName.MatchString(indexHint.Index) {
		return ""
	}
	return "indexed by " + indexHint.Index
}

// CompileJSONLength answers SQLiteGrammar::compileJsonLength.
func (g *SQLiteGrammar) CompileJSONLength(column any, operator, value string) (string, error) {
	field, path := g.wrapJSONFieldAndPath(column)
	return "json_array_length(" + field + path + ") " + operator + " " + value, nil
}

// CompileJSONContains answers SQLiteGrammar::compileJsonContains: SQLite has no
// containment operator, so the array is walked.
func (g *SQLiteGrammar) CompileJSONContains(column any, value string) (string, error) {
	field, path := g.wrapJSONFieldAndPath(column)
	return "exists (select 1 from json_each(" + field + path + ") where " +
		g.self.Wrap("json_each.value") + " is " + value + ")", nil
}

// PrepareBindingForJSONContains answers
// SQLiteGrammar::prepareBindingForJsonContains: the value is compared against
// one element, so it stays a scalar rather than becoming JSON text.
func (g *SQLiteGrammar) PrepareBindingForJSONContains(binding any) (any, error) {
	return binding, nil
}

// CompileJSONContainsKey answers SQLiteGrammar::compileJsonContainsKey.
func (g *SQLiteGrammar) CompileJSONContainsKey(column any) (string, error) {
	field, path := g.wrapJSONFieldAndPath(column)
	return "json_type(" + field + path + ") is not null", nil
}

// CompileGroupLimit answers SQLiteGrammar::compileGroupLimit.
//
// Window functions arrived in SQLite 3.25.0. Before that the PHP drops the
// group limit and compiles an ordinary select -- every row of every group, and
// the caller cuts them itself. A connection that cannot report a version is
// taken to be current.
func (g *SQLiteGrammar) CompileGroupLimit(q *query.Builder) string {
	if version, ok := serverVersion(q.GetConnection()); ok && versionLess(version, "3.25.0") {
		return g.compileSelectWithoutGroupLimit(q)
	}
	return g.Grammar.CompileGroupLimit(q)
}

// CompileUpdate answers SQLiteGrammar::compileUpdate.
func (g *SQLiteGrammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	if len(q.Joins) > 0 || q.GetLimit() != nil {
		return g.compileUpdateWithJoinsOrLimit(q, values)
	}
	return g.Grammar.CompileUpdate(q, values)
}

// CompileInsertOrIgnore answers SQLiteGrammar::compileInsertOrIgnore.
func (g *SQLiteGrammar) CompileInsertOrIgnore(q *query.Builder, values []map[string]any) string {
	return strings.Replace(g.self.CompileInsert(q, values), "insert", "insert or ignore", 1)
}

// CompileInsertOrIgnoreReturning answers
// SQLiteGrammar::compileInsertOrIgnoreReturning.
func (g *SQLiteGrammar) CompileInsertOrIgnoreReturning(q *query.Builder, values []map[string]any, uniqueBy, returning []string) (string, error) {
	d := g.self
	return d.CompileInsert(q, values) +
		" on conflict (" + d.Columnize(toAny(uniqueBy)) + ") do nothing" +
		" returning " + d.Columnize(toAny(returning)), nil
}

// CompileInsertOrIgnoreUsing answers SQLiteGrammar::compileInsertOrIgnoreUsing.
func (g *SQLiteGrammar) CompileInsertOrIgnoreUsing(q *query.Builder, columns []any, sql string) (string, error) {
	return strings.Replace(g.self.CompileInsertUsing(q, columns, sql), "insert", "insert or ignore", 1), nil
}

// CompileUpdateColumns answers SQLiteGrammar::compileUpdateColumns.
//
// SQLite cannot set one path of a document at a time, so every path written
// into the same column is collected and applied as one patch.
func (g *SQLiteGrammar) CompileUpdateColumns(q *query.Builder, values map[string]any) string {
	d := g.self
	groups := g.groupJSONColumnsForUpdate(values)

	merged := make(map[string]any, len(values))
	for key, value := range values {
		if isJSONSelector(key) {
			continue
		}
		merged[key] = value
	}
	for key, value := range groups {
		merged[key] = value
	}

	parts := make([]string, 0, len(merged))
	for _, key := range sortedKeys(merged) {
		column := lastSegment(key, ".")

		if _, patched := groups[key]; patched {
			parts = append(parts, d.Wrap(column)+" = "+g.compileJSONPatch(column, merged[key]))
			continue
		}

		parts = append(parts, d.Wrap(column)+" = "+d.Parameter(merged[key]))
	}

	return strings.Join(parts, ", ")
}

// CompileUpsert answers SQLiteGrammar::compileUpsert.
func (g *SQLiteGrammar) CompileUpsert(q *query.Builder, values []map[string]any, uniqueBy []string, update []string) string {
	d := g.self

	sql := d.CompileInsert(q, values) + " on conflict (" + d.Columnize(toAny(uniqueBy)) + ") do update set "

	columns := make([]string, 0, len(update))
	for _, column := range update {
		columns = append(columns, d.Wrap(column)+" = "+d.WrapValue("excluded")+"."+d.Wrap(column))
	}

	return sql + strings.Join(columns, ", ")
}

// groupJSONColumnsForUpdate answers
// SQLiteGrammar::groupJsonColumnsForUpdate: every JSON path in the update,
// rebuilt as the nested document it describes, keyed by its column.
func (g *SQLiteGrammar) groupJSONColumnsForUpdate(values map[string]any) map[string]any {
	groups := make(map[string]any)

	for _, key := range sortedKeys(values) {
		if !isJSONSelector(key) {
			continue
		}
		path := strings.ReplaceAll(after(key, "."), "->", ".")
		setPath(groups, strings.Split(path, "."), values[key])
	}

	return groups
}

// compileJSONPatch answers SQLiteGrammar::compileJsonPatch.
func (g *SQLiteGrammar) compileJSONPatch(column string, value any) string {
	d := g.self
	return "json_patch(ifnull(" + d.Wrap(column) + ", json('{}')), json(" + d.Parameter(value) + "))"
}

// compileUpdateWithJoinsOrLimit answers
// SQLiteGrammar::compileUpdateWithJoinsOrLimit.
//
// SQLite builds without UPDATE FROM or a limit on an update, so the rows are
// named by rowid and chosen by a select that can have both. The select is
// compiled from a clone, where the PHP adds the column to the caller's builder.
func (g *SQLiteGrammar) compileUpdateWithJoinsOrLimit(q *query.Builder, values map[string]any) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())
	columns := d.CompileUpdateColumns(q, values)

	alias := lastAliasSegment(text(q.GetFrom()))
	selectSQL := d.CompileSelect(q.Clone().Select(alias + ".rowid"))

	return "update " + table + " set " + columns + " where " + d.Wrap("rowid") + " in (" + selectSQL + ")"
}

// PrepareBindingsForUpdate answers SQLiteGrammar::prepareBindingsForUpdate: the
// JSON paths are bound as the one patch document they were compiled into.
func (g *SQLiteGrammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	groups := g.groupJSONColumnsForUpdate(values)

	merged := make(map[string]any, len(values))
	for key, value := range values {
		if isJSONSelector(key) {
			continue
		}
		merged[key] = value
	}
	for key, value := range groups {
		merged[key] = value
	}

	out := make([]any, 0, len(merged)+8)
	for _, key := range sortedKeys(merged) {
		value := merged[key]
		if isStructured(value) {
			if encoded, err := encodeJSON(value); err == nil {
				value = encoded
			}
		}
		out = append(out, value)
	}

	for _, key := range bindingOrder {
		if key == "select" {
			continue
		}
		out = append(out, bindings[key]...)
	}

	return out
}

// CompileDelete answers SQLiteGrammar::compileDelete.
func (g *SQLiteGrammar) CompileDelete(q *query.Builder) string {
	if len(q.Joins) > 0 || q.GetLimit() != nil {
		return g.compileDeleteWithJoinsOrLimit(q)
	}
	return g.Grammar.CompileDelete(q)
}

// compileDeleteWithJoinsOrLimit answers
// SQLiteGrammar::compileDeleteWithJoinsOrLimit.
func (g *SQLiteGrammar) compileDeleteWithJoinsOrLimit(q *query.Builder) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())

	alias := lastAliasSegment(text(q.GetFrom()))
	selectSQL := d.CompileSelect(q.Clone().Select(alias + ".rowid"))

	return "delete from " + table + " where " + d.Wrap("rowid") + " in (" + selectSQL + ")"
}

// CompileTruncate answers SQLiteGrammar::compileTruncate.
//
// SQLite has no truncate: the rows are deleted and the auto increment counter
// is reset by hand. Both statements are returned, and the order they run in
// does not matter -- one clears the table, the other clears the counter, and
// neither depends on the other.
//
// The PHP asks the schema builder to split the schema off the table name; the
// split is done here, because a query grammar reaching into a schema builder to
// read a string is a dependency for nothing.
func (g *SQLiteGrammar) CompileTruncate(q *query.Builder) map[string][]any {
	d := g.self
	from := text(q.GetFrom())

	schema, table := "", from
	if i := strings.Index(from, "."); i >= 0 {
		schema, table = from[:i], from[i+1:]
	}
	if schema != "" {
		schema = d.WrapValue(schema) + "."
	}

	return map[string][]any{
		"delete from " + schema + "sqlite_sequence where name = ?": {d.GetTablePrefix() + table},
		"delete from " + d.WrapTable(q.GetFrom()):                  {},
	}
}

// WrapJSONSelector answers SQLiteGrammar::wrapJsonSelector.
func (g *SQLiteGrammar) WrapJSONSelector(value string) string {
	field, path := g.wrapJSONFieldAndPath(value)
	return "json_extract(" + field + path + ")"
}

// after answers Str::after: what follows the first occurrence of the separator,
// or the whole string when there is none.
func after(value, separator string) string {
	if i := strings.Index(value, separator); i >= 0 {
		return value[i+len(separator):]
	}
	return value
}

// setPath answers Arr::set with a dotted key: it walks the nested maps into
// existence and writes the value at the end of the path.
func setPath(target map[string]any, path []string, value any) {
	for i, key := range path {
		if i == len(path)-1 {
			target[key] = value
			return
		}

		next, ok := target[key].(map[string]any)
		if !ok {
			next = make(map[string]any)
			target[key] = next
		}
		target = next
	}
}
