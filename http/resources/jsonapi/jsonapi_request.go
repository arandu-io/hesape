package jsonapi

import (
	"net/http"
	"strings"
)

// JsonApiRequest mirrors Illuminate\Http\Resources\JsonApi\JsonApiRequest.
//
// It parses JSON:API-specific query parameters: sparse fieldsets
// (fields[type]=field1,field2) and included resources (include=relation).
type JsonApiRequest struct {
	Request  *http.Request
	fields   map[string][]string
	includes []string
	parsed   bool
}

// NewJsonApiRequest creates a JsonApiRequest from an *http.Request.
func NewJsonApiRequest(req *http.Request) *JsonApiRequest {
	return &JsonApiRequest{Request: req}
}

// SparseFields returns the sparse fieldset for the given resource type.
// It parses the fields[type]=... query parameter.
func (r *JsonApiRequest) SparseFields(resourceType string) []string {
	r.parse()
	return r.fields[resourceType]
}

// SparseIncluded returns the included resource types from the
// include=... query parameter. If key is non-empty, only includes
// matching that key are returned.
func (r *JsonApiRequest) SparseIncluded(key string) []string {
	r.parse()
	if key == "" {
		return r.includes
	}
	var filtered []string
	for _, inc := range r.includes {
		if strings.HasPrefix(inc, key+".") || inc == key {
			filtered = append(filtered, inc)
		}
	}
	return filtered
}

func (r *JsonApiRequest) parse() {
	if r.parsed || r.Request == nil {
		return
	}
	r.parsed = true
	r.fields = make(map[string][]string)
	r.includes = []string{}

	query := r.Request.URL.Query()
	for key, values := range query {
		if strings.HasPrefix(key, "fields[") && strings.HasSuffix(key, "]") {
			resourceType := key[7 : len(key)-1]
			parts := strings.Split(values[0], ",")
			var trimmed []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					trimmed = append(trimmed, p)
				}
			}
			r.fields[resourceType] = trimmed
		}
		if key == "include" {
			for _, v := range values {
				parts := strings.Split(v, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						r.includes = append(r.includes, p)
					}
				}
			}
		}
	}
}
