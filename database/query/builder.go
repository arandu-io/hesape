package query

import (
	"strings"
)

// Builder answers Illuminate\Database\Query\Builder: the fluent query builder.
//
// It builds SQL and does not decide who may run it. Authorization lives one
// layer up, in the repository that holds a security.Grant and filters by the
// tenant that Grant carries -- on reads exactly as on writes. A Builder reached
// without going through that layer is SQL nobody authorized, which is why
// nothing outside a repository should be constructing one.
//
// # Fields the PHP makes public, and the three that could not stay
//
// The grammar reads the builder's state directly in PHP, where $query->wheres
// is a public property, so the state is exported here too. Three groups could
// not keep the name unchanged, because PHP holds properties and methods in
// separate namespaces and Go holds them in one: from, limit, offset, distinct,
// lock, aggregate, timeout, groupLimit and indexHint are all a property AND a
// method there. The property is unexported here and reachable through a getter
// -- GetLimit, GetOffset and GetColumns are the PHP's own; GetFrom, GetDistinct,
// GetLock, GetAggregate, GetTimeout, GetGroupLimit and GetIndexHint are
// mechanical, and exist only because the fluent method claimed the name.
type Builder struct {
	// Connection is Builder::$connection.
	Connection Connection

	// Grammar is Builder::$grammar.
	Grammar Grammar

	// Processor is Builder::$processor.
	Processor Processor

	// Bindings is Builder::$bindings, keyed by the same seven segments the PHP
	// uses. The order of the keys is the order they are concatenated in, which
	// is why GetBindings walks bindingOrder rather than ranging the map.
	Bindings map[string][]any

	// Columns is Builder::$columns. Nil means "not selected yet", which is what
	// lets Get default to * while an explicit Select of nothing selects nothing.
	Columns []any

	// Wheres is Builder::$wheres.
	Wheres []Where

	// Joins is Builder::$joins.
	Joins []*JoinClause

	// Groups is Builder::$groups.
	Groups []any

	// Havings is Builder::$havings.
	Havings []Having

	// Orders is Builder::$orders.
	Orders []Order

	// Unions is Builder::$unions.
	Unions []Union

	// UnionOrders is Builder::$unionOrders.
	UnionOrders []Order

	// UnionLimit is Builder::$unionLimit.
	UnionLimit *int

	// UnionOffset is Builder::$unionOffset.
	UnionOffset *int

	// BeforeQueryCallbacks is Builder::$beforeQueryCallbacks.
	BeforeQueryCallbacks []func(*Builder)

	// AfterQueryCallbacks is Builder::$afterQueryCallbacks. Each runs over the
	// result of Get, Pluck or Cursor before it is handed back.
	AfterQueryCallbacks []func([]Record) []Record

	// subqueries records the queries compiled into a from, a select or a join,
	// so that scopeSubqueryClauses can put the tenant on each of them when the
	// statement runs. See pendingSub.
	subqueries []pendingSub

	from        any
	distinct    any
	indexHint   *IndexHint
	limit       *int
	groupLimit  *GroupLimit
	offset      *int
	lock        any
	aggregate   *Aggregate
	timeout     *int
	useWritePDO bool
}

// Connection answers Illuminate\Database\ConnectionInterface, narrowed to what
// a query builder asks of it.
//
// It is declared here rather than imported from the database package because
// the interface belongs with its consumer in Go, and because database imports
// this package: naming it there would close the cycle.
type Connection interface {
	// Select runs a select and returns its rows.
	Select(query string, bindings []any, useReadPDO bool) ([]Record, error)

	// Insert runs an insert.
	Insert(query string, bindings []any) (bool, error)

	// Update runs an update and returns the number of rows affected.
	Update(query string, bindings []any) (int64, error)

	// Delete runs a delete and returns the number of rows affected.
	Delete(query string, bindings []any) (int64, error)

	// Statement runs a statement that returns neither rows nor a count.
	Statement(query string, bindings []any) (bool, error)
}

// Processor answers Illuminate\Database\Query\Processors\Processor: the hook
// that lets a driver adjust results on the way out.
type Processor interface {
	// ProcessSelect answers Processor::processSelect.
	ProcessSelect(query *Builder, results []Record) []Record

	// ProcessInsertGetID answers Processor::processInsertGetId. The PHP spells
	// the last word Id.
	ProcessInsertGetID(query *Builder, sql string, values []any, sequence string) (int64, error)
}

