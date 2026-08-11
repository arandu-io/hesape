package grammars

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/arandu-io/hesape/database/query"
)

// PostgresGrammar answers Illuminate\Database\Query\Grammars\PostgresGrammar.
//
// What it changes about standard SQL: a like is compared against the column
// cast to text, a bitwise comparison is cast to bool, a date comparison is an
// extract, distinct takes columns, an upsert is ON CONFLICT DO UPDATE, an
// insert can return the identifier it assigned, an update or delete narrowed by
// a limit goes through ctid, and the JSON operators are -> and ->> rather than
// functions.
//
// Placeholders stay "?" here, as they do in every grammar. See Grammar.Parameter
// for where the $1, $2 numbering happens and why it happens there.
type PostgresGrammar struct {
	*Grammar
}

var _ query.Grammar = (*PostgresGrammar)(nil)

// NewPostgresGrammar answers `new PostgresGrammar`.
func NewPostgresGrammar() *PostgresGrammar {
	g := &PostgresGrammar{Grammar: NewGrammar()}
	g.Grammar.self = g
	return g
}

// postgresOperators answers PostgresGrammar::$operators.
var postgresOperators = []string{
	"=", "<", ">", "<=", ">=", "<>", "!=",
	"like", "not like", "between", "ilike", "not ilike",
	"~", "&", "|", "#", "<<", ">>", "<<=", ">>=",
	"&&", "@>", "<@", "?", "?|", "?&", "||", "-", "@?", "@@", "#-",
	"is distinct from", "is not distinct from",
}

// postgresBitwiseOperators answers PostgresGrammar::$bitwiseOperators.
var postgresBitwiseOperators = []string{"~", "&", "|", "#", "<<", ">>", "<<=", ">>="}

// The two pieces of state the PHP keeps in static properties. They are process
// wide there and process wide here, and a grammar is read from every request,
// so both are guarded: a test that flips one while another goroutine compiles a
// statement would otherwise be a data race rather than a surprise.
var (
	customOperatorsMu sync.RWMutex
	customOperators   []string

	cascadeTruncate = newAtomicTrue()
)

func newAtomicTrue() *atomic.Bool {
	value := &atomic.Bool{}
	value.Store(true)
	return value
}

// CustomOperators answers PostgresGrammar::customOperators: operators an
// extension adds, which Builder has to accept before it will compile them.
func CustomOperators(operators []string) {
	customOperatorsMu.Lock()
	defer customOperatorsMu.Unlock()

	for _, operator := range operators {
		if operator != "" {
			customOperators = append(customOperators, operator)
		}
	}
}

// CascadeOnTruncate answers PostgresGrammar::cascadeOnTruncate: whether
// truncating a table also truncates the tables whose foreign keys point at it.
func CascadeOnTruncate(value bool) { cascadeTruncate.Store(value) }

// CascadeOnTrucate answers PostgresGrammar::cascadeOnTrucate.
//
// Deprecated: the name is missing a letter in Illuminate too. Use
// CascadeOnTruncate.
func CascadeOnTrucate(value bool) { CascadeOnTruncate(value) }

// GetOperators answers PostgresGrammar::getOperators.
func (g *PostgresGrammar) GetOperators() []string {
	customOperatorsMu.RLock()
	defer customOperatorsMu.RUnlock()

	out := append(g.Grammar.GetOperators(), postgresOperators...)
	out = append(out, customOperators...)

	slices.Sort(out)
	return slices.Compact(out)
}

// GetBitwiseOperators answers PostgresGrammar::$bitwiseOperators.
func (g *PostgresGrammar) GetBitwiseOperators() []string {
	return append(g.Grammar.GetBitwiseOperators(), postgresBitwiseOperators...)
}

// WhereBasic answers PostgresGrammar::whereBasic.
//
// A like against a non-text column is an error in Postgres and a silent cast
// everywhere else, so the column is cast rather than the comparison refused.
func (g *PostgresGrammar) WhereBasic(q *query.Builder, where query.Where) string {
	d := g.self
	if strings.Contains(strings.ToLower(where.Operator), "like") {
		return d.Wrap(where.Column) + "::text " + where.Operator + " " + d.Parameter(where.Value)
	}
	return g.Grammar.WhereBasic(q, where)
}

