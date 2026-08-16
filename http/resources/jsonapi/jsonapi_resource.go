package jsonapi

import (
	"encoding/json"
	"net/http"
	"reflect"
)

// JsonApiResource is a resource object as JSON:API defines it: type, id,
// attributes, relationships, links, and meta.
type JsonApiResource struct {
	ID            string
	Type          string
	Attributes    map[string]any
	Relationships map[string]any
	Links         map[string]any
	Meta          map[string]any

	// usesRequestQueryString is set by RespectFieldsAndIncludesInQueryString.
	usesRequestQueryString bool
	// includesPreviouslyLoadedRelationships is set by
	// IncludePreviouslyLoadedRelationships.
	includesPreviouslyLoadedRelationships bool
}

// NewJsonApiResource creates a JsonApiResource.
func NewJsonApiResource(id, resourceType string) *JsonApiResource {
	return &JsonApiResource{
		ID:            id,
		Type:          resourceType,
		Attributes:    make(map[string]any),
		Relationships: make(map[string]any),
		Links:         make(map[string]any),
		Meta:          make(map[string]any),

		// The default: the query string drives the sparse fieldsets and the
		// includes until IgnoreFieldsAndIncludesInQueryString says
		// otherwise.
		usesRequestQueryString: true,
	}
}

// ToArray returns the JSON:API resource object.
func (r *JsonApiResource) ToArray() map[string]any {
	result := map[string]any{
		"type": r.Type,
		"id":   r.ID,
	}

	if len(r.Attributes) > 0 {
		result["attributes"] = r.Attributes
	}
	if len(r.Relationships) > 0 {
		result["relationships"] = r.Relationships
	}
	if len(r.Links) > 0 {
		result["links"] = r.Links
	}
	if len(r.Meta) > 0 {
		result["meta"] = r.Meta
	}

	return result
}

// ToResponse returns the data for a JSON:API response, wrapped under
// "data". JSON:API fixes the wrapper, so this always wraps regardless of
// the parent resources package's Wrap state.
func (r *JsonApiResource) ToResponse() map[string]any {
	return map[string]any{"data": r.ToArray()}
}

// BuildHTTPResponse builds an *http.Response with the JSON:API
// content type (application/vnd.api+json).
func (r *JsonApiResource) BuildHTTPResponse(status int) (*http.Response, error) {
	data := r.ToResponse()
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/vnd.api+json"},
		},
		ContentLength: int64(len(body)),
		Status:        http.StatusText(status),
	}, nil
}

// AnonymousJsonApiResourceCollection is a collection of JsonApiResources.
type AnonymousJsonApiResourceCollection struct {
	Resources []*JsonApiResource
}

// NewAnonymousJsonApiResourceCollection creates a collection.
func NewAnonymousJsonApiResourceCollection(resources []*JsonApiResource) *AnonymousJsonApiResourceCollection {
	return &AnonymousJsonApiResourceCollection{Resources: resources}
}

// ToResponse returns the collection as a JSON:API response with
// included resources.
func (c *AnonymousJsonApiResourceCollection) ToResponse() map[string]any {
	data := make([]map[string]any, len(c.Resources))
	included := make([]map[string]any, 0)

	for i, r := range c.Resources {
		item := r.ToArray()
		data[i] = item

		for _, rel := range r.Relationships {
			if relData, ok := rel.(map[string]any); ok {
				if relDataItems, ok := relData["data"]; ok {
					switch v := relDataItems.(type) {
					case map[string]any:
						included = append(included, v)
					case []map[string]any:
						included = append(included, v...)
					}
				}
			}
		}
	}

	result := map[string]any{
		"data": data,
	}
	if len(included) > 0 {
		result["included"] = included
	}
	return result
}

// ResourceIdentificationError is returned when a resource object cannot be
// given the id or the type that JSON:API requires of every one of them: the
// value being serialized carries neither, and there is nothing to derive one
// from. The document cannot be built at all, so there is no partial answer to
// send.
//
// Build it through [AttemptingToDetermineIdFor] or
// [AttemptingToDetermineTypeFor], which name which of the two is missing and
// on what Go type -- the fix is on the resource, not at the call site.
type ResourceIdentificationError struct {
	Message string
}

func (e *ResourceIdentificationError) Error() string {
	return "jsonapi resource identification error: " + e.Message
}

// AttemptingToDetermineIdFor builds a [ResourceIdentificationError]: the
// resource object has no id and nothing to derive one from.
func AttemptingToDetermineIdFor(resource any) *ResourceIdentificationError {
	return &ResourceIdentificationError{
		Message: "unable to resolve resource object ID for [" + typeName(resource) + "]",
	}
}

// AttemptingToDetermineTypeFor builds a [ResourceIdentificationError]: the
// resource object has no type and nothing to derive one from.
func AttemptingToDetermineTypeFor(resource any) *ResourceIdentificationError {
	return &ResourceIdentificationError{
		Message: "unable to resolve resource object type for [" + typeName(resource) + "]",
	}
}

// typeName is the Go type name of v, or "nil".
func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}
