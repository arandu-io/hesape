package query

import (
	"context"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/pagination"
)

// The aggregate and pagination half of Illuminate\Database\Query\Builder.
//
// Every function here goes through Get, so every one of them carries the tenant
// of the Grant: a count that counted rows the caller may not read is the same
// leak as a select that returned them, one integer at a time.

// Aggregate answers Builder::aggregate.
//
// The result is whatever the engine returned for the aggregate column -- an
// int64, a float64, a string or a []byte, depending on the driver and the
// function. Count and NumericAggregate are the two that commit to a Go type.
// A nil result means the query matched nothing, which for min and max is the
// answer rather than an error.
func (b *Builder) Aggregate(ctx context.Context, g auth.Grant, function string, columns ...any) (any, error) {
	columns = wrapColumns(columns)

	// A union or a having needs the selected columns kept: the aggregate is
	// computed over what they produce, and dropping the select changes what
	// there is to aggregate.
	query := b.Clone()
	if len(b.Unions) == 0 && len(b.Havings) == 0 {
		query = b.CloneWithout("columns").CloneWithoutBindings("select")
	}
	query.setAggregate(function, columns)

	rows, err := query.Get(ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return aggregateValue(rows[0]), nil
}

// setAggregate answers Builder::setAggregate, which is protected there.
//
// The PHP does two things: it records the aggregate, and -- unless the query
// groups -- it drops the ordering, because ordering a single computed row is
// work the engine does for nobody and is a syntax error on engines that check.
// Builder.SetAggregate in builder.go publishes only the first half, so the
// second is here.
func (b *Builder) setAggregate(function string, columns []any) *Builder {
	b.SetAggregate(function, columns)
	if len(b.Groups) == 0 {
		b.Orders = nil
		b.Bindings["order"] = nil
	}
	return b
}

// aggregateValue reads the column the grammar named "aggregate".
//
// The PHP runs array_change_key_case over the row first, because engines
// disagree about the case they hand a column name back in -- Oracle and some
// ODBC drivers upper-case it. The lookup here is the same tolerance, without
// rewriting the row.
func aggregateValue(row Record) any {
	if value, ok := row["aggregate"]; ok {
		return value
	}
	for key, value := range row {
		if strings.EqualFold(key, "aggregate") {
			return value
		}
	}
	return nil
}

// NumericAggregate answers Builder::numericAggregate.
//
// The PHP returns int or float depending on whether the string the driver gave
// it has a decimal point. Go has one number type that holds both, so it returns
// a float64, and nothing is lost: an int64 that survives a float64 round trip is
// every count a database will produce short of 2^53 rows.
func (b *Builder) NumericAggregate(ctx context.Context, g auth.Grant, function string, columns ...any) (float64, error) {
	result, err := b.Aggregate(ctx, g, function, columns...)
	if err != nil {
		return 0, err
	}
	number, ok := castNumber(result)
	if !ok {
		return 0, nil
	}
	return number, nil
}

// Count answers Builder::count.
func (b *Builder) Count(ctx context.Context, g auth.Grant, columns ...any) (int, error) {
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	result, err := b.Aggregate(ctx, g, "count", columns...)
	if err != nil {
		return 0, err
	}
	number, _ := castNumber(result)
	return int(number), nil
}

// Min answers Builder::min.
func (b *Builder) Min(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "min", column)
}

// Max answers Builder::max.
func (b *Builder) Max(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "max", column)
}

// Sum answers Builder::sum. A sum over no rows is zero rather than null, which
// is the PHP's `$result ?: 0`.
func (b *Builder) Sum(ctx context.Context, g auth.Grant, column any) (any, error) {
	result, err := b.Aggregate(ctx, g, "sum", column)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return 0, nil
	}
	return result, nil
}

// Avg answers Builder::avg.
func (b *Builder) Avg(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "avg", column)
}

// Average answers Builder::average, the alias of avg.
func (b *Builder) Average(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Avg(ctx, g, column)
}

