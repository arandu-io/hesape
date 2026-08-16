package factories

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// BelongsToManyRelationship is the models a factory attaches to the one it made,
// with the pivot row's own columns.
type BelongsToManyRelationship struct {
	// factory and models are PHP's Factory|Collection|Model|array union: either
	// the related models are created here, or they were created already and are
	// only attached.
	factory *Factory
	models  []Model

	// pivot is PHP's callable|array: the extra columns of the pivot row, or a
	// function of the parent that answers them.
	pivot         map[string]any
	pivotResolver func(parent Model) map[string]any

	relationship string

	// relation answers PHP's $model->{$this->relationship}(), for the reason
	// Relationship.relation does.
	relation func(parent Model) any
}

// NewBelongsToManyRelationship answers
// BelongsToManyRelationship::__construct.
//
// related is the Factory|Collection|Model|array union: a *Factory, a Model or a
// []Model. pivot is the callable|array one: a map[string]any or a
// func(Model) map[string]any.
func NewBelongsToManyRelationship(related any, pivot any, relationship string, relation func(parent Model) any) *BelongsToManyRelationship {
	r := &BelongsToManyRelationship{relationship: relationship, relation: relation}

	switch typed := related.(type) {
	case *Factory:
		r.factory = typed
	case []Model:
		r.models = typed
	case Model:
		r.models = []Model{typed}
	}

	switch typed := pivot.(type) {
	case map[string]any:
		r.pivot = typed
	case func(Model) map[string]any:
		r.pivotResolver = typed
	}

	return r
}

// CreateFor answers BelongsToManyRelationship::createFor: the related models,
// created if a factory was given, and attached to the parent one by one.
func (r *BelongsToManyRelationship) CreateFor(ctx context.Context, g auth.Grant, parent Model) error {
	if r.relation == nil {
		return fmt.Errorf("factories: the %q relationship has no relation function, and Go cannot find one by name", r.relationship)
	}

	relation, ok := r.relation(parent).(belongsToMany)
	if !ok {
		return fmt.Errorf("factories: the %q relationship is not a belongs-to-many, and only a belongs-to-many can be attached", r.relationship)
	}

	attachable := r.models

	if r.factory != nil {
		created, err := r.factory.Create(ctx, g, nil, parent)
		if err != nil {
			return err
		}
		attachable = created
	}

	attributes := r.pivot
	if r.pivotResolver != nil {
		attributes = r.pivotResolver(parent)
	}

	for _, model := range attachable {
		if err := relation.Attach(ctx, g, model.GetKey(), attributes); err != nil {
			return err
		}
	}

	return nil
}

// Recycle answers BelongsToManyRelationship::recycle. It mutates the receiver
// and answers it, as the PHP does.
func (r *BelongsToManyRelationship) Recycle(recycle map[string][]Model) ChildRelationship {
	if r.factory != nil {
		r.factory = r.factory.Recycle(flatten(recycle)...)
	}
	return r
}