// WhereBitwise answers PostgresGrammar::whereBitwise: the result of a bitwise
// operator is a number, and a where wants a boolean.
func (g *PostgresGrammar) WhereBitwise(q *query.Builder, where query.Where) string {
	d := g.self
	value := d.Parameter(where.Value)
	operator := strings.ReplaceAll(where.Operator, "?", "??")

	return "(" + d.Wrap(where.Column) + " " + operator + " " + value + ")::bool"
}

// WhereLike answers PostgresGrammar::whereLike: Postgres is the one engine
// whose like is case sensitive, so the insensitive form is the ilike.
func (g *PostgresGrammar) WhereLike(q *query.Builder, where query.Where) string {
	operator := ""
	if where.Not {
		operator = "not "
	}
	if where.CaseSensitive {
		operator += "like"
	} else {
		operator += "ilike"
	}

	where.Operator = operator

	return g.self.WhereBasic(q, where)
}

// WhereDate answers PostgresGrammar::whereDate.
func (g *PostgresGrammar) WhereDate(q *query.Builder, where query.Where) string {
	d := g.self
	column := d.Wrap(where.Column)
	if isJSONSelector(where.Column) {
		column = "(" + column + ")"
	}
	return column + "::date " + where.Operator + " " + d.Parameter(where.Value)
}

// WhereTime answers PostgresGrammar::whereTime.
func (g *PostgresGrammar) WhereTime(q *query.Builder, where query.Where) string {
	d := g.self
	column := d.Wrap(where.Column)
	if isJSONSelector(where.Column) {
		column = "(" + column + ")"
	}
	return column + "::time " + where.Operator + " " + d.Parameter(where.Value)
}

// DateBasedWhere answers PostgresGrammar::dateBasedWhere.
func (g *PostgresGrammar) DateBasedWhere(typ string, q *query.Builder, where query.Where) string {
	d := g.self
	return "extract(" + typ + " from " + d.Wrap(where.Column) + ") " + where.Operator + " " + d.Parameter(where.Value)
}

// fullTextLanguages answers PostgresGrammar::validFullTextLanguages.
var fullTextLanguages = []string{
	"simple", "arabic", "danish", "dutch", "english", "finnish", "french",
	"german", "hungarian", "indonesian", "irish", "italian", "lithuanian",
	"nepali", "norwegian", "portuguese", "romanian", "russian", "spanish",
	"swedish", "tamil", "turkish",
}

// WhereFullText answers PostgresGrammar::whereFullText.
//
// The language is written into the statement, so it is checked against the list
// of the ones Postgres ships and anything else falls back to english -- the
// same guard the PHP has, and the reason the value is not simply quoted.
func (g *PostgresGrammar) WhereFullText(q *query.Builder, where query.Where) (string, error) {
	d := g.self

	language := text(where.Options["language"])
	if !slices.Contains(fullTextLanguages, language) {
		language = "english"
	}

	isVector := truthy(where.Options["vector"])

	parts := make([]string, 0, len(where.Columns))
	for _, column := range where.Columns {
		if isVector {
			parts = append(parts, d.Wrap(column))
			continue
		}
		parts = append(parts, "to_tsvector('"+language+"', "+d.Wrap(column)+")")
	}
	columns := strings.Join(parts, " || ")

	mode := "plainto_tsquery"
	switch text(where.Options["mode"]) {
	case "phrase":
		mode = "phraseto_tsquery"
	case "websearch":
		mode = "websearch_to_tsquery"
	case "raw":
		mode = "to_tsquery"
	}

	return "(" + columns + ") @@ " + mode + "('" + language + "', " + d.Parameter(where.Value) + ")", nil
}

// CompileColumns answers PostgresGrammar::compileColumns: only Postgres takes
// columns for its distinct.
func (g *PostgresGrammar) CompileColumns(q *query.Builder, columns []any) string {
	d := g.self

	if q.GetAggregate() != nil {
		return ""
	}

	switch distinct := q.GetDistinct().(type) {
	case []any:
		return "select distinct on (" + d.Columnize(distinct) + ") " + d.Columnize(columns)
	case bool:
		if distinct {
			return "select distinct " + d.Columnize(columns)
		}
	}

	return "select " + d.Columnize(columns)
}

