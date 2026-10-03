package model

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// The where clauses and the aggregates of the base builder, forwarded.
//
// The only other way to reach them is GetQuery(), which hands back the base
// builder and ends the chain -- so a query as ordinary as "these three ids"
// could not be written without leaving the API it was written in.
//
// Each of these is the same two lines: hand the clause to the base builder,
// return this one so the chain continues.

// WhereIn adds a where-in clause.
func (b *Builder) WhereIn(column any, values []any) *Builder {
	b.query.WhereIn(column, values)
	return b
}

// WhereNotIn adds a where-not-in clause.
func (b *Builder) WhereNotIn(column any, values []any) *Builder {
	b.query.WhereNotIn(column, values)
	return b
}

// WhereNull adds a where-null clause for each column named.
func (b *Builder) WhereNull(columns ...any) *Builder {
	b.query.WhereNull(columns...)
	return b
}

// WhereNotNull adds a where-not-null clause for each column named.
func (b *Builder) WhereNotNull(columns ...any) *Builder {
	b.query.WhereNotNull(columns...)
	return b
}

// WhereBetween adds a where-between clause.
func (b *Builder) WhereBetween(column any, from, to any) *Builder {
	b.query.WhereBetween(column, from, to)
	return b
}

// WhereExists adds a where-exists clause built by callback.
//
// The callback takes the base builder rather than this one, and that is not an
// oversight: the subquery of an exists names another table, so a builder of this
// table would be the wrong one for it. Has and WhereHas ask the same question
// about a relation, and they are what most callers want.
func (b *Builder) WhereExists(callback func(*query.Builder)) *Builder {
	b.query.WhereExists(callback, "and", false)
	return b
}

// WhereNotExists adds a where-not-exists clause built by callback.
func (b *Builder) WhereNotExists(callback func(*query.Builder)) *Builder {
	b.query.WhereExists(callback, "and", true)
	return b
}

// DoesntExist reports whether the query matches no rows.
//
// It is Exists read the other way round, and it exists as its own method for
// the reason the base builder has one: `if !exists` after a call that also
// returns an error reads as a mistake even when it is not.
func (b *Builder) DoesntExist(ctx context.Context, g auth.Grant) (bool, error) {
	exists, err := b.Exists(ctx, g)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// Sum returns the sum of column over the matching rows.
func (b *Builder) Sum(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "sum", column)
}

// Avg returns the average of column over the matching rows.
func (b *Builder) Avg(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "avg", column)
}

// Min returns the smallest value of column over the matching rows.
func (b *Builder) Min(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "min", column)
}

// Max returns the largest value of column over the matching rows.
func (b *Builder) Max(ctx context.Context, g auth.Grant, column any) (any, error) {
	return b.Aggregate(ctx, g, "max", column)
}

// WhereColumn compares two columns.
func (b *Builder) WhereColumn(first any, args ...any) *Builder {
	b.query.WhereColumn(first, args...)
	return b
}

// Join adds an inner join.
func (b *Builder) Join(table any, first any, args ...any) *Builder {
	b.query.Join(table, first, args...)
	return b
}

// GroupBy groups the rows.
func (b *Builder) GroupBy(groups ...any) *Builder {
	b.query.GroupBy(groups...)
	return b
}