// bindingOrder is the key order of Builder::$bindings. The list is the order
// the fragments appear in the compiled statement, and GetBindings depends on
// it: a binding read out of order is a value landing in the wrong placeholder,
// which is a wrong answer rather than an error.
var bindingOrder = []string{"select", "from", "join", "where", "groupBy", "having", "order", "union", "unionOrder"}

// NewBuilder answers Builder::__construct.
func NewBuilder(connection Connection, grammar Grammar, processor Processor) *Builder {
	b := &Builder{
		Connection: connection,
		Grammar:    grammar,
		Processor:  processor,
		Bindings:   make(map[string][]any, len(bindingOrder)),
	}
	for _, key := range bindingOrder {
		b.Bindings[key] = nil
	}
	return b
}

// Select answers Builder::select.
//
// Calling it replaces any previous selection, as the PHP does. AddSelect is how
// a column is appended.
func (b *Builder) Select(columns ...any) *Builder {
	b.Columns = nil
	b.Bindings["select"] = nil
	b.forgetSubqueriesOfKind("select")
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	b.Columns = append(b.Columns, columns...)
	return b
}

// SelectRaw answers Builder::selectRaw.
func (b *Builder) SelectRaw(expression string, bindings ...any) *Builder {
	b.AddSelect(Raw(expression))
	if len(bindings) > 0 {
		b.AddBinding(bindings, "select")
	}
	return b
}

// AddSelect answers Builder::addSelect.
func (b *Builder) AddSelect(columns ...any) *Builder {
	b.Columns = append(b.Columns, columns...)
	return b
}

// Distinct answers Builder::distinct.
//
// With no argument it is a plain DISTINCT; with columns it is the engine's
// DISTINCT ON, which only Postgres compiles.
func (b *Builder) Distinct(columns ...any) *Builder {
	if len(columns) > 0 {
		b.distinct = columns
	} else {
		b.distinct = true
	}
	return b
}

// From answers Builder::from. The table may be a string or an Expression.
func (b *Builder) From(table any, as ...string) *Builder {
	b.forgetSubqueriesOfKind("from")
	if len(as) > 0 && as[0] != "" {
		b.from = stringify(table) + " as " + as[0]
		return b
	}
	b.from = table
	return b
}

// FromRaw answers Builder::fromRaw.
func (b *Builder) FromRaw(expression string, bindings ...any) *Builder {
	b.forgetSubqueriesOfKind("from")
	b.from = Raw(expression)
	if len(bindings) > 0 {
		b.AddBinding(bindings, "from")
	}
	return b
}

// Where answers Builder::where, the two and three argument forms both.
//
// Called with two arguments the operator is "=", which is Builder's
// prepareValueAndOperator: where('id', 1) and where('id', '=', 1) are the same
// clause. Called with a func it opens a nested group, as passing a Closure does.
func (b *Builder) Where(column any, args ...any) *Builder {
	return b.addWhere("and", column, args...)
}

// OrWhere answers Builder::orWhere.
func (b *Builder) OrWhere(column any, args ...any) *Builder {
	return b.addWhere("or", column, args...)
}

func (b *Builder) addWhere(boolean string, column any, args ...any) *Builder {
	if nested, ok := column.(func(*Builder)); ok {
		return b.WhereNested(nested, boolean)
	}

	operator, value := prepareValueAndOperator(args...)
	b.Wheres = append(b.Wheres, Where{
		Type:     "Basic",
		Column:   column,
		Operator: operator,
		Value:    value,
		Boolean:  boolean,
	})
	if !IsExpression(value) {
		b.AddBinding([]any{value}, "where")
	}
	return b
}

// prepareValueAndOperator answers Builder::prepareValueAndOperator.
//
// One argument is a value compared with "=". Two are an operator and a value.
// The PHP also guards against where('id', '>') -- an operator with no value --
// by throwing; here the missing value is nil, which compiles to a comparison
// against NULL and is visible in the SQL rather than hidden in an exception.
func prepareValueAndOperator(args ...any) (operator string, value any) {
	switch len(args) {
	case 0:
		return "=", nil
	case 1:
		return "=", args[0]
	default:
		return stringify(args[0]), args[1]
	}
}

