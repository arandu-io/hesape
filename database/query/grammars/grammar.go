package grammars

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/database/query"
)

// dialect is the set of methods the compile pipeline calls on itself.
//
// PHP resolves `$this->compileLock(...)` at run time, so a subclass that
// redefines compileLock changes what compileSelect emits without compileSelect
// knowing. Go resolves an embedded method at compile time against the type that
// declared it, so the same code would always reach the base version and every
// driver difference would silently disappear -- backticks would never appear,
// "for update" would never appear, and nothing would fail.
//
// The self reference restores late binding: Grammar holds the outermost
// grammar, and every internal call goes through it. It is unexported because
// the four grammars that satisfy it live in this package, and nothing outside
// implements a dialect without also being one of them.
type dialect interface {
	// Identifier and value wrapping, from Illuminate\Database\Grammar.
	Wrap(value any) string
	WrapTable(table any) string
	WrapValue(value string) string
	WrapArray(values []any) []string
	WrapJSONSelector(value string) string
	WrapJSONBooleanSelector(value string) string
	WrapJSONBooleanValue(value string) string
	WrapUnion(sql string) string
	Columnize(columns []any) string
	Parameter(value any) string
	Parameterize(values []any) string
	QuoteString(value any) string
	Escape(value any, binary bool) (string, error)
	GetValue(expression any) any
	GetTablePrefix() string

	// The select pipeline.
	CompileSelect(q *query.Builder) string
	CompileAggregate(q *query.Builder, aggregate *query.Aggregate) string
	CompileColumns(q *query.Builder, columns []any) string
	CompileFrom(q *query.Builder, table any) string
	CompileIndexHint(q *query.Builder, indexHint *query.IndexHint) string
	CompileJoins(q *query.Builder, joins []*query.JoinClause) string
	CompileWheres(q *query.Builder) string
	CompileGroups(q *query.Builder, groups []any) string
	CompileHavings(q *query.Builder) string
	CompileHaving(having query.Having) string
	CompileOrders(q *query.Builder, orders []query.Order) string
	CompileLimit(q *query.Builder, limit int) string
	CompileOffset(q *query.Builder, offset int) string
	CompileLock(q *query.Builder, value any) string
	CompileGroupLimit(q *query.Builder) string
	CompileRandom(seed string) string
	SupportsStraightJoins() (bool, error)
	CompileJoinLateral(join *query.JoinClause, expression string) (string, error)

	// The where compilers. The PHP reaches them by name --
	// $this->{"where{$where['type']}"} -- so every one of them is an override
	// point whether or not a driver in this package takes it.
	WhereRaw(q *query.Builder, where query.Where) string
	WhereBasic(q *query.Builder, where query.Where) string
	WhereBitwise(q *query.Builder, where query.Where) string
	WhereLike(q *query.Builder, where query.Where) string
	WhereIn(q *query.Builder, where query.Where) string
	WhereNotIn(q *query.Builder, where query.Where) string
	WhereInRaw(q *query.Builder, where query.Where) string
	WhereNotInRaw(q *query.Builder, where query.Where) string
	WhereNull(q *query.Builder, where query.Where) string
	WhereNotNull(q *query.Builder, where query.Where) string
	WhereBetween(q *query.Builder, where query.Where) string
	WhereBetweenColumns(q *query.Builder, where query.Where) string
	WhereValueBetween(q *query.Builder, where query.Where) string
	WhereDate(q *query.Builder, where query.Where) string
	WhereTime(q *query.Builder, where query.Where) string
	WhereDay(q *query.Builder, where query.Where) string
	WhereMonth(q *query.Builder, where query.Where) string
	WhereYear(q *query.Builder, where query.Where) string
	DateBasedWhere(typ string, q *query.Builder, where query.Where) string
	WhereColumn(q *query.Builder, where query.Where) string
	WhereNested(q *query.Builder, where query.Where) string
	WhereSub(q *query.Builder, where query.Where) string
	WhereExists(q *query.Builder, where query.Where) string
	WhereNotExists(q *query.Builder, where query.Where) string
	WhereRowValues(q *query.Builder, where query.Where) string
	WhereJSONBoolean(q *query.Builder, where query.Where) string
	WhereJSONContains(q *query.Builder, where query.Where) string
	WhereJSONOverlaps(q *query.Builder, where query.Where) string
	WhereJSONContainsKey(q *query.Builder, where query.Where) string
	WhereJSONLength(q *query.Builder, where query.Where) string
	WhereFullText(q *query.Builder, where query.Where) (string, error)
	WhereExpression(q *query.Builder, where query.Where) string

	// The JSON operators, which is where the three dialects agree least.
	CompileJSONContains(column any, value string) (string, error)
	CompileJSONOverlaps(column any, value string) (string, error)
	CompileJSONContainsKey(column any) (string, error)
	CompileJSONLength(column any, operator, value string) (string, error)
	CompileJSONValueCast(value string) string
	PrepareBindingForJSONContains(binding any) (any, error)

	// The write pipeline.
	CompileInsert(q *query.Builder, values []map[string]any) string
	CompileInsertUsing(q *query.Builder, columns []any, sql string) string
	CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string
	CompileUpdate(q *query.Builder, values map[string]any) string
	CompileUpdateColumns(q *query.Builder, values map[string]any) string
	CompileUpdateWithoutJoins(q *query.Builder, table, columns, where string) string
	CompileUpdateWithJoins(q *query.Builder, table, columns, where string) string
	CompileDelete(q *query.Builder) string
	CompileDeleteWithoutJoins(q *query.Builder, table, where string) string
	CompileDeleteWithJoins(q *query.Builder, table, where string) string
	CompileTruncate(q *query.Builder) map[string][]any
	PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any
	SubstituteBindingsIntoRawSQL(sql string, bindings []any) (string, error)
}

// Grammar answers Illuminate\Database\Query\Grammars\Grammar: the standard SQL
// a driver grammar starts from.
//
// It embeds query.BaseGrammar, which already carries the half of
// Illuminate\Database\Grammar that no dialect disagrees about -- the operator
// lists, the table prefix, the savepoint statements, the date format, literal
// quoting and escaping. What it adds is the compile pipeline, which
// query.BaseGrammar does not have.
//
// # It is the abstract class, and the compiler knows it
//
// The PHP declares compileInsertOrIgnore and compileUpsert by throwing
// RuntimeException: every engine spells them differently and none of them can
// use the standard form. Here they are simply absent, so *Grammar does not
// satisfy query.Grammar and cannot be handed to a builder at all. The PHP finds
// out when the statement runs; this finds out when the package compiles.
type Grammar struct {
	query.BaseGrammar

	self dialect
}

// NewGrammar answers `new Grammar`, wiring the self reference the compile
// pipeline dispatches through.
//
// A driver grammar embeds the result and points self at itself, which is what
// `extends Grammar` does in PHP for the part that is late binding.
func NewGrammar() *Grammar {
	g := &Grammar{}
	g.self = g
	return g
}

