package concerns

import (
	"strings"

	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/str"
)

// QueriesRelationships filters a query by what a relation contains, without
// loading it.
//
// Every function here compiles to a subquery correlated by column, never by
// value -- `where exists (select * from posts where posts.user_id = users.id)`.
// That correlation is the whole trick: the inner query names the outer query's
// column, so the engine evaluates it per row and nothing has to be fetched to
// be counted.
//
// They are functions rather than builder methods because the Eloquent builder
// lives in the eloquent package, which imports this one. The signature carries
// what a method would read off the receiver: the query being filtered, and the
// relation.
//
// The relation must be built with relations.NoConstraints, or the subquery
// arrives already narrowed to one parent, which is the mistake that makes
// WhereHas return everything.

// Has answers QueriesRelationships::has.
//
// The `>= 1` case compiles to a plain EXISTS, which is the PHP's
// canUseExistsForExistenceCheck: an engine can stop at the first matching row
// rather than counting them all, and on a relation with many children that is
// the difference between an index probe and a scan.
func Has(q relations.Builder, relation relations.Relation, operator string, count int, boolean string) relations.Builder {
	if operator == "" {
		operator = ">="
	}
	if boolean == "" {
		boolean = "and"
	}

	existence := relationExistenceQuery(q, relation)

	if canUseExistsForExistenceCheck(operator, count) {
		q.GetQuery().AddWhereExistsQuery(existence.GetQuery(), boolean, operator == "<" && count == 1)
		return q
	}

	return addWhereCountQuery(q, existence, operator, count, boolean)
}

// OrHas answers QueriesRelationships::orHas.
func OrHas(q relations.Builder, relation relations.Relation, operator string, count int) relations.Builder {
	return Has(q, relation, operator, count, "or")
}

// DoesntHave answers QueriesRelationships::doesntHave.
func DoesntHave(q relations.Builder, relation relations.Relation, boolean string) relations.Builder {
	return Has(q, relation, "<", 1, boolean)
}

// OrDoesntHave answers QueriesRelationships::orDoesntHave.
func OrDoesntHave(q relations.Builder, relation relations.Relation) relations.Builder {
	return DoesntHave(q, relation, "or")
}

// WhereHas answers QueriesRelationships::whereHas: has, with the caller's own
// constraints on the subquery.
func WhereHas(q relations.Builder, relation relations.Relation, callback func(relations.Builder), operator string, count int) relations.Builder {
	if operator == "" {
		operator = ">="
	}
	if count == 0 {
		count = 1
	}

	existence := relationExistenceQuery(q, relation)
	if callback != nil {
		callback(existence)
	}

	if canUseExistsForExistenceCheck(operator, count) {
		q.GetQuery().AddWhereExistsQuery(existence.GetQuery(), "and", operator == "<" && count == 1)
		return q
	}
	return addWhereCountQuery(q, existence, operator, count, "and")
}

// OrWhereHas answers QueriesRelationships::orWhereHas.
func OrWhereHas(q relations.Builder, relation relations.Relation, callback func(relations.Builder), operator string, count int) relations.Builder {
	existence := relationExistenceQuery(q, relation)
	if callback != nil {
		callback(existence)
	}
	q.GetQuery().AddWhereExistsQuery(existence.GetQuery(), "or", false)
	return q
}

// WhereDoesntHave answers QueriesRelationships::whereDoesntHave.
func WhereDoesntHave(q relations.Builder, relation relations.Relation, callback func(relations.Builder)) relations.Builder {
	existence := relationExistenceQuery(q, relation)
	if callback != nil {
		callback(existence)
	}
	q.GetQuery().AddWhereExistsQuery(existence.GetQuery(), "and", true)
	return q
}

// OrWhereDoesntHave answers QueriesRelationships::orWhereDoesntHave.
func OrWhereDoesntHave(q relations.Builder, relation relations.Relation, callback func(relations.Builder)) relations.Builder {
	existence := relationExistenceQuery(q, relation)
	if callback != nil {
		callback(existence)
	}
	q.GetQuery().AddWhereExistsQuery(existence.GetQuery(), "or", true)
	return q
}

// WhereRelation answers QueriesRelationships::whereRelation: whereHas with one
// column comparison, which is the shape nine calls in ten actually want.
func WhereRelation(q relations.Builder, relation relations.Relation, column string, args ...any) relations.Builder {
	return WhereHas(q, relation, func(sub relations.Builder) {
		sub.Where(column, args...)
	}, ">=", 1)
}

// OrWhereRelation answers QueriesRelationships::orWhereRelation.
func OrWhereRelation(q relations.Builder, relation relations.Relation, column string, args ...any) relations.Builder {
	return OrWhereHas(q, relation, func(sub relations.Builder) {
		sub.Where(column, args...)
	}, ">=", 1)
}

// WhereDoesntHaveRelation answers
// QueriesRelationships::whereDoesntHaveRelation.
func WhereDoesntHaveRelation(q relations.Builder, relation relations.Relation, column string, args ...any) relations.Builder {
	return WhereDoesntHave(q, relation, func(sub relations.Builder) {
		sub.Where(column, args...)
	})
}

