package concerns

import (
	"fmt"
	"sort"

	"github.com/arandu-io/hesape/database/eloquent/casts"
)

// HasAttributes answers Illuminate\Database\Eloquent\Concerns\HasAttributes:
// the attribute bag, what it looked like when it was loaded, and everything
// that follows from comparing the two.
//
// A model embeds it and gets getAttribute, setAttribute, the dirty tracking and
// the change log. What it does not get here is casting: the cast registry is
// eloquent/casts, and a cast that lived in two places would be two answers to
// what a column means.
type HasAttributes struct {
	attributes map[string]any
	original   map[string]any
	changes    map[string]any
	previous   map[string]any
	appends    []string

	// casts answers the PHP's $casts property: what each column is declared to
	// be. A value is either a string -- the built-in cast name, "datetime",
	// "decimal:2" -- or a casts.CastsAttributes. The PHP holds a class-string
	// there and resolves it with class_exists; Go cannot reach a type from a
	// name in a string, so the value itself is held (ADR 0056, motive 1).
	casts map[string]any

	// mutators is the registry the PHP builds by reflection: a method whose
	// return type is Attribute is that attribute's accessor and mutator. Go
	// has no return-type reflection over methods, so the model registers the
	// Attribute under its key and every has*Mutator lookup reads this.
	mutators map[string]*casts.Attribute

	// mutated is what getMutatedAttributes answers, and mutatedCached is the
	// PHP's static $mutatorCache: computed once, rebuilt by
	// CacheMutatedAttributes.
	mutated       []string
	mutatedCached bool

	// dateFormat answers the PHP's $dateFormat property.
	dateFormat string
}

// InitializeHasAttributes answers HasAttributes::initializeHasAttributes.
//
// Go's zero value for a map is nil, and writing to a nil map panics, so the
// bags are made on first use rather than in a constructor the embedding model
// might not call.
func (h *HasAttributes) InitializeHasAttributes() {
	if h.attributes == nil {
		h.attributes = map[string]any{}
	}
	if h.original == nil {
		h.original = map[string]any{}
	}
	if h.changes == nil {
		h.changes = map[string]any{}
	}
	if h.previous == nil {
		h.previous = map[string]any{}
	}
}

// HasAttribute answers HasAttributes::hasAttribute.
func (h *HasAttributes) HasAttribute(key string) bool {
	h.InitializeHasAttributes()
	_, ok := h.attributes[key]
	return ok
}

// GetAttribute answers HasAttributes::getAttribute.
func (h *HasAttributes) GetAttribute(key string) any {
	h.InitializeHasAttributes()
	return h.attributes[key]
}

// GetAttributeValue answers HasAttributes::getAttributeValue.
func (h *HasAttributes) GetAttributeValue(key string) any { return h.GetAttribute(key) }

// SetAttribute answers HasAttributes::setAttribute.
func (h *HasAttributes) SetAttribute(key string, value any) {
	h.InitializeHasAttributes()
	h.attributes[key] = value
}

// UnsetAttribute answers unset($model->$key).
func (h *HasAttributes) UnsetAttribute(key string) {
	h.InitializeHasAttributes()
	delete(h.attributes, key)
}

// GetAttributes answers HasAttributes::getAttributes.
func (h *HasAttributes) GetAttributes() map[string]any {
	h.InitializeHasAttributes()
	return h.attributes
}

// SetRawAttributes answers HasAttributes::setRawAttributes: the whole bag at
// once, as hydration does after a select.
func (h *HasAttributes) SetRawAttributes(attributes map[string]any, sync bool) {
	h.attributes = copyBag(attributes)
	h.InitializeHasAttributes()
	if sync {
		h.SyncOriginal()
	}
}

