package factories

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// hasOneOrMany is the part of Illuminate\Database\Eloquent\Relations\HasOneOrMany
// that Relationship::createFor reaches.
//
// PHP tests the relation with instanceof and then calls two methods on it. Go
// has no instanceof over a class hierarchy, so the two methods are the test:
// the type switch in CreateFor asks for this set rather than for a class.
type hasOneOrMany interface {
	GetForeignKeyName() string
	GetParentKey() any
}

// morphOneOrMany is hasOneOrMany plus the two columns a polymorphic child
// carries. MorphOneOrMany satisfies hasOneOrMany as well, so the type switch
// asks for this one first.
type morphOneOrMany interface {
	hasOneOrMany
	GetMorphType() string
	GetMorphClass() string
}

// belongsToMany is the part of BelongsToMany that createFor reaches.
type belongsToMany interface {
	Attach(ctx context.Context, g auth.Grant, ids any, attributes map[string]any, touch ...bool) error
}

// Relationship answers
// Illuminate\Database\Eloquent\Factories\Relationship: a child relationship a
// factory creates once the parent has been saved.
type Relationship struct {
	factory      *Factory
	relationship string

	// relation answers PHP's $parent->{$this->relationship}().
	//
	// Go cannot look up a method by name, so the relation arrives as a function
	// the caller supplies -- the same answer eloquent gives with
	// RelationResolvers, and the same one relations gives with the morph map.
	relation func(parent Model) any
}

// NewRelationship answers Relationship::__construct.
func NewRelationship(factory *Factory, relationship string, relation func(parent Model) any) *Relationship {
	return &Relationship{factory: factory, relationship: relationship, relation: relation}
}

// CreateFor answers Relationship::createFor: the child rows, created with the
// parent's key already on them.
//
// A has-one or has-many gets the foreign key as a prepended state, a morph
// relation gets the type as well, and a many-to-many is created first and
// attached after. That is the PHP's three branches, in the PHP's order.
func (r *Relationship) CreateFor(ctx context.Context, g auth.Grant, parent Model) error {
	if r.relation == nil {
		return fmt.Errorf("factories: the %q relationship has no relation function, and Go cannot find one by name", r.relationship)
	}

	relation := r.relation(parent)

	switch typed := relation.(type) {
	case morphOneOrMany:
		_, err := r.factory.State(map[string]any{
			typed.GetMorphType():      typed.GetMorphClass(),
			typed.GetForeignKeyName(): typed.GetParentKey(),
		}).Create(ctx, g, nil, parent)
		return err

	case hasOneOrMany:
		_, err := r.factory.State(map[string]any{
			typed.GetForeignKeyName(): typed.GetParentKey(),
		}).Create(ctx, g, nil, parent)
		return err

	case belongsToMany:
		created, err := r.factory.Create(ctx, g, nil, parent)
		if err != nil {
			return err
		}
		for _, model := range created {
			if err := typed.Attach(ctx, g, model.GetKey(), nil); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("factories: the %q relationship is a %T, and a factory can only create for a has-one, a has-many, a morph-one, a morph-many or a belongs-to-many", r.relationship, relation)
	}
}

// Recycle answers Relationship::recycle. It mutates the receiver and answers
// it, as the PHP does.
func (r *Relationship) Recycle(recycle map[string][]Model) ChildRelationship {
	if r.factory != nil {
		r.factory = r.factory.Recycle(flatten(recycle)...)
	}
	return r
}
