package concerns

import (
	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/database/query"
)

// The polymorphic half of QueriesRelationships, and the two shorthands that
// filter by a model rather than by a column.
//
// A morph existence check is not one subquery: the relation points at several
// tables, so it is one subquery per type, OR-ed inside a group and each one
// guarded by its own type clause. That structure is what keeps `whereHasMorph`
// from matching a video's comments against a post's id.

// WhereMorphedTo answers QueriesRelationships::whereMorphedTo: rows whose
// polymorphic relation is this exact model.
//
// Two columns, always. Matching the id alone is the polymorphic bug: id 7
// exists on every table in the schema.
func WhereMorphedTo(q relations.Builder, relation *relations.MorphTo, model relations.Model, boolean string) relations.Builder {
	alias, err := relations.GetMorphAlias(model)
	if err != nil {
		alias = model.GetMorphClass()
	}

	apply := func(sub *query.Builder) {
		sub.Where(relation.GetMorphType(), alias)
		sub.Where(relation.GetForeignKeyName(), model.GetKey())
	}

	if boolean == "or" {
		q.GetQuery().OrWhere(func(sub *query.Builder) { apply(sub) })
		return q
	}
	q.GetQuery().Where(func(sub *query.Builder) { apply(sub) })
	return q
}

// OrWhereMorphedTo answers QueriesRelationships::orWhereMorphedTo.
func OrWhereMorphedTo(q relations.Builder, relation *relations.MorphTo, model relations.Model) relations.Builder {
	return WhereMorphedTo(q, relation, model, "or")
}

// WhereNotMorphedTo answers QueriesRelationships::whereNotMorphedTo.
func WhereNotMorphedTo(q relations.Builder, relation *relations.MorphTo, model relations.Model, boolean string) relations.Builder {
	alias, err := relations.GetMorphAlias(model)
	if err != nil {
		alias = model.GetMorphClass()
	}

	apply := func(sub *query.Builder) {
		sub.Where(relation.GetMorphType(), "<>", alias)
		sub.OrWhere(relation.GetForeignKeyName(), "<>", model.GetKey())
	}

	if boolean == "or" {
		q.GetQuery().OrWhere(func(sub *query.Builder) { apply(sub) })
		return q
	}
	q.GetQuery().Where(func(sub *query.Builder) { apply(sub) })
	return q
}

// OrWhereNotMorphedTo answers QueriesRelationships::orWhereNotMorphedTo.
func OrWhereNotMorphedTo(q relations.Builder, relation *relations.MorphTo, model relations.Model) relations.Builder {
	return WhereNotMorphedTo(q, relation, model, "or")
}

// WhereBelongsTo answers QueriesRelationships::whereBelongsTo: the child rows
// of this parent, without naming the foreign key at the call site.
func WhereBelongsTo(q relations.Builder, relation *relations.BelongsTo, model relations.Model, boolean string) relations.Builder {
	column := relation.GetQualifiedForeignKeyName()
	value := model.GetAttribute(relation.GetOwnerKeyName())

	if boolean == "or" {
		q.GetQuery().OrWhere(column, "=", value)
		return q
	}
	return q.Where(column, "=", value)
}

// OrWhereBelongsTo answers QueriesRelationships::orWhereBelongsTo.
func OrWhereBelongsTo(q relations.Builder, relation *relations.BelongsTo, model relations.Model) relations.Builder {
	return WhereBelongsTo(q, relation, model, "or")
}

// WhereAttachedTo answers QueriesRelationships::whereAttachedTo: the rows a
// many-to-many attaches to this model.
func WhereAttachedTo(q relations.Builder, relation relations.Relation, model relations.Model, boolean string) relations.Builder {
	return WhereHas(q, relation, func(sub relations.Builder) {
		sub.WhereKey(model.GetKey())
	}, ">=", 1)
}