// GetOriginal answers HasAttributes::getOriginal for one key.
func (h *HasAttributes) GetOriginal(key string, def ...any) any {
	h.InitializeHasAttributes()
	if value, ok := h.original[key]; ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

// GetRawOriginal answers HasAttributes::getRawOriginal: the whole loaded bag.
func (h *HasAttributes) GetRawOriginal() map[string]any {
	h.InitializeHasAttributes()
	return h.original
}

// SyncOriginal answers HasAttributes::syncOriginal.
func (h *HasAttributes) SyncOriginal() {
	h.InitializeHasAttributes()
	h.original = copyBag(h.attributes)
}

// SyncOriginalAttribute answers HasAttributes::syncOriginalAttribute.
func (h *HasAttributes) SyncOriginalAttribute(attribute string) {
	h.SyncOriginalAttributes(attribute)
}

// SyncOriginalAttributes answers HasAttributes::syncOriginalAttributes.
func (h *HasAttributes) SyncOriginalAttributes(attributes ...string) {
	h.InitializeHasAttributes()
	for _, attribute := range attributes {
		h.original[attribute] = h.attributes[attribute]
	}
}

// SyncChanges answers HasAttributes::syncChanges.
//
// It is called after a save, and it is what lets an "updated" listener ask what
// changed: the dirty set becomes the change set, and the values that were
// replaced become the previous set.
func (h *HasAttributes) SyncChanges() {
	h.InitializeHasAttributes()

	h.changes = h.GetDirty()
	h.previous = map[string]any{}
	for key := range h.changes {
		h.previous[key] = h.original[key]
	}
}

// IsDirty answers HasAttributes::isDirty.
func (h *HasAttributes) IsDirty(attributes ...string) bool {
	dirty := h.GetDirty()
	if len(attributes) == 0 {
		return len(dirty) > 0
	}
	for _, attribute := range attributes {
		if _, ok := dirty[attribute]; ok {
			return true
		}
	}
	return false
}

// IsClean answers HasAttributes::isClean.
func (h *HasAttributes) IsClean(attributes ...string) bool { return !h.IsDirty(attributes...) }

// GetDirty answers HasAttributes::getDirty: the attributes whose value is not
// the one that was loaded.
func (h *HasAttributes) GetDirty() map[string]any {
	h.InitializeHasAttributes()

	dirty := map[string]any{}
	for key, value := range h.attributes {
		if !h.OriginalIsEquivalent(key) {
			dirty[key] = value
		}
	}
	return dirty
}

// OriginalIsEquivalent answers HasAttributes::originalIsEquivalent.
//
// The PHP compares loosely for numbers and strictly otherwise. Here the two
// values are rendered and compared, which is the same answer for the types a
// column can hold and the only one available for `any`.
func (h *HasAttributes) OriginalIsEquivalent(key string) bool {
	h.InitializeHasAttributes()

	original, hadOriginal := h.original[key]
	current, hasCurrent := h.attributes[key]

	if !hadOriginal {
		return !hasCurrent
	}
	if original == nil || current == nil {
		return original == nil && current == nil
	}
	return fmt.Sprint(original) == fmt.Sprint(current)
}

// GetChanges answers HasAttributes::getChanges.
func (h *HasAttributes) GetChanges() map[string]any {
	h.InitializeHasAttributes()
	return h.changes
}

// GetPrevious answers HasAttributes::getPrevious.
func (h *HasAttributes) GetPrevious() map[string]any {
	h.InitializeHasAttributes()
	return h.previous
}

// WasChanged answers HasAttributes::wasChanged.
func (h *HasAttributes) WasChanged(attributes ...string) bool {
	h.InitializeHasAttributes()

	if len(attributes) == 0 {
		return len(h.changes) > 0
	}
	for _, attribute := range attributes {
		if _, ok := h.changes[attribute]; ok {
			return true
		}
	}
	return false
}

// DiscardChanges answers HasAttributes::discardChanges: back to what was
// loaded.
func (h *HasAttributes) DiscardChanges() {
	h.InitializeHasAttributes()
	h.attributes = copyBag(h.original)
	h.changes = map[string]any{}
	h.previous = map[string]any{}
}

// Only answers HasAttributes::only.
func (h *HasAttributes) Only(attributes ...string) map[string]any {
	h.InitializeHasAttributes()

	out := map[string]any{}
	for _, attribute := range attributes {
		if value, ok := h.attributes[attribute]; ok {
			out[attribute] = value
		}
	}
	return out
}

// Except answers HasAttributes::except.
func (h *HasAttributes) Except(attributes ...string) map[string]any {
	h.InitializeHasAttributes()

	excluded := make(map[string]struct{}, len(attributes))
	for _, attribute := range attributes {
		excluded[attribute] = struct{}{}
	}

	out := map[string]any{}
	for key, value := range h.attributes {
		if _, ok := excluded[key]; ok {
			continue
		}
		out[key] = value
	}
	return out
}

// Append answers HasAttributes::append.
func (h *HasAttributes) Append(attributes ...string) { h.appends = append(h.appends, attributes...) }

// GetAppends answers HasAttributes::getAppends.
func (h *HasAttributes) GetAppends() []string { return h.appends }

// SetAppends answers HasAttributes::setAppends.
func (h *HasAttributes) SetAppends(appends []string) { h.appends = appends }

// MergeAppends answers HasAttributes::mergeAppends.
func (h *HasAttributes) MergeAppends(appends []string) { h.appends = append(h.appends, appends...) }

// HasAppended answers HasAttributes::hasAppended.
func (h *HasAttributes) HasAppended(attribute string) bool {
	for _, appended := range h.appends {
		if appended == attribute {
			return true
		}
	}
	return false
}

// WithoutAppends answers HasAttributes::withoutAppends.
func (h *HasAttributes) WithoutAppends() { h.appends = nil }

// AttributesToArray answers HasAttributes::attributesToArray, filtered by the
// hidden and visible lists the model carries.
//
// The keys come back sorted, which the PHP does not do because a PHP array
// remembers its insertion order. A Go map does not, and a serializer that
// emitted the columns in a different order on every request would make every
// diff of a response body useless.
func (h *HasAttributes) AttributesToArray(hides *HidesAttributes) map[string]any {
	h.InitializeHasAttributes()

	out := map[string]any{}
	for key, value := range h.attributes {
		if hides != nil && !hides.IsVisible(key) {
			continue
		}
		out[key] = value
	}
	return out
}

// SortedAttributeNames returns the attribute names in a stable order, for
// anything that has to emit them one after another.
func (h *HasAttributes) SortedAttributeNames() []string {
	h.InitializeHasAttributes()

	keys := make([]string, 0, len(h.attributes))
	for key := range h.attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyBag(bag map[string]any) map[string]any {
	out := make(map[string]any, len(bag))
	for key, value := range bag {
		out[key] = value
	}
	return out
}