// CompileJSONContains answers PostgresGrammar::compileJsonContains.
func (g *PostgresGrammar) CompileJSONContains(column any, value string) (string, error) {
	wrapped := strings.ReplaceAll(g.self.Wrap(column), "->>", "->")
	return "(" + wrapped + ")::jsonb @> " + value, nil
}

// jsonArrayIndex is the PHP's /\[(-?[0-9]+)\]$/.
var jsonArrayIndex = regexp.MustCompile(`\[(-?[0-9]+)\]$`)

// CompileJSONContainsKey answers PostgresGrammar::compileJsonContainsKey.
//
// An index into an array is a different question from a key in an object, so a
// path ending in one compiles to a length test instead. A negative index counts
// from the end, which is why it is compared by absolute value.
func (g *PostgresGrammar) CompileJSONContainsKey(column any) (string, error) {
	segments := strings.Split(text(column), "->")
	lastSegment := segments[len(segments)-1]
	segments = segments[:len(segments)-1]

	index, indexed := 0, false

	if number, err := strconv.Atoi(lastSegment); err == nil {
		index, indexed = number, true
	} else if matches := jsonArrayIndex.FindStringSubmatch(lastSegment); matches != nil {
		segments = append(segments, strings.TrimSuffix(lastSegment, matches[0]))
		index, _ = strconv.Atoi(matches[1])
		indexed = true
	}

	wrapped := strings.ReplaceAll(g.self.Wrap(strings.Join(segments, "->")), "->>", "->")

	if indexed {
		length := index + 1
		if index < 0 {
			length = -index
		}
		return "case when jsonb_typeof((" + wrapped + ")::jsonb) = 'array' then " +
			"jsonb_array_length((" + wrapped + ")::jsonb) >= " + strconv.Itoa(length) +
			" else false end", nil
	}

	key := "'" + strings.ReplaceAll(lastSegment, "'", "''") + "'"

	return "coalesce((" + wrapped + ")::jsonb ?? " + key + ", false)", nil
}

// CompileJSONLength answers PostgresGrammar::compileJsonLength.
func (g *PostgresGrammar) CompileJSONLength(column any, operator, value string) (string, error) {
	wrapped := strings.ReplaceAll(g.self.Wrap(column), "->>", "->")
	return "jsonb_array_length((" + wrapped + ")::jsonb) " + operator + " " + value, nil
}

// CompileHaving answers PostgresGrammar::compileHaving.
func (g *PostgresGrammar) CompileHaving(having query.Having) string {
	if strings.EqualFold(having.Type, "Bitwise") {
		return g.CompileHavingBitwise(having)
	}
	return g.Grammar.CompileHaving(having)
}

// CompileHavingBitwise answers PostgresGrammar::compileHavingBitwise.
func (g *PostgresGrammar) CompileHavingBitwise(having query.Having) string {
	d := g.self
	return "(" + d.Wrap(having.Column) + " " + having.Operator + " " + d.Parameter(having.Value) + ")::bool"
}

// CompileLock answers PostgresGrammar::compileLock.
func (g *PostgresGrammar) CompileLock(q *query.Builder, value any) string {
	if lock, ok := value.(string); ok {
		return lock
	}
	if lock, ok := value.(bool); ok && lock {
		return "for update"
	}
	return "for share"
}

// CompileInsertOrIgnore answers PostgresGrammar::compileInsertOrIgnore.
func (g *PostgresGrammar) CompileInsertOrIgnore(q *query.Builder, values []map[string]any) string {
	return g.self.CompileInsert(q, values) + " on conflict do nothing"
}

// CompileInsertOrIgnoreReturning answers
// PostgresGrammar::compileInsertOrIgnoreReturning.
func (g *PostgresGrammar) CompileInsertOrIgnoreReturning(q *query.Builder, values []map[string]any, uniqueBy, returning []string) (string, error) {
	d := g.self
	return d.CompileInsert(q, values) +
		" on conflict (" + d.Columnize(toAny(uniqueBy)) + ") do nothing" +
		" returning " + d.Columnize(toAny(returning)), nil
}

