package resources

import (
	"encoding/json"
	"errors"
	"iter"
	"sync"

	hhttp "github.com/arandu-io/hesape/http"
)

// DefaultWrap is JsonResource::$wrap in its initial state: the key the
// outermost resource array is nested under.
const DefaultWrap = "data"

// wrapState holds JsonResource::$wrap and JsonResource::$forceWrapping, the
// two statics the class carries. A mutex guards them because Go runs tests
// with -race and PHP has no concurrency to speak of.
var wrapState = struct {
	sync.RWMutex
	wrap          string
	forceWrapping bool
}{wrap: DefaultWrap}

// Wrap is JsonResource::wrap: set the string that wraps the outermost
// resource array.
func Wrap(value string) {
	wrapState.Lock()
	wrapState.wrap = value
	wrapState.Unlock()
}

// WithoutWrapping is JsonResource::withoutWrapping: send the resource array at
// the top level, with no wrapper key.
//
// It is the static, and it changes every resource in the process.
// [JsonResponseBuilder.WithoutWrapping] is the same choice for one response.
func WithoutWrapping() {
	Wrap("")
}

// ForceWrapping is JsonResource::$forceWrapping: wrap even when the resource
// data already carries the wrapper key.
//
// The PHP writes the property directly, having no setter for it; a Go field on
// a package-level variable is not reachable from outside, so this is the
// setter it would have had.
func ForceWrapping(force bool) {
	wrapState.Lock()
	wrapState.forceWrapping = force
	wrapState.Unlock()
}

// Wrapper is ResourceResponse::wrapper: the key in force, empty when wrapping
// is off.
func Wrapper() string {
	wrapState.RLock()
	defer wrapState.RUnlock()
	return wrapState.wrap
}