// GetCountForPagination answers Builder::getCountForPagination.
func (b *Builder) GetCountForPagination(ctx context.Context, g auth.Grant, columns ...any) (int, error) {
	rows, err := b.runPaginationCountQuery(ctx, g, wrapColumns(columns))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	number, _ := castNumber(aggregateValue(rows[0]))
	return int(number), nil
}

// runPaginationCountQuery answers Builder::runPaginationCountQuery.
//
// A grouped or having query cannot be counted by replacing its select list:
// count(*) over a group by counts the groups' rows, not the groups. So the whole
// query becomes a derived table and the count is taken over that. The ordering,
// the limit and the offset come off first -- counting a page tells you the size
// of the page, which is the number you already had.
func (b *Builder) runPaginationCountQuery(ctx context.Context, g auth.Grant, columns []any) ([]Record, error) {
	if len(b.Groups) > 0 || len(b.Havings) > 0 {
		clone := b.cloneForPaginationCount()
		if clone.Columns == nil && len(b.Joins) > 0 {
			clone.Select(stringify(b.GetFrom()) + ".*")
		}

		// The tenant clause goes on the inner query, which is the one that
		// touches the tables. The wrapper selects from a derived table that has
		// no tenant column of its own, so it runs unscoped -- and it is only
		// reachable through this function, which just scoped what it wraps.
		inner, err := clone.scoped(ctx, g)
		if err != nil {
			return nil, err
		}

		outer := b.NewQuery()
		outer.From(Raw("(" + inner.ToSQL() + ") as " + b.Grammar.Wrap("aggregate_table")))
		outer.MergeBindings(inner)
		outer.setAggregate("count", withoutSelectAliases(columns))
		outer.Columns = []any{"*"}
		return outer.runSelect()
	}

	without := []string{"columns", "orders", "limit", "offset"}
	except := []string{"select", "order"}
	if len(b.Unions) > 0 {
		without = nil
		except = []string{"unionOrder"}
	}

	clone := b.CloneWithout(without...).CloneWithoutBindings(except...)
	if len(b.Unions) > 0 {
		// CloneWithout in builder.go answers to the nine properties the PHP's
		// cloneWithout is called with elsewhere, and the three union ones are
		// not among them, so they are cleared here rather than left on a query
		// whose page they no longer describe.
		clone.UnionOrders = nil
		clone.UnionLimit = nil
		clone.UnionOffset = nil
	}
	clone.setAggregate("count", withoutSelectAliases(columns))
	return clone.Get(ctx, g)
}

// cloneForPaginationCount answers Builder::cloneForPaginationCount.
func (b *Builder) cloneForPaginationCount() *Builder {
	return b.CloneWithout("orders", "limit", "offset").CloneWithoutBindings("order")
}

// withoutSelectAliases answers Builder::withoutSelectAliases: "total as t"
// counted as "total", because the alias of a column that is about to be
// replaced by count(*) is a syntax error waiting in the select list.
func withoutSelectAliases(columns []any) []any {
	out := make([]any, len(columns))
	for i, column := range columns {
		name, ok := column.(string)
		if !ok {
			out[i] = column
			continue
		}
		if j := aliasIndex(name); j >= 0 {
			out[i] = name[:j]
			continue
		}
		out[i] = name
	}
	return out
}

// Paginate answers Builder::paginate.
//
// Two arguments of the PHP are not arguments there: it asks Paginator to resolve
// the current page out of the request and the current path out of the router,
// both through the container. There is no container (ADR 0001) and no request
// reachable from here, so the page number is an argument and the path is a field
// of pagination.Options, along with the name of the page parameter.
//
// total is the PHP's fifth argument: a count the caller already has. Leaving it
// out runs GetCountForPagination, which is one extra statement -- the one
// simplePaginate exists to avoid.
func (b *Builder) Paginate(ctx context.Context, g auth.Grant, perPage, page int, columns []any, opts pagination.Options, total ...int) (*pagination.LengthAwarePaginator[Record], error) {
	if page < 1 {
		page = 1
	}

	count := 0
	if len(total) > 0 {
		count = total[0]
	} else {
		counted, err := b.GetCountForPagination(ctx, g)
		if err != nil {
			return nil, err
		}
		count = counted
	}

	var rows []Record
	if count > 0 {
		fetched, err := b.ForPage(page, perPage).Get(ctx, g, columns...)
		if err != nil {
			return nil, err
		}
		rows = fetched
	}
	return pagination.Paginate(rows, count, perPage, page, opts), nil
}