// selectComponents is Grammar::$selectComponents: the clauses of a select, in
// the order they are concatenated.
var selectComponents = []string{
	"aggregate", "columns", "from", "indexHint", "joins",
	"wheres", "groups", "havings", "orders", "limit", "offset", "lock",
}

// bindingOrder repeats Builder::$bindings' key order.
//
// The list also lives in query, unexported, and PrepareBindingsForUpdate has to
// walk it to flatten the map the way the PHP flattens the array: a binding read
// out of order lands in the wrong placeholder, which is a wrong answer rather
// than an error.
var bindingOrder = []string{"select", "from", "join", "where", "groupBy", "having", "order", "union", "unionOrder"}

// component is one entry of Grammar::compileComponents' return.
//
// The PHP returns an array keyed by component name and relies on PHP arrays
// being ordered; a Go map is not, and the order is the statement, so the pieces
// are carried in a slice and looked up by name where compileGroupLimit needs
// them.
type component struct {
	name string
	sql  string
}

// CompileSelect answers Grammar::compileSelect.
func (g *Grammar) CompileSelect(q *query.Builder) string {
	if (len(q.Unions) > 0 || len(q.Havings) > 0) && q.GetAggregate() != nil {
		return g.compileUnionAggregate(q)
	}

	// A group limit needs a different shape entirely, which is what makes an
	// eager load with a limit per parent possible.
	if q.GetGroupLimit() != nil {
		if q.Columns == nil {
			q.Columns = []any{"*"}
		}
		return g.self.CompileGroupLimit(q)
	}

	return g.compileSelectWithoutGroupLimit(q)
}

// compileSelectWithoutGroupLimit is compileSelect's ordinary path.
//
// The PHP reaches it from a grammar with no window functions by nulling
// $query->groupLimit and calling compileSelect again. The group limit is not
// settable to nothing from outside query -- Builder keeps it unexported and
// CloneWithout does not name it -- so the path it leads to is named instead,
// which is the same statement without a round trip through a mutation.
func (g *Grammar) compileSelectWithoutGroupLimit(q *query.Builder) string {
	d := g.self

	original := q.Columns
	if q.Columns == nil {
		q.Columns = []any{"*"}
	}

	sql := strings.TrimSpace(concatenate(g.compileComponents(q)))

	if len(q.Unions) > 0 {
		sql = d.WrapUnion(sql) + " " + g.compileUnions(q)
	}

	q.Columns = original

	return sql
}

// compileComponents answers Grammar::compileComponents.
//
// The PHP tests isset($query->$component), which is false for null and true for
// an empty array; the empty cases compile to the empty string and concatenate
// drops them, so the emptiness test here reaches the same statement.
func (g *Grammar) compileComponents(q *query.Builder) []component {
	d := g.self
	out := make([]component, 0, len(selectComponents))

	if aggregate := q.GetAggregate(); aggregate != nil {
		out = append(out, component{"aggregate", d.CompileAggregate(q, aggregate)})
	}
	if q.Columns != nil {
		out = append(out, component{"columns", d.CompileColumns(q, q.Columns)})
	}
	if from := q.GetFrom(); from != nil {
		out = append(out, component{"from", d.CompileFrom(q, from)})
	}
	if indexHint := q.GetIndexHint(); indexHint != nil {
		out = append(out, component{"indexHint", d.CompileIndexHint(q, indexHint)})
	}
	if len(q.Joins) > 0 {
		out = append(out, component{"joins", d.CompileJoins(q, q.Joins)})
	}
	out = append(out, component{"wheres", d.CompileWheres(q)})
	if len(q.Groups) > 0 {
		out = append(out, component{"groups", d.CompileGroups(q, q.Groups)})
	}
	if len(q.Havings) > 0 {
		out = append(out, component{"havings", d.CompileHavings(q)})
	}
	if len(q.Orders) > 0 {
		out = append(out, component{"orders", d.CompileOrders(q, q.Orders)})
	}
	if limit := q.GetLimit(); limit != nil {
		out = append(out, component{"limit", d.CompileLimit(q, *limit)})
	}
	if offset := q.GetOffset(); offset != nil {
		out = append(out, component{"offset", d.CompileOffset(q, *offset)})
	}
	if lock := q.GetLock(); lock != nil {
		out = append(out, component{"lock", d.CompileLock(q, lock)})
	}

	return out
}

// CompileAggregate answers Grammar::compileAggregate.
func (g *Grammar) CompileAggregate(q *query.Builder, aggregate *query.Aggregate) string {
	d := g.self
	column := d.Columnize(aggregate.Columns)

	// A distinct aggregate counts distinct values, not distinct rows, so the
	// keyword goes inside the function call.
	switch distinct := q.GetDistinct().(type) {
	case []any:
		column = "distinct " + d.Columnize(distinct)
	case bool:
		if distinct && column != "*" {
			column = "distinct " + column
		}
	}

	return "select " + aggregate.Function + "(" + column + ") as aggregate"
}

// CompileColumns answers Grammar::compileColumns.
func (g *Grammar) CompileColumns(q *query.Builder, columns []any) string {
	if q.GetAggregate() != nil {
		return ""
	}
	if distinct, ok := q.GetDistinct().(bool); ok && distinct {
		return "select distinct " + g.self.Columnize(columns)
	}
	if _, ok := q.GetDistinct().([]any); ok {
		return "select distinct " + g.self.Columnize(columns)
	}
	return "select " + g.self.Columnize(columns)
}

// CompileFrom answers Grammar::compileFrom.
func (g *Grammar) CompileFrom(q *query.Builder, table any) string {
	return "from " + g.self.WrapTable(table)
}

// CompileIndexHint answers the index hint component.
//
// The PHP has no compileIndexHint on the base class at all, so a grammar that
// does not declare one fatals when a query carries a hint. Returning the empty
// string is what an engine without index hints does with the request anyway --
// a hint changes the plan, never the rows.
func (g *Grammar) CompileIndexHint(q *query.Builder, indexHint *query.IndexHint) string {
	return ""
}

// CompileJoins answers Grammar::compileJoins.
func (g *Grammar) CompileJoins(q *query.Builder, joins []*query.JoinClause) string {
	d := g.self
	parts := make([]string, 0, len(joins))

	for _, join := range joins {
		table := d.WrapTable(join.Table)

		tableAndNestedJoins := table
		if len(join.Joins) > 0 {
			tableAndNestedJoins = "(" + table + " " + d.CompileJoins(q, join.Joins) + ")"
		}

		// A lateral join has a shape of its own -- it takes no on clause, and
		// the engines that have one spell it differently -- so the whole of it
		// comes from the dialect. The PHP asks `$join instanceof
		// JoinLateralClause`; query.JoinClause carries the flag instead.
		if join.Lateral {
			sql, err := d.CompileJoinLateral(join, tableAndNestedJoins)
			if err != nil {
				parts = append(parts, unsupportedJoin(err))
				continue
			}
			parts = append(parts, sql)
			continue
		}

		// A straight join is MySQL's; every other engine compiles the type
		// straight through and lets the engine reject it, where the PHP throws
		// from supportsStraightJoins.
		joinWord := " join"
		if join.Type == "straight_join" {
			if supported, err := d.SupportsStraightJoins(); err == nil && supported {
				joinWord = ""
			}
		}

		parts = append(parts, strings.TrimSpace(
			join.Type+joinWord+" "+tableAndNestedJoins+" "+g.compileJoinConstraints(join),
		))
	}

	return strings.Join(parts, " ")
}

