package model

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// Paginate runs the query for one page and returns its rows, beside the
// length-aware page that does the arithmetic.
//
// The page number is an argument: no request is reachable from here, and the
// caller reads it with pagination.ResolveCurrentPage.
//
// perPage of zero means the table's own.
func (b *Builder) Paginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (Rows, *pagination.LengthAwarePage, error) {
	return paginate(b, ctx, g, perPage, page, opts, columns...)
}

// paginate is Paginate, and what the relation seam answers its own Paginate
// with: the counting is done here, once, because a page that counts differently
// in two places is the bug that would follow from writing it twice.
func paginate(b *Builder, ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (Rows, *pagination.LengthAwarePage, error) {
	if perPage <= 0 {
		perPage = b.table.perPage
	}
	if page < 1 {
		page = 1
	}

	total, err := b.GetCountForPagination(ctx, g)
	if err != nil {
		return nil, nil, err
	}

	items := Rows{}
	if total > 0 {
		paged := clone(b)
		paged.query.ForPage(page, perPage)
		items, err = paged.get(ctx, g, columns...)
		if err != nil {
			return nil, nil, err
		}
	}
	return items, pagination.NewLengthAwarePage(len(items), int(total), perPage, page, opts), nil
}

// SimplePaginate returns the rows of one page, without the count, beside the
// page that says whether there is another.
func (b *Builder) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (Rows, *pagination.Page, error) {
	return simplePaginate(b, ctx, g, perPage, page, opts, columns...)
}

// simplePaginate is SimplePaginate: the rows on the page, with the probe row
// already dropped, and the page. See paginate.
func simplePaginate(b *Builder, ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (Rows, *pagination.Page, error) {
	if perPage <= 0 {
		perPage = b.table.perPage
	}
	if page < 1 {
		page = 1
	}

	paged := clone(b)
	paged.query.Offset((page - 1) * perPage).Limit(perPage + 1)
	items, err := paged.get(ctx, g, columns...)
	if err != nil {
		return nil, nil, err
	}
	meta := pagination.NewPage(len(items), perPage, page, opts)
	return items[:meta.Count()], meta, nil
}

// GetCountForPagination returns the row count of the query, ignoring its
// order, limit and offset.
//
// The orders, the limit and the offset come off before the count, because a
// count with a limit on it counts the page rather than the result set.
func (b *Builder) GetCountForPagination(ctx context.Context, g auth.Grant) (int64, error) {
	counted := clone(b)
	counted.query = counted.query.CloneWithout("columns", "orders", "limit", "offset")
	return counted.Count(ctx, g)
}

// CursorPaginate returns the rows of the page after (or before) a boundary
// named by cursor rather than by offset, in reading order, beside the page and
// its cursors.
//
// It is the paginator to reach for past the first few pages: ForPage makes the
// engine count and discard every row it skips, and a row inserted between two
// requests shifts the boundary so that a row is never shown.
//
// cursor is nil for the first page. The columns the query orders by are the
// cursor's parameters, so every one of them has to be selected -- the cursor is
// built out of the rows that come back.
func (b *Builder) CursorPaginate(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, opts pagination.Options, columns ...any) (Rows, *pagination.CursorPage, error) {
	return cursorPaginate(b, ctx, g, perPage, cursor, opts, columns...)
}

// cursorPaginate is CursorPaginate. See paginate.
//
// The cursors are built here, off the models at the two edges of the page: the
// value of an ordering column is an attribute of the row, which is what
// GetAttribute reads by name.
func cursorPaginate(b *Builder, ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, opts pagination.Options, columns ...any) (Rows, *pagination.CursorPage, error) {
	if perPage <= 0 {
		perPage = b.table.perPage
	}
	if len(b.query.Unions) > 0 {
		return nil, nil, fmt.Errorf("model: cursor pagination over a union is not supported: the boundary conditions have to be repeated inside every branch, and this builder has no way to reach them")
	}

	paginated := clone(b)
	orders := ensureOrderForCursorPagination(paginated.query, paginated.qualify(b.table.keyName), cursor != nil && cursor.PointsToPreviousItems())
	if len(orders) == 0 {
		return nil, nil, fmt.Errorf("model: cursor pagination needs an order it can compare against")
	}

	if cursor != nil {
		if err := addCursorConditions(paginated.query, *cursor, orders, 0); err != nil {
			return nil, nil, err
		}
	}

	paginated.query.Limit(perPage + 1)
	items, err := paginated.get(ctx, g, columns...)
	if err != nil {
		return nil, nil, err
	}

	key := func(i int) map[string]string {
		m := items[i].base()
		parameter := make(map[string]string, len(orders))
		for _, order := range orders {
			parameter[order.column] = fmt.Sprint(m.GetAttribute(afterLastDot(order.column)))
		}
		return parameter
	}
	meta := pagination.NewCursorPage(len(items), perPage, cursor, key, opts)
	items = items[:meta.Count()]
	if meta.Reversed() {
		slices.Reverse(items)
	}
	return items, meta, nil
}

// cursorOrder is one entry of what ensureOrderForCursorPagination returns: a
// column the cursor compares against, and the direction it is read in.
type cursorOrder struct {
	column    string
	direction string
}

// ensureOrderForCursorPagination returns the columns to compare the cursor
// against, reversing their direction first when walking backward. A query with
// no order is ordered by key, the column named by qualifiedKey.
func ensureOrderForCursorPagination(q *query.Builder, qualifiedKey string, shouldReverse bool) []cursorOrder {
	enforceOrderBy(q, qualifiedKey)

	if shouldReverse {
		for i := range q.Orders {
			q.Orders[i].Direction = flipDirection(q.Orders[i].Direction)
		}
		for i := range q.UnionOrders {
			q.UnionOrders[i].Direction = flipDirection(q.UnionOrders[i].Direction)
		}
	}

	orders := q.Orders
	if len(q.UnionOrders) > 0 {
		orders = q.UnionOrders
	}

	out := make([]cursorOrder, 0, len(orders))
	for _, order := range orders {
		if order.Direction == "" || order.Column == nil {
			// An orderByRaw has no direction to compare against, so it is
			// filtered out before the conditions are built.
			continue
		}
		out = append(out, cursorOrder{column: fmt.Sprint(order.Column), direction: order.Direction})
	}
	return out
}

func flipDirection(direction string) string {
	if direction == "asc" {
		return "desc"
	}
	return "asc"
}

// addCursorConditions adds the where clauses that skip every row up to and
// including the cursor's position.
//
// It reads: past the first ordering column, every earlier one has to be
// equal, and this one has to be past the boundary -- or, if there is
// another column after it, equal here and past the boundary there. That
// nesting is what makes a compound cursor skip exactly the rows already
// seen.
func addCursorConditions(q *query.Builder, cursor pagination.Cursor, orders []cursorOrder, i int) error {
	if i > 0 {
		previous := orders[i-1].column
		value, err := cursor.Parameter(previous)
		if err != nil {
			return err
		}
		q.Where(cursorColumn(previous), "=", value)
	}

	order := orders[i]
	value, err := cursor.Parameter(order.column)
	if err != nil {
		return err
	}

	operator := ">"
	if order.direction != "asc" {
		operator = "<"
	}

	var inner error
	q.Where(func(nested *query.Builder) {
		nested.Where(cursorColumn(order.column), operator, value)
		if i < len(orders)-1 {
			nested.OrWhere(func(deeper *query.Builder) {
				inner = addCursorConditions(deeper, cursor, orders, i+1)
			})
		}
	})
	return inner
}

// cursorColumn returns column unchanged when it is a plain name, or wrapped
// as a raw expression when it looks like one, so that ordering by a
// function still compares against the same function.
func cursorColumn(column string) any {
	if strings.ContainsAny(column, "()") {
		return query.Raw(column)
	}
	return column
}