// FlushState is JsonResource::flushState: put the wrapper back to "data" and
// turn forced wrapping off.
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
//
// Answers Illuminate\Contracts\Support\Arrayable.
type Arrayable interface {
	// ToArray answers to Arrayable::toArray.
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
//
// Answers Illuminate\Contracts\Routing\UrlRoutable.
type UrlRoutable interface {
	// GetRouteKey answers to UrlRoutable::getRouteKey.
	GetRouteKey() any
	// GetRouteKeyName answers to UrlRoutable::getRouteKeyName.
	GetRouteKeyName() string
}

// ErrNotRouteBindable is what DelegatesToResource::resolveRouteBinding and
// ::resolveChildRouteBinding throw: a resource is a view of a model, not a
// thing the router can look up.
var ErrNotRouteBindable = errors.New("http/resources: resources may not be implicitly resolved from route bindings")

// Resource is Illuminate\Http\Resources\Json\JsonResource: one model dressed
// for one response.
//
// The PHP is a class an application extends, overriding toArray. Go has no
// inheritance, so the application either embeds Resource and shadows ToArray,
// or implements the [JsonResource] interface on its own type. Either way it
// goes into [NewJsonResponse].
type Resource struct {
	// Resource is JsonResource::$resource: the thing being dressed.
	Resource any

	// WithData is JsonResource::$with: what goes alongside the data key.
	WithData map[string]any

	// AdditionalData is JsonResource::$additional: what the caller added on
	// the way out.
	AdditionalData map[string]any
}

// Make is JsonResource::make: a resource around the given value.
func Make(resource any) *Resource {
	return &Resource{Resource: resource}
}

// Collection is JsonResource::collection: an anonymous collection of the
// resources, which is what a controller returns for an index action.
func Collection(resources []JsonResource) *AnonymousResourceCollection {
	return NewAnonymousResourceCollection(resources, "")
}

// ToArray is JsonResource::toArray: the resource as a map.
//
// The PHP calls toArray on the wrapped model, or returns the array it was
// given. Go has no such universal call, so a wrapped value that is a map or an
// [Arrayable] is taken as is and anything else goes through encoding/json,
// which is the same set of fields the response would have carried anyway.
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

// With is JsonResource::with: the data that goes alongside the resource.
func (r *Resource) With() map[string]any {
	return r.WithData
}

// Additional is JsonResource::additional: metadata to add to the response.
func (r *Resource) Additional(data map[string]any) *Resource {
	r.AdditionalData = data
	return r
}

// ToAttributes is JsonResource::toAttributes.
//
// The PHP returns the $attributes property when the class declares one, and
// falls back to toArray. A Go type that wants the shortcut shadows ToArray,
// so this is the fallback and nothing else.
func (r *Resource) ToAttributes() map[string]any {
	return r.ToArray()
}

// ResolveResourceData is JsonResource::resolveResourceData.
func (r *Resource) ResolveResourceData() map[string]any {
	return r.ToAttributes()
}

// Resolve is JsonResource::resolve: the resource data with every missing
// value filtered out.
//
// The PHP takes the request, defaulting to the one in the container; ADR 0001
// rejected the container, and nothing in the resolution reads the request, so
// it takes none.
func (r *Resource) Resolve() map[string]any {
	return Filter(r.ResolveResourceData())
}

// JsonSerialize is JsonResource::jsonSerialize.
func (r *Resource) JsonSerialize() map[string]any {
	return r.Resolve()
}

// ToJson is JsonResource::toJson.
func (r *Resource) ToJson() ([]byte, error) {
	return json.Marshal(r.JsonSerialize())
}

// ToPrettyJson is JsonResource::toPrettyJson.
func (r *Resource) ToPrettyJson() ([]byte, error) {
	return json.MarshalIndent(r.JsonSerialize(), "", "    ")
}

// Response is JsonResource::response: the resource as a JSON response.
func (r *Resource) Response() (*hhttp.JsonResponse, error) {
	return r.ToResponse()
}

// ToResponse is JsonResource::toResponse.
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

// WithResponse is JsonResource::withResponse: the hook a resource overrides to
// touch the response on its way out.
//
// It does nothing here, as it does in the PHP. A type embedding Resource
// shadows it, and calls the response methods it wants.
func (r *Resource) WithResponse(response *hhttp.JsonResponse) {}

// GetRouteKey is DelegatesToResource::getRouteKey: forwarded to the wrapped
// value when it is routable, nil when it is not.
func (r *Resource) GetRouteKey() any {
	if routable, ok := r.Resource.(UrlRoutable); ok {
		return routable.GetRouteKey()
	}
	return nil
}

// GetRouteKeyName is DelegatesToResource::getRouteKeyName.
func (r *Resource) GetRouteKeyName() string {
	if routable, ok := r.Resource.(UrlRoutable); ok {
		return routable.GetRouteKeyName()
	}
	return ""
}

// ResolveRouteBinding is DelegatesToResource::resolveRouteBinding: always
// [ErrNotRouteBindable], as the PHP always throws.
func (r *Resource) ResolveRouteBinding(value any, field string) (any, error) {
	return nil, ErrNotRouteBindable
}

// ResolveChildRouteBinding is DelegatesToResource::resolveChildRouteBinding:
// always [ErrNotRouteBindable], as the PHP always throws.
func (r *Resource) ResolveChildRouteBinding(childType string, value any, field string) (any, error) {
	return nil, ErrNotRouteBindable
}

// GetIterator is CollectsResources::getIterator: the resources in the
// collection, in order.
func (c *ResourceCollection) GetIterator() iter.Seq[JsonResource] {
	return func(yield func(JsonResource) bool) {
		for _, resource := range c.Resources {
			if !yield(resource) {
				return
			}
		}
	}
}

// PreserveQuery is ResourceCollection::preserveQuery: keep every query string
// parameter of the current request on the pagination links.
func (c *ResourceCollection) PreserveQuery() *ResourceCollection {
	c.preserveAllQueryParameters = true
	c.queryParameters = nil
	return c
}

// WithQuery is ResourceCollection::withQuery: the query string parameters that
// should be on the pagination links, and only those.
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
// The PHP reads $preserveAllQueryParameters and $queryParameters straight off
// the object, from PaginatedResourceResponse. Go has no such reach across
// types without exporting the fields, and exporting them would be a second way
// to set what the two methods above set.
func (c *ResourceCollection) QueryParameters() (map[string]string, bool) {
	return c.queryParameters, c.preserveAllQueryParameters
}