// WhereNested answers Builder::whereNested.
func (b *Builder) WhereNested(callback func(*Builder), boolean string) *Builder {
	nested := b.ForNestedWhere()
	callback(nested)
	return b.AddNestedWhereQuery(nested, boolean)
}

// ForNestedWhere answers Builder::forNestedWhere.
func (b *Builder) ForNestedWhere() *Builder {
	nested := b.NewQuery()
	nested.from = b.from
	return nested
}

// AddNestedWhereQuery answers Builder::addNestedWhereQuery.
//
// A group with no clauses is dropped rather than compiled, because "()" is a
// syntax error on every engine and an empty callback is an ordinary outcome of
// a conditional filter.
func (b *Builder) AddNestedWhereQuery(query *Builder, boolean string) *Builder {
	if len(query.Wheres) == 0 {
		return b
	}
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{Type: "Nested", Query: query, Boolean: boolean})
	b.AddBinding(query.Bindings["where"], "where")
	return b
}

// WhereColumn answers Builder::whereColumn.
func (b *Builder) WhereColumn(first any, args ...any) *Builder {
	operator, second := prepareValueAndOperator(args...)
	b.Wheres = append(b.Wheres, Where{
		Type:     "Column",
		First:    first,
		Operator: operator,
		Second:   second,
		Boolean:  "and",
	})
	return b
}

// WhereRaw answers Builder::whereRaw.
//
// The bindings are recorded on the clause as well as appended to the segment.
// PHP only appends, because nothing there ever needs to rebuild the flat list;
// here the tenant filter does -- scoping a subquery changes how many bindings
// it contributes, and the list can only be put back in order if every clause
// knows which of them are its own.
func (b *Builder) WhereRaw(sql string, bindings ...any) *Builder {
	b.Wheres = append(b.Wheres, Where{Type: "Raw", SQL: sql, Boolean: "and", Values: bindings})
	if len(bindings) > 0 {
		b.AddBinding(bindings, "where")
	}
	return b
}

// OrWhereRaw answers Builder::orWhereRaw.
func (b *Builder) OrWhereRaw(sql string, bindings ...any) *Builder {
	b.Wheres = append(b.Wheres, Where{Type: "Raw", SQL: sql, Boolean: "or", Values: bindings})
	if len(bindings) > 0 {
		b.AddBinding(bindings, "where")
	}
	return b
}

// WhereBindings rebuilds the where segment from the clauses, in the order the
// compiled statement consumes them.
//
// It exists so that a clause carrying a subquery can be replaced -- which is
// what putting the tenant on a subquery does -- without the values afterwards
// sliding onto the wrong placeholders. Every clause type that contributes a
// binding is listed here, and a type that starts contributing one has to be
// added: a clause this does not know about loses its values silently, which is
// a wrong answer rather than an error.
func (b *Builder) WhereBindings() []any {
	out := make([]any, 0, len(b.Bindings["where"]))
	for _, where := range b.Wheres {
		switch where.Type {
		case "Basic":
			// A Basic clause carries a subquery only when WhereSubCount built
			// it, and then the subquery is on the left of the comparison: its
			// bindings come first because that is the order the clause
			// compiles in. Reading them off the clause is what lets this list be
			// rebuilt after scopeSubqueries has put the tenant on the subquery.
			if where.Query != nil {
				out = append(out, where.Query.GetBindings()...)
			}
			if !IsExpression(where.Value) {
				out = append(out, where.Value)
			}
		case "In", "Between":
			// An In carries either a list of values or a subquery, never both.
			// The subquery form is the one that loses its bindings silently if
			// this branch only ever looks at Values.
			if where.Query != nil {
				out = append(out, where.Query.GetBindings()...)
				continue
			}
			for _, value := range where.Values {
				if !IsExpression(value) {
					out = append(out, value)
				}
			}
		case "Raw":
			out = append(out, where.Values...)
		case "Like", "valueBetween", "Fulltext",
			"Date", "Time", "Day", "Month", "Year",
			"JsonContains", "JsonOverlaps", "JsonLength":
			// One value each, bound unless it compiled into the statement. The
			// clause carries the value that was bound and not the one that was
			// written, which is what makes this rebuildable -- see WhereLike.
			if !IsExpression(where.Value) {
				out = append(out, where.Value)
			}
		case "RowValues":
			for _, value := range where.Values {
				if !IsExpression(value) {
					out = append(out, value)
				}
			}
		case "Nested":
			if where.Query != nil {
				out = append(out, where.Query.WhereBindings()...)
			}
		case "Exists":
			if where.Query != nil {
				out = append(out, where.Query.GetBindings()...)
			}
		}
	}
	return out
}

