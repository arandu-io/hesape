package resources

import (
	"encoding/json"
	"net/http"
)

// JsonResource is the interface that resource types implement.
//
// It mirrors Illuminate\Http\Resources\Json\JsonResource. Types that
// implement this interface can be serialized to JSON responses with
// conditional field inclusion and wrapping.
type JsonResource interface {
	// ToArray returns the resource attributes as a slice of key-value pairs
	// or a map. MissingValue entries are filtered out during serialization.
	ToArray() map[string]any

	// With returns additional data to include at the top level of the
	// response alongside the data key.
	With() map[string]any
}

// JsonResponseBuilder builds a JSON response from a resource.
type JsonResponseBuilder struct {
	resource   JsonResource
	status     int
	additional map[string]any
	wrap       string
}

// NewJsonResponse creates a JsonResponseBuilder for the given resource.
func NewJsonResponse(resource JsonResource) *JsonResponseBuilder {
	return &JsonResponseBuilder{
		resource:   resource,
		status:     http.StatusOK,
		additional: make(map[string]any),
		// The wrapper the class statics are on, so that [Wrap] and
		// [WithoutWrapping] reach a response nobody passed them to -- which is
		// the point of them being statics in the PHP.
		wrap: Wrapper(),
	}
}

// Status sets the HTTP status code.
func (b *JsonResponseBuilder) Status(code int) *JsonResponseBuilder {
	b.status = code
	return b
}

// Additional adds top-level metadata.
func (b *JsonResponseBuilder) Additional(data map[string]any) *JsonResponseBuilder {
	for k, v := range data {
		b.additional[k] = v
	}
	return b
}

// WithoutWrapping removes the data wrapper, returning the resource data
// directly at the top level.
func (b *JsonResponseBuilder) WithoutWrapping() *JsonResponseBuilder {
	b.wrap = ""
	return b
}

// WithWrap sets a custom wrapper key.
func (b *JsonResponseBuilder) WithWrap(key string) *JsonResponseBuilder {
	b.wrap = key
	return b
}

// Build builds the JSON byte response.
func (b *JsonResponseBuilder) Build() ([]byte, error) {
	data := Filter(b.resource.ToArray())

	with := b.resource.With()

	result := make(map[string]any)

	if b.wrap != "" {
		result[b.wrap] = data
	} else {
		for k, v := range data {
			result[k] = v
		}
	}

	for k, v := range b.additional {
		result[k] = v
	}
	for k, v := range with {
		result[k] = v
	}

	return json.Marshal(result)
}

// ToHTTPResponse builds an *http.Response (useful for testing).
func (b *JsonResponseBuilder) ToHTTPResponse() *http.Response {
	body, _ := b.Build()
	return &http.Response{
		StatusCode: b.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Status:     http.StatusText(b.status),
		Body:       newByteReadCloser(body),
	}
}

// ResourceCollection represents a collection of JsonResources.
type ResourceCollection struct {
	Resources    []JsonResource
	PreserveKeys bool
	collects     string

	// preserveAllQueryParameters is ResourceCollection::$preserveAllQueryParameters.
	preserveAllQueryParameters bool
	// queryParameters is ResourceCollection::$queryParameters.
	queryParameters map[string]string
}

// NewResourceCollection creates a ResourceCollection.
func NewResourceCollection(resources []JsonResource) *ResourceCollection {
	return &ResourceCollection{Resources: resources}
}

// Collects returns the name of the resource class being collected.
func (c *ResourceCollection) Collects() string {
	return c.collects
}

// ToArray implements JsonResource.
func (c *ResourceCollection) ToArray() map[string]any {
	items := make([]map[string]any, len(c.Resources))
	for i, r := range c.Resources {
		items[i] = Filter(r.ToArray())
	}
	return map[string]any{"data": items}
}

// With implements JsonResource.
func (c *ResourceCollection) With() map[string]any {
	return nil
}

// Count returns the number of resources in the collection.
func (c *ResourceCollection) Count() int {
	return len(c.Resources)
}

// AnonymousResourceCollection is a [ResourceCollection] that also records the
// name of the resource type it was built from, and that is all it adds.
//
// It exists for the ordinary index action: a handler returning a list of
// resources has no collection type of its own to name, but something still has
// to carry the wrapping and the pagination metadata for the list. [Collection]
// returns one of these, and a caller who wants the collection named declares
// their own type instead.
//
// Answers Illuminate\Http\Resources\Json\AnonymousResourceCollection.
type AnonymousResourceCollection struct {
	ResourceCollection
}

// NewAnonymousResourceCollection creates an AnonymousResourceCollection.
func NewAnonymousResourceCollection(resources []JsonResource, collects string) *AnonymousResourceCollection {
	return &AnonymousResourceCollection{
		ResourceCollection: ResourceCollection{
			Resources: resources,
			collects:  collects,
		},
	}
}

// PreserveKeys sets whether array keys should be preserved.
func (c *AnonymousResourceCollection) PreserveKeys(preserve bool) *AnonymousResourceCollection {
	c.ResourceCollection.PreserveKeys = preserve
	return c
}

// CollectsResources is a helper to wrap a single resource or a slice.
func CollectsResources(resource any) JsonResource {
	switch v := resource.(type) {
	case JsonResource:
		return v
	case []JsonResource:
		return NewResourceCollection(v)
	default:
		return nil
	}
}

// PaginatedResourceResponse wraps a collection with pagination metadata.
type PaginatedResourceResponse struct {
	Collection *ResourceCollection
	Page       int
	PerPage    int
	Total      int
	LastPage   int
}

// NewPaginatedResourceResponse creates a PaginatedResourceResponse.
func NewPaginatedResourceResponse(collection *ResourceCollection, page, perPage, total int) *PaginatedResourceResponse {
	lastPage := total / perPage
	if total%perPage > 0 {
		lastPage++
	}
	return &PaginatedResourceResponse{
		Collection: collection,
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		LastPage:   lastPage,
	}
}

// ToArray returns the paginated data with links and meta.
func (p *PaginatedResourceResponse) ToArray() map[string]any {
	data := Filter(p.Collection.ToArray())
	return map[string]any{
		"data": data["data"],
		"links": map[string]any{
			"first": nil,
			"last":  nil,
			"prev":  nil,
			"next":  nil,
		},
		"meta": map[string]any{
			"current_page": p.Page,
			"from":         (p.Page-1)*p.PerPage + 1,
			"last_page":    p.LastPage,
			"per_page":     p.PerPage,
			"to":           min(p.Page*p.PerPage, p.Total),
			"total":        p.Total,
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// byteReadCloser wraps a byte slice as io.ReadCloser.
type byteReadCloser struct {
	data   []byte
	offset int
	closed bool
}

func newByteReadCloser(data []byte) *byteReadCloser {
	return &byteReadCloser{data: data}
}

func (b *byteReadCloser) Read(p []byte) (int, error) {
	if b.closed || b.offset >= len(b.data) {
		return 0, nil
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *byteReadCloser) Close() error {
	b.closed = true
	return nil
}
