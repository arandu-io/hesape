package relations

import (
	"context"

	"github.com/arandu-io/hesape/auth"
)

// HasMany answers Illuminate\Database\Eloquent\Relations\HasMany.
//
// The $user->posts of the introduction: every row on the other table whose
// foreign key points here.
type HasMany struct {
	HasOneOrMany
}

// NewHasMany answers HasMany::__construct, by way of HasOneOrMany's.
func NewHasMany(query Builder, parent Model, foreignKey, localKey string) *HasMany {
	relation := &HasMany{HasOneOrMany: NewHasOneOrMany(query, parent, foreignKey, localKey)}
	relation.AddConstraints()
	return relation
}

// One answers HasMany::one: the same relation, read as a single model.
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

// GetResults answers HasMany::getResults.
func (r *HasMany) GetResults(ctx context.Context, g auth.Grant) (any, error) {
	if r.GetParentKey() == nil {
		return []Model{}, nil
	}
	return r.Get(ctx, g)
}

// InitRelation answers HasMany::initRelation.
//
// Seeding every parent with an empty collection is what makes a parent with no
// children answer "none" instead of going back to the database to ask.
func (r *HasMany) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, []Model{})
	}
	return models
}

// Match answers HasMany::match.
func (r *HasMany) Match(models []Model, results []Model, relation string) ([]Model, error) {
	return r.MatchMany(models, results, relation)
}
