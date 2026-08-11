package eloquent

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/str"
)

// This file answers Illuminate\Database\Eloquent\Concerns\QueriesRelationships.
//
// Every method here builds SQL and runs nothing, so none of them takes a Grant.
// A relation whose name is not registered on the model is an error the builder
// holds until something runs -- see Builder.err.

// Has answers QueriesRelationships::has.
//
// Go has no default arguments, so the full form is spelled out and the short
// ones -- WhereHas, DoesntHave, OrHas, which the PHP has too -- are the ones to
// reach for. callback may be nil.
func (b *Builder[T]) Has(relation, operator string, count int, boolean string, callback func(*query.Builder)) *Builder[T] {
	if strings.Contains(relation, ".") {
		return b.hasNested(relation, operator, count, boolean, callback)
	}

	rel, err := b.GetRelationWithoutConstraints(relation)
	if err != nil {
		return b.fail(err)
	}
	return b.addHasWhere(rel, operator, count, boolean, callback)
}

// addHasWhere answers QueriesRelationships::addHasWhere.
//
// A "> = 1" or a "< 1" is an exists rather than a count, which is the
// optimisation canUseExistsForExistenceCheck exists for: the engine can stop at
// the first matching row instead of counting every one.
func (b *Builder[T]) addHasWhere(rel Relation, operator string, count int, boolean string, callback func(*query.Builder)) *Builder[T] {
	if boolean == "" {
		boolean = "and"
	}
	if canUseExistsForExistenceCheck(operator, count) {
		sub := rel.GetRelationExistenceQuery(b.query, query.Raw("*"))
		if callback != nil {
			callback(sub)
		}
		b.query.AddWhereExistsQuery(sub, boolean, operator == "<" && count == 1)
		return b
	}

	sub := rel.GetRelationExistenceQuery(b.query, query.Raw("count(*)"))
	if callback != nil {
		callback(sub)
	}
	return b.addWhereCountQuery(sub, operator, count, boolean)
}

// addWhereCountQuery answers QueriesRelationships::addWhereCountQuery: the
// subquery compared against a number, as an expression on the left of the
// operator.
func (b *Builder[T]) addWhereCountQuery(sub *query.Builder, operator string, count int, boolean string) *Builder[T] {
	sql := sub.ToSQL()
	b.query.AddBinding(sub.GetBindings(), "where")
	where := query.Where{
		Type:     "Basic",
		Column:   query.Raw("(" + sql + ")"),
		Operator: operator,
		Value:    query.Raw(fmt.Sprint(count)),
		Boolean:  boolean,
	}
	b.query.Wheres = append(b.query.Wheres, where)
	return b
}

// canUseExistsForExistenceCheck answers
// QueriesRelationships::canUseExistsForExistenceCheck.
func canUseExistsForExistenceCheck(operator string, count int) bool {
	return (operator == ">=" || operator == "<") && count == 1
}

// hasNested answers QueriesRelationships::hasNested.
//
// PHP recurses by calling whereHas on the related model's builder, which it
// reaches because a relation there hands back a builder of another model class.
// Go cannot hold "a builder of some other model" in a variable, so the recursion
// happens on the relation instead: each segment asks the one before it for the
// next, and the existence queries nest the same way the closures did.
//
// A relation that cannot answer for the next segment is an error, never a query
// with the filter silently missing.
func (b *Builder[T]) hasNested(relations, operator string, count int, boolean string, callback func(*query.Builder)) *Builder[T] {
	segments := strings.Split(relations, ".")

	rel, err := b.GetRelationWithoutConstraints(segments[0])
	if err != nil {
		return b.fail(err)
	}

	chain := []Relation{rel}
	for _, segment := range segments[1:] {
		nested, ok := chain[len(chain)-1].(NestedRelation)
		if !ok {
			return b.fail(fmt.Errorf("%w: %s does not reach %s", ErrRelationNotFound, relations, segment))
		}
		next, err := nested.Nested(segment)
		if err != nil {
			return b.fail(err)
		}
		chain = append(chain, next)
	}

	// The innermost relation carries the caller's constraints; every outer one
	// only has to exist.
	return b.addHasWhere(chain[0], operator, count, boolean, func(sub *query.Builder) {
		nest(sub, chain[1:], callback)
	})
}

func nest(parent *query.Builder, chain []Relation, callback func(*query.Builder)) {
	if len(chain) == 0 {
		if callback != nil {
			callback(parent)
		}
		return
	}
	sub := chain[0].GetRelationExistenceQuery(parent, query.Raw("*"))
	nest(sub, chain[1:], callback)
	parent.AddWhereExistsQuery(sub, "and", false)
}

// NestedRelation is the relation a dotted existence check walks through. See
// hasNested.
type NestedRelation interface {
	Relation

	// Nested answers the relation the next segment of a dotted path names, on
	// the related model.
	Nested(name string) (Relation, error)
}

// OrHas answers QueriesRelationships::orHas.
func (b *Builder[T]) OrHas(relation, operator string, count int) *Builder[T] {
	return b.Has(relation, operator, count, "or", nil)
}