// CompileInsertOrIgnoreUsing answers
// PostgresGrammar::compileInsertOrIgnoreUsing.
func (g *PostgresGrammar) CompileInsertOrIgnoreUsing(q *query.Builder, columns []any, sql string) (string, error) {
	return g.self.CompileInsertUsing(q, columns, sql) + " on conflict do nothing", nil
}

// CompileInsertGetID answers PostgresGrammar::compileInsertGetId.
//
// Postgres hands the identifier back as a row, which is why PostgresProcessor
// reads it from the result set instead of asking the connection for the last
// one it assigned.
func (g *PostgresGrammar) CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string {
	if sequence == "" {
		sequence = "id"
	}
	return g.self.CompileInsert(q, []map[string]any{values}) + " returning " + g.self.Wrap(sequence)
}

// CompileUpdate answers PostgresGrammar::compileUpdate.
func (g *PostgresGrammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	if len(q.Joins) > 0 || q.GetLimit() != nil {
		return g.compileUpdateWithJoinsOrLimit(q, values)
	}
	return g.Grammar.CompileUpdate(q, values)
}

// CompileUpdateColumns answers PostgresGrammar::compileUpdateColumns: the set
// list names columns of the table being updated, so a qualified name loses its
// table.
func (g *PostgresGrammar) CompileUpdateColumns(q *query.Builder, values map[string]any) string {
	d := g.self
	parts := make([]string, 0, len(values))

	for _, key := range sortedKeys(values) {
		column := lastSegment(key, ".")

		if isJSONSelector(key) {
			parts = append(parts, g.compileJSONUpdateColumn(column, values[key]))
			continue
		}

		parts = append(parts, d.Wrap(column)+" = "+d.Parameter(values[key]))
	}

	return strings.Join(parts, ", ")
}

// CompileUpsert answers PostgresGrammar::compileUpsert.
//
// The update list is a list of column names, as query.Grammar declares it, so
// each one takes the value the conflicting insert carried -- that is what
// "excluded" names.
func (g *PostgresGrammar) CompileUpsert(q *query.Builder, values []map[string]any, uniqueBy []string, update []string) string {
	d := g.self

	sql := d.CompileInsert(q, values) + " on conflict (" + d.Columnize(toAny(uniqueBy)) + ") do update set "

	columns := make([]string, 0, len(update))
	for _, column := range update {
		columns = append(columns, d.Wrap(column)+" = "+d.WrapValue("excluded")+"."+d.Wrap(column))
	}

	return sql + strings.Join(columns, ", ")
}

// CompileJoinLateral answers PostgresGrammar::compileJoinLateral.
func (g *PostgresGrammar) CompileJoinLateral(join *query.JoinClause, expression string) (string, error) {
	return strings.TrimSpace(join.Type + " join lateral " + expression + " on true"), nil
}

// compileJSONUpdateColumn answers PostgresGrammar::compileJsonUpdateColumn.
func (g *PostgresGrammar) compileJSONUpdateColumn(key string, value any) string {
	d := g.self

	segments := strings.Split(key, "->")
	field := d.Wrap(segments[0])
	path := "'{" + strings.Join(g.wrapJSONPathAttributes(segments[1:], `"`), ",") + "}'"

	return field + " = jsonb_set(" + field + "::jsonb, " + path + ", " + d.Parameter(value) + ")"
}

// CompileUpdateFrom answers PostgresGrammar::compileUpdateFrom: Postgres lists
// the joined tables in a from clause rather than beside the table being
// updated.
func (g *PostgresGrammar) CompileUpdateFrom(q *query.Builder, values map[string]any) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())
	columns := d.CompileUpdateColumns(q, values)

	from := ""
	if len(q.Joins) > 0 {
		tables := make([]string, 0, len(q.Joins))
		for _, join := range q.Joins {
			tables = append(tables, d.WrapTable(join.Table))
		}
		from = " from " + strings.Join(tables, ", ")
	}

	return strings.TrimSpace("update " + table + " set " + columns + from + " " + g.compileUpdateWheres(q))
}

