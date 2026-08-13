package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// The subquery half of Illuminate\Database\Query\Builder: the methods that take
// a whole query and compile it into a clause of this one -- selectSub, fromSub,
// joinSub and their kin.
//
// # Why a subquery is not compiled where it is written
//
// The PHP compiles it on the spot: fromSub calls createSub, which turns the
// argument into SQL and bindings and hands them to fromRaw. That cannot happen
// here, because a compiled subquery is a query nobody scoped.
//
// `from (select * from invoices) as i` reads the invoices table whole. A tenant
// filter on the outer query says which rows of that result survive, not which
// rows went into it -- an aggregate, a distinct or a limit inside the subquery
// has already looked at every customer by then. The same is true of a join and
// of a select subquery. Two leaks have already been found in this builder with
// exactly this shape, in the union and in the `where exists` subquery.
//
// So the subquery is compiled twice: once here, so that ToSQL on an unscoped
// builder shows what was written, and once in scopeSubqueryClauses, under the
// Grant, where its own tenant filter is added and its bindings replace the ones
// this pass wrote. The bookkeeping that makes the second pass possible is
// pendingSub.

// pendingSub is one subquery compiled into this statement, and where its pieces
// went, so that scopeSubqueryClauses can replace both when the tenant is known.
//
// The clause is named by kind and index rather than by pointer because the
// statement that runs is a clone: a pointer into the builder the caller holds
// would write the tenant filter onto the caller's query, and running it a
// second time under another Grant would keep the first tenant's filter.
type pendingSub struct {
	query   *Builder // the subquery as the caller built it, unscoped
	as      string   // the alias it is given
	table   bool     // whether the alias is a table name or a column name
	kind    string   // "select", "from" or "join"
	index   int      // which column, or which join
	segment string   // the binding segment its values live in
	offset  int      // where in that segment they start
	length  int      // how many of them there are
}

// resolveSub unwraps the Closure|Builder|string a subquery argument may be.
//
// It answers the builder to scope later, or nil for an argument that is already
// SQL: nothing here can add a tenant filter to a string, which is the reason to
// hand these methods a builder instead.
func (b *Builder) resolveSub(query any) (*Builder, string, []any, error) {
	switch sub := query.(type) {
	case func(*Builder):
		nested := b.NewQuery()
		sub(nested)
		return nested, nested.ToSQL(), nested.GetBindings(), nil
	case *Builder:
		return sub, sub.ToSQL(), sub.GetBindings(), nil
	case *JoinClause:
		return sub.Builder, sub.ToSQL(), sub.GetBindings(), nil
	case string:
		return nil, sub, nil, nil
	case Expression:
		return nil, stringify(sub), nil, nil
	case *Expression:
		return nil, stringify(sub), nil, nil
	default:
		return nil, "", nil, errors.New("query: a subquery must be a query builder, a callback or a string")
	}
}

// aliased spells "(sql) as alias", quoting the alias as a table or as a column.
func (b *Builder) aliased(sql, as string, table bool) Expression {
	if b.Grammar == nil {
		return Raw("(" + sql + ") as " + as)
	}
	if table {
		return Raw("(" + sql + ") as " + b.Grammar.WrapTable(as))
	}
	return Raw("(" + sql + ") as " + b.Grammar.Wrap(as))
}

// pend records a subquery so the tenant can be put on it at execution.
func (b *Builder) pend(sub *Builder, as string, table bool, kind, segment string, index, offset, length int) {
	if sub == nil {
		return
	}
	b.subqueries = append(b.subqueries, pendingSub{
		query:   sub,
		as:      as,
		table:   table,
		kind:    kind,
		index:   index,
		segment: segment,
		offset:  offset,
		length:  length,
	})
}