// WhereIn answers Builder::whereIn.
//
// An empty list is kept rather than dropped: the PHP compiles it to "0 = 1", so
// a filter over an empty set returns nothing. Dropping it would return
// everything, which is the difference between an empty page and a data leak.
func (b *Builder) WhereIn(column any, values []any) *Builder {
	return b.addWhereIn(column, values, "and", false)
}

// OrWhereIn answers Builder::orWhereIn.
func (b *Builder) OrWhereIn(column any, values []any) *Builder {
	return b.addWhereIn(column, values, "or", false)
}

// WhereNotIn answers Builder::whereNotIn.
func (b *Builder) WhereNotIn(column any, values []any) *Builder {
	return b.addWhereIn(column, values, "and", true)
}

// OrWhereNotIn answers Builder::orWhereNotIn.
func (b *Builder) OrWhereNotIn(column any, values []any) *Builder {
	return b.addWhereIn(column, values, "or", true)
}

func (b *Builder) addWhereIn(column any, values []any, boolean string, not bool) *Builder {
	b.Wheres = append(b.Wheres, Where{
		Type: "In", Column: column, Values: values, Boolean: boolean, Not: not,
	})
	for _, value := range values {
		if !IsExpression(value) {
			b.AddBinding([]any{value}, "where")
		}
	}
	return b
}

// WhereNull answers Builder::whereNull.
func (b *Builder) WhereNull(columns ...any) *Builder {
	return b.addWhereNull(columns, "and", false)
}

// OrWhereNull answers Builder::orWhereNull.
func (b *Builder) OrWhereNull(columns ...any) *Builder {
	return b.addWhereNull(columns, "or", false)
}

// WhereNotNull answers Builder::whereNotNull.
func (b *Builder) WhereNotNull(columns ...any) *Builder {
	return b.addWhereNull(columns, "and", true)
}

// OrWhereNotNull answers Builder::orWhereNotNull.
func (b *Builder) OrWhereNotNull(columns ...any) *Builder {
	return b.addWhereNull(columns, "or", true)
}

func (b *Builder) addWhereNull(columns []any, boolean string, not bool) *Builder {
	for _, column := range columns {
		b.Wheres = append(b.Wheres, Where{Type: "Null", Column: column, Boolean: boolean, Not: not})
	}
	return b
}

// WhereBetween answers Builder::whereBetween.
func (b *Builder) WhereBetween(column any, from, to any) *Builder {
	return b.addWhereBetween(column, from, to, "and", false)
}

// OrWhereBetween answers Builder::orWhereBetween.
func (b *Builder) OrWhereBetween(column any, from, to any) *Builder {
	return b.addWhereBetween(column, from, to, "or", false)
}

// WhereNotBetween answers Builder::whereNotBetween.
func (b *Builder) WhereNotBetween(column any, from, to any) *Builder {
	return b.addWhereBetween(column, from, to, "and", true)
}

// OrWhereNotBetween answers Builder::orWhereNotBetween.
func (b *Builder) OrWhereNotBetween(column any, from, to any) *Builder {
	return b.addWhereBetween(column, from, to, "or", true)
}

func (b *Builder) addWhereBetween(column, from, to any, boolean string, not bool) *Builder {
	b.Wheres = append(b.Wheres, Where{
		Type: "Between", Column: column, Values: []any{from, to}, Boolean: boolean, Not: not,
	})
	for _, value := range []any{from, to} {
		if !IsExpression(value) {
			b.AddBinding([]any{value}, "where")
		}
	}
	return b
}

// WhereExists answers Builder::whereExists.
func (b *Builder) WhereExists(callback func(*Builder), boolean string, not bool) *Builder {
	query := b.NewQuery()
	callback(query)
	return b.AddWhereExistsQuery(query, boolean, not)
}

// AddWhereExistsQuery answers Builder::addWhereExistsQuery.
func (b *Builder) AddWhereExistsQuery(query *Builder, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{Type: "Exists", Query: query, Boolean: boolean, Not: not})
	b.AddBinding(query.GetBindings(), "where")
	return b
}

