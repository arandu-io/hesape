package query

// The having vocabulary of Illuminate\Database\Query\Builder.
//
// A having filters the groups a group by produced, so it is compiled after them
// and its bindings live in their own segment. The tenant filter never lands
// here: it is a where, because a row that belongs to another customer must not
// reach the grouping in the first place.

// addHaving answers Builder::having, with the conjunction as a parameter rather
// than as the trailing argument PHP defaults.
//
// A func opens a nested group, as passing a Closure does there. A bitwise
// operator marks the clause "Bitwise", which is the name the PHP writes and
// which its own compileHaving does not match -- the switch there lists "bit" --
// so a bitwise having compiles as a basic one on both sides. Mirroring the name
// keeps the two in step; correcting it here would make this the only builder in
// the world whose bitwise having means something else.
func (b *Builder) addHaving(boolean string, column any, args ...any) *Builder {
	if nested, ok := column.(func(*Builder)); ok {
		return b.HavingNested(nested, boolean)
	}

	operator, value := prepareValueAndOperator(args...)
	if len(args) > 1 {
		operator, value = b.shortcutOperator(operator, value)
	}

	typ := "Basic"
	if b.isBitwiseOperator(operator) {
		typ = "Bitwise"
	}

	b.Havings = append(b.Havings, Having{
		Type: typ, Column: column, Operator: operator, Value: value, Boolean: boolean,
	})
	if !IsExpression(value) {
		b.AddBinding([]any{flattenValue(value)}, "having")
	}
	return b
}

// isBitwiseOperator answers Builder::isBitwiseOperator.
func (b *Builder) isBitwiseOperator(operator string) bool {
	if containsFold(bitwiseOperators, operator) {
		return true
	}
	if b.Grammar == nil {
		return false
	}
	return containsFold(b.Grammar.GetBitwiseOperators(), operator)
}

// OrHaving answers Builder::orHaving.
func (b *Builder) OrHaving(column any, args ...any) *Builder {
	return b.addHaving("or", column, args...)
}

// HavingNested answers Builder::havingNested.
func (b *Builder) HavingNested(callback func(*Builder), boolean string) *Builder {
	nested := b.ForNestedWhere()
	callback(nested)
	return b.AddNestedHavingQuery(nested, boolean)
}

// AddNestedHavingQuery answers Builder::addNestedHavingQuery.
//
// A group with no clauses is dropped rather than compiled, for the reason
// AddNestedWhereQuery gives: "()" is a syntax error on every engine, and an
// empty callback is an ordinary outcome of a conditional filter.
func (b *Builder) AddNestedHavingQuery(query *Builder, boolean string) *Builder {
	if query == nil || len(query.Havings) == 0 {
		return b
	}
	if boolean == "" {
		boolean = "and"
	}
	b.Havings = append(b.Havings, Having{Type: "Nested", Query: query, Boolean: boolean})
	b.AddBinding(query.GetRawBindings()["having"], "having")
	return b
}

// HavingNull answers Builder::havingNull.
func (b *Builder) HavingNull(columns []any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	typ := "Null"
	if not {
		typ = "NotNull"
	}
	for _, column := range columns {
		b.Havings = append(b.Havings, Having{Type: typ, Column: column, Boolean: boolean})
	}
	return b
}

// OrHavingNull answers Builder::orHavingNull.
func (b *Builder) OrHavingNull(columns ...any) *Builder {
	return b.HavingNull(columns, "or", false)
}

// HavingNotNull answers Builder::havingNotNull.
func (b *Builder) HavingNotNull(columns []any, boolean string) *Builder {
	return b.HavingNull(columns, boolean, true)
}

// OrHavingNotNull answers Builder::orHavingNotNull.
func (b *Builder) OrHavingNotNull(columns ...any) *Builder {
	return b.HavingNotNull(columns, "or")
}

// HavingBetween answers Builder::havingBetween.
//
// Only the first two values are bound, as the PHP's array_slice(..., 0, 2)
// does: a between has two bounds, and a longer list is a caller mistake that
// would otherwise leave bindings with no placeholder to fill.
func (b *Builder) HavingBetween(column any, values []any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Havings = append(b.Havings, Having{
		Type: "between", Column: column, Values: values, Boolean: boolean, Not: not,
	})

	bounds := b.CleanBindings(values)
	if len(bounds) > 2 {
		bounds = bounds[:2]
	}
	b.AddBinding(bounds, "having")
	return b
}

// HavingNotBetween answers Builder::havingNotBetween.
func (b *Builder) HavingNotBetween(column any, values []any, boolean string) *Builder {
	return b.HavingBetween(column, values, boolean, true)
}

// OrHavingBetween answers Builder::orHavingBetween.
func (b *Builder) OrHavingBetween(column any, values []any) *Builder {
	return b.HavingBetween(column, values, "or", false)
}

// OrHavingNotBetween answers Builder::orHavingNotBetween.
func (b *Builder) OrHavingNotBetween(column any, values []any) *Builder {
	return b.HavingBetween(column, values, "or", true)
}

// OrHavingRaw answers Builder::orHavingRaw.
func (b *Builder) OrHavingRaw(sql string, bindings ...any) *Builder {
	b.Havings = append(b.Havings, Having{Type: "Raw", SQL: sql, Boolean: "or"})
	if len(bindings) > 0 {
		b.AddBinding(bindings, "having")
	}
	return b
}