// forgetSubqueries drops the records whose clause is no longer on the builder.
//
// A record points at a clause by position and at its bindings by offset, so a
// method that replaces the whole of either has to say so here. Leaving the
// record would do one of two things at execution, and both are wrong: put the
// subquery back into a from that was replaced, or refuse the statement because
// the bindings it recorded are not where it left them.
func (b *Builder) forgetSubqueries(match func(pendingSub) bool) {
	if len(b.subqueries) == 0 {
		return
	}
	kept := make([]pendingSub, 0, len(b.subqueries))
	for _, sub := range b.subqueries {
		if match(sub) {
			continue
		}
		kept = append(kept, sub)
	}
	b.subqueries = kept
}

// forgetSubqueriesOfKind drops the records of one kind of clause.
func (b *Builder) forgetSubqueriesOfKind(kind string) {
	b.forgetSubqueries(func(sub pendingSub) bool { return sub.kind == kind })
}

// forgetSubqueriesOfSegment drops the records whose bindings lived in a segment
// that has just been replaced.
func (b *Builder) forgetSubqueriesOfSegment(segment string) {
	b.forgetSubqueries(func(sub pendingSub) bool { return sub.segment == segment })
}

// SelectSub answers Builder::selectSub: a subquery as one column of the select
// list.
func (b *Builder) SelectSub(query any, as string) *Builder {
	sub, sql, bindings, err := b.resolveSub(query)
	if err != nil {
		return b.refuse("and", err)
	}

	offset := len(b.Bindings["select"])
	b.AddSelect(b.aliased(sql, as, false))
	b.AddBinding(bindings, "select")
	b.pend(sub, as, false, "select", "select", len(b.Columns)-1, offset, len(bindings))
	return b
}

// SelectExpression answers Builder::selectExpression: an expression as one
// column, under an alias. It binds nothing, because an expression is SQL.
func (b *Builder) SelectExpression(expression any, as string) *Builder {
	return b.AddSelect(b.aliased(stringify(expression), as, false))
}

// FromSub answers Builder::fromSub: the query reads from a subquery rather than
// from a table.
func (b *Builder) FromSub(query any, as string) *Builder {
	sub, sql, bindings, err := b.resolveSub(query)
	if err != nil {
		return b.refuse("and", err)
	}

	offset := len(b.Bindings["from"])
	b.from = b.aliased(sql, as, true)
	b.AddBinding(bindings, "from")
	b.pend(sub, as, true, "from", "from", 0, offset, len(bindings))
	return b
}

// JoinSub answers Builder::joinSub: a join against a subquery.
//
// typ is the join type -- inner, left, right, cross or straight_join -- and
// isWhere says whether the condition compares a column with a value rather than
// with another column, which is what the PHP's $where flag does.
func (b *Builder) JoinSub(query any, as string, first any, operator, second any, typ string, isWhere bool) *Builder {
	sub, sql, bindings, err := b.resolveSub(query)
	if err != nil {
		return b.refuse("and", err)
	}
	if typ == "" {
		typ = "inner"
	}

	offset := len(b.Bindings["join"])
	b.AddBinding(bindings, "join")

	expression := b.aliased(sql, as, true)
	b.addJoinClause(typ, isWhere, expression, first, operator, second)
	b.pend(sub, as, true, "join", "join", len(b.Joins)-1, offset, len(bindings))
	return b
}

// LeftJoinSub answers Builder::leftJoinSub.
func (b *Builder) LeftJoinSub(query any, as string, first any, operator, second any) *Builder {
	return b.JoinSub(query, as, first, operator, second, "left", false)
}

// RightJoinSub answers Builder::rightJoinSub.
func (b *Builder) RightJoinSub(query any, as string, first any, operator, second any) *Builder {
	return b.JoinSub(query, as, first, operator, second, "right", false)
}

// StraightJoinSub answers Builder::straightJoinSub.
func (b *Builder) StraightJoinSub(query any, as string, first any, operator, second any) *Builder {
	return b.JoinSub(query, as, first, operator, second, "straight_join", false)
}