// CompileJoinLateral answers Grammar::compileJoinLateral.
//
// It carries an error where the PHP throws, and takes a *query.JoinClause
// because query has no JoinLateralClause yet; in PHP the lateral clause extends
// the ordinary one, so the parameter is its parent type.
func (g *Grammar) CompileJoinLateral(join *query.JoinClause, expression string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support lateral joins")
}

// SupportsStraightJoins answers Grammar::supportsStraightJoins, carrying an
// error where the PHP throws.
func (g *Grammar) SupportsStraightJoins() (bool, error) {
	return false, fmt.Errorf("query/grammars: this database engine does not support straight joins")
}

// CompileWheres answers Grammar::compileWheres.
func (g *Grammar) CompileWheres(q *query.Builder) string {
	clauses := g.whereClauses(q)
	if clauses == "" {
		return ""
	}
	return "where " + clauses
}

// compileJoinConstraints is compileWheres for a join clause, where the
// conjunction is "on" rather than "where".
//
// The PHP tells the two apart with `$query instanceof JoinClause`. Here the
// grammar is handed the JoinClause itself, because query.JoinClause embeds
// *Builder and the embedded value has forgotten what wraps it.
func (g *Grammar) compileJoinConstraints(join *query.JoinClause) string {
	clauses := g.whereClauses(join.Builder)
	if clauses == "" {
		return ""
	}
	return "on " + clauses
}

// whereClauses is compileWheresToArray followed by concatenateWhereClauses,
// minus the conjunction.
//
// Keeping the conjunction out until the caller adds it is what lets whereNested
// and compileJoinConstraints reuse this. The PHP compiles the full clause and
// then cuts the first six characters back off with substr, which is the same
// string by a route that breaks if the conjunction ever changes length.
func (g *Grammar) whereClauses(q *query.Builder) string {
	if len(q.Wheres) == 0 {
		return ""
	}

	parts := make([]string, 0, len(q.Wheres))
	for _, where := range q.Wheres {
		boolean := where.Boolean
		if boolean == "" {
			boolean = "and"
		}
		parts = append(parts, boolean+" "+g.compileWhere(q, where))
	}

	return removeLeadingBoolean(strings.Join(parts, " "))
}

// compileWhere answers the PHP's `$this->{"where{$where['type']}"}` dispatch.
//
// The Go builder spells negation with a Not flag where the PHP spells it in the
// type name -- "In" plus not, against "NotIn" -- so both forms arrive at the
// same compiler.
func (g *Grammar) compileWhere(q *query.Builder, where query.Where) string {
	d := g.self
	typ, not := whereType(where)
	where.Not = not

	switch typ {
	case "raw":
		return d.WhereRaw(q, where)
	case "basic":
		return d.WhereBasic(q, where)
	case "bitwise":
		return d.WhereBitwise(q, where)
	case "like":
		return d.WhereLike(q, where)
	case "in":
		if not {
			return d.WhereNotIn(q, where)
		}
		return d.WhereIn(q, where)
	case "inraw":
		if not {
			return d.WhereNotInRaw(q, where)
		}
		return d.WhereInRaw(q, where)
	case "null":
		if not {
			return d.WhereNotNull(q, where)
		}
		return d.WhereNull(q, where)
	case "between":
		return d.WhereBetween(q, where)
	case "betweencolumns":
		return d.WhereBetweenColumns(q, where)
	case "valuebetween":
		return d.WhereValueBetween(q, where)
	case "date":
		return d.WhereDate(q, where)
	case "time":
		return d.WhereTime(q, where)
	case "day":
		return d.WhereDay(q, where)
	case "month":
		return d.WhereMonth(q, where)
	case "year":
		return d.WhereYear(q, where)
	case "column":
		return d.WhereColumn(q, where)
	case "nested":
		return d.WhereNested(q, where)
	case "sub":
		return d.WhereSub(q, where)
	case "exists":
		if not {
			return d.WhereNotExists(q, where)
		}
		return d.WhereExists(q, where)
	case "rowvalues":
		return d.WhereRowValues(q, where)
	case "jsonboolean":
		return d.WhereJSONBoolean(q, where)
	case "jsoncontains":
		return d.WhereJSONContains(q, where)
	case "jsonoverlaps":
		return d.WhereJSONOverlaps(q, where)
	case "jsoncontainskey":
		return d.WhereJSONContainsKey(q, where)
	case "jsonlength":
		return d.WhereJSONLength(q, where)
	case "fulltext":
		sql, err := d.WhereFullText(q, where)
		if err != nil {
			return unsupportedClause(err)
		}
		return sql
	case "expression":
		return d.WhereExpression(q, where)
	default:
		// The PHP fatals on an undefined method. A clause the grammar cannot
		// spell must not be dropped: a filter that quietly disappears is a
		// tenant reading another tenant's rows, so the query is made false
		// instead and carries the reason.
		return unsupportedClause(fmt.Errorf("query/grammars: no compiler for where clause type %q", where.Type))
	}
}

// whereType normalises a clause type to the switch's spelling and folds the
// PHP's "Not..." names into the Not flag.
func whereType(where query.Where) (typ string, not bool) {
	typ = strings.ToLower(where.Type)
	not = where.Not

	switch typ {
	case "notnull":
		return "null", true
	case "notin":
		return "in", true
	case "notinraw":
		return "inraw", true
	case "notexists":
		return "exists", true
	case "notbetween":
		return "between", true
	}

	return typ, not
}

// WhereRaw answers Grammar::whereRaw.
func (g *Grammar) WhereRaw(q *query.Builder, where query.Where) string {
	return text(g.self.GetValue(where.SQL))
}

// WhereBasic answers Grammar::whereBasic.
//
// The operator has its question marks doubled, as the PHP does: Postgres spells
// several of its operators with one -- ?, ?| and ?& all test for a JSON key --
// and a bare one would be read as a placeholder. PostgresGrammar's
// SubstituteBindingsIntoRawSQL undoes the doubling, which is the other half of
// the same convention.
func (g *Grammar) WhereBasic(q *query.Builder, where query.Where) string {
	d := g.self
	value := d.Parameter(where.Value)
	operator := strings.ReplaceAll(where.Operator, "?", "??")

	return d.Wrap(where.Column) + " " + operator + " " + value
}

// WhereBitwise answers Grammar::whereBitwise.
func (g *Grammar) WhereBitwise(q *query.Builder, where query.Where) string {
	return g.self.WhereBasic(q, where)
}