// compileUpdateWheres answers PostgresGrammar::compileUpdateWheres.
func (g *PostgresGrammar) compileUpdateWheres(q *query.Builder) string {
	baseWheres := g.self.CompileWheres(q)

	if len(q.Joins) == 0 {
		return baseWheres
	}

	joinWheres := g.compileUpdateJoinWheres(q)

	if strings.TrimSpace(baseWheres) == "" {
		return "where " + removeLeadingBoolean(joinWheres)
	}

	return baseWheres + " " + joinWheres
}

// compileUpdateJoinWheres answers PostgresGrammar::compileUpdateJoinWheres: the
// join conditions become where clauses, because there is no join to hang them
// on.
func (g *PostgresGrammar) compileUpdateJoinWheres(q *query.Builder) string {
	parts := make([]string, 0)

	for _, join := range q.Joins {
		for _, where := range join.Wheres {
			boolean := where.Boolean
			if boolean == "" {
				boolean = "and"
			}
			parts = append(parts, boolean+" "+g.compileWhere(q, where))
		}
	}

	return strings.Join(parts, " ")
}

// PrepareBindingsForUpdateFrom answers
// PostgresGrammar::prepareBindingsForUpdateFrom.
func (g *PostgresGrammar) PrepareBindingsForUpdateFrom(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0, len(values)+8)

	for _, column := range sortedKeys(values) {
		out = append(out, g.jsonBinding(column, values[column]))
	}

	out = append(out, bindings["where"]...)

	for _, key := range bindingOrder {
		if key == "select" || key == "where" {
			continue
		}
		out = append(out, bindings[key]...)
	}

	return out
}

// compileUpdateWithJoinsOrLimit answers
// PostgresGrammar::compileUpdateWithJoinsOrLimit.
//
// Postgres has no limit on an update, so the rows are named by their ctid --
// their physical address -- and chosen by a select that does have one. The
// select is compiled from a clone, where the PHP adds the column to the
// caller's builder and leaves it there.
func (g *PostgresGrammar) compileUpdateWithJoinsOrLimit(q *query.Builder, values map[string]any) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())
	columns := d.CompileUpdateColumns(q, values)

	alias := lastAliasSegment(text(q.GetFrom()))
	selectSQL := d.CompileSelect(q.Clone().Select(alias + ".ctid"))

	return "update " + table + " set " + columns + " where " + d.Wrap("ctid") + " in (" + selectSQL + ")"
}

// PrepareBindingsForUpdate answers PostgresGrammar::prepareBindingsForUpdate.
//
// The join bindings are not moved to the front the way the base grammar moves
// them, because a Postgres update compiles its joins after the set list. An
// Expression stays in the list as itself; see Grammar.PrepareBindingsForUpdate.
func (g *PostgresGrammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0, len(values)+8)

	for _, column := range sortedKeys(values) {
		out = append(out, g.jsonBinding(column, values[column]))
	}

	for _, key := range bindingOrder {
		if key == "select" {
			continue
		}
		out = append(out, bindings[key]...)
	}

	return out
}

// jsonBinding is the value transformation both of the Postgres binding
// preparers do: an array or an object, and anything bound to a JSON path,
// travels as JSON text.
func (g *PostgresGrammar) jsonBinding(column string, value any) any {
	if isStructured(value) || (isJSONSelector(column) && !query.IsExpression(value)) {
		if encoded, err := encodeJSON(value); err == nil {
			return encoded
		}
	}
	return value
}

// CompileDelete answers PostgresGrammar::compileDelete.
func (g *PostgresGrammar) CompileDelete(q *query.Builder) string {
	if len(q.Joins) > 0 || q.GetLimit() != nil {
		return g.compileDeleteWithJoinsOrLimit(q)
	}
	return g.Grammar.CompileDelete(q)
}

// compileDeleteWithJoinsOrLimit answers
// PostgresGrammar::compileDeleteWithJoinsOrLimit.
func (g *PostgresGrammar) compileDeleteWithJoinsOrLimit(q *query.Builder) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())

	alias := lastAliasSegment(text(q.GetFrom()))
	selectSQL := d.CompileSelect(q.Clone().Select(alias + ".ctid"))

	return "delete from " + table + " where " + d.Wrap("ctid") + " in (" + selectSQL + ")"
}