// Join answers Builder::join.
func (b *Builder) Join(table any, first any, args ...any) *Builder {
	return b.addJoin("inner", table, first, args...)
}

// LeftJoin answers Builder::leftJoin.
func (b *Builder) LeftJoin(table any, first any, args ...any) *Builder {
	return b.addJoin("left", table, first, args...)
}

// RightJoin answers Builder::rightJoin.
func (b *Builder) RightJoin(table any, first any, args ...any) *Builder {
	return b.addJoin("right", table, first, args...)
}

// CrossJoin answers Builder::crossJoin. With no condition it is a bare cross
// join; with one it compiles as an inner join, exactly as the PHP does.
func (b *Builder) CrossJoin(table any, first ...any) *Builder {
	if len(first) == 0 {
		b.Joins = append(b.Joins, NewJoinClause(b, "cross", table))
		return b
	}
	return b.addJoin("cross", table, first[0], first[1:]...)
}

func (b *Builder) addJoin(typ string, table any, first any, args ...any) *Builder {
	return b.addJoinClause(typ, false, table, first, args...)
}

// GroupBy answers Builder::groupBy.
func (b *Builder) GroupBy(groups ...any) *Builder {
	b.Groups = append(b.Groups, groups...)
	return b
}

// GroupByRaw answers Builder::groupByRaw.
func (b *Builder) GroupByRaw(sql string, bindings ...any) *Builder {
	b.Groups = append(b.Groups, Raw(sql))
	if len(bindings) > 0 {
		b.AddBinding(bindings, "groupBy")
	}
	return b
}

// Having answers Builder::having. The body is in havings.go, shared with
// OrHaving: the two differ by their conjunction and by nothing else.
func (b *Builder) Having(column any, args ...any) *Builder {
	return b.addHaving("and", column, args...)
}

// HavingRaw answers Builder::havingRaw.
func (b *Builder) HavingRaw(sql string, bindings ...any) *Builder {
	b.Havings = append(b.Havings, Having{Type: "Raw", SQL: sql, Boolean: "and"})
	if len(bindings) > 0 {
		b.AddBinding(bindings, "having")
	}
	return b
}

// OrderBy answers Builder::orderBy. A direction other than asc or desc is a
// caller writing SQL into a direction, so anything else reads as asc.
func (b *Builder) OrderBy(column any, direction ...string) *Builder {
	dir := "asc"
	if len(direction) > 0 && lower(direction[0]) == "desc" {
		dir = "desc"
	}
	order := Order{Column: column, Direction: dir}
	if len(b.Unions) > 0 {
		b.UnionOrders = append(b.UnionOrders, order)
	} else {
		b.Orders = append(b.Orders, order)
	}
	return b
}

// OrderByDesc answers Builder::orderByDesc.
func (b *Builder) OrderByDesc(column any) *Builder { return b.OrderBy(column, "desc") }

// Latest answers Builder::latest. The column defaults to created_at.
func (b *Builder) Latest(column ...any) *Builder {
	return b.OrderBy(defaultTimestampColumn(column), "desc")
}

// Oldest answers Builder::oldest. The column defaults to created_at.
func (b *Builder) Oldest(column ...any) *Builder {
	return b.OrderBy(defaultTimestampColumn(column), "asc")
}

func defaultTimestampColumn(column []any) any {
	if len(column) > 0 && column[0] != nil {
		return column[0]
	}
	return "created_at"
}

// OrderByRaw answers Builder::orderByRaw.
func (b *Builder) OrderByRaw(sql string, bindings ...any) *Builder {
	order := Order{SQL: Raw(sql)}
	if len(b.Unions) > 0 {
		b.UnionOrders = append(b.UnionOrders, order)
		b.AddBinding(bindings, "unionOrder")
	} else {
		b.Orders = append(b.Orders, order)
		b.AddBinding(bindings, "order")
	}
	return b
}

// InRandomOrder answers Builder::inRandomOrder.
func (b *Builder) InRandomOrder(seed ...string) *Builder {
	s := ""
	if len(seed) > 0 {
		s = seed[0]
	}
	return b.OrderByRaw(b.Grammar.CompileRandom(s))
}

