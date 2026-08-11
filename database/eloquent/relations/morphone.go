package relations

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations/concerns"
	"github.com/arandu-io/hesape/database/query"
)

// MorphOne answers Illuminate\Database\Eloquent\Relations\MorphOne: one row on
// a polymorphic table, the image of a post or of a user.
type MorphOne struct {
	MorphOneOrMany
	concerns.SupportsDefaultModels
	concerns.ComparesRelatedModels
	concerns.CanBeOneOfMany
}

// NewMorphOne answers MorphOne::__construct.
func NewMorphOne(query Builder, parent Model, typ, id, localKey string) *MorphOne {
	relation := &MorphOne{MorphOneOrMany: NewMorphOneOrMany(query, parent, typ, id, localKey)}

	relation.SupportsInverseRelations.PossibleInverseRelations = relation.PossibleInverseRelations
	relation.SupportsDefaultModels = concerns.SupportsDefaultModels{
		NewRelatedInstanceFor: relation.newRelatedInstanceFor,
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

// GetRelationQuery answers CanBeOneOfMany::getRelationQuery.
func (r *MorphOne) GetRelationQuery() Builder {
	return r.CanBeOneOfMany.GetRelationQuery(r.Query)
}

// AddConstraints answers MorphOneOrMany::addConstraints, redeclared so that it
// reaches this type's GetRelationQuery.
func (r *MorphOne) AddConstraints() {
	if !ConstraintsEnabled() {
		return
	}
	q := r.GetRelationQuery()
	q.Where(r.GetQualifiedMorphType(), r.GetMorphClass())
	q.Where(r.GetQualifiedForeignKeyName(), "=", r.GetParentKey())
	q.WhereNotNull(r.GetQualifiedForeignKeyName())
}

// GetResults answers MorphOne::getResults.
func (r *MorphOne) GetResults(ctx context.Context, g auth.Grant) (any, error) {
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

// InitRelation answers MorphOne::initRelation.
func (r *MorphOne) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, r.GetDefaultFor(model))
	}
	return models
}

// Match answers MorphOne::match.
func (r *MorphOne) Match(models []Model, results []Model, relation string) ([]Model, error) {
	return r.MatchOne(models, results, relation)
}

// GetRelationExistenceQuery answers MorphOne::getRelationExistenceQuery.
func (r *MorphOne) GetRelationExistenceQuery(q Builder, parentQuery Builder, columns ...any) Builder {
	if r.IsOneOfMany() {
		r.MergeOneOfManyJoinsTo(q)
	}
	return r.MorphOneOrMany.GetRelationExistenceQuery(q, parentQuery, columns...)
}

// AddOneOfManySubQueryConstraints answers the method of the same name: the
// subquery has to carry the type column too, or the join would pick the newest
// comment of any commentable with that id.
func (r *MorphOne) AddOneOfManySubQueryConstraints(q Builder, column, aggregate string) {
	q.AddSelect(r.GetQualifiedForeignKeyName(), r.GetQualifiedMorphType())
}

// GetOneOfManySubQuerySelectColumns answers the method of the same name.
func (r *MorphOne) GetOneOfManySubQuerySelectColumns() []any {
	return []any{r.GetQualifiedForeignKeyName(), r.GetQualifiedMorphType()}
}

// AddOneOfManyJoinSubQueryConstraints answers the method of the same name.
func (r *MorphOne) AddOneOfManyJoinSubQueryConstraints(join *query.JoinClause) {
	join.
		On(r.QualifySubSelectColumn(r.GetMorphType()), "=", r.QualifyRelatedColumn(r.GetMorphType())).
		On(r.QualifySubSelectColumn(r.GetForeignKeyName()), "=", r.QualifyRelatedColumn(r.GetForeignKeyName()))
}

// newRelatedInstanceFor answers MorphOne::newRelatedInstanceFor.
func (r *MorphOne) newRelatedInstanceFor(parent Model) Model {
	instance := r.Related.NewInstance(nil)
	instance.SetAttribute(r.GetForeignKeyName(), parent.GetAttribute(r.GetLocalKeyName()))
	instance.SetAttribute(r.GetMorphType(), r.GetMorphClass())
	r.ApplyInverseRelationToModel(instance, parent)
	return instance
}

// getRelatedKeyFrom answers MorphOne::getRelatedKeyFrom.
func (r *MorphOne) getRelatedKeyFrom(model Model) any {
	return model.GetAttribute(r.GetForeignKeyName())
}
