// Package jsonapi mirrors Illuminate\Http\Resources\JsonApi.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Resources/JsonApi:
//
//	AnonymousResourceCollection.php            -> jsonapi_resource.go
//	JsonApiRequest.php                         -> jsonapi_request.go
//	JsonApiResource.php                        -> jsonapi_resource.go, elements.go
//	RelationResolver.php                       -> relation_resolver.go
//	Concerns/ResolvesJsonApiElements.php       -> elements.go
//	Exceptions/ResourceIdentificationException.php -> jsonapi_resource.go
//
// JSON:API is a specification for building APIs in JSON. This package
// provides the resource types and helpers for building JSON:API-compliant
// responses, including sparse fieldsets, included resources, and
// relationship resolution.
//
// # The identification methods answer from the resource, not from a model
//
// The PHP resolves an id and a type from the Eloquent model behind the
// resource: the primary key, the morph alias, the class basename. There is no
// Eloquent here and no class name that would mean anything to an API consumer,
// so [NewJsonApiResource] takes both, [JsonApiResource.ResolveResourceIdentifier]
// and [JsonApiResource.ResolveResourceType] read them back, and an empty one is
// [AttemptingToDetermineIdFor] or [AttemptingToDetermineTypeFor] -- the same two
// errors, raised at the same point.
//
// # Not mirrored, and why (ADR 0056)
//
//	JsonApiResource::wrap                the PHP overrides both to throw,
//	JsonApiResource::withoutWrapping     because JSON:API fixes the wrapper at
//	                                     "data". Go has no override to make
//	                                     throw: resources.Wrap and
//	                                     resources.WithoutWrapping are in the
//	                                     parent package and nothing here calls
//	                                     them, so the wrapper is already fixed.
package jsonapi