// OrWhereAttachedTo answers QueriesRelationships::orWhereAttachedTo.
func OrWhereAttachedTo(q relations.Builder, relation relations.Relation, model relations.Model) relations.Builder {
	return OrWhereHas(q, relation, func(sub relations.Builder) {
		sub.WhereKey(model.GetKey())
	}, ">=", 1)
}

// MorphConstraint is the callback whereHasMorph applies per type: it is handed
// the subquery and the type it is for, because a constraint on a video is
// rarely a constraint on a post.
type MorphConstraint func(sub relations.Builder, morphType string)

// HasMorph answers QueriesRelationships::hasMorph.
//
// One existence check per type, OR-ed together and each guarded by its type
// clause. types may be empty, which the PHP spells "*" and means every alias in
// the morph map.
func HasMorph(q relations.Builder, relation *relations.MorphTo, types []string, operator string, count int, boolean string, callback MorphConstraint) relations.Builder {
	if len(types) == 0 {
		for alias := range relations.GetMorphMap() {
			types = append(types, alias)
		}
	}

	group := func(outer *query.Builder) {
		for _, morphType := range types {
			related, err := relations.CreateModelByType(morphType)
			if err != nil {
				// An alias nobody registered contributes no branch. It cannot
				// contribute one: there is no table to select from.
				continue
			}

			belongsTo := belongsToForMorph(q.GetModel(), related, relation)
			existence := relationExistenceQuery(q, belongsTo)
			if callback != nil {
				callback(existence, morphType)
			}

			existence.GetQuery().ApplyBeforeQueryCallbacks()

			outer.OrWhere(func(sub *query.Builder) {
				sub.Where(relation.GetMorphType(), morphType)
				sub.AddWhereExistsQuery(existence.GetQuery(), "and", operator == "<" && count == 1)
			})
		}
	}

	// The base builder decides and/or by which method opens the group, so the
	// boolean is a branch here rather than an argument.
	if boolean == "or" {
		q.GetQuery().OrWhere(group)
		return q
	}
	q.GetQuery().Where(group)
	return q
}

// OrHasMorph answers QueriesRelationships::orHasMorph.
func OrHasMorph(q relations.Builder, relation *relations.MorphTo, types []string, operator string, count int) relations.Builder {
	return HasMorph(q, relation, types, operator, count, "or", nil)
}

// DoesntHaveMorph answers QueriesRelationships::doesntHaveMorph.
func DoesntHaveMorph(q relations.Builder, relation *relations.MorphTo, types []string, boolean string, callback MorphConstraint) relations.Builder {
	return HasMorph(q, relation, types, "<", 1, boolean, callback)
}

// OrDoesntHaveMorph answers QueriesRelationships::orDoesntHaveMorph.
func OrDoesntHaveMorph(q relations.Builder, relation *relations.MorphTo, types []string) relations.Builder {
	return DoesntHaveMorph(q, relation, types, "or", nil)
}

// WhereHasMorph answers QueriesRelationships::whereHasMorph.
func WhereHasMorph(q relations.Builder, relation *relations.MorphTo, types []string, callback MorphConstraint, operator string, count int) relations.Builder {
	if operator == "" {
		operator = ">="
	}
	if count == 0 {
		count = 1
	}
	return HasMorph(q, relation, types, operator, count, "and", callback)
}

// OrWhereHasMorph answers QueriesRelationships::orWhereHasMorph.
func OrWhereHasMorph(q relations.Builder, relation *relations.MorphTo, types []string, callback MorphConstraint, operator string, count int) relations.Builder {
	return HasMorph(q, relation, types, operator, count, "or", callback)
}

// WhereDoesntHaveMorph answers QueriesRelationships::whereDoesntHaveMorph.
func WhereDoesntHaveMorph(q relations.Builder, relation *relations.MorphTo, types []string, callback MorphConstraint) relations.Builder {
	return HasMorph(q, relation, types, "<", 1, "and", callback)
}

// OrWhereDoesntHaveMorph answers
// QueriesRelationships::orWhereDoesntHaveMorph.
func OrWhereDoesntHaveMorph(q relations.Builder, relation *relations.MorphTo, types []string, callback MorphConstraint) relations.Builder {
	return HasMorph(q, relation, types, "<", 1, "or", callback)
}

