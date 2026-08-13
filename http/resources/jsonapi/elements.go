package jsonapi

import (
	"sync"
)

// DefaultMaxRelationshipDepth is ResolvesJsonApiElements::$maxRelationshipDepth
// in its initial state: how far the resolver walks into relationships before it
// stops.
const DefaultMaxRelationshipDepth = 3

// The class statics of JsonApiResource and the trait it composes:
// $jsonApiInformation and $maxRelationshipDepth. A mutex guards them because
// Go runs tests with -race and PHP has no concurrency to speak of.
var jsonApiState = struct {
	sync.RWMutex
	information         map[string]any
	maxRelationshipDept int
}{maxRelationshipDept: DefaultMaxRelationshipDepth}

// Configure is JsonApiResource::configure: the "jsonapi" member every response
// from this process carries -- the specification version the API implements,
// and the extensions, profiles and meta that go with it.
//
// The PHP drops the empty entries with array_filter; so does this, so that an
// unconfigured member does not appear as an empty object in the body.
func Configure(version string, ext []string, profile []string, meta map[string]any) {
	information := map[string]any{}
	if version != "" {
		information["version"] = version
	}
	if len(ext) > 0 {
		information["ext"] = ext
	}
	if len(profile) > 0 {
		information["profile"] = profile
	}
	if len(meta) > 0 {
		information["meta"] = meta
	}

	jsonApiState.Lock()
	jsonApiState.information = information
	jsonApiState.Unlock()
}

// JsonApiInformation is JsonApiResource::$jsonApiInformation: what [Configure]
// was told, empty until it is called.
func JsonApiInformation() map[string]any {
	jsonApiState.RLock()
	defer jsonApiState.RUnlock()
	out := make(map[string]any, len(jsonApiState.information))
	for key, value := range jsonApiState.information {
		out[key] = value
	}
	return out
}

// MaxRelationshipDepth is ResolvesJsonApiElements::maxRelationshipDepth: how
// far into nested relationships the resolver walks.
//
// The PHP clamps a negative depth to zero, and so does this: a negative depth
// is a caller asking for nothing, not a caller asking for everything.
func MaxRelationshipDepth(depth int) {
	if depth < 0 {
		depth = 0
	}
	jsonApiState.Lock()
	jsonApiState.maxRelationshipDept = depth
	jsonApiState.Unlock()
}

// CurrentMaxRelationshipDepth reports the depth in force.
//
// The PHP reads the static property directly, which Go cannot do across
// packages without exporting a variable that anything could write. This is the
// read half of [MaxRelationshipDepth].
func CurrentMaxRelationshipDepth() int {
	jsonApiState.RLock()
	defer jsonApiState.RUnlock()
	return jsonApiState.maxRelationshipDept
}

// FlushState is JsonApiResource::flushState: forget the configuration and put
// the relationship depth back to its default.
func FlushState() {
	jsonApiState.Lock()
	jsonApiState.information = nil
	jsonApiState.maxRelationshipDept = DefaultMaxRelationshipDepth
	jsonApiState.Unlock()
}

// ToId is JsonApiResource::toId: the resource object's id.
//
// The PHP returns null and expects the subclass to override; Go has no
// override, so a resource built with [NewJsonApiResource] carries the id it was
// given and this reads it.
func (r *JsonApiResource) ToId() string { return r.ID }

// ToType is JsonApiResource::toType: the resource object's type.
func (r *JsonApiResource) ToType() string { return r.Type }

// ToAttributes is JsonApiResource::toAttributes.
func (r *JsonApiResource) ToAttributes() map[string]any { return r.Attributes }

// ToRelationships is JsonApiResource::toRelationships.
func (r *JsonApiResource) ToRelationships() map[string]any { return r.Relationships }

// ToLinks is JsonApiResource::toLinks.
func (r *JsonApiResource) ToLinks() map[string]any { return r.Links }

// ToMeta is JsonApiResource::toMeta.
func (r *JsonApiResource) ToMeta() map[string]any { return r.Meta }

