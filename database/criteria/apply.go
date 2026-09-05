package criteria

import "strings"

// Target is the query a plan is applied to.
//
// It is an interface over a builder rather than a dependency on one, and the
// self-referential type parameter is what lets a fluent builder satisfy it:
// every method returns the builder itself, so Self is the builder's own type.
// *model.Builder[T] satisfies Target[*model.Builder[T]] as written.
//
// It is an interface at all because this package translates and does not query.
// Naming a builder here would put a data path inside the door, and the door
// would become a second place where a query is built.
type Target[Self any] interface {
	Where(column any, args ...any) Self
	WhereIn(column any, values []any) Self
	OrderBy(column any, direction ...string) Self
	Select(columns ...any) Self
	With(relations ...string) Self
}

// Apply adds the plan's clauses to the query and returns it.
//
// The operator it passes is the one the declaration chose, and the query
// grammar normalizes that against the dialect which will compile the statement
// -- so an operator still goes through the one allowlist there is, and this
// package does not become a second grammar.
//
// It adds no page. PerPage and Page are carried on the plan for the paginator,
// which needs the size to count with; a limit put on here would be a second way
// to page, and the two would disagree about the total.
//
// It reaches nothing: running the query it returns still takes the Grant every
// read takes, and the tenant scope of the model it came from is untouched.
func Apply[B Target[B]](query B, plan Plan) B {
	for _, clause := range plan.Filters {
		query = filter(query, clause)
	}

	for _, order := range plan.Orders {
		direction := "asc"
		if order.Descending {
			direction = "desc"
		}
		query = query.OrderBy(order.Column, direction)
	}

	if len(plan.Columns) > 0 {
		columns := make([]any, 0, len(plan.Columns))
		for _, column := range plan.Columns {
			columns = append(columns, column)
		}
		query = query.Select(columns...)
	}

	if len(plan.Includes) > 0 {
		query = query.With(plan.Includes...)
	}

	return query
}

// filter adds one clause, which is where a declared match becomes a
// comparison. It is a function and not a method because a method cannot carry
// a type parameter, and the builder's own type is the parameter.
func filter[B Target[B]](query B, clause Clause) B {
	if clause.Match == OneOf {
		list, _ := clause.Value.([]string)
		values := make([]any, 0, len(list))
		for _, value := range list {
			values = append(values, value)
		}
		return query.WhereIn(clause.Column, values)
	}

	value, _ := clause.Value.(string)
	switch clause.Match {
	case Partial:
		value = "%" + escapeWildcards(value) + "%"
	case Prefix:
		value = escapeWildcards(value) + "%"
	case Suffix:
		value = "%" + escapeWildcards(value)
	}
	return query.Where(clause.Column, clause.Match.operator(), value)
}

// escapeWildcards stops a value from carrying its own pattern: a search for
// "%" would otherwise match every row, and a search for "a_c" would match
// "abc".
//
// The escape character is the backslash, which is what PostgreSQL and MySQL
// read one as by default. A dialect that reads it as an ordinary character --
// SQLite does -- makes such a search find nothing rather than everything, which
// is the direction to be wrong in: too few rows is visible to the person
// searching, and too many is a page of somebody else's records.
func escapeWildcards(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return replacer.Replace(value)
}
