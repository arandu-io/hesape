// Package jsonapi provides the resource types and helpers for building
// JSON:API-compliant responses, including sparse fieldsets, included
// resources, and relationship resolution.
//
// # The identification methods answer from the resource, not from a model
//
// There is no ORM model behind a resource here, and no class name that
// would mean anything to an API consumer, so [NewJsonApiResource] takes
// both an id and a type. [JsonApiResource.ResolveResourceIdentifier] and
// [JsonApiResource.ResolveResourceType] read them back, and an empty one is
// [AttemptingToDetermineIdFor] or [AttemptingToDetermineTypeFor] -- the same
// two errors, raised at the same point.
package jsonapi