// SimplePaginate answers Builder::simplePaginate.
//
// It reads one row more than the page holds, and that extra row is the whole of
// how it answers "is there a next page" without a count. pagination.SimplePaginate
// drops it before anybody sees it.
func (b *Builder) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, columns []any, opts pagination.Options) (*pagination.Paginator[Record], error) {
	if page < 1 {
		page = 1
	}
	rows, err := b.Offset((page-1)*perPage).Limit(perPage+1).Get(ctx, g, columns...)
	if err != nil {
		return nil, err
	}
	return pagination.SimplePaginate(rows, perPage, page, opts), nil
}

// CursorPaginate answers Builder::cursorPaginate.
//
// The cursor is resolved by the caller -- pagination.ResolveCurrentCursor reads
// it off a URL -- because the PHP resolves it from the request through the
// container. A nil cursor is the first page.
func (b *Builder) CursorPaginate(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, columns []any, opts pagination.Options) (*pagination.CursorPaginator[Record], error) {
	return b.paginateUsingCursor(ctx, g, perPage, cursor, columns, opts)
}

// paginateUsingCursor answers Concerns\BuildsQueries::paginateUsingCursor,
// which is protected there.
//
// This is the where clause that makes keyset paging exact. For an ordering of
// (a, b, c) and a cursor naming one row, the boundary is
//
//	a > ?a or (a = ?a and (b > ?b or (b = ?b and c > ?c)))
//
// which is built by recursing over the orders, one level per column. The
// comparison flips to < for a descending column, and the whole ordering is
// reversed when the cursor points backwards -- the rows then come back in
// reverse and pagination.CursorPaginate turns them around again.
func (b *Builder) paginateUsingCursor(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, columns []any, opts pagination.Options) (*pagination.CursorPaginator[Record], error) {
	orders, err := b.ensureOrderForCursorPagination(cursor != nil && cursor.PointsToPreviousItems())
	if err != nil {
		return nil, err
	}

	if cursor != nil {
		// The union bindings are collected again from scratch, so that the
		// cursor conditions of each union query land in the order the compiled
		// statement reads them.
		b.SetBindings(nil, "union")
		if err := b.addCursorConditions(b, *cursor, orders, nil, "", 0); err != nil {
			return nil, err
		}
	}

	rows, err := b.Limit(perPage+1).Get(ctx, g, columns...)
	if err != nil {
		return nil, err
	}

	// The cursor of a row is the value it has in every ordering column. The
	// names are the columns as the ordering wrote them; the values are read
	// under the name the driver keys the row by, which is the column without
	// its table or alias.
	key := func(row Record) map[string]string {
		parameters := make(map[string]string, len(orders))
		for _, order := range orders {
			parameters[stringify(order.Column)] = stringify(row[stripTableForPluck(order.Column)])
		}
		return parameters
	}
	return pagination.CursorPaginate(rows, perPage, cursor, key, opts), nil
}