// WhereLike answers Grammar::whereLike, carrying the refusal the PHP throws as
// a false clause: an engine without a case sensitive like cannot answer the
// question that was asked, and answering the case insensitive one instead would
// return rows nobody filtered for.
func (g *Grammar) WhereLike(q *query.Builder, where query.Where) string {
	if where.CaseSensitive {
		return unsupportedClause(fmt.Errorf("query/grammars: this database engine does not support case sensitive like operations"))
	}

	where.Operator = "like"
	if where.Not {
		where.Operator = "not like"
	}

	return g.self.WhereBasic(q, where)
}

// WhereIn answers Grammar::whereIn.
//
// An empty list compiles to "0 = 1" rather than being dropped: a filter over an
// empty set matches nothing, and dropping it would match everything.
func (g *Grammar) WhereIn(q *query.Builder, where query.Where) string {
	if len(where.Values) == 0 {
		return "0 = 1"
	}
	d := g.self
	return d.Wrap(where.Column) + " in (" + d.Parameterize(where.Values) + ")"
}

// WhereNotIn answers Grammar::whereNotIn.
func (g *Grammar) WhereNotIn(q *query.Builder, where query.Where) string {
	if len(where.Values) == 0 {
		return "1 = 1"
	}
	d := g.self
	return d.Wrap(where.Column) + " not in (" + d.Parameterize(where.Values) + ")"
}

// WhereInRaw answers Grammar::whereInRaw. The values are written into the
// statement rather than bound, which is only safe because whereIntegerInRaw is
// the one caller and it has already cast every value to an integer.
func (g *Grammar) WhereInRaw(q *query.Builder, where query.Where) string {
	if len(where.Values) == 0 {
		return "0 = 1"
	}
	return g.self.Wrap(where.Column) + " in (" + joinValues(where.Values) + ")"
}

// WhereNotInRaw answers Grammar::whereNotInRaw.
func (g *Grammar) WhereNotInRaw(q *query.Builder, where query.Where) string {
	if len(where.Values) == 0 {
		return "1 = 1"
	}
	return g.self.Wrap(where.Column) + " not in (" + joinValues(where.Values) + ")"
}

// WhereNull answers Grammar::whereNull.
func (g *Grammar) WhereNull(q *query.Builder, where query.Where) string {
	return g.self.Wrap(where.Column) + " is null"
}

// WhereNotNull answers Grammar::whereNotNull.
func (g *Grammar) WhereNotNull(q *query.Builder, where query.Where) string {
	return g.self.Wrap(where.Column) + " is not null"
}

// WhereBetween answers Grammar::whereBetween.
func (g *Grammar) WhereBetween(q *query.Builder, where query.Where) string {
	d := g.self
	between := "between"
	if where.Not {
		between = "not between"
	}
	first, last := ends(where.Values)
	return d.Wrap(where.Column) + " " + between + " " + d.Parameter(first) + " and " + d.Parameter(last)
}

// WhereBetweenColumns answers Grammar::whereBetweenColumns.
func (g *Grammar) WhereBetweenColumns(q *query.Builder, where query.Where) string {
	d := g.self
	between := "between"
	if where.Not {
		between = "not between"
	}
	first, last := ends(where.Values)
	return d.Wrap(where.Column) + " " + between + " " + d.Wrap(first) + " and " + d.Wrap(last)
}

// WhereValueBetween answers Grammar::whereValueBetween: the value is the
// literal and the two columns are the bounds, which is the comparison the other
// way round.
func (g *Grammar) WhereValueBetween(q *query.Builder, where query.Where) string {
	d := g.self
	between := "between"
	if where.Not {
		between = "not between"
	}
	first, last := ends(where.Columns)
	return d.Parameter(where.Value) + " " + between + " " + d.Wrap(first) + " and " + d.Wrap(last)
}

// WhereDate answers Grammar::whereDate.
func (g *Grammar) WhereDate(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("date", q, where)
}

// WhereTime answers Grammar::whereTime.
func (g *Grammar) WhereTime(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("time", q, where)
}

// WhereDay answers Grammar::whereDay.
func (g *Grammar) WhereDay(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("day", q, where)
}

// WhereMonth answers Grammar::whereMonth.
func (g *Grammar) WhereMonth(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("month", q, where)
}

// WhereYear answers Grammar::whereYear.
func (g *Grammar) WhereYear(q *query.Builder, where query.Where) string {
	return g.self.DateBasedWhere("year", q, where)
}

// DateBasedWhere answers Grammar::dateBasedWhere.
func (g *Grammar) DateBasedWhere(typ string, q *query.Builder, where query.Where) string {
	d := g.self
	return typ + "(" + d.Wrap(where.Column) + ") " + where.Operator + " " + d.Parameter(where.Value)
}

// WhereColumn answers Grammar::whereColumn: both sides are names, so neither
// becomes a binding.
func (g *Grammar) WhereColumn(q *query.Builder, where query.Where) string {
	d := g.self
	return d.Wrap(where.First) + " " + where.Operator + " " + d.Wrap(where.Second)
}

// WhereNested answers Grammar::whereNested.
func (g *Grammar) WhereNested(q *query.Builder, where query.Where) string {
	if where.Query == nil {
		return ""
	}
	return "(" + g.whereClauses(where.Query) + ")"
}

// WhereSub answers Grammar::whereSub.
//
// A clause of this type with no subquery is a builder bug, and the PHP would
// fatal on it. It compiles false here instead: a filter that cannot be built is
// still a filter, and a panic inside a grammar takes down the request that was
// about to be told no.
func (g *Grammar) WhereSub(q *query.Builder, where query.Where) string {
	if where.Query == nil {
		return unsupportedClause(errMissingSubquery)
	}
	d := g.self
	return d.Wrap(where.Column) + " " + where.Operator + " (" + d.CompileSelect(where.Query) + ")"
}

// WhereExists answers Grammar::whereExists.
func (g *Grammar) WhereExists(q *query.Builder, where query.Where) string {
	if where.Query == nil {
		return unsupportedClause(errMissingSubquery)
	}
	return "exists (" + g.self.CompileSelect(where.Query) + ")"
}

// WhereNotExists answers Grammar::whereNotExists.
func (g *Grammar) WhereNotExists(q *query.Builder, where query.Where) string {
	if where.Query == nil {
		return unsupportedClause(errMissingSubquery)
	}
	return "not exists (" + g.self.CompileSelect(where.Query) + ")"
}

// errMissingSubquery is the clause that names a subquery and does not carry one.
var errMissingSubquery = errors.New("query/grammars: the where clause has no subquery to compile")

// WhereRowValues answers Grammar::whereRowValues.
func (g *Grammar) WhereRowValues(q *query.Builder, where query.Where) string {
	d := g.self
	return "(" + d.Columnize(where.Columns) + ") " + where.Operator + " (" + d.Parameterize(where.Values) + ")"
}

// WhereJSONBoolean answers Grammar::whereJsonBoolean.
func (g *Grammar) WhereJSONBoolean(q *query.Builder, where query.Where) string {
	d := g.self
	column := d.WrapJSONBooleanSelector(text(where.Column))
	value := d.WrapJSONBooleanValue(d.Parameter(where.Value))
	return column + " " + where.Operator + " " + value
}