// DoesntHave answers QueriesRelationships::doesntHave.
func (b *Builder[T]) DoesntHave(relation, boolean string, callback func(*query.Builder)) *Builder[T] {
	return b.Has(relation, "<", 1, boolean, callback)
}

// OrDoesntHave answers QueriesRelationships::orDoesntHave.
func (b *Builder[T]) OrDoesntHave(relation string) *Builder[T] {
	return b.DoesntHave(relation, "or", nil)
}

// WhereHas answers QueriesRelationships::whereHas.
func (b *Builder[T]) WhereHas(relation string, callback func(*query.Builder)) *Builder[T] {
	return b.Has(relation, ">=", 1, "and", callback)
}

// WhereHasCount answers whereHas with the operator and count PHP defaults.
func (b *Builder[T]) WhereHasCount(relation string, callback func(*query.Builder), operator string, count int) *Builder[T] {
	return b.Has(relation, operator, count, "and", callback)
}

// OrWhereHas answers QueriesRelationships::orWhereHas.
func (b *Builder[T]) OrWhereHas(relation string, callback func(*query.Builder)) *Builder[T] {
	return b.Has(relation, ">=", 1, "or", callback)
}

// WhereDoesntHave answers QueriesRelationships::whereDoesntHave.
func (b *Builder[T]) WhereDoesntHave(relation string, callback func(*query.Builder)) *Builder[T] {
	return b.DoesntHave(relation, "and", callback)
}

// OrWhereDoesntHave answers QueriesRelationships::orWhereDoesntHave.
func (b *Builder[T]) OrWhereDoesntHave(relation string, callback func(*query.Builder)) *Builder[T] {
	return b.DoesntHave(relation, "or", callback)
}

// WhereRelation answers QueriesRelationships::whereRelation.
func (b *Builder[T]) WhereRelation(relation string, column any, args ...any) *Builder[T] {
	return b.WhereHas(relation, func(sub *query.Builder) {
		sub.Where(column, args...)
	})
}

// OrWhereRelation answers QueriesRelationships::orWhereRelation.
func (b *Builder[T]) OrWhereRelation(relation string, column any, args ...any) *Builder[T] {
	return b.OrWhereHas(relation, func(sub *query.Builder) {
		sub.Where(column, args...)
	})
}

// WhereDoesntHaveRelation answers
// QueriesRelationships::whereDoesntHaveRelation.
func (b *Builder[T]) WhereDoesntHaveRelation(relation string, column any, args ...any) *Builder[T] {
	return b.WhereDoesntHave(relation, func(sub *query.Builder) {
		sub.Where(column, args...)
	})
}

// WhereMorphRelation answers QueriesRelationships::whereMorphRelation.
//
// The PHP walks the types, builds the concrete relation for each by
// instantiating the class named in the morph column, and ors the branches
// together. Go cannot make a type from a string, so the relation answers for its
// own types through RelationForMorphType; everything else is the same shape.
//
// types of {"*"} is not carried over: PHP resolves it by selecting the distinct
// morph column first, which is a query, and this method builds SQL and runs
// nothing.
func (b *Builder[T]) WhereMorphRelation(relation string, types []string, column any, args ...any) *Builder[T] {
	rel, err := b.GetRelationWithoutConstraints(relation)
	if err != nil {
		return b.fail(err)
	}
	morph, ok := rel.(MorphRelation)
	if !ok {
		return b.fail(fmt.Errorf("%w: %s is not a polymorphic relation", ErrRelationNotFound, relation))
	}
	if len(types) == 0 || (len(types) == 1 && types[0] == "*") {
		return b.fail(fmt.Errorf("eloquent: %s needs the morph types spelled out, because resolving \"*\" is a query and this builds SQL", relation))
	}

	return b.Where(func(outer *Builder[T]) {
		for _, morphType := range types {
			typed, err := morph.RelationForMorphType(morphType)
			if err != nil {
				// The failure is recorded on the outer builder and not on the
				// nested one, which is thrown away as soon as its wheres are
				// merged -- an error left on it would never be reported.
				b.fail(err)
				return
			}
			outer.OrWhere(func(branch *Builder[T]) {
				branch.Where(b.model.QualifyColumn(morph.GetMorphType()), "=", morphType)
				branch.addHasWhere(typed, ">=", 1, "and", func(sub *query.Builder) {
					sub.Where(column, args...)
				})
			})
		}
	})
}

// WhereBelongsTo answers QueriesRelationships::whereBelongsTo.
//
// It is a function and not a method because the related models are of another
// type and a Go method cannot introduce a type parameter. The relation is named,
// as it is in PHP whenever the guess from the class name would be wrong.
func WhereBelongsTo[T, R any](b *Builder[T], relationshipName string, related ...*Model[R]) *Builder[T] {
	if len(related) == 0 {
		return b.fail(fmt.Errorf("eloquent: WhereBelongsTo was given no models to belong to"))
	}
	rel, err := b.GetRelationWithoutConstraints(relationshipName)
	if err != nil {
		return b.fail(err)
	}
	belongsTo, ok := rel.(BelongsToRelation)
	if !ok {
		return b.fail(fmt.Errorf("%w: %s is not a belongs-to relation", ErrRelationNotFound, relationshipName))
	}

	keys := make([]any, 0, len(related))
	for _, model := range related {
		keys = append(keys, model.GetAttribute(belongsTo.GetOwnerKeyName()))
	}
	b.query.WhereIn(belongsTo.GetQualifiedForeignKeyName(), keys)
	return b
}

