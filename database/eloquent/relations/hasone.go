package relations

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations/concerns"
	"github.com/arandu-io/hesape/database/query"
)

// HasOne answers Illuminate\Database\Eloquent\Relations\HasOne.
//
// One row on the other table, found by a foreign key pointing here. It is
// HasMany with matchOne instead of matchMany, plus the three traits that make
// it comparable, defaultable and reducible to one of many.
type HasOne struct {
	HasOneOrMany
	concerns.SupportsDefaultModels
	concerns.ComparesRelatedModels
	concerns.CanBeOneOfMany
}

// NewHasOne answers HasOne::__construct, by way of HasOneOrMany's.
func NewHasOne(query Builder, parent Model, foreignKey, localKey string) *HasOne {
	relation := &HasOne{HasOneOrMany: NewHasOneOrMany(query, parent, foreignKey, localKey)}

	relation.SupportsDefaultModels = concerns.SupportsDefaultModels{
		NewRelatedInstanceFor: relation.NewRelatedInstanceFor,
	}
	relation.ComparesRelatedModels = concerns.ComparesRelatedModels{
		CompareRelated:        relation.Related,
		CompareParentKey:      relation.GetParentKey,
		CompareRelatedKeyFrom: relation.getRelatedKeyFrom,
	}
	relation.CanBeOneOfMany = concerns.CanBeOneOfMany{
		OneOfManyQuery:                      relation.GetQuery,
		OneOfManyRelated:                    relation.GetRelated,
		OneOfManyAddConstraints:             relation.AddConstraints,
		AddOneOfManySubQueryConstraints:     relation.AddOneOfManySubQueryConstraints,
		GetOneOfManySubQuerySelectColumns:   relation.GetOneOfManySubQuerySelectColumns,
		AddOneOfManyJoinSubQueryConstraints: relation.AddOneOfManyJoinSubQueryConstraints,
	}

	relation.AddConstraints()
	return relation
}

// GetRelationQuery answers CanBeOneOfMany::getRelationQuery, which shadows
// Relation's: on a one-of-many relation the constraints belong on the subquery.
func (r *HasOne) GetRelationQuery() Builder {
	return r.CanBeOneOfMany.GetRelationQuery(r.Query)
}

// AddConstraints answers HasOneOrMany::addConstraints, redeclared so that the
// constraints reach the one-of-many subquery when there is one. Go promotes the
// embedded method but not the overridden GetRelationQuery it calls, which is
// the whole of what "no virtual dispatch" costs.
func (r *HasOne) AddConstraints() {
	if !ConstraintsEnabled() {
		return
	}
	q := r.GetRelationQuery()
	q.Where(r.GetQualifiedForeignKeyName(), "=", r.GetParentKey())
	q.WhereNotNull(r.GetQualifiedForeignKeyName())
}

// GetResults answers HasOne::getResults.
func (r *HasOne) GetResults(ctx context.Context, g auth.Grant) (any, error) {
	if r.GetParentKey() == nil {
		return r.GetDefaultFor(r.Parent), nil
	}

	result, err := r.First(ctx, g)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return r.GetDefaultFor(r.Parent), nil
	}
	return result, nil
}

// InitRelation answers HasOne::initRelation.
func (r *HasOne) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, r.GetDefaultFor(model))
	}
	return models
}

// Match answers HasOne::match.
func (r *HasOne) Match(models []Model, results []Model, relation string) ([]Model, error) {
	return r.MatchOne(models, results, relation)
}

// GetRelationExistenceQuery answers HasOne::getRelationExistenceQuery.
func (r *HasOne) GetRelationExistenceQuery(q Builder, parentQuery Builder, columns ...any) Builder {
	if r.IsOneOfMany() {
		r.MergeOneOfManyJoinsTo(q)
	}
	return r.HasOneOrMany.GetRelationExistenceQuery(q, parentQuery, columns...)
}

// AddOneOfManySubQueryConstraints answers the method of the same name.
func (r *HasOne) AddOneOfManySubQueryConstraints(q Builder, column, aggregate string) {
	q.AddSelect(r.GetQualifiedForeignKeyName())
}

// GetOneOfManySubQuerySelectColumns answers the method of the same name.
func (r *HasOne) GetOneOfManySubQuerySelectColumns() []any {
	return []any{r.GetQualifiedForeignKeyName()}
}

// AddOneOfManyJoinSubQueryConstraints answers the method of the same name.
func (r *HasOne) AddOneOfManyJoinSubQueryConstraints(join *query.JoinClause) {
	join.On(
		r.QualifySubSelectColumn(r.GetForeignKeyName()),
		"=",
		r.QualifyRelatedColumn(r.GetForeignKeyName()),
	)
}

// NewRelatedInstanceFor answers HasOne::newRelatedInstanceFor: the default
// model, already pointing back at its parent.
func (r *HasOne) NewRelatedInstanceFor(parent Model) Model {
	instance := r.Related.NewInstance(nil)
	instance.SetAttribute(r.GetForeignKeyName(), parent.GetAttribute(r.GetLocalKeyName()))
	r.ApplyInverseRelationToModel(instance, parent)
	return instance
}

// getRelatedKeyFrom answers HasOne::getRelatedKeyFrom.
func (r *HasOne) getRelatedKeyFrom(model Model) any {
	return model.GetAttribute(r.GetForeignKeyName())
}