// WhereJSONContains answers Grammar::whereJsonContains.
func (g *Grammar) WhereJSONContains(q *query.Builder, where query.Where) string {
	d := g.self
	sql, err := d.CompileJSONContains(where.Column, d.Parameter(where.Value))
	if err != nil {
		return unsupportedClause(err)
	}
	if where.Not {
		return "not " + sql
	}
	return sql
}

// WhereJSONOverlaps answers Grammar::whereJsonOverlaps.
func (g *Grammar) WhereJSONOverlaps(q *query.Builder, where query.Where) string {
	d := g.self
	sql, err := d.CompileJSONOverlaps(where.Column, d.Parameter(where.Value))
	if err != nil {
		return unsupportedClause(err)
	}
	if where.Not {
		return "not " + sql
	}
	return sql
}

// WhereJSONContainsKey answers Grammar::whereJsonContainsKey.
func (g *Grammar) WhereJSONContainsKey(q *query.Builder, where query.Where) string {
	sql, err := g.self.CompileJSONContainsKey(where.Column)
	if err != nil {
		return unsupportedClause(err)
	}
	if where.Not {
		return "not " + sql
	}
	return sql
}

// WhereJSONLength answers Grammar::whereJsonLength.
func (g *Grammar) WhereJSONLength(q *query.Builder, where query.Where) string {
	d := g.self
	sql, err := d.CompileJSONLength(where.Column, where.Operator, d.Parameter(where.Value))
	if err != nil {
		return unsupportedClause(err)
	}
	return sql
}

// WhereFullText answers Grammar::whereFullText, carrying an error where the PHP
// throws.
func (g *Grammar) WhereFullText(q *query.Builder, where query.Where) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support fulltext search operations")
}

// WhereExpression answers Grammar::whereExpression.
func (g *Grammar) WhereExpression(q *query.Builder, where query.Where) string {
	return text(g.self.GetValue(where.Column))
}

// CompileJSONContains answers Grammar::compileJsonContains. The PHP spells the
// middle word Json; Go initialisms are upper case throughout.
func (g *Grammar) CompileJSONContains(column any, value string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support JSON contains operations")
}

// CompileJSONOverlaps answers Grammar::compileJsonOverlaps.
func (g *Grammar) CompileJSONOverlaps(column any, value string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support JSON overlaps operations")
}

// CompileJSONContainsKey answers Grammar::compileJsonContainsKey.
func (g *Grammar) CompileJSONContainsKey(column any) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support JSON contains key operations")
}

// CompileJSONLength answers Grammar::compileJsonLength.
func (g *Grammar) CompileJSONLength(column any, operator, value string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support JSON length operations")
}

// CompileJSONValueCast answers Grammar::compileJsonValueCast.
func (g *Grammar) CompileJSONValueCast(value string) string { return value }

// PrepareBindingForJSONContains answers Grammar::prepareBindingForJsonContains.
func (g *Grammar) PrepareBindingForJSONContains(binding any) (any, error) {
	return encodeJSON(binding)
}

// CompileGroups answers Grammar::compileGroups.
func (g *Grammar) CompileGroups(q *query.Builder, groups []any) string {
	return "group by " + g.self.Columnize(groups)
}

// CompileHavings answers Grammar::compileHavings.
func (g *Grammar) CompileHavings(q *query.Builder) string {
	clauses := g.havingClauses(q)
	if clauses == "" {
		return ""
	}
	return "having " + clauses
}

// havingClauses is compileHavings without the keyword, so that a nested having
// can reuse it. The PHP cuts the keyword back off with substr.
func (g *Grammar) havingClauses(q *query.Builder) string {
	if q == nil || len(q.Havings) == 0 {
		return ""
	}

	parts := make([]string, 0, len(q.Havings))
	for _, having := range q.Havings {
		boolean := having.Boolean
		if boolean == "" {
			boolean = "and"
		}
		parts = append(parts, boolean+" "+g.self.CompileHaving(having))
	}

	return removeLeadingBoolean(strings.Join(parts, " "))
}

// CompileHaving answers Grammar::compileHaving.
func (g *Grammar) CompileHaving(having query.Having) string {
	switch strings.ToLower(having.Type) {
	case "raw":
		return having.SQL
	case "between":
		return g.CompileHavingBetween(having)
	case "null":
		return g.CompileHavingNull(having)
	case "notnull":
		return g.CompileHavingNotNull(having)
	case "bit":
		return g.CompileHavingBit(having)
	case "expression":
		return g.CompileHavingExpression(having)
	case "nested":
		return g.CompileNestedHavings(having)
	default:
		return g.CompileBasicHaving(having)
	}
}

// CompileBasicHaving answers Grammar::compileBasicHaving.
func (g *Grammar) CompileBasicHaving(having query.Having) string {
	d := g.self
	return d.Wrap(having.Column) + " " + having.Operator + " " + d.Parameter(having.Value)
}

// CompileHavingBetween answers Grammar::compileHavingBetween.
func (g *Grammar) CompileHavingBetween(having query.Having) string {
	d := g.self
	between := "between"
	if having.Not {
		between = "not between"
	}
	first, last := ends(having.Values)
	return d.Wrap(having.Column) + " " + between + " " + d.Parameter(first) + " and " + d.Parameter(last)
}

// CompileHavingNull answers Grammar::compileHavingNull.
func (g *Grammar) CompileHavingNull(having query.Having) string {
	return g.self.Wrap(having.Column) + " is null"
}

// CompileHavingNotNull answers Grammar::compileHavingNotNull.
func (g *Grammar) CompileHavingNotNull(having query.Having) string {
	return g.self.Wrap(having.Column) + " is not null"
}

// CompileHavingBit answers Grammar::compileHavingBit.
func (g *Grammar) CompileHavingBit(having query.Having) string {
	d := g.self
	return "(" + d.Wrap(having.Column) + " " + having.Operator + " " + d.Parameter(having.Value) + ") != 0"
}

// CompileHavingExpression answers Grammar::compileHavingExpression.
func (g *Grammar) CompileHavingExpression(having query.Having) string {
	return text(g.self.GetValue(having.Column))
}

// CompileNestedHavings answers Grammar::compileNestedHavings.
func (g *Grammar) CompileNestedHavings(having query.Having) string {
	return "(" + g.havingClauses(having.Query) + ")"
}

// CompileOrders answers Grammar::compileOrders.
func (g *Grammar) CompileOrders(q *query.Builder, orders []query.Order) string {
	if len(orders) == 0 {
		return ""
	}
	return "order by " + strings.Join(g.compileOrdersToArray(q, orders), ", ")
}

// compileOrdersToArray answers Grammar::compileOrdersToArray.
func (g *Grammar) compileOrdersToArray(q *query.Builder, orders []query.Order) []string {
	d := g.self
	out := make([]string, 0, len(orders))
	for _, order := range orders {
		if order.SQL != nil {
			out = append(out, text(d.GetValue(order.SQL)))
			continue
		}
		out = append(out, d.Wrap(order.Column)+" "+order.Direction)
	}
	return out
}