// WithAggregate answers QueriesRelationships::withAggregate: a subselect per
// relation, aliased onto the row.
//
// A name may carry an alias -- "posts as recent_posts" -- exactly as there.
func (b *Builder[T]) WithAggregate(relations []string, column, function string) *Builder[T] {
	if len(relations) == 0 {
		return b
	}
	if b.query.Columns == nil {
		b.query.Select(query.Raw(b.model.Grammar.WrapTable(b.model.GetTable()) + ".*"))
	}

	for _, entry := range relations {
		name, alias := splitAggregateAlias(entry)

		rel, err := b.GetRelationWithoutConstraints(name)
		if err != nil {
			return b.fail(err)
		}

		var expression string
		switch {
		case function == "":
			expression = column
		case function == "exists":
			expression = b.aggregateColumn(column)
		default:
			expression = fmt.Sprintf("%s(%s)", function, b.aggregateColumn(column))
		}

		sub := rel.GetRelationExistenceQuery(b.query, query.Raw(expression))
		if constraints, ok := b.eagerLoad[name]; ok && constraints != nil {
			// The eager-load constraints narrow the subquery, not the query the
			// subselect hangs off -- which is what callScope does to the relation's
			// own builder there.
			constraints(sub)
		}

		if alias == "" {
			alias = aggregateAlias(name, function, column)
		}

		if function == "exists" {
			b.SelectRaw(fmt.Sprintf("exists(%s) as %s", sub.ToSQL(), b.model.Grammar.Wrap(alias)), sub.GetBindings()...)
			continue
		}
		if function == "" {
			sub.Limit(1)
		}
		b.AddSelect(query.Raw("(" + sub.ToSQL() + ") as " + b.model.Grammar.Wrap(alias)))
		b.query.AddBinding(sub.GetBindings(), "select")
	}
	return b
}

// aggregateColumn wraps the column an aggregate is taken over, leaving "*"
// alone, which is what the PHP's `$column === '*' ? $column : wrap(...)` does.
func (b *Builder[T]) aggregateColumn(column string) string {
	if column == "*" {
		return column
	}
	return b.model.Grammar.Wrap(column)
}

// splitAggregateAlias reads "posts as recent" into its two halves.
func splitAggregateAlias(entry string) (name, alias string) {
	segments := strings.Fields(entry)
	if len(segments) == 3 && strings.EqualFold(segments[1], "as") {
		return segments[0], segments[2]
	}
	return entry, ""
}

// aggregateAlias answers the alias withAggregate builds when none was given:
// the relation, the function and the column, snake cased, with the punctuation
// dropped. "posts", "count", "*" is posts_count.
func aggregateAlias(name, function, column string) string {
	raw := fmt.Sprintf("%s %s %s", name, function, strings.ToLower(column))
	var kept strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '_':
			kept.WriteRune(r)
		}
	}
	return str.Snake(strings.TrimSpace(kept.String()), "_")
}

// WithCount answers QueriesRelationships::withCount.
func (b *Builder[T]) WithCount(relations ...string) *Builder[T] {
	return b.WithAggregate(relations, "*", "count")
}

// WithMax answers QueriesRelationships::withMax.
func (b *Builder[T]) WithMax(relation, column string) *Builder[T] {
	return b.WithAggregate([]string{relation}, column, "max")
}

// WithMin answers QueriesRelationships::withMin.
func (b *Builder[T]) WithMin(relation, column string) *Builder[T] {
	return b.WithAggregate([]string{relation}, column, "min")
}

// WithSum answers QueriesRelationships::withSum.
func (b *Builder[T]) WithSum(relation, column string) *Builder[T] {
	return b.WithAggregate([]string{relation}, column, "sum")
}

// WithAvg answers QueriesRelationships::withAvg.
func (b *Builder[T]) WithAvg(relation, column string) *Builder[T] {
	return b.WithAggregate([]string{relation}, column, "avg")
}

// WithExists answers QueriesRelationships::withExists.
func (b *Builder[T]) WithExists(relation string) *Builder[T] {
	return b.WithAggregate([]string{relation}, "*", "exists")
}

// loadAggregateModels is the query Collection::loadAggregate runs: the keys of
// the collection, with the aggregate columns beside them.
func (b *Builder[T]) loadAggregateModels(g auth.Grant, keys []any, relations []string, column, function string) (Collection[T], error) {
	return b.WhereKey(keys).
		Select(b.model.GetQualifiedKeyName()).
		WithAggregate(relations, column, function).
		Get(g)
}