// addCursorConditions is the closure the PHP declares inside
// paginateUsingCursor and calls recursively.
func (b *Builder) addCursorConditions(builder *Builder, cursor pagination.Cursor, orders []Order, previousColumn any, originalColumn string, i int) error {
	unionBuilders := builder.unionBuilders()

	if previousColumn != nil {
		if originalColumn == "" {
			originalColumn = getOriginalColumnNameForCursorPagination(b, stringify(previousColumn))
		}
		value, err := cursor.Parameter(stringify(previousColumn))
		if err != nil {
			return err
		}
		builder.Where(cursorColumn(originalColumn), "=", value)

		for _, unionBuilder := range unionBuilders {
			unionBuilder.Where(
				cursorColumn(getOriginalColumnNameForCursorPagination(unionBuilder, stringify(previousColumn))),
				"=", value)
			b.AddBinding(unionBuilder.GetRawBindings()["where"], "union")
		}
	}

	order := orders[i]
	operator := ">"
	if order.Direction == "desc" {
		operator = "<"
	}
	value, err := cursor.Parameter(stringify(order.Column))
	if err != nil {
		return err
	}
	original := getOriginalColumnNameForCursorPagination(b, stringify(order.Column))

	var nestedErr error
	builder.Where(func(second *Builder) {
		second.Where(cursorColumn(original), operator, value)

		if i < len(orders)-1 {
			second.OrWhere(func(third *Builder) {
				if err := b.addCursorConditions(third, cursor, orders, order.Column, original, i+1); err != nil {
					nestedErr = err
				}
			})
		}

		for _, unionBuilder := range unionBuilders {
			unionWheres := unionBuilder.GetRawBindings()["where"]
			unionOriginal := getOriginalColumnNameForCursorPagination(unionBuilder, stringify(order.Column))

			unionBuilder.Where(func(nested *Builder) {
				nested.Where(cursorColumn(unionOriginal), operator, value)

				if i < len(orders)-1 {
					nested.OrWhere(func(fourth *Builder) {
						if err := b.addCursorConditions(fourth, cursor, orders, order.Column, unionOriginal, i+1); err != nil {
							nestedErr = err
						}
					})
				}

				b.AddBinding(unionWheres, "union")
				b.AddBinding(nested.GetRawBindings()["where"], "union")
			})
		}
	})
	return nestedErr
}

// cursorColumn wraps a computed column in an Expression, as the PHP does when
// the name contains a parenthesis: "lower(email)" is SQL and quoting it as an
// identifier produces a column nobody has.
func cursorColumn(column string) any {
	if strings.ContainsAny(column, "()") {
		return Raw(column)
	}
	return column
}

// unionBuilders answers Builder::getUnionBuilders, which is protected there.
func (b *Builder) unionBuilders() []*Builder {
	out := make([]*Builder, 0, len(b.Unions))
	for _, union := range b.Unions {
		out = append(out, union.Query)
	}
	return out
}

// ensureOrderForCursorPagination answers
// Builder::ensureOrderForCursorPagination.
//
// A keyset walk with no ordering is a walk with no boundary, so an unordered
// query is refused here rather than answered with rows in whatever order the
// engine found them -- which is the failure that repeats a row on one page and
// drops it from the next.
//
// Only the orders with a direction are returned: an orderByRaw has no column to
// take a cursor value from.
func (b *Builder) ensureOrderForCursorPagination(shouldReverse bool) ([]Order, error) {
	if err := b.enforceOrderBy(); err != nil {
		return nil, err
	}

	if shouldReverse {
		b.Orders = reverseDirections(b.Orders)
		b.UnionOrders = reverseDirections(b.UnionOrders)
	}

	orders := b.Orders
	if len(b.UnionOrders) > 0 {
		orders = b.UnionOrders
	}

	out := make([]Order, 0, len(orders))
	for _, order := range orders {
		if order.Direction != "" {
			out = append(out, order)
		}
	}
	if len(out) == 0 {
		return nil, errRequiresOrderBy
	}
	return out, nil
}

func reverseDirections(orders []Order) []Order {
	out := make([]Order, len(orders))
	for i, order := range orders {
		if order.Direction == "asc" {
			order.Direction = "desc"
		} else if order.Direction == "desc" {
			order.Direction = "asc"
		}
		out[i] = order
	}
	return out
}

// getOriginalColumnNameForCursorPagination answers
// Builder::getOriginalColumnNameForCursorPagination: given the name a column is
// selected under, it finds the expression that was aliased to it, so the cursor
// compares against the expression rather than an alias no engine allows in a
// where clause.
func getOriginalColumnNameForCursorPagination(builder *Builder, parameter string) string {
	for _, column := range builder.GetColumns() {
		name, ok := column.(string)
		if !ok {
			continue
		}
		position := strings.LastIndex(lower(name), " as ")
		if position < 0 {
			continue
		}
		original := name[:position]
		alias := name[position+4:]
		if parameter == alias || builder.Grammar.Wrap(parameter) == alias {
			return original
		}
	}
	return parameter
}