// CrossJoinSub answers Builder::crossJoinSub: a cross join against a subquery,
// which has no condition at all.
//
// A cross join multiplies the rows of both sides, so the subquery is the whole
// of what the join contributes: without a tenant filter of its own it would
// pair this customer's rows with every customer's. It gets one in
// scopeSubqueryClauses, like every other subquery here.
func (b *Builder) CrossJoinSub(query any, as string) *Builder {
	sub, sql, bindings, err := b.resolveSub(query)
	if err != nil {
		return b.refuse("and", err)
	}

	offset := len(b.Bindings["join"])
	b.AddBinding(bindings, "join")

	b.Joins = append(b.Joins, NewJoinClause(b, "cross", b.aliased(sql, as, true)))
	b.pend(sub, as, true, "join", "join", len(b.Joins)-1, offset, len(bindings))
	return b
}

// JoinLateral answers Builder::joinLateral: a join against a subquery that may
// name the columns of the rows already joined.
func (b *Builder) JoinLateral(query any, as string, typ string) *Builder {
	sub, sql, bindings, err := b.resolveSub(query)
	if err != nil {
		return b.refuse("and", err)
	}
	if typ == "" {
		typ = "inner"
	}

	offset := len(b.Bindings["join"])
	b.AddBinding(bindings, "join")

	b.Joins = append(b.Joins, NewJoinLateralClause(b, typ, b.aliased(sql, as, true)))
	b.pend(sub, as, true, "join", "join", len(b.Joins)-1, offset, len(bindings))
	return b
}

// LeftJoinLateral answers Builder::leftJoinLateral.
func (b *Builder) LeftJoinLateral(query any, as string) *Builder {
	return b.JoinLateral(query, as, "left")
}

// scopeSubqueryClauses puts the tenant on every subquery this statement
// compiled into a from, a select or a join, and puts its bindings back where
// the old ones were.
//
// Scoping a subquery changes how many bindings it contributes -- it gains the
// tenant's -- so the values after it would slide onto the wrong placeholders if
// they were merely appended. The recorded offset says where its old values
// start, and the running shift accounts for the ones already replaced.
//
// It runs on the clone the statement is built from, never on the builder the
// caller holds. See pendingSub.
func (b *Builder) scopeSubqueryClauses(ctx context.Context, g auth.Grant) error {
	if len(b.subqueries) == 0 {
		return nil
	}

	shift := make(map[string]int, 3)
	for _, sub := range b.subqueries {
		scoped, err := sub.query.scoped(ctx, g)
		if err != nil {
			return err
		}

		expression := b.aliased(scoped.ToSQL(), sub.as, sub.table)
		switch sub.kind {
		case "from":
			b.from = expression
		case "select":
			if sub.index < len(b.Columns) {
				b.Columns[sub.index] = expression
			}
		case "join":
			if sub.index < len(b.Joins) {
				// The clone shares the JoinClause the caller's builder points
				// at, so the clause is copied before its table is replaced.
				join := *b.Joins[sub.index]
				join.Table = expression
				b.Joins[sub.index] = &join
			}
		default:
			return fmt.Errorf("query: a subquery was recorded against the unknown clause %q", sub.kind)
		}

		bindings := scoped.GetBindings()
		segment := b.Bindings[sub.segment]
		start := sub.offset + shift[sub.segment]
		if start < 0 || start+sub.length > len(segment) {
			return fmt.Errorf("query: the bindings of the %s subquery are no longer where they were recorded", sub.kind)
		}

		replaced := make([]any, 0, len(segment)-sub.length+len(bindings))
		replaced = append(replaced, segment[:start]...)
		replaced = append(replaced, bindings...)
		replaced = append(replaced, segment[start+sub.length:]...)
		b.Bindings[sub.segment] = replaced
		shift[sub.segment] += len(bindings) - sub.length
	}

	// The clauses now carry their tenant. Leaving the records in place would
	// scope them again if this statement were scoped twice.
	b.subqueries = nil
	return nil
}
