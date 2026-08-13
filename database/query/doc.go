// Package query mirrors Illuminate\Database\Query.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Query:
//
//	Builder.php
//	Expression.php
//	IndexHint.php
//	JoinClause.php
//	JoinLateralClause.php
//
// Builder is split across files by what each half does rather than kept in one
// of Builder.php's size: builder.go holds the state and the clauses everything
// else builds on, wheres.go and havings.go the two filter vocabularies,
// joins.go and subqueries.go the join and subquery families, aggregates.go the
// counting and the pagination, chunk.go the walks, write.go the statements that
// change rows, execute.go the ones that read them, and misc.go the rest.
//
// # Nothing here decides who may run what
//
// A Builder compiles SQL. The tenant of the security.Grant is put on the
// statement at execution, in scoped, and on every subquery the statement
// carries -- a union, a `where exists`, a from, a join or a select subquery.
// That is RULE 17 and it is not optional: a subquery is compiled whole, so a
// filter on the outer query says which of its rows survive and nothing about
// which rows went in.
//
// JoinLateralClause is a subclass with no body in PHP, and here it is
// JoinClause.Lateral: Go has no subclassing, and a second type would be a
// second thing for the grammar to hold. The names this package does not answer
// to -- Builder::dd, ddRawSql and fetchUsing -- are named in the database
// package's doc.go, together with the reason and with what to reach for
// instead.
package query
