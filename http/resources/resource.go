package resources

import (
	"encoding/json"
	"errors"
	"iter"
	"sync"

	hhttp "github.com/arandu-io/hesape/http"
)

// DefaultWrap is the key the outermost resource map is nested under, by
// default.
const DefaultWrap = "data"

// wrapState holds the wrap key and force-wrapping flag every resource in
// the process shares. A mutex guards them since Go runs tests -- and
// requests -- with a race detector watching.
var wrapState = struct {
	sync.RWMutex
	wrap          string
	forceWrapping bool
}{wrap: DefaultWrap}

// Wrap sets the string that wraps the outermost resource map.
func Wrap(value string) {
	wrapState.Lock()
	wrapState.wrap = value
	wrapState.Unlock()
}

// WithoutWrapping sends the resource map at the top level, with no wrapper
// key.
//
// It changes every resource in the process. [JsonResponseBuilder.WithoutWrapping]
// is the same choice for one response.
func WithoutWrapping() {
	Wrap("")
}

// ForceWrapping sets whether to wrap even when the resource data already
// carries the wrapper key.
//
// Package-level state needs an exported function to reach it from outside
// the package, since an unexported field on a package variable is otherwise
// unreachable.
func ForceWrapping(force bool) {
	wrapState.Lock()
	wrapState.forceWrapping = force
	wrapState.Unlock()
}

// Wrapper is the key in force, empty when wrapping is off.
func Wrapper() string {
	wrapState.RLock()
	defer wrapState.RUnlock()
	return wrapState.wrap
}

// FlushState puts the wrapper back to "data" and turns forced wrapping off.
func FlushState() {
	wrapState.Lock()
	wrapState.wrap = DefaultWrap
	wrapState.forceWrapping = false
	wrapState.Unlock()
}

// Arrayable is what a wrapped value implements when it knows how to present
// itself as a map. [Resource.ToArray] looks for it before falling back to
// encoding/json, so a model that implements it decides exactly which fields
// reach the response instead of having them read off its struct tags.
type Arrayable interface {
	// ToArray is the map representation.
	ToArray() map[string]any
}

// UrlRoutable is what a wrapped value implements when a URL segment can
// identify it: it reports the key that does the identifying and the name that
// key goes by in a route.
//
// [Resource.GetRouteKey] and [Resource.GetRouteKeyName] forward to the wrapped
// value when it implements this, and answer empty when it does not. A resource
// is never routable itself -- [Resource.ResolveRouteBinding] always fails with
// [ErrNotRouteBindable] -- so what a resource can do is repeat the identity of
// the model it dresses, and no more.
type UrlRoutable interface {
	// GetRouteKey is the value that identifies the resource in a route.
	GetRouteKey() any
	// GetRouteKeyName is the name GetRouteKey's value goes by in a route.
	GetRouteKeyName() string
}

// ErrNotRouteBindable is returned by ResolveRouteBinding and
// ResolveChildRouteBinding: a resource is a view of a model, not a thing the
// router can look up.
var ErrNotRouteBindable = errors.New("http/resources: resources may not be implicitly resolved from route bindings")

// Resource is one model dressed for one response.
//
// An application either embeds Resource and shadows ToArray, or implements
// the [JsonResource] interface on its own type. Either way it goes into
// [NewJsonResponse].
type Resource struct {
	// Resource is the thing being dressed.
	Resource any

	// WithData is what goes alongside the data key.
	WithData map[string]any

	// AdditionalData is what the caller added on the way out.
	AdditionalData map[string]any
}

// Make is a Resource around the given value.
func Make(resource any) *Resource {
	return &Resource{Resource: resource}
}

// Collection is an anonymous collection of the resources, which is what a
// controller returns for an index action.
func Collection(resources []JsonResource) *AnonymousResourceCollection {
	return NewAnonymousResourceCollection(resources, "")
}

// ToArray is the resource as a map.
//
// A wrapped value that is a map or an [Arrayable] is taken as is; anything
// else goes through encoding/json, which is the same set of fields the
// response would have carried anyway.
func (r *Resource) ToArray() map[string]any {
	switch value := r.Resource.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return value
	case Arrayable:
		return value.ToArray()
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return map[string]any{}
		}
		var out map[string]any
		if err := json.Unmarshal(encoded, &out); err != nil {
			return map[string]any{}
		}
		return out
	}
}

