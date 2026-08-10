package query

// JoinClause answers Illuminate\Database\Query\JoinClause.
//
// In PHP it extends Builder, so a join condition is written with the same where
// vocabulary as the query itself -- On is where for two columns, and Where
// inside a join is a where. Here it embeds *Builder, which is what Go offers
// for the same thing: every Builder method is reachable on a JoinClause, and
// the two that behave differently are declared below and shadow the embedded
// ones.
type JoinClause struct {
	*Builder

	// Type is JoinClause::$type: inner, left, right or cross.
	Type string

	// Table is JoinClause::$table.
	Table any

	parentQuery *Builder
}

// NewJoinClause answers JoinClause::__construct.
//
// The clause carries the parent's connection, grammar and processor so that a
// nested query built inside the join compiles against the same dialect. The PHP
// keeps them in $parentConnection, $parentGrammar and $parentProcessor for the
// same reason.
func NewJoinClause(parentQuery *Builder, typ string, table any) *JoinClause {
	return &JoinClause{
		Builder:     NewBuilder(parentQuery.Connection, parentQuery.Grammar, parentQuery.Processor),
		Type:        typ,
		Table:       table,
		parentQuery: parentQuery,
	}
}

// On answers JoinClause::on.
//
// It compares two columns, so neither side becomes a binding: `on('users.id',
// '=', 'posts.user_id')` names a column on the right, not the string
// "posts.user_id". This is the one difference that catches people moving a
// condition from where to on -- in a where, the right side is a value.
//
// Passing a func opens a nested group, as passing a Closure does in PHP.
func (j *JoinClause) On(first any, args ...any) *JoinClause {
	if nested, ok := first.(func(*JoinClause)); ok {
		return j.onNested(nested, "and")
	}
	operator, second := prepareValueAndOperator(args...)
	j.Wheres = append(j.Wheres, Where{
		Type:     "Column",
		First:    first,
		Operator: operator,
		Second:   second,
		Boolean:  "and",
	})
	return j
}

// OrOn answers JoinClause::orOn.
func (j *JoinClause) OrOn(first any, args ...any) *JoinClause {
	if nested, ok := first.(func(*JoinClause)); ok {
		return j.onNested(nested, "or")
	}
	operator, second := prepareValueAndOperator(args...)
	j.Wheres = append(j.Wheres, Where{
		Type:     "Column",
		First:    first,
		Operator: operator,
		Second:   second,
		Boolean:  "or",
	})
	return j
}

// onNested runs a callback against a fresh clause and folds the result in as
// one parenthesised group, which is what whereNested does for a query.
func (j *JoinClause) onNested(callback func(*JoinClause), boolean string) *JoinClause {
	nested := j.NewJoinClause()
	callback(nested)
	if len(nested.Wheres) == 0 {
		return j
	}
	j.Wheres = append(j.Wheres, Where{Type: "Nested", Query: nested.Builder, Boolean: boolean})
	j.AddBinding(nested.Builder.Bindings["where"], "where")
	return j
}

// NewJoinClause answers JoinClause::newQuery, which returns another JoinClause
// rather than a Builder.
//
// The PHP overrides newQuery() itself. Go resolves an embedded method by name,
// and a method on JoinClause called NewQuery could not return *JoinClause while
// the embedded one returns *Builder, so the two are separate names: NewQuery
// still returns a plain builder, for a subquery inside the join, and this
// returns the clause.
func (j *JoinClause) NewJoinClause() *JoinClause {
	return NewJoinClause(j.parentQuery, j.Type, j.Table)
}

// NewParentQuery answers JoinClause::newParentQuery: a fresh builder on the
// table the join was declared against.
func (j *JoinClause) NewParentQuery() *Builder {
	return j.parentQuery.NewQuery()
}
