package jsonapi

// RelationResolver resolves a named relation on a resource. Callers supply
// a resolver function.
type RelationResolver struct {
	RelationName          string
	Resolver              func(resource any) any
	RelationResourceClass string
}

// NewRelationResolver creates a RelationResolver from a name and a resolver
// function.
func NewRelationResolver(relationName string, resolver func(any) any) *RelationResolver {
	return &RelationResolver{
		RelationName: relationName,
		Resolver:     resolver,
	}
}

// Handle resolves the relation on the given resource. Returns nil when no
// resolver function was given.
func (r *RelationResolver) Handle(resource any) any {
	if r.Resolver == nil {
		return nil
	}
	return r.Resolver(resource)
}

// ResourceClass returns the resource class name configured for this
// relation.
func (r *RelationResolver) ResourceClass() string {
	return r.RelationResourceClass
}

// RelationGetter is an interface a resource can implement to expose a named
// relation. It is not yet consulted by [RelationResolver.Handle].
type RelationGetter interface {
	GetRelation(name string) any
}