// With is the data that goes alongside the resource.
func (r *Resource) With() map[string]any {
	return r.WithData
}

// Additional sets metadata to add to the response.
func (r *Resource) Additional(data map[string]any) *Resource {
	r.AdditionalData = data
	return r
}

// ToAttributes is the resource's attributes.
//
// A Go type that wants a shortcut shadows ToArray directly; this is the
// fallback, and calls ToArray.
func (r *Resource) ToAttributes() map[string]any {
	return r.ToArray()
}

// ResolveResourceData is an alias for ToAttributes.
func (r *Resource) ResolveResourceData() map[string]any {
	return r.ToAttributes()
}

// Resolve is the resource data with every missing value filtered out.
func (r *Resource) Resolve() map[string]any {
	return Filter(r.ResolveResourceData())
}

// JsonSerialize is an alias for Resolve.
func (r *Resource) JsonSerialize() map[string]any {
	return r.Resolve()
}

// ToJson is the JSON encoding of Resolve.
func (r *Resource) ToJson() ([]byte, error) {
	return json.Marshal(r.JsonSerialize())
}

// ToPrettyJson is the indented JSON encoding of Resolve.
func (r *Resource) ToPrettyJson() ([]byte, error) {
	return json.MarshalIndent(r.JsonSerialize(), "", "    ")
}

// Response is an alias for ToResponse.
func (r *Resource) Response() (*hhttp.JsonResponse, error) {
	return r.ToResponse()
}

// ToResponse builds the resource into a *hhttp.JsonResponse, calling
// WithResponse on the way out.
func (r *Resource) ToResponse() (*hhttp.JsonResponse, error) {
	body, err := NewJsonResponse(r).Build()
	if err != nil {
		return nil, err
	}
	response, err := hhttp.FromJsonString(string(body))
	if err != nil {
		return nil, err
	}
	r.WithResponse(response)
	return response, nil
}

// WithResponse is the hook a resource overrides to touch the response on
// its way out.
//
// It does nothing here. A type embedding Resource shadows it, and calls the
// response methods it wants.
func (r *Resource) WithResponse(response *hhttp.JsonResponse) {}

// GetRouteKey forwards to the wrapped value when it is routable, nil when
// it is not.
func (r *Resource) GetRouteKey() any {
	if routable, ok := r.Resource.(UrlRoutable); ok {
		return routable.GetRouteKey()
	}
	return nil
}

// GetRouteKeyName forwards to the wrapped value when it is routable, empty
// when it is not.
func (r *Resource) GetRouteKeyName() string {
	if routable, ok := r.Resource.(UrlRoutable); ok {
		return routable.GetRouteKeyName()
	}
	return ""
}

// ResolveRouteBinding always returns [ErrNotRouteBindable].
func (r *Resource) ResolveRouteBinding(value any, field string) (any, error) {
	return nil, ErrNotRouteBindable
}

// ResolveChildRouteBinding always returns [ErrNotRouteBindable].
func (r *Resource) ResolveChildRouteBinding(childType string, value any, field string) (any, error) {
	return nil, ErrNotRouteBindable
}

// GetIterator is the resources in the collection, in order.
func (c *ResourceCollection) GetIterator() iter.Seq[JsonResource] {
	return func(yield func(JsonResource) bool) {
		for _, resource := range c.Resources {
			if !yield(resource) {
				return
			}
		}
	}
}

// PreserveQuery keeps every query string parameter of the current request
// on the pagination links.
func (c *ResourceCollection) PreserveQuery() *ResourceCollection {
	c.preserveAllQueryParameters = true
	c.queryParameters = nil
	return c
}

// WithQuery sets the query string parameters that should be on the
// pagination links, and only those.
func (c *ResourceCollection) WithQuery(query map[string]string) *ResourceCollection {
	c.preserveAllQueryParameters = false
	c.queryParameters = query
	return c
}

// QueryParameters reports what [ResourceCollection.PreserveQuery] and
// [ResourceCollection.WithQuery] left behind: the parameters to put on the
// pagination links, and whether every parameter of the current request goes on
// them instead.
//
// It exists because PaginatedResourceResponse needs to read this state from
// outside the package, and exporting the fields directly would be a second
// way to set what the two methods above set.
func (c *ResourceCollection) QueryParameters() (map[string]string, bool) {
	return c.queryParameters, c.preserveAllQueryParameters
}
