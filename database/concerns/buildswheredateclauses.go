package concerns

import (
	"time"

	"github.com/arandu-io/hesape/database/query"
)

// Now is where BuildsWhereDateClauses reads the clock.
//
// The PHP calls Carbon::now(), which Laravel's test suite freezes with
// Carbon::setTestNow(). Go has no such global, so the clock is a variable a
// test replaces and restores. Everything below reads it, so freezing it freezes
// all eighteen clauses at once.
var Now = time.Now

// WherePast answers BuildsWhereDateClauses::wherePast: the column is before now.
//
// The PHP takes a string or an array of them; a Go variadic is the same thing
// without the array wrapper Arr::wrap exists to undo.
func WherePast(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, "<", "and")
}

// WhereNowOrPast answers BuildsWhereDateClauses::whereNowOrPast.
func WhereNowOrPast(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, "<=", "and")
}

// OrWherePast answers BuildsWhereDateClauses::orWherePast.
func OrWherePast(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, "<", "or")
}

// OrWhereNowOrPast answers BuildsWhereDateClauses::orWhereNowOrPast.
func OrWhereNowOrPast(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, "<=", "or")
}

// WhereFuture answers BuildsWhereDateClauses::whereFuture.
func WhereFuture(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, ">", "and")
}

// WhereNowOrFuture answers BuildsWhereDateClauses::whereNowOrFuture.
func WhereNowOrFuture(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, ">=", "and")
}

// OrWhereFuture answers BuildsWhereDateClauses::orWhereFuture.
func OrWhereFuture(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, ">", "or")
}

// OrWhereNowOrFuture answers BuildsWhereDateClauses::orWhereNowOrFuture.
func OrWhereNowOrFuture(b *query.Builder, columns ...any) *query.Builder {
	return wherePastOrFuture(b, columns, ">=", "or")
}

// wherePastOrFuture answers the protected
// BuildsWhereDateClauses::wherePastOrFuture: a Basic where against the clock,
// with one binding per column.
func wherePastOrFuture(b *query.Builder, columns []any, operator, boolean string) *query.Builder {
	value := Now()

	for _, column := range columns {
		b.Wheres = append(b.Wheres, query.Where{
			Type:     "Basic",
			Column:   column,
			Boolean:  boolean,
			Operator: operator,
			Value:    value,
		})
		b.AddBinding([]any{value}, "where")
	}
	return b
}

// WhereToday answers BuildsWhereDateClauses::whereToday.
//
// boolean is the PHP's optional second argument; empty means "and", which is
// its default.
func WhereToday(b *query.Builder, boolean string, columns ...any) *query.Builder {
	if boolean == "" {
		boolean = "and"
	}
	return whereTodayBeforeOrAfter(b, columns, "=", boolean)
}

// WhereBeforeToday answers BuildsWhereDateClauses::whereBeforeToday.
func WhereBeforeToday(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, "<", "and")
}

// WhereTodayOrBefore answers BuildsWhereDateClauses::whereTodayOrBefore.
func WhereTodayOrBefore(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, "<=", "and")
}

// WhereAfterToday answers BuildsWhereDateClauses::whereAfterToday.
func WhereAfterToday(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, ">", "and")
}

// WhereTodayOrAfter answers BuildsWhereDateClauses::whereTodayOrAfter.
func WhereTodayOrAfter(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, ">=", "and")
}

// OrWhereToday answers BuildsWhereDateClauses::orWhereToday.
func OrWhereToday(b *query.Builder, columns ...any) *query.Builder {
	return WhereToday(b, "or", columns...)
}

// OrWhereBeforeToday answers BuildsWhereDateClauses::orWhereBeforeToday.
func OrWhereBeforeToday(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, "<", "or")
}

// OrWhereTodayOrBefore answers BuildsWhereDateClauses::orWhereTodayOrBefore.
func OrWhereTodayOrBefore(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, "<=", "or")
}

// OrWhereAfterToday answers BuildsWhereDateClauses::orWhereAfterToday.
func OrWhereAfterToday(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, ">", "or")
}

// OrWhereTodayOrAfter answers BuildsWhereDateClauses::orWhereTodayOrAfter.
func OrWhereTodayOrAfter(b *query.Builder, columns ...any) *query.Builder {
	return whereTodayBeforeOrAfter(b, columns, ">=", "or")
}

// whereTodayBeforeOrAfter answers the protected
// BuildsWhereDateClauses::whereTodayBeforeOrAfter: a Date where against
// today's date, formatted the way the column stores it.
//
// The PHP formats with 'Y-m-d'; the Go layout for the same shape is
// "2006-01-02". The value is a string on purpose -- a Date where compares the
// date part, and handing the driver a time.Time here would let the engine
// decide whether the time part counts, which is exactly the disagreement
// database.Day exists to settle.
func whereTodayBeforeOrAfter(b *query.Builder, columns []any, operator, boolean string) *query.Builder {
	value := Now().Format("2006-01-02")

	for _, column := range columns {
		b.Wheres = append(b.Wheres, query.Where{
			Type:     "Date",
			Column:   column,
			Boolean:  boolean,
			Operator: operator,
			Value:    value,
		})
		if !query.IsExpression(value) {
			b.AddBinding([]any{value}, "where")
		}
	}
	return b
}
