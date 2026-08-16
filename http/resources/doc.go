// Package resources mirrors Illuminate\Http\Resources.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Resources:
//
//	CollectsResources.php            -> resource.go
//	ConditionallyLoadsAttributes.php -> conditional.go
//	DelegatesToResource.php          -> resource.go
//	MergeValue.php                   -> conditional.go
//	MissingValue.php                 -> conditional.go
//	PotentiallyMissing.php           -> conditional.go
//	Json/JsonResource.php            -> resource.go, json_resource.go
//	Json/ResourceCollection.php      -> resource.go, json_resource.go
//	Json/ResourceResponse.php        -> json_resource.go
//
// Sub-packages:
//
//	resources/json    — the Json namespace's own types
//	resources/jsonapi — JsonApiResource, JsonApiRequest
//
// [JsonResource] is an interface that any type can implement, and [Resource] is
// the concrete one that answers to the class an application extends in PHP.
// ConditionallyLoadsAttributes is a set of helper functions for building JSON
// responses with conditional fields — when a value is missing, it is omitted
// from the output rather than appearing as null.
//
// # Not mirrored, and why (ADR 0056)
//
//	JsonResource::jsonOptions        is a bitmask of json_encode flags --
//	CollectsResources::jsonOptions   JSON_PRETTY_PRINT, JSON_UNESCAPED_SLASHES,
//	                                 JSON_THROW_ON_ERROR. encoding/json has no
//	                                 flag word: escaping is fixed, errors are
//	                                 returned rather than thrown, and the one
//	                                 flag that changes the output is
//	                                 Resource.ToPrettyJson.
//
//	DelegatesToResource::offsetExists   are PHP's ArrayAccess, the interface
//	DelegatesToResource::offsetGet      behind $resource['name']. Go has no
//	DelegatesToResource::offsetSet      operator to overload; Resource.ToArray
//	DelegatesToResource::offsetUnset    hands back the map to index.
package resources