// CompileTruncate answers PostgresGrammar::compileTruncate.
func (g *PostgresGrammar) CompileTruncate(q *query.Builder) map[string][]any {
	sql := "truncate " + g.self.WrapTable(q.GetFrom()) + " restart identity"
	if cascadeTruncate.Load() {
		sql += " cascade"
	}
	return map[string][]any{sql: {}}
}

// CompileThreadCount answers PostgresGrammar::compileThreadCount.
func (g *PostgresGrammar) CompileThreadCount() string {
	return `select count(*) as "Value" from pg_stat_activity`
}

// WrapJSONSelector answers PostgresGrammar::wrapJsonSelector: the last step of
// a path is ->> so the value comes back as text, and the ones before it are ->
// so they stay JSON.
func (g *PostgresGrammar) WrapJSONSelector(value string) string {
	path := strings.Split(value, "->")

	field := g.wrapSegments(strings.Split(path[0], "."))

	wrappedPath := g.wrapJSONPathAttributes(path[1:], "'")
	if len(wrappedPath) == 0 {
		return field
	}

	attribute := wrappedPath[len(wrappedPath)-1]
	wrappedPath = wrappedPath[:len(wrappedPath)-1]

	if len(wrappedPath) > 0 {
		return field + "->" + strings.Join(wrappedPath, "->") + "->>" + attribute
	}

	return field + "->>" + attribute
}

// WrapJSONBooleanSelector answers PostgresGrammar::wrapJsonBooleanSelector.
func (g *PostgresGrammar) WrapJSONBooleanSelector(value string) string {
	return "(" + strings.ReplaceAll(g.self.WrapJSONSelector(value), "->>", "->") + ")::jsonb"
}

// WrapJSONBooleanValue answers PostgresGrammar::wrapJsonBooleanValue.
func (g *PostgresGrammar) WrapJSONBooleanValue(value string) string {
	return "'" + value + "'::jsonb"
}

// wrapJSONPathAttributes answers PostgresGrammar::wrapJsonPathAttributes.
//
// The quote is a parameter because the same path is spelled with single quotes
// inside an operator chain and with double quotes inside the array literal
// jsonb_set takes.
func (g *PostgresGrammar) wrapJSONPathAttributes(path []string, quote string) []string {
	out := make([]string, 0, len(path))

	for _, attribute := range path {
		for _, key := range parseJSONPathArrayKeys(attribute) {
			if _, err := strconv.Atoi(key); err == nil {
				out = append(out, key)
				continue
			}
			out = append(out, quote+key+quote)
		}
	}

	return out
}

// jsonPathKeys is the PHP's /\[([^\]]+)\]/.
var jsonPathKeys = regexp.MustCompile(`\[([^\]]+)\]`)

// parseJSONPathArrayKeys answers PostgresGrammar::parseJsonPathArrayKeys:
// "tags[0][1]" is one attribute and two array indexes.
func parseJSONPathArrayKeys(attribute string) []string {
	parts := jsonPathArrayKeys.FindString(attribute)
	if parts == "" {
		return []string{attribute}
	}

	out := make([]string, 0, 2)
	if key := strings.TrimSuffix(attribute, parts); key != "" {
		out = append(out, key)
	}

	for _, match := range jsonPathKeys.FindAllStringSubmatch(parts, -1) {
		if match[1] != "" {
			out = append(out, match[1])
		}
	}

	return out
}

// SubstituteBindingsIntoRawSQL answers
// PostgresGrammar::substituteBindingsIntoRawSql: the doubled question marks the
// operators carry are written back as single ones, since the result is for
// reading rather than for running.
func (g *PostgresGrammar) SubstituteBindingsIntoRawSQL(sql string, bindings []any) (string, error) {
	out, err := g.Grammar.SubstituteBindingsIntoRawSQL(sql, bindings)
	if err != nil {
		return "", err
	}

	for _, operator := range postgresOperators {
		if !strings.Contains(operator, "?") {
			continue
		}
		out = strings.ReplaceAll(out, strings.ReplaceAll(operator, "?", "??"), operator)
	}

	return out, nil
}

// lastAliasSegment answers `last(preg_split('/\s+as\s+/i', $from))`: the name a
// query refers to its own table by.
func lastAliasSegment(from string) string {
	segments := aliasPattern.Split(from, -1)
	return segments[len(segments)-1]
}