// CompileLimit answers Grammar::compileLimit.
func (g *Grammar) CompileLimit(q *query.Builder, limit int) string {
	return "limit " + strconv.Itoa(limit)
}

// CompileOffset answers Grammar::compileOffset.
func (g *Grammar) CompileOffset(q *query.Builder, offset int) string {
	return "offset " + strconv.Itoa(offset)
}

// CompileGroupLimit answers Grammar::compileGroupLimit: a window function that
// numbers the rows of each group so the outer query can cut each group at the
// same depth.
//
// The binding move is the PHP's, and it has to happen on the query the caller
// holds, because the caller reads GetBindings after the SQL is compiled. The
// offset is dropped on a clone instead of on the caller's query, which reaches
// the same statement without leaving the builder changed behind the caller's
// back.
func (g *Grammar) CompileGroupLimit(q *query.Builder) string {
	d := g.self

	selectBindings := make([]any, 0, len(q.Bindings["select"])+len(q.Bindings["order"]))
	selectBindings = append(selectBindings, q.Bindings["select"]...)
	selectBindings = append(selectBindings, q.Bindings["order"]...)
	q.SetBindings(selectBindings, "select")
	q.SetBindings(nil, "order")

	groupLimit := q.GetGroupLimit()
	limit := groupLimit.Value

	offset := q.GetOffset()
	if offset != nil {
		limit += *offset
		q = q.CloneWithout("offset")
	}

	components := g.compileComponents(q)
	orders := take(&components, "orders")
	setComponent(components, "columns", get(components, "columns")+g.compileRowNumber(groupLimit.Column, orders))

	table := d.Wrap("laravel_table")
	row := d.Wrap("laravel_row")

	sql := "select * from (" + concatenate(components) + ") as " + table +
		" where " + row + " <= " + strconv.Itoa(limit)

	if offset != nil {
		sql += " and " + row + " > " + strconv.Itoa(*offset)
	}

	return sql + " order by " + row
}

// compileRowNumber answers Grammar::compileRowNumber.
//
// The aliases are Illuminate's, spelled the way it spells them. They never
// leave the compiled statement, and a developer reading the SQL of a limited
// eager load recognises it from Laravel.
func (g *Grammar) compileRowNumber(partition string, orders string) string {
	over := strings.TrimSpace("partition by " + g.self.Wrap(partition) + " " + orders)
	return ", row_number() over (" + over + ") as " + g.self.Wrap("laravel_row")
}

// compileUnions answers Grammar::compileUnions.
func (g *Grammar) compileUnions(q *query.Builder) string {
	d := g.self
	var sql strings.Builder

	for _, union := range q.Unions {
		sql.WriteString(g.compileUnion(union))
	}
	if len(q.UnionOrders) > 0 {
		sql.WriteString(" " + d.CompileOrders(q, q.UnionOrders))
	}
	if q.UnionLimit != nil {
		sql.WriteString(" " + d.CompileLimit(q, *q.UnionLimit))
	}
	if q.UnionOffset != nil {
		sql.WriteString(" " + d.CompileOffset(q, *q.UnionOffset))
	}

	return strings.TrimLeft(sql.String(), " ")
}

// compileUnion answers Grammar::compileUnion.
func (g *Grammar) compileUnion(union query.Union) string {
	conjunction := " union "
	if union.All {
		conjunction = " union all "
	}
	return conjunction + g.self.WrapUnion(union.Query.ToSQL())
}

// WrapUnion answers Grammar::wrapUnion.
func (g *Grammar) WrapUnion(sql string) string { return "(" + sql + ")" }

// compileUnionAggregate answers Grammar::compileUnionAggregate.
//
// The PHP nulls the aggregate on the query so the inner select does not compile
// it a second time; here the inner select runs against a clone without it,
// which is the same statement without mutating the caller's builder.
func (g *Grammar) compileUnionAggregate(q *query.Builder) string {
	d := g.self
	sql := d.CompileAggregate(q, q.GetAggregate())
	return sql + " from (" + d.CompileSelect(q.CloneWithout("aggregate")) + ") as " + d.WrapTable("temp_table")
}

// CompileExists answers Grammar::compileExists.
func (g *Grammar) CompileExists(q *query.Builder) string {
	return "select exists(" + g.self.CompileSelect(q) + ") as " + g.self.Wrap("exists")
}

// CompileInsert answers Grammar::compileInsert.
//
// # Column order
//
// The PHP takes the column list from the first record's keys, and a PHP array
// remembers the order they were written in. A Go map does not, so the columns
// are sorted by name: the statement has to be the same on every run, and the
// bindings the builder flattens have to line up with it. Every method here that
// walks a values map sorts it the same way.
func (g *Grammar) CompileInsert(q *query.Builder, values []map[string]any) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())

	if len(values) == 0 {
		return "insert into " + table + " default values"
	}

	columns := sortedKeys(values[0])

	parameters := make([]string, 0, len(values))
	for _, record := range values {
		ordered := make([]any, 0, len(columns))
		for _, column := range columns {
			ordered = append(ordered, record[column])
		}
		parameters = append(parameters, "("+d.Parameterize(ordered)+")")
	}

	return "insert into " + table + " (" + d.Columnize(toAny(columns)) + ") values " + strings.Join(parameters, ", ")
}

// CompileInsertGetID answers Grammar::compileInsertGetId.
func (g *Grammar) CompileInsertGetID(q *query.Builder, values map[string]any, sequence string) string {
	return g.self.CompileInsert(q, []map[string]any{values})
}

// CompileInsertUsing answers Grammar::compileInsertUsing.
func (g *Grammar) CompileInsertUsing(q *query.Builder, columns []any, sql string) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())

	if len(columns) == 0 || (len(columns) == 1 && text(columns[0]) == "*") {
		return "insert into " + table + " " + sql
	}

	return "insert into " + table + " (" + d.Columnize(columns) + ") " + sql
}

// CompileInsertOrIgnoreReturning answers
// Grammar::compileInsertOrIgnoreReturning, carrying an error where the PHP
// throws.
func (g *Grammar) CompileInsertOrIgnoreReturning(q *query.Builder, values []map[string]any, uniqueBy, returning []string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support insert or ignore with returning")
}

// CompileInsertOrIgnoreUsing answers Grammar::compileInsertOrIgnoreUsing,
// carrying an error where the PHP throws.
func (g *Grammar) CompileInsertOrIgnoreUsing(q *query.Builder, columns []any, sql string) (string, error) {
	return "", fmt.Errorf("query/grammars: this database engine does not support inserting while ignoring errors")
}

// CompileUpdate answers Grammar::compileUpdate.
func (g *Grammar) CompileUpdate(q *query.Builder, values map[string]any) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())
	columns := d.CompileUpdateColumns(q, values)
	where := d.CompileWheres(q)

	if len(q.Joins) > 0 {
		return strings.TrimSpace(d.CompileUpdateWithJoins(q, table, columns, where))
	}
	return strings.TrimSpace(d.CompileUpdateWithoutJoins(q, table, columns, where))
}