// ResolveResourceIdentifier is ResolvesJsonApiElements::resolveResourceIdentifier:
// the id the resource object goes out with.
//
// The PHP falls back to the model's key when toId returns null and throws when
// there is no key either; there is no model here, so an empty id is the
// [AttemptingToDetermineIdFor] error.
func (r *JsonApiResource) ResolveResourceIdentifier(request *JsonApiRequest) (string, error) {
	if id := r.ToId(); id != "" {
		return id, nil
	}
	return "", AttemptingToDetermineIdFor(r)
}

// ResolveResourceType is ResolvesJsonApiElements::resolveResourceType: the type
// the resource object goes out with.
//
// The PHP falls back to the class basename, then to the model's morph alias,
// and throws when neither is there. Go has no class name to read at runtime
// that would mean anything to an API consumer, so an empty type is the
// [AttemptingToDetermineTypeFor] error.
func (r *JsonApiResource) ResolveResourceType(request *JsonApiRequest) (string, error) {
	if resourceType := r.ToType(); resourceType != "" {
		return resourceType, nil
	}
	return "", AttemptingToDetermineTypeFor(r)
}

// ResolveIncludedResourceObjects is
// ResolvesJsonApiElements::resolveIncludedResourceObjects: the resource objects
// that go in the "included" member, one per unique type and id.
//
// The PHP walks the loaded Eloquent relations, tracking visited objects in a
// WeakMap so that a circular chaperone does not loop forever, and stops at
// [CurrentMaxRelationshipDepth]. There are no Eloquent relations here, so this
// walks the relationship identifiers already on the resource and deduplicates
// them the same way.
func (r *JsonApiResource) ResolveIncludedResourceObjects(request *JsonApiRequest) []map[string]any {
	included := make([]map[string]any, 0)
	seen := map[string]bool{}

	for _, relationship := range r.Relationships {
		relation, ok := relationship.(map[string]any)
		if !ok {
			continue
		}
		data, ok := relation["data"]
		if !ok {
			continue
		}
		switch value := data.(type) {
		case map[string]any:
			included = appendUniqueResourceObject(included, seen, value)
		case []map[string]any:
			for _, item := range value {
				included = appendUniqueResourceObject(included, seen, item)
			}
		case []any:
			for _, item := range value {
				if object, ok := item.(map[string]any); ok {
					included = appendUniqueResourceObject(included, seen, object)
				}
			}
		}
	}

	return included
}

// appendUniqueResourceObject is the uniqueStrict('_uniqueKey') step of the PHP:
// a type and an id name a resource object once, however many relationships
// point at it.
func appendUniqueResourceObject(included []map[string]any, seen map[string]bool, object map[string]any) []map[string]any {
	key, _ := object["type"].(string)
	if id, ok := object["id"].(string); ok {
		key += "\x00" + id
	}
	if seen[key] {
		return included
	}
	seen[key] = true
	return append(included, object)
}

// RespectFieldsAndIncludesInQueryString is
// ResolvesJsonApiElements::respectFieldsAndIncludesInQueryString: read the
// sparse fieldsets and the includes off the request's query string.
//
// It is on by default, as it is in the PHP.
func (r *JsonApiResource) RespectFieldsAndIncludesInQueryString(value bool) *JsonApiResource {
	r.usesRequestQueryString = value
	return r
}

// IgnoreFieldsAndIncludesInQueryString is
// ResolvesJsonApiElements::ignoreFieldsAndIncludesInQueryString: build the
// resource object from what the resource declares, whatever the query string
// asked for.
func (r *JsonApiResource) IgnoreFieldsAndIncludesInQueryString() *JsonApiResource {
	return r.RespectFieldsAndIncludesInQueryString(false)
}

// UsesRequestQueryString reports which of the two above is in force.
func (r *JsonApiResource) UsesRequestQueryString() bool {
	return r.usesRequestQueryString
}

// IncludePreviouslyLoadedRelationships is
// ResolvesJsonApiElements::includePreviouslyLoadedRelationships: put the
// relationships that were already loaded into "included", not only the ones the
// request asked for.
func (r *JsonApiResource) IncludePreviouslyLoadedRelationships() *JsonApiResource {
	r.includesPreviouslyLoadedRelationships = true
	return r
}

// IncludesPreviouslyLoadedRelationships reports what
// [JsonApiResource.IncludePreviouslyLoadedRelationships] left behind.
func (r *JsonApiResource) IncludesPreviouslyLoadedRelationships() bool {
	return r.includesPreviouslyLoadedRelationships
}
