package model

import (
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Relation is what the builder and the eager loader ask of a relation.
//
// It is declared here and not imported from model/relations for the reason
// query.Connection is declared in query: in Go an interface belongs with its
// consumer, and relations imports this package for Builder and Model, so naming
// the type there would close the cycle.
//
// A relation is registered on the model by name, in RelationResolvers, because
// Go cannot look up a method by name and stay type safe.
type Relation interface {
	// GetRelationExistenceQuery returns the correlated subquery over the
	// related table, selecting the given expression and constrained to the
	// parent row.
	//
	// It is what Has, WhereHas and WithCount compile into an exists() or a
	// scalar subselect.
	GetRelationExistenceQuery(parent *query.Builder, columns any) *query.Builder

	// Match runs the related query and returns, per parent key, the value
	// the builder sets with SetRelation.
	//
	// Go has no dynamic property to assign into, so running the query and
	// assigning the results are one call rather than two.
	//
	// The keys are the parent keys the query found, so they are whatever
	// the key column holds -- an int64 or a string in practice, and
	// comparable either way.
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

	// GetQualifiedForeignKeyName returns the foreign key column on this
	// table, qualified with the table name.
	GetQualifiedForeignKeyName() string

	// GetOwnerKeyName returns the column on the related table the foreign
	// key points at.
	GetOwnerKeyName() string
}

// MorphRelation is the polymorphic relation WhereMorphRelation walks.
type MorphRelation interface {
	Relation

	// GetMorphType returns the column holding the type of the related row.
	GetMorphType() string

	// RelationForMorphType returns the concrete relation for one of the
	// types a polymorphic relation can point at.
	//
	// Go cannot make a type from a string, so the relation resolves its
	// own map of type to model instead of a caller building one
	// generically.
	RelationForMorphType(morphType string) (Relation, error)
}

// GetRelationWithoutConstraints resolves relation by calling its registered
// resolver, with no constraints applied.
func (b *Builder[T]) GetRelationWithoutConstraints(name string) (Relation, error) {
	resolver, ok := b.model.RelationResolvers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s on %s", ErrRelationNotFound, name, b.model.GetTable())
	}
	return resolver(b.model), nil
}