// WithCount answers QueriesRelationships::withCount.
//
// The count arrives as a column on the parent row -- posts_count -- rather than
// as a second query, which is what makes a list of users with their post counts
// one statement.
func WithCount(q relations.Builder, name string, relation relations.Relation) relations.Builder {
	return WithAggregate(q, name, relation, query.Raw("*"), "count")
}

// WithMax answers QueriesRelationships::withMax.
func WithMax(q relations.Builder, name string, relation relations.Relation, column string) relations.Builder {
	return WithAggregate(q, name, relation, column, "max")
}

// WithMin answers QueriesRelationships::withMin.
func WithMin(q relations.Builder, name string, relation relations.Relation, column string) relations.Builder {
	return WithAggregate(q, name, relation, column, "min")
}

// WithSum answers QueriesRelationships::withSum.
func WithSum(q relations.Builder, name string, relation relations.Relation, column string) relations.Builder {
	return WithAggregate(q, name, relation, column, "sum")
}

// WithAvg answers QueriesRelationships::withAvg.
func WithAvg(q relations.Builder, name string, relation relations.Relation, column string) relations.Builder {
	return WithAggregate(q, name, relation, column, "avg")
}

// WithExists answers QueriesRelationships::withExists.
func WithExists(q relations.Builder, name string, relation relations.Relation) relations.Builder {
	return WithAggregate(q, name, relation, query.Raw("*"), "exists")
}

// WithAggregate answers QueriesRelationships::withAggregate: the subquery goes
// into the select list, aliased to relation_function.
//
// The column goes in through query.SelectSub, which is the method that records a
// subquery so the tenant can be put on it when the Grant arrives. This wrote the
// column itself -- AddSelect over sub.ToSQL(), with the bindings added by hand --
// which is the same leak the eloquent builder's copy of this function had, found
// by the audit that ran Users.WithCount("posts").Get(auth.SystemGrant(
// "user.list", "acme")) and got a posts_count over every tenant's posts. A
// subquery compiled into a raw column is a subquery nothing can scope
// afterwards, and there is one way to write one.
func WithAggregate(q relations.Builder, name string, relation relations.Relation, column any, function string) relations.Builder {
	existence := relation.GetQuery()
	if relationQuery, ok := relation.(existenceQueryBuilder); ok {
		existence = relationQuery.GetRelationExistenceQuery(relation.GetRelated().NewQuery(), q)
	}

	rendered := renderColumn(existence, column)
	existence.Select(query.Raw(function + "(" + rendered + ")"))

	sub := existence.GetQuery()
	sub.ApplyBeforeQueryCallbacks()

	q.GetQuery().SelectSub(sub, AggregateAlias(name, function, column))
	return q
}

// AggregateAlias answers the alias withAggregate builds: posts_count for a
// count over *, orders_sum_total for a sum over a column.
//
// The PHP strips everything that is not alphanumeric, space or underscore from
// "$name $function $column" and snake cases the rest, which is how the * of a
// count disappears. The same three steps, spelled out.
func AggregateAlias(name, function string, column any) string {
	rendered := ""
	if text, ok := column.(string); ok {
		rendered = text
	}

	var cleaned strings.Builder
	for _, r := range name + " " + function + " " + rendered {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '_':
			cleaned.WriteRune(r)
		}
	}

	return str.Snake(strings.TrimSpace(cleaned.String()), "_")
}

// relationExistenceQuery answers
// QueriesRelationships::getRelationWithoutConstraints followed by
// getRelationExistenceQuery.
func relationExistenceQuery(parent relations.Builder, relation relations.Relation) relations.Builder {
	sub := relation.GetRelated().NewQuery()

	if relationQuery, ok := relation.(existenceQueryBuilder); ok {
		return relationQuery.GetRelationExistenceQuery(sub, parent, query.Raw("*"))
	}
	return sub
}

// existenceQueryBuilder is the half of a relation that a has-query needs. Every
// relation implements it; the assertion is what lets this package take the
// Relation interface and still reach the method.
type existenceQueryBuilder interface {
	GetRelationExistenceQuery(q relations.Builder, parentQuery relations.Builder, columns ...any) relations.Builder
}

// addWhereCountQuery answers QueriesRelationships::addWhereCountQuery: the
// comparison forms, where EXISTS cannot answer.
//
// The clause is built by query.WhereSubCount, which keeps the subquery on it so
// the tenant can be put on it when the Grant arrives. This froze sub.ToSQL()
// into a Raw column instead -- the same clause, written the same way, in the
// eloquent builder's copy of this function, and the audit proved that one leaked:
// Has("posts", ">", 3) compared against a count over every tenant's posts.
func addWhereCountQuery(q relations.Builder, existence relations.Builder, operator string, count int, boolean string) relations.Builder {
	sub := existence.GetQuery()
	sub.ApplyBeforeQueryCallbacks()

	q.GetQuery().WhereSubCount(sub, operator, count, boolean)
	return q
}

// canUseExistsForExistenceCheck answers the method of the same name.
func canUseExistsForExistenceCheck(operator string, count int) bool {
	return (operator == ">=" || operator == "<") && count == 1
}

func renderColumn(b relations.Builder, column any) string {
	if query.IsExpression(column) {
		return strings.TrimSpace(column.(query.Expression).String())
	}
	return b.GetQuery().GetGrammar().Wrap(column)
}