// CompileUpdateColumns answers Grammar::compileUpdateColumns.
func (g *Grammar) CompileUpdateColumns(q *query.Builder, values map[string]any) string {
	d := g.self
	parts := make([]string, 0, len(values))
	for _, column := range sortedKeys(values) {
		parts = append(parts, d.Wrap(column)+" = "+d.Parameter(values[column]))
	}
	return strings.Join(parts, ", ")
}

// CompileUpdateWithoutJoins answers Grammar::compileUpdateWithoutJoins.
func (g *Grammar) CompileUpdateWithoutJoins(q *query.Builder, table, columns, where string) string {
	return "update " + table + " set " + columns + " " + where
}

// CompileUpdateWithJoins answers Grammar::compileUpdateWithJoins.
func (g *Grammar) CompileUpdateWithJoins(q *query.Builder, table, columns, where string) string {
	joins := g.self.CompileJoins(q, q.Joins)
	return "update " + table + " " + joins + " set " + columns + " " + where
}

// PrepareBindingsForUpdate answers Grammar::prepareBindingsForUpdate.
//
// The join bindings come first because the joins are compiled before the set
// list, then the values, then everything else in the order Builder::$bindings
// declares. Select bindings are dropped: an update has no select clause to bind
// them to.
//
// An Expression stays in the list as itself. CompileUpdateColumns wrote it into
// the statement rather than leaving a placeholder for it, so it must not be
// bound -- and Builder::cleanBindings is what drops it, by recognising the type.
// Unwrapping it here would hide it from that check and shift every binding
// after it by one.
func (g *Grammar) PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any {
	out := make([]any, 0, len(values)+8)
	out = append(out, bindings["join"]...)

	for _, column := range sortedKeys(values) {
		out = append(out, values[column])
	}

	for _, key := range bindingOrder {
		if key == "select" || key == "join" {
			continue
		}
		out = append(out, bindings[key]...)
	}

	return out
}

// CompileDelete answers Grammar::compileDelete.
func (g *Grammar) CompileDelete(q *query.Builder) string {
	d := g.self
	table := d.WrapTable(q.GetFrom())
	where := d.CompileWheres(q)

	if len(q.Joins) > 0 {
		return strings.TrimSpace(d.CompileDeleteWithJoins(q, table, where))
	}
	return strings.TrimSpace(d.CompileDeleteWithoutJoins(q, table, where))
}

// CompileDeleteWithoutJoins answers Grammar::compileDeleteWithoutJoins.
func (g *Grammar) CompileDeleteWithoutJoins(q *query.Builder, table, where string) string {
	return "delete from " + table + " " + where
}

// CompileDeleteWithJoins answers Grammar::compileDeleteWithJoins.
func (g *Grammar) CompileDeleteWithJoins(q *query.Builder, table, where string) string {
	alias := lastSegment(table, " as ")
	joins := g.self.CompileJoins(q, q.Joins)
	return "delete " + alias + " from " + table + " " + joins + " " + where
}

// PrepareBindingsForDelete answers Grammar::prepareBindingsForDelete.
func (g *Grammar) PrepareBindingsForDelete(bindings map[string][]any) []any {
	out := make([]any, 0, 8)
	for _, key := range bindingOrder {
		if key == "select" {
			continue
		}
		out = append(out, bindings[key]...)
	}
	return out
}

// CompileTruncate answers Grammar::compileTruncate.
func (g *Grammar) CompileTruncate(q *query.Builder) map[string][]any {
	return map[string][]any{"truncate table " + g.self.WrapTable(q.GetFrom()): {}}
}

// CompileLock answers Grammar::compileLock.
func (g *Grammar) CompileLock(q *query.Builder, value any) string {
	if lock, ok := value.(string); ok {
		return lock
	}
	return ""
}

// CompileThreadCount answers Grammar::compileThreadCount. The PHP returns null
// for an engine that cannot be asked; the empty string is that here.
func (g *Grammar) CompileThreadCount() string { return "" }

// SubstituteBindingsIntoRawSQL answers Grammar::substituteBindingsIntoRawSql.
//
// It is for showing a statement, never for running one: the result is a string
// with the values written into it, which is exactly what a placeholder exists
// to avoid. It carries an error because escaping needs a connection, and a
// grammar without one refuses rather than emitting something that looks escaped
// and is not.
func (g *Grammar) SubstituteBindingsIntoRawSQL(sql string, bindings []any) (string, error) {
	d := g.self

	escaped := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		_, binary := binding.([]byte)
		value, err := d.Escape(binding, binary)
		if err != nil {
			return "", err
		}
		escaped = append(escaped, value)
	}

	var out strings.Builder
	isStringLiteral := false

	for i := 0; i < len(sql); i++ {
		char := sql[i]
		next := byte(0)
		if i+1 < len(sql) {
			next = sql[i+1]
		}

		pair := string([]byte{char, next})
		switch {
		case next != 0 && (pair == `\'` || pair == "''" || pair == "??"):
			// An escaped quote, or a Postgres operator whose question mark was
			// doubled so it would not read as a placeholder.
			out.WriteString(pair)
			i++
		case char == '\'':
			out.WriteByte(char)
			isStringLiteral = !isStringLiteral
		case char == '?' && !isStringLiteral:
			if len(escaped) > 0 {
				out.WriteString(escaped[0])
				escaped = escaped[1:]
			} else {
				out.WriteByte('?')
			}
		default:
			out.WriteByte(char)
		}
	}

	return out.String(), nil
}

// Wrap answers Illuminate\Database\Grammar::wrap.
//
// query.BaseGrammar has a Wrap of its own, and it cannot be the one that runs:
// it quotes through an unexported wrapValue that Go binds at compile time, so
// MySQL's backtick could never reach it from this package. The wrapping family
// is therefore declared here over the exported WrapValue hook, and BaseGrammar
// stays embedded for everything that is not identifier quoting.
func (g *Grammar) Wrap(value any) string {
	d := g.self

	if query.IsExpression(value) {
		return text(d.GetValue(value))
	}

	name := text(value)

	if segments := aliasSplit(name); len(segments) > 1 {
		return d.Wrap(segments[0]) + " as " + d.WrapValue(segments[1])
	}

	if isJSONSelector(name) {
		return d.WrapJSONSelector(name)
	}

	return g.wrapSegments(strings.Split(name, "."))
}

// WrapTable answers Grammar::wrapTable.
//
// The table prefix is applied here and nowhere else, which is why a bare table
// name written into a where clause is a bug that only appears on a prefixed
// connection.
func (g *Grammar) WrapTable(table any) string {
	return g.wrapTableWithPrefix(table, g.self.GetTablePrefix())
}

