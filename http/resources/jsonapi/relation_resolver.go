package jsonapi

// RelationResolver mirrors Illuminate\Http\Resources\JsonApi\RelationResolver.
//
// It resolves a named relation on a resource. In the PHP version, the
// default resolver calls $resource->getRelation($name); in Go, callers
// supply a resolver function.
type RelationResolver struct {
	RelationName          string
	Resolver              func(resource any) any
	RelationResourceClass string
}

// NewRelationResolver creates a RelationResolver. If resolver is nil,
// the default resolver attempts to call GetRelation on the resource
// if it implements the RelationGetter interface.
func NewRelationResolver(relationName string, resolver func(any) any) *RelationResolver {
	return &RelationResolver{
		RelationName: relationName,
		Resolver:     resolver,
	}
}

// Handle resolves the relation on the given resource.
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

// RelationGetter is an interface that resources can implement to
// provide relation access.
type RelationGetter interface {
	GetRelation(name string) any
}
