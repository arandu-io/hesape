// Package resources mirrors Illuminate\Http\Resources.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Resources:
//
//	CollectsResources.php
//	ConditionallyLoadsAttributes.php
//	DelegatesToResource.php
//	MergeValue.php
//	MissingValue.php
//	PotentiallyMissing.php
//
// Sub-packages:
//
//	resources/json — JsonResource, ResourceCollection, ResourceResponse
//	resources/jsonapi — JsonApiResource, JsonApiRequest
//
// In Go, JsonResource is an interface that any type can implement.
// ConditionallyLoadsAttributes is a set of helper functions for building
// JSON responses with conditional fields — when a value is missing,
// it is omitted from the output rather than appearing as null.
package resources