// Reorder answers Builder::reorder: it drops every order, and adds one back
// when given a column. Pass nil to drop the ordering and add nothing.
func (b *Builder) Reorder(column any, direction ...string) *Builder {
	b.Orders = nil
	b.UnionOrders = nil
	b.Bindings["order"] = nil
	b.Bindings["unionOrder"] = nil
	if column == nil {
		return b
	}
	return b.OrderBy(column, direction...)
}

// ReorderDesc answers Builder::reorderDesc.
func (b *Builder) ReorderDesc(column any) *Builder { return b.Reorder(column, "desc") }

// Limit answers Builder::limit. A negative limit is dropped, as the PHP's
// `if ($value >= 0)` does.
func (b *Builder) Limit(value int) *Builder {
	if value < 0 {
		return b
	}
	if len(b.Unions) > 0 {
		b.UnionLimit = intPtr(value)
	} else {
		b.limit = intPtr(value)
	}
	return b
}

// Take answers Builder::take, the alias of limit.
func (b *Builder) Take(value int) *Builder { return b.Limit(value) }

// Offset answers Builder::offset. A negative offset reads as zero, as the PHP's
// max(0, $value) does.
func (b *Builder) Offset(value int) *Builder {
	if value < 0 {
		value = 0
	}
	if len(b.Unions) > 0 {
		b.UnionOffset = intPtr(value)
	} else {
		b.offset = intPtr(value)
	}
	return b
}

// Skip answers Builder::skip, the alias of offset.
func (b *Builder) Skip(value int) *Builder { return b.Offset(value) }

// ForPage answers Builder::forPage.
//
// It is offset pagination, and it is the wrong tool past the first few pages:
// the engine counts and discards every row it skips, and a row inserted between
// two requests shifts the boundary so that a row is never shown. CursorPaginate
// in the pagination package names the boundary by value instead.
func (b *Builder) ForPage(page, perPage int) *Builder {
	if page < 1 {
		page = 1
	}
	return b.Offset((page - 1) * perPage).Limit(perPage)
}

// GroupLimit answers Builder::groupLimit.
func (b *Builder) GroupLimit(value int, column string) *Builder {
	if value >= 0 {
		b.groupLimit = &GroupLimit{Value: value, Column: column}
	}
	return b
}

// Union answers Builder::union.
func (b *Builder) Union(query *Builder, all ...bool) *Builder {
	isAll := len(all) > 0 && all[0]
	b.Unions = append(b.Unions, Union{Query: query, All: isAll})
	b.AddBinding(query.GetBindings(), "union")
	return b
}

// UnionAll answers Builder::unionAll.
func (b *Builder) UnionAll(query *Builder) *Builder { return b.Union(query, true) }

// Lock answers Builder::lock.
func (b *Builder) Lock(value any) *Builder {
	b.lock = value
	return b
}

// LockForUpdate answers Builder::lockForUpdate.
func (b *Builder) LockForUpdate() *Builder { return b.Lock(true) }

// SharedLock answers Builder::sharedLock.
func (b *Builder) SharedLock() *Builder { return b.Lock(false) }

// Timeout answers Builder::timeout.
func (b *Builder) Timeout(seconds int) *Builder {
	b.timeout = intPtr(seconds)
	return b
}

// UseIndex answers Builder::useIndex.
func (b *Builder) UseIndex(index string) *Builder {
	b.indexHint = NewIndexHint("hint", index)
	return b
}

// ForceIndex answers Builder::forceIndex.
func (b *Builder) ForceIndex(index string) *Builder {
	b.indexHint = NewIndexHint("force", index)
	return b
}

// IgnoreIndex answers Builder::ignoreIndex.
func (b *Builder) IgnoreIndex(index string) *Builder {
	b.indexHint = NewIndexHint("ignore", index)
	return b
}

// BeforeQuery answers Builder::beforeQuery.
func (b *Builder) BeforeQuery(callback func(*Builder)) *Builder {
	b.BeforeQueryCallbacks = append(b.BeforeQueryCallbacks, callback)
	return b
}

// ApplyBeforeQueryCallbacks answers Builder::applyBeforeQueryCallbacks.
func (b *Builder) ApplyBeforeQueryCallbacks() {
	for _, callback := range b.BeforeQueryCallbacks {
		callback(b)
	}
	b.BeforeQueryCallbacks = nil
}

// ToSQL answers Builder::toSql. The PHP spells it toSql; Go initialisms are
// upper case throughout.
func (b *Builder) ToSQL() string {
	b.ApplyBeforeQueryCallbacks()
	return b.Grammar.CompileSelect(b)
}

