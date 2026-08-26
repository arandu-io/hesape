package factories

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// belongsTo is the part of a belongs-to relation that
// BelongsToRelationship.AttributesFor reaches.
type belongsTo interface {
	GetForeignKeyName() string
	GetOwnerKeyName() string
}

// morphTo is belongsTo plus the column holding the owner's type. MorphTo
// satisfies belongsTo too, so the type switch asks for this one first.
type morphTo interface {
	belongsTo
	GetMorphType() string
}

// BelongsToRelationship is a parent relationship, whose key is a column on the
// model the factory makes.
//
// The parent is created once, lazily, and its key reused for every child of the
// run: a factory for three users with one team makes one team and three users,
// not three teams.
type BelongsToRelationship struct {
	// factory is the parent's factory. PHP allows a Model here as well, and so
	// does For: model holds it when the caller passed one already created.
	factory *Factory
	model   Model

	relationship string

	// relation answers PHP's $model->{$this->relationship}(), for the reason
	// Relationship.relation does.
	relation func(child Model) any

	// resolved is the cached parent key. PHP keeps it on the instance and the
	// instance is shared across the run.
	resolved   any
	isResolved bool
}

// NewBelongsToRelationship answers BelongsToRelationship::__construct.
//
// parent is PHP's Factory|Model union: a *Factory to create the owner, or a
// Model already created.
func NewBelongsToRelationship(parent any, relationship string, relation func(child Model) any) *BelongsToRelationship {
	r := &BelongsToRelationship{relationship: relationship, relation: relation}

	switch typed := parent.(type) {
	case *Factory:
		r.factory = typed
	case Model:
		r.model = typed
	}

	return r
}

// AttributesFor answers BelongsToRelationship::attributesFor: the columns the
// child carries to point at its owner.
//
// The foreign key is an Attribute rather than a value, because the owner is not
// created until the child's attributes are expanded -- and a parent created for
// a child that is never made is a row nobody asked for.
func (r *BelongsToRelationship) AttributesFor(model Model) map[string]any {
	relation := r.relation(model)

	switch typed := relation.(type) {
	case morphTo:
		return map[string]any{
			typed.GetMorphType():      r.ownerName(),
			typed.GetForeignKeyName(): r.resolver(typed.GetOwnerKeyName()),
		}
	case belongsTo:
		return map[string]any{
			typed.GetForeignKeyName(): r.resolver(typed.GetOwnerKeyName()),
		}
	default:
		return map[string]any{}
	}
}

// ownerName answers the getMorphClass half of attributesFor: the name the owner
// is stored under in the type column.
func (r *BelongsToRelationship) ownerName() string {
	if r.factory != nil {
		return r.factory.NewModel(nil).GetMorphClass()
	}
	if r.model != nil {
		return r.model.GetMorphClass()
	}
	return ""
}

// resolver answers BelongsToRelationship::resolver: the deferred owner key,
// resolved at most once.
func (r *BelongsToRelationship) resolver(key string) Attribute {
	return func(ctx context.Context, g auth.Grant, _ map[string]any) (any, error) {
		if r.isResolved {
			return r.resolved, nil
		}

		instance := r.model

		if instance == nil {
			if r.factory == nil {
				return nil, fmt.Errorf("factories: the %q relationship has neither a factory nor a model to point at", r.relationship)
			}

			instance = r.factory.GetRandomRecycledModel(r.factory.ModelName())

			if instance == nil {
				created, err := r.factory.Create(ctx, g, nil, nil)
				if err != nil {
					return nil, err
				}
				instance = first(created)
			}
		}

		if instance == nil {
			return nil, fmt.Errorf("factories: the %q relationship created no owner", r.relationship)
		}

		if key != "" {
			r.resolved = instance.GetAttribute(key)
		} else {
			r.resolved = instance.GetKey()
		}
		r.isResolved = true

		return r.resolved, nil
	}
}

// Recycle answers BelongsToRelationship::recycle.
//
// It mutates the receiver and answers it, which is what the PHP does, and here
// it is load-bearing rather than incidental: parentResolvers calls it once per
// model in the run, and a copy would lose the resolved key and create one owner
// per child instead of one for all of them.
func (r *BelongsToRelationship) Recycle(recycle map[string][]Model) *BelongsToRelationship {
	if r.factory != nil {
		r.factory = r.factory.Recycle(flatten(recycle)...)
	}
	return r
}
