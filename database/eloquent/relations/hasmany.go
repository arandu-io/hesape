package relations

import (
	"context"

	"github.com/arandu-io/hesape/auth"
)

// HasMany is every row on the other table whose foreign key points here.
type HasMany struct {
	HasOneOrMany
}

// NewHasMany builds a HasMany over query for parent, joining on foreignKey
// and localKey, applies its constraints, and returns it.
func NewHasMany(query Builder, parent Model, foreignKey, localKey string) *HasMany {
	relation := &HasMany{HasOneOrMany: NewHasOneOrMany(query, parent, foreignKey, localKey)}
	relation.AddConstraints()
	return relation
}

// One returns the same relation read as a single model instead of a slice.
//
// It is built with constraints off and then given them back, because the new
// HasOne would otherwise add a second copy of the where clause the HasMany
// already put on the shared query.
func (r *HasMany) One() *HasOne {
	var one *HasOne
	NoConstraints(func() {
		one = NewHasOne(r.Query, r.Parent, r.GetQualifiedForeignKeyName(), r.GetLocalKeyName())
	})
	if inverse := r.GetInverseRelationship(); inverse != "" {
		_ = one.Inverse(inverse)
	}
	return one
}

// GetResults returns every related model for the parent, or an empty slice
// if the parent has no key yet.
func (r *HasMany) GetResults(ctx context.Context, g auth.Grant) (any, error) {
	if r.GetParentKey() == nil {
		return []Model{}, nil
	}
	return r.Get(ctx, g)
}

// InitRelation seeds relation on every model in models with an empty slice.
//
// Seeding every parent with an empty collection is what makes a parent with no
// children answer "none" instead of going back to the database to ask.
func (r *HasMany) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, []Model{})
	}
	return models
}

// Match assigns each model in models every result in results that belongs to
// it, via MatchMany, and stores the slice under relation.
func (r *HasMany) Match(models []Model, results []Model, relation string) ([]Model, error) {
	return r.MatchMany(models, results, relation)
}