// NewQuery answers Builder::newQuery.
func (b *Builder) NewQuery() *Builder {
	return NewBuilder(b.Connection, b.Grammar, b.Processor)
}

// AddBinding answers Builder::addBinding.
//
// An unknown binding type is dropped rather than appended to a key the grammar
// never reads, where the PHP throws InvalidArgumentException. The seven names
// are in bindingOrder and are not open to extension.
func (b *Builder) AddBinding(value []any, typ string) *Builder {
	if typ == "" {
		typ = "where"
	}
	if _, ok := b.Bindings[typ]; !ok {
		return b
	}
	b.Bindings[typ] = append(b.Bindings[typ], value...)
	return b
}

// GetBindings answers Builder::getBindings: every binding, flattened in the
// order the compiled statement consumes them.
func (b *Builder) GetBindings() []any {
	out := make([]any, 0)
	for _, key := range bindingOrder {
		out = append(out, b.Bindings[key]...)
	}
	return out
}

// GetRawBindings answers Builder::getRawBindings.
func (b *Builder) GetRawBindings() map[string][]any { return b.Bindings }

// SetBindings answers Builder::setBindings.
func (b *Builder) SetBindings(bindings []any, typ string) *Builder {
	if typ == "" {
		typ = "where"
	}
	if _, ok := b.Bindings[typ]; !ok {
		return b
	}
	b.Bindings[typ] = bindings
	b.forgetSubqueriesOfSegment(typ)
	return b
}

// MergeBindings answers Builder::mergeBindings.
func (b *Builder) MergeBindings(query *Builder) *Builder {
	for _, key := range bindingOrder {
		b.Bindings[key] = append(b.Bindings[key], query.Bindings[key]...)
	}
	return b
}

// UseWritePDO answers Builder::useWritePdo.
//
// It sends the read to the write connection, for the case a replica has not
// caught up yet: a row inserted and read back in the same request is not there
// on a replica that is two hundred milliseconds behind.
//
// The state behind it is unexported and read through UsingWritePDO, for the
// same reason From, Limit and Offset are: PHP holds $useWritePdo and
// useWritePdo() in separate namespaces and Go holds them in one.
func (b *Builder) UseWritePDO() *Builder {
	b.useWritePDO = true
	return b
}

// UsingWritePDO reports whether the read goes to the write connection.
// Mechanical, for the reason UseWritePDO gives.
func (b *Builder) UsingWritePDO() bool { return b.useWritePDO }

// AfterQuery answers Builder::afterQuery.
func (b *Builder) AfterQuery(callback func([]Record) []Record) *Builder {
	b.AfterQueryCallbacks = append(b.AfterQueryCallbacks, callback)
	return b
}

// ApplyAfterQueryCallbacks answers Builder::applyAfterQueryCallbacks. Each
// callback receives what the one before it returned, so they compose.
func (b *Builder) ApplyAfterQueryCallbacks(result []Record) []Record {
	for _, callback := range b.AfterQueryCallbacks {
		result = callback(result)
	}
	return result
}

// GetColumns answers Builder::getColumns.
//
// The PHP maps each column through the grammar's getValue, so an Expression
// comes back as the SQL it carries rather than as the object, and returns []
// rather than null when nothing was selected. Both are reproduced: a caller
// ranging the result must not have to special-case nil, and must not receive a
// type it would have to unwrap.
func (b *Builder) GetColumns() []any {
	out := make([]any, 0, len(b.Columns))
	for _, column := range b.Columns {
		if IsExpression(column) {
			out = append(out, stringify(column))
			continue
		}
		out = append(out, column)
	}
	return out
}

// GetLimit answers Builder::getLimit.
func (b *Builder) GetLimit() *int { return b.limit }

// GetOffset answers Builder::getOffset.
func (b *Builder) GetOffset() *int { return b.offset }

// GetConnection answers Builder::getConnection.
func (b *Builder) GetConnection() Connection { return b.Connection }

// GetGrammar answers Builder::getGrammar.
func (b *Builder) GetGrammar() Grammar { return b.Grammar }

// GetProcessor answers Builder::getProcessor.
func (b *Builder) GetProcessor() Processor { return b.Processor }

