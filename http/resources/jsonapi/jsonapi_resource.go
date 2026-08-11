package jsonapi

import (
	"encoding/json"
	"net/http"
)

// JsonApiResource mirrors Illuminate\Http\Resources\JsonApi\JsonApiResource.
//
// It extends the JsonResource concept for JSON:API compliance,
// adding type, id, relationships, links, and meta to the response
// structure.
type JsonApiResource struct {
	ID            string
	Type          string
	Attributes    map[string]any
	Relationships map[string]any
	Links         map[string]any
	Meta          map[string]any
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

// ToResponse returns the data for a JSON:API response.
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

// ResourceIdentificationError mirrors
// Illuminate\Http\Resources\JsonApi\Exceptions\ResourceIdentificationException.
type ResourceIdentificationError struct {
	Message string
}

func (e *ResourceIdentificationError) Error() string {
	return "jsonapi resource identification error: " + e.Message
}

// AttemptingToDetermineID creates an error for resources without a
// recognizable ID.
func AttemptingToDetermineID(resource any) *ResourceIdentificationError {
	return &ResourceIdentificationError{
		Message: "attempting to determine ID for resource of type " + typeName(resource),
	}
}

// AttemptingToDetermineType creates an error for resources without a
// recognizable type.
func AttemptingToDetermineType(resource any) *ResourceIdentificationError {
	return &ResourceIdentificationError{
		Message: "attempting to determine type for resource of type " + typeName(resource),
	}
}

func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return "resource"
}
