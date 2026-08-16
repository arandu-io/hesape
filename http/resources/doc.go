// Package resources transforms data into the shapes a JSON response takes:
// conditional fields, wrapping, pagination metadata and resource
// collections.
//
// Sub-packages:
//
//	resources/json    -- the JSON response building blocks
//	resources/jsonapi -- JsonApiResource, JsonApiRequest
//
// [JsonResource] is an interface that any type can implement, and [Resource]
// is the concrete one most applications embed. A set of helper functions --
// When, Unless, WhenNotNull, MergeWhen and the rest -- build JSON responses
// with conditional fields: when a value is missing, it is omitted from the
// output rather than appearing as null.
package resources