// wrapTableWithPrefix is wrapTable's second parameter, which Go cannot spell as
// an optional argument without changing the signature query.Grammar declares.
func (g *Grammar) wrapTableWithPrefix(table any, prefix string) string {
	d := g.self

	if query.IsExpression(table) {
		return text(d.GetValue(table))
	}

	name := text(table)

	if segments := aliasSplit(name); len(segments) > 1 {
		return g.wrapTableWithPrefix(segments[0], prefix) + " as " + d.WrapValue(prefix+segments[1])
	}

	// A schema qualified name is prefixed on the table, not on the schema.
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[:i+1] + prefix + name[i+1:]
		segments := strings.Split(name, ".")
		wrapped := make([]string, 0, len(segments))
		for _, segment := range segments {
			wrapped = append(wrapped, d.WrapValue(segment))
		}
		return strings.Join(wrapped, ".")
	}

	return d.WrapValue(prefix + name)
}

// wrapSegments answers Grammar::wrapSegments: the first of several segments is
// a table, so it takes the prefix.
func (g *Grammar) wrapSegments(segments []string) string {
	d := g.self
	out := make([]string, 0, len(segments))
	for i, segment := range segments {
		if i == 0 && len(segments) > 1 {
			out = append(out, d.WrapTable(segment))
			continue
		}
		out = append(out, d.WrapValue(segment))
	}
	return strings.Join(out, ".")
}

// WrapValue answers Grammar::wrapValue: one identifier segment, quoted the way
// the standard says. MySQL overrides it with a backtick.
//
// A quote inside the identifier is doubled rather than dropped, because
// dropping it would silently rename the column.
func (g *Grammar) WrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// WrapArray answers Grammar::wrapArray.
func (g *Grammar) WrapArray(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, g.self.Wrap(value))
	}
	return out
}

// Columnize answers Grammar::columnize.
func (g *Grammar) Columnize(columns []any) string {
	return strings.Join(g.self.WrapArray(columns), ", ")
}

// Parameterize answers Grammar::parameterize.
func (g *Grammar) Parameterize(values []any) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, g.self.Parameter(value))
	}
	return strings.Join(out, ", ")
}

// Parameter answers Grammar::parameter.
//
// # Why every dialect writes "?", Postgres included
//
// Postgres numbers its placeholders, and this grammar still emits "?" for it.
// The numbering happens once, at the connection, in database.Dialect.Rebind,
// which walks the finished statement and rewrites each "?" to $1, $2 and so on
// while leaving the ones inside string literals and comments alone. Numbering
// here instead would mean the grammar had to know a fragment's position in a
// statement it has not finished building yet -- a subquery compiled on its own
// would start over at $1 -- and it would give the project two placeholder
// conventions where one will do.
func (g *Grammar) Parameter(value any) string {
	if query.IsExpression(value) {
		return text(g.self.GetValue(value))
	}
	return "?"
}

// GetValue answers Grammar::getValue: an expression collapses to what it
// carries, anything else is itself.
func (g *Grammar) GetValue(expression any) any {
	switch e := expression.(type) {
	case query.Expression:
		return g.GetValue(e.Value())
	case *query.Expression:
		if e == nil {
			return nil
		}
		return g.GetValue(e.Value())
	}
	return expression
}

// IsExpression answers Grammar::isExpression.
func (g *Grammar) IsExpression(value any) bool { return query.IsExpression(value) }

// SetTablePrefix answers Grammar::setTablePrefix.
//
// query.BaseGrammar returns nil from it, because it cannot name the grammar
// that embedded it; this returns the outermost grammar, which is what `$this`
// is in PHP.
func (g *Grammar) SetTablePrefix(prefix string) query.Grammar {
	g.BaseGrammar.SetTablePrefix(prefix)
	if grammar, ok := g.self.(query.Grammar); ok {
		return grammar
	}
	return nil
}

// concatenate answers Grammar::concatenate: the pieces of a statement, with the
// empty ones dropped.
func concatenate(components []component) string {
	parts := make([]string, 0, len(components))
	for _, c := range components {
		if c.sql != "" {
			parts = append(parts, c.sql)
		}
	}
	return strings.Join(parts, " ")
}

func get(components []component, name string) string {
	for _, c := range components {
		if c.name == name {
			return c.sql
		}
	}
	return ""
}

func setComponent(components []component, name, sql string) {
	for i := range components {
		if components[i].name == name {
			components[i].sql = sql
			return
		}
	}
}

// take removes a component and returns its SQL, which is what `unset` does to
// the orders before a group limit folds them into the window function.
func take(components *[]component, name string) string {
	for i, c := range *components {
		if c.name == name {
			*components = append((*components)[:i], (*components)[i+1:]...)
			return c.sql
		}
	}
	return ""
}

// leadingBoolean matches the "and " or "or " the builder writes in front of
// every clause for the compilers' convenience.
var leadingBoolean = regexp.MustCompile(`(?i)^(and |or )`)

// removeLeadingBoolean answers Grammar::removeLeadingBoolean.
//
// The PHP's regular expression is unanchored and replaces the first match
// anywhere in the string; anchoring it here reaches the same result for every
// clause list -- they all start with the boolean -- without being able to eat a
// conjunction out of the middle of a raw clause.
func removeLeadingBoolean(value string) string {
	return leadingBoolean.ReplaceAllString(value, "")
}

// aliasPattern is the PHP's /\s+as\s+/i.
var aliasPattern = regexp.MustCompile(`(?i)\s+as\s+`)

// aliasSplit splits "column as alias" the way preg_split does.
func aliasSplit(value string) []string {
	return aliasPattern.Split(value, 2)
}

// lastSegment answers `last(explode($separator, $value))`.
func lastSegment(value, separator string) string {
	if i := strings.LastIndex(strings.ToLower(value), separator); i >= 0 {
		return value[i+len(separator):]
	}
	return value
}

// unsupportedClause answers the RuntimeException the PHP throws from inside a
// where compiler.
//
// CompileSelect returns a string, because that is what query.Grammar declares
// and what a builder can use, so the refusal cannot travel as an error from
// here. It travels as a clause that is false: the query returns nothing rather
// than returning rows nobody filtered for, and the reason rides along in a
// comment so the log says what happened. Every entry point that can return an
// error does.
func unsupportedClause(err error) string {
	return "1 = 0 /* " + strings.ReplaceAll(err.Error(), "*/", "* /") + " */"
}

// unsupportedJoin is a join the grammar cannot spell.
//
// A where clause that cannot be compiled becomes a false one, because a filter
// nobody can spell must not widen the result. A join cannot take that route:
// dropping it changes which rows come back in a direction that depends on the
// join type, and an inner join dropped from a query is rows that were never
// meant to be there. So the fragment is deliberately not SQL, the engine
// refuses the statement, and the reason travels in the message.
func unsupportedJoin(err error) string {
	return "/* " + strings.ReplaceAll(err.Error(), "*/", "* /") + " */ unsupported join"
}

// sortedKeys is the column order of a values map. See CompileInsert.
func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func toAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// ends answers `array_first` and `array_last` of a between clause's bounds.
func ends(values []any) (first, last any) {
	if len(values) == 0 {
		return nil, nil
	}
	return values[0], values[len(values)-1]
}

// joinValues writes values into the statement, for the raw "in" clauses whose
// caller has already made every value an integer.
func joinValues(values []any) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, text(value))
	}
	return strings.Join(out, ", ")
}