// GetFrom returns the table the query reads from.
//
// The PHP reads $query->from directly; the fluent From claimed the name here,
// so the state is reachable through a getter the PHP does not need.
func (b *Builder) GetFrom() any { return b.from }

// GetDistinct returns the DISTINCT state: false, true, or the columns of a
// DISTINCT ON. Mechanical, for the same reason as GetFrom.
func (b *Builder) GetDistinct() any { return b.distinct }

// GetLock returns the lock state. Mechanical, for the same reason as GetFrom.
func (b *Builder) GetLock() any { return b.lock }

// GetAggregate returns the aggregate being compiled, if any. Mechanical, for
// the same reason as GetFrom.
func (b *Builder) GetAggregate() *Aggregate { return b.aggregate }

// SetAggregate answers Builder::setAggregate.
//
// It also drops the ordering when there is no GROUP BY, which is the half of
// the PHP that is easy to miss and the half that matters: ORDER BY on an
// aggregate with no grouping is a sort of one row, and MySQL in strict mode
// refuses it outright when the ordering column is not in the select. The order
// bindings go with it, or the placeholders and the values stop lining up.
func (b *Builder) SetAggregate(function string, columns []any) *Builder {
	b.aggregate = &Aggregate{Function: function, Columns: columns}
	if len(b.Groups) == 0 {
		b.Orders = nil
		b.Bindings["order"] = nil
	}
	return b
}

// GetTimeout returns the statement timeout in seconds, if one was set.
// Mechanical, for the same reason as GetFrom.
func (b *Builder) GetTimeout() *int { return b.timeout }

// GetGroupLimit returns the per-group limit, if one was set. Mechanical, for
// the same reason as GetFrom.
func (b *Builder) GetGroupLimit() *GroupLimit { return b.groupLimit }

// GetIndexHint returns the index hint, if one was set. Mechanical, for the same
// reason as GetFrom.
func (b *Builder) GetIndexHint() *IndexHint { return b.indexHint }

// Clone answers Builder::clone.
//
// The slices and the binding map are copied, so a clone that gains a where does
// not grow one on the query it was cloned from. That sharing is the bug behind
// every "the count query has the pagination limit on it" report.
func (b *Builder) Clone() *Builder {
	out := *b
	out.Columns = append([]any(nil), b.Columns...)
	out.Wheres = append([]Where(nil), b.Wheres...)
	out.Joins = append([]*JoinClause(nil), b.Joins...)
	out.Groups = append([]any(nil), b.Groups...)
	out.Havings = append([]Having(nil), b.Havings...)
	out.Orders = append([]Order(nil), b.Orders...)
	out.Unions = append([]Union(nil), b.Unions...)
	out.UnionOrders = append([]Order(nil), b.UnionOrders...)
	out.subqueries = append([]pendingSub(nil), b.subqueries...)
	out.Bindings = make(map[string][]any, len(b.Bindings))
	for key, values := range b.Bindings {
		out.Bindings[key] = append([]any(nil), values...)
	}
	return &out
}

// CloneWithout answers Builder::cloneWithout: a clone with the named properties
// reset to their zero value.
func (b *Builder) CloneWithout(properties ...string) *Builder {
	out := b.Clone()
	for _, property := range properties {
		switch strings.ToLower(property) {
		case "columns":
			out.Columns = nil
			out.forgetSubqueriesOfKind("select")
		case "wheres":
			out.Wheres = nil
		case "joins":
			out.Joins = nil
			out.forgetSubqueriesOfKind("join")
		case "groups":
			out.Groups = nil
		case "havings":
			out.Havings = nil
		case "orders":
			out.Orders = nil
		case "unions":
			out.Unions = nil
		case "unionorders":
			out.UnionOrders = nil
		case "unionlimit":
			out.UnionLimit = nil
		case "unionoffset":
			out.UnionOffset = nil
		case "limit":
			out.limit = nil
		case "offset":
			out.offset = nil
		case "aggregate":
			out.aggregate = nil
		}
	}
	return out
}

// CloneWithoutBindings answers Builder::cloneWithoutBindings.
func (b *Builder) CloneWithoutBindings(except ...string) *Builder {
	out := b.Clone()
	for _, typ := range except {
		if _, ok := out.Bindings[typ]; ok {
			out.Bindings[typ] = nil
			out.forgetSubqueriesOfSegment(typ)
		}
	}
	return out
}
