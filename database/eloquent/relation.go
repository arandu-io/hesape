package eloquent

import (
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Relation answers Illuminate\Database\Eloquent\Relations\Relation, narrowed to
// what the builder and the eager loader ask of it.
//
// It is declared here and not imported from eloquent/relations for the reason
// query.Connection is declared in query: in Go an interface belongs with its
// consumer, and relations imports this package for Builder and Model, so naming
// the type there would close the cycle.
//
// A relation is registered on the model by name, in RelationResolvers, because
// Go cannot look up a method by name and stay type safe. That map is what
// $model->posts() is in PHP.
type Relation interface {
	// GetRelationExistenceQuery answers Relation::getRelationExistenceQuery: the
	// correlated subquery over the related table, selecting the given expression
	// and constrained to the parent row.
	//
	// It is what Has, WhereHas and WithCount compile into an exists() or a
	// scalar subselect.
	GetRelationExistenceQuery(parent *query.Builder, columns any) *query.Builder

	// Match answers Relation::getEager and Relation::match at once.
	//
	// PHP runs the related query, then walks the parents assigning the rows that
	// belong to each. The assignment target there is a dynamic property, which Go
	// does not have, so the two halves are one call: it returns, per parent key,
	// the value the builder sets with SetRelation.
	//
	// The keys are the parent keys the query found, so they are whatever the key
	// column holds -- an int64 or a string in practice, and comparable either way.
	//
	// constraints is what the caller passed to WithConstraints, which
	// eagerLoadRelation applies to the relation before it runs. It is nil when
	// there were none.
	Match(g auth.Grant, keys []any, constraints func(*query.Builder)) (map[any]any, error)
}

// BelongsToRelation is the part of a relation that WhereBelongsTo needs: the
// column on this table, and the column on the other one it points at.
type BelongsToRelation interface {
	Relation

	// GetQualifiedForeignKeyName answers BelongsTo::getQualifiedForeignKeyName.
	GetQualifiedForeignKeyName() string

	// GetOwnerKeyName answers BelongsTo::getOwnerKeyName.
	GetOwnerKeyName() string
}

// MorphRelation is the polymorphic relation WhereMorphRelation walks.
type MorphRelation interface {
	Relation

	// GetMorphType answers MorphTo::getMorphType: the column holding the class
	// of the related row.
	GetMorphType() string

	// RelationForMorphType answers Builder::getBelongsToRelation, moved onto the
	// relation: PHP builds the concrete relation for a type by instantiating the
	// class named in the morph column, and Go cannot make a type from a string.
	// The relation knows its own map of type to model, so it answers instead.
	RelationForMorphType(morphType string) (Relation, error)
}

// GetRelationWithoutConstraints answers
// QueriesRelationships::getRelationWithoutConstraints.
//
// The PHP calls the model's relation method with the global relation constraints
// switched off; here the resolver is a function the model registered, and it is
// called with no constraints because there are none to switch off.
func (b *Builder[T]) GetRelationWithoutConstraints(name string) (Relation, error) {
	resolver, ok := b.model.RelationResolvers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s on %s", ErrRelationNotFound, name, b.model.GetTable())
	}
	return resolver(b.model), nil
}