// WhereMorphRelation answers QueriesRelationships::whereMorphRelation.
func WhereMorphRelation(q relations.Builder, relation *relations.MorphTo, types []string, column string, args ...any) relations.Builder {
	return WhereHasMorph(q, relation, types, func(sub relations.Builder, _ string) {
		sub.Where(column, args...)
	}, ">=", 1)
}

// OrWhereMorphRelation answers QueriesRelationships::orWhereMorphRelation.
func OrWhereMorphRelation(q relations.Builder, relation *relations.MorphTo, types []string, column string, args ...any) relations.Builder {
	return OrWhereHasMorph(q, relation, types, func(sub relations.Builder, _ string) {
		sub.Where(column, args...)
	}, ">=", 1)
}

// WhereMorphDoesntHaveRelation answers
// QueriesRelationships::whereMorphDoesntHaveRelation.
func WhereMorphDoesntHaveRelation(q relations.Builder, relation *relations.MorphTo, types []string, column string, args ...any) relations.Builder {
	return WhereDoesntHaveMorph(q, relation, types, func(sub relations.Builder, _ string) {
		sub.Where(column, args...)
	})
}

// OrWhereMorphDoesntHaveRelation answers
// QueriesRelationships::orWhereMorphDoesntHaveRelation.
func OrWhereMorphDoesntHaveRelation(q relations.Builder, relation *relations.MorphTo, types []string, column string, args ...any) relations.Builder {
	return OrWhereDoesntHaveMorph(q, relation, types, func(sub relations.Builder, _ string) {
		sub.Where(column, args...)
	})
}

// OrWhereDoesntHaveRelation answers
// QueriesRelationships::orWhereDoesntHaveRelation.
func OrWhereDoesntHaveRelation(q relations.Builder, relation relations.Relation, column string, args ...any) relations.Builder {
	return OrWhereDoesntHave(q, relation, func(sub relations.Builder) {
		sub.Where(column, args...)
	})
}

// WithWhereHas answers QueriesRelationships::withWhereHas: filter by the
// relation and load it with the same constraint, which is the pair people write
// by hand and get subtly different.
//
// It returns the constraint alongside the query so that the caller's eager load
// applies the same one -- the PHP passes it to with() inside, and there is no
// with() on the builder contract here.
func WithWhereHas(q relations.Builder, relation relations.Relation, callback func(relations.Builder)) (relations.Builder, func(relations.Builder)) {
	return WhereHas(q, relation, callback, ">=", 1), callback
}

// WithWhereRelation answers QueriesRelationships::withWhereRelation.
func WithWhereRelation(q relations.Builder, relation relations.Relation, column string, args ...any) (relations.Builder, func(relations.Builder)) {
	constraint := func(sub relations.Builder) { sub.Where(column, args...) }
	return WhereHas(q, relation, constraint, ">=", 1), constraint
}

// MergeConstraintsFrom answers QueriesRelationships::mergeConstraintsFrom: the
// where clauses of one query copied onto another, which is how MorphTo carries
// the relation's constraints onto the per-type query.
func MergeConstraintsFrom(target relations.Builder, from relations.Builder) relations.Builder {
	source := from.GetQuery()
	destination := target.GetQuery()

	destination.Wheres = append(destination.Wheres, source.Wheres...)
	destination.AddBinding(source.Bindings["where"], "where")
	return target
}

// belongsToForMorph answers QueriesRelationships::getBelongsToRelation.
func belongsToForMorph(parent, related relations.Model, relation *relations.MorphTo) relations.Relation {
	var belongsTo *relations.BelongsTo
	relations.NoConstraints(func() {
		belongsTo = relations.NewBelongsTo(
			related.NewQuery(), parent,
			relation.GetForeignKeyName(), relation.GetOwnerKeyName(), relation.GetRelationName(),
		)
	})
	return belongsTo
}
