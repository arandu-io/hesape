package jsonapi

import (
	"sync"
)

// DefaultMaxRelationshipDepth is how far the resolver walks into
// relationships before it stops, by default.
const DefaultMaxRelationshipDepth = 3

// jsonApiState holds the "jsonapi" member information and the
// relationship-walk depth every resource in the process shares. A mutex
// guards them since Go runs tests -- and requests -- with a race detector
// watching.
var jsonApiState = struct {
	sync.RWMutex
	information         map[string]any
	maxRelationshipDept int
}{maxRelationshipDept: DefaultMaxRelationshipDepth}

// Configure sets the "jsonapi" member every response from this process
// carries -- the specification version the API implements, and the
// extensions, profiles and meta that go with it.
//
// Empty entries are dropped, so that an unconfigured member does not
// appear as an empty object in the body.
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

// JsonApiInformation is what [Configure] was told, empty until it is
// called.
func JsonApiInformation() map[string]any {
	jsonApiState.RLock()
	defer jsonApiState.RUnlock()
	out := make(map[string]any, len(jsonApiState.information))
	for key, value := range jsonApiState.information {
		out[key] = value
	}
	return out
}

// MaxRelationshipDepth sets how far into nested relationships the resolver
// walks.
//
// A negative depth is clamped to zero: it is a caller asking for nothing,
// not a caller asking for everything.
func MaxRelationshipDepth(depth int) {
	if depth < 0 {
		depth = 0
	}
	jsonApiState.Lock()
	jsonApiState.maxRelationshipDept = depth
	jsonApiState.Unlock()
}

// CurrentMaxRelationshipDepth reports the depth in force. It is the read
// half of [MaxRelationshipDepth].
func CurrentMaxRelationshipDepth() int {
	jsonApiState.RLock()
	defer jsonApiState.RUnlock()
	return jsonApiState.maxRelationshipDept
}

// FlushState forgets the configuration and puts the relationship depth back
// to its default.
func FlushState() {
	jsonApiState.Lock()
	jsonApiState.information = nil
	jsonApiState.maxRelationshipDept = DefaultMaxRelationshipDepth
	jsonApiState.Unlock()
}

// ToId is the resource object's id: whatever [NewJsonApiResource] was
// given.
func (r *JsonApiResource) ToId() string { return r.ID }

// ToType is the resource object's type: whatever [NewJsonApiResource] was
// given.
func (r *JsonApiResource) ToType() string { return r.Type }

// ToAttributes is the resource object's attributes member.
func (r *JsonApiResource) ToAttributes() map[string]any { return r.Attributes }

// ToRelationships is the resource object's relationships member.
func (r *JsonApiResource) ToRelationships() map[string]any { return r.Relationships }

// ToLinks is the resource object's links member.
func (r *JsonApiResource) ToLinks() map[string]any { return r.Links }

// ToMeta is the resource object's meta member.
func (r *JsonApiResource) ToMeta() map[string]any { return r.Meta }

// ResolveResourceIdentifier is the id the resource object goes out with.
//
// An empty id -- ToId returned nothing to fall back on -- is the
// [AttemptingToDetermineIdFor] error.
func (r *JsonApiResource) ResolveResourceIdentifier(request *JsonApiRequest) (string, error) {
	if id := r.ToId(); id != "" {
		return id, nil
	}
	return "", AttemptingToDetermineIdFor(r)
}

// ResolveResourceType is the type the resource object goes out with.
//
// There is no class name to fall back on that would mean anything to an
// API consumer, so an empty type -- ToType returned nothing -- is the
// [AttemptingToDetermineTypeFor] error.
func (r *JsonApiResource) ResolveResourceType(request *JsonApiRequest) (string, error) {
	if resourceType := r.ToType(); resourceType != "" {
		return resourceType, nil
	}
	return "", AttemptingToDetermineTypeFor(r)
}

// ResolveIncludedResourceObjects is the resource objects that go in the
// "included" member, one per unique type and id.
//
// It walks the relationship identifiers already on the resource and
// deduplicates them by type and id, so a circular or repeated reference
// does not appear twice.
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

// appendUniqueResourceObject appends object unless its type and id
// combination has already been seen: a type and an id name a resource
// object once, however many relationships point at it.
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

// RespectFieldsAndIncludesInQueryString sets whether to read the sparse
// fieldsets and the includes off the request's query string. It is on by
// default.
func (r *JsonApiResource) RespectFieldsAndIncludesInQueryString(value bool) *JsonApiResource {
	r.usesRequestQueryString = value
	return r
}

// IgnoreFieldsAndIncludesInQueryString builds the resource object from what
// the resource declares, whatever the query string asked for.
func (r *JsonApiResource) IgnoreFieldsAndIncludesInQueryString() *JsonApiResource {
	return r.RespectFieldsAndIncludesInQueryString(false)
}

// UsesRequestQueryString reports which of the two above is in force.
func (r *JsonApiResource) UsesRequestQueryString() bool {
	return r.usesRequestQueryString
}

// IncludePreviouslyLoadedRelationships puts the relationships that were
// already loaded into "included", not only the ones the request asked for.
func (r *JsonApiResource) IncludePreviouslyLoadedRelationships() *JsonApiResource {
	r.includesPreviouslyLoadedRelationships = true
	return r
}

// IncludesPreviouslyLoadedRelationships reports what
// [JsonApiResource.IncludePreviouslyLoadedRelationships] left behind.
func (r *JsonApiResource) IncludesPreviouslyLoadedRelationships() bool {
	return r.includesPreviouslyLoadedRelationships
}
