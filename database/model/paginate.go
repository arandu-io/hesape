package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// Paginate runs the query for one page and returns a length-aware
// paginator.
//
// The page number is an argument: no request is reachable from here, and the
// caller reads it with pagination.ResolveCurrentPage.
//
// perPage of zero means the model's own.
func (b *Builder[T]) Paginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (*pagination.LengthAwarePaginator[*T], error) {
	items, total, perPage, page, err := paginateRows(b, ctx, g, perPage, page, columns...)
	if err != nil {
		return nil, err
	}
	return pagination.Paginate([]*T(entitiesOf[T](items)), int(total), perPage, page, opts), nil
}

// paginateRows is Paginate up to the page: the models on it, the total, and
// the page size and number it settled on.
//
// It stops short of the paginator because the same page has to be answered as
// *T to the caller who typed the query and as a relation's Model to the
// relation seam. Each converts the models it is handed; the counting is done
// here, once, because a paginator that counts differently in two places is the
// bug that would follow from writing it twice.
func paginateRows(b rowsBuilder, ctx context.Context, g auth.Grant, perPage, page int, columns ...any) (models, int64, int, int, error) {
	if perPage <= 0 {
		perPage = b.modelRow().GetPerPage()
	}
	if page < 1 {
		page = 1
	}

	total, err := b.GetCountForPagination(ctx, g)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	items := models{}
	if total > 0 {
		paged := b.cloneRows()
		paged.GetQuery().ForPage(page, perPage)
		items, err = paged.get(ctx, g, columns...)
		if err != nil {
			return nil, 0, 0, 0, err
		}
	}
	return items, total, perPage, page, nil
}

// SimplePaginate returns one page and whether there is another, without the
// count.
func (b *Builder[T]) SimplePaginate(ctx context.Context, g auth.Grant, perPage, page int, opts pagination.Options, columns ...any) (*pagination.Paginator[*T], error) {
	items, perPage, page, err := simplePaginateRows(b, ctx, g, perPage, page, columns...)
	if err != nil {
		return nil, err
	}
	return pagination.SimplePaginate([]*T(entitiesOf[T](items)), perPage, page, opts), nil
}

// simplePaginateRows is SimplePaginate up to the page. See paginateRows.
func simplePaginateRows(b rowsBuilder, ctx context.Context, g auth.Grant, perPage, page int, columns ...any) (models, int, int, error) {
	if perPage <= 0 {
		perPage = b.modelRow().GetPerPage()
	}
	if page < 1 {
		page = 1
	}

	paged := b.cloneRows()
	paged.GetQuery().Offset((page - 1) * perPage).Limit(perPage + 1)
	items, err := paged.get(ctx, g, columns...)
	if err != nil {
		return nil, 0, 0, err
	}
	return items, perPage, page, nil
}

// GetCountForPagination returns the row count of the query, ignoring its
// order, limit and offset.
//
// The orders, the limit and the offset come off before the count, because a
// count with a limit on it counts the page rather than the result set.
func (b *Builder[T]) GetCountForPagination(ctx context.Context, g auth.Grant) (int64, error) {
	counted := b.clone()
	counted.query = counted.query.CloneWithout("columns", "orders", "limit", "offset")
	return counted.Count(ctx, g)
}

// CursorPaginate returns the page after (or before) a boundary named by
// cursor rather than by offset.
//
// It is the paginator to reach for past the first few pages: ForPage makes the
// engine count and discard every row it skips, and a row inserted between two
// requests shifts the boundary so that a row is never shown.
//
// cursor is nil for the first page. The columns the query orders by are the
// cursor's parameters, so every one of them has to be selected -- the cursor is
// built out of the rows that come back.
func (b *Builder[T]) CursorPaginate(ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, opts pagination.Options, columns ...any) (*pagination.CursorPaginator[*T], error) {
	items, values, perPage, err := cursorPaginateRows(b, ctx, g, perPage, cursor, columns...)
	if err != nil {
		return nil, err
	}
	rows := entitiesOf[T](items)
	cursors := make(map[*T]map[string]string, len(rows))
	for i, row := range rows {
		cursors[row] = values[i]
	}
	key := func(item *T) map[string]string { return cursors[item] }
	return pagination.CursorPaginate([]*T(rows), perPage, cursor, key, opts), nil
}

// cursorPaginateRows is CursorPaginate up to the page: the models on it, the
// cursor parameters of each one at the same index, and the page size it
// settled on. See paginateRows.
//
// The cursor is built here, off the models, and handed back beside the items
// rather than read off them later. It is not read off the item itself because
// the item is whatever the caller converts it to: a row of a T that does not
// embed Model[T] has fields and no GetAttribute.
func cursorPaginateRows(b rowsBuilder, ctx context.Context, g auth.Grant, perPage int, cursor *pagination.Cursor, columns ...any) (models, []map[string]string, int, error) {
	if perPage <= 0 {
		perPage = b.modelRow().GetPerPage()
	}
	if len(b.GetQuery().Unions) > 0 {
		return nil, nil, 0, fmt.Errorf("model: cursor pagination over a union is not supported: the boundary conditions have to be repeated inside every branch, and this builder has no way to reach them")
	}

	paginated := b.cloneRows()
	orders := ensureOrderForCursorPagination(paginated.GetQuery(), paginated.modelRow().GetQualifiedKeyName(), cursor != nil && cursor.PointsToPreviousItems())
	if len(orders) == 0 {
		return nil, nil, 0, fmt.Errorf("model: cursor pagination needs an order it can compare against")
	}

	if cursor != nil {
		if err := addCursorConditions(paginated.GetQuery(), *cursor, orders, 0); err != nil {
			return nil, nil, 0, err
		}
	}

	paginated.GetQuery().Limit(perPage + 1)
	items, err := paginated.get(ctx, g, columns...)
	if err != nil {
		return nil, nil, 0, err
	}

	parameters := make([]string, 0, len(orders))
	for _, order := range orders {
		parameters = append(parameters, order.column)
	}

	values := make([]map[string]string, len(items))
	for i, item := range items {
		parameter := make(map[string]string, len(parameters))
		for _, name := range parameters {
			parameter[name] = fmt.Sprint(item.GetAttribute(afterLastDot(name)))
		}
		values[i] = parameter
	}
	return items, values, perPage, nil
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
