package concerns

import (
	"fmt"
	"sort"

	"github.com/arandu-io/hesape/database/eloquent/casts"
)

// HasAttributes is the attribute bag, what it looked like when it was loaded,
// and everything that follows from comparing the two.
//
// A model embeds it and gets GetAttribute, SetAttribute, the dirty tracking
// and the change log. What it does not get here is casting: the cast
// registry is eloquent/casts, and a cast that lived in two places would be
// two sources of truth for what a column means.
type HasAttributes struct {
	attributes map[string]any
	original   map[string]any
	changes    map[string]any
	previous   map[string]any
	appends    []string

	// casts is what each column is declared to be. A value is either a string
	// -- the built-in cast name, "datetime", "decimal:2" -- or a
	// casts.CastsAttributes. Go cannot reach a type from a name in a string,
	// so the value itself is held.
	casts map[string]any

	// mutators is the registry of accessors and mutators, keyed by the
	// attribute name. Go has no return-type reflection over methods, so the
	// model registers the Attribute under its key instead of it being
	// discovered from a method's return type, and every has*Mutator lookup
	// reads this.
	mutators map[string]*casts.Attribute

	// mutated is what GetMutatedAttributes returns, computed once and
	// rebuilt by CacheMutatedAttributes. mutatedCached says whether it is
	// still valid.
	mutated       []string
	mutatedCached bool

	// dateFormat is the layout dates are read and written in.
	dateFormat string
}

// InitializeHasAttributes allocates the attribute bags on first use.
//
// Go's zero value for a map is nil, and writing to a nil map panics, so the
// bags are made on first use rather than in a constructor the embedding
// model might not call.
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

// HasAttribute reports whether key is in the attribute bag.
func (h *HasAttributes) HasAttribute(key string) bool {
	h.InitializeHasAttributes()
	_, ok := h.attributes[key]
	return ok
}

// GetAttribute returns the value for key, or nil.
func (h *HasAttributes) GetAttribute(key string) any {
	h.InitializeHasAttributes()
	return h.attributes[key]
}

// GetAttributeValue is GetAttribute.
func (h *HasAttributes) GetAttributeValue(key string) any { return h.GetAttribute(key) }

// SetAttribute writes value for key.
func (h *HasAttributes) SetAttribute(key string, value any) {
	h.InitializeHasAttributes()
	h.attributes[key] = value
}

// UnsetAttribute removes key from the attribute bag.
func (h *HasAttributes) UnsetAttribute(key string) {
	h.InitializeHasAttributes()
	delete(h.attributes, key)
}

// GetAttributes returns the whole attribute bag.
func (h *HasAttributes) GetAttributes() map[string]any {
	h.InitializeHasAttributes()
	return h.attributes
}

// SetRawAttributes replaces the whole bag at once, as hydration does after
// a select.
func (h *HasAttributes) SetRawAttributes(attributes map[string]any, sync bool) {
	h.attributes = copyBag(attributes)
	h.InitializeHasAttributes()
	if sync {
		h.SyncOriginal()
	}
}

// GetOriginal returns the original value for one key, or def when there is
// none.
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

// GetRawOriginal returns the whole original bag.
func (h *HasAttributes) GetRawOriginal() map[string]any {
	h.InitializeHasAttributes()
	return h.original
}

// SyncOriginal replaces the original bag with the current attributes.
func (h *HasAttributes) SyncOriginal() {
	h.InitializeHasAttributes()
	h.original = copyBag(h.attributes)
}

// SyncOriginalAttribute replaces the original value for one key with its
// current value.
func (h *HasAttributes) SyncOriginalAttribute(attribute string) {
	h.SyncOriginalAttributes(attribute)
}

// SyncOriginalAttributes replaces the original value for these keys with
// their current values.
func (h *HasAttributes) SyncOriginalAttributes(attributes ...string) {
	h.InitializeHasAttributes()
	for _, attribute := range attributes {
		h.original[attribute] = h.attributes[attribute]
	}
}

// SyncChanges records the current dirty set as the change set.
//
// It is called after a save, and it is what lets an "updated" listener ask
// what changed: the dirty set becomes the change set, and the values that
// were replaced become the previous set.
func (h *HasAttributes) SyncChanges() {
	h.InitializeHasAttributes()

	h.changes = h.GetDirty()
	h.previous = map[string]any{}
	for key := range h.changes {
		h.previous[key] = h.original[key]
	}
}

// IsDirty reports whether any of attributes differ from the original, or
// any attribute at all with none given.
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

// IsClean reports the opposite of IsDirty.
func (h *HasAttributes) IsClean(attributes ...string) bool { return !h.IsDirty(attributes...) }

// GetDirty returns the attributes whose value is not the one that was
// loaded.
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

// OriginalIsEquivalent reports whether key's current value equals its
// original value.
//
// The two values are rendered as text and compared, which is the only
// comparison available for a value held as `any`.
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

// GetChanges returns what changed on the last SyncChanges.
func (h *HasAttributes) GetChanges() map[string]any {
	h.InitializeHasAttributes()
	return h.changes
}

// GetPrevious returns what the changed attributes held before the last
// SyncChanges.
func (h *HasAttributes) GetPrevious() map[string]any {
	h.InitializeHasAttributes()
	return h.previous
}

// WasChanged reports whether the last SyncChanges touched these attributes,
// or any attribute at all with none given.
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

// DiscardChanges resets the attributes to what was loaded.
func (h *HasAttributes) DiscardChanges() {
	h.InitializeHasAttributes()
	h.attributes = copyBag(h.original)
	h.changes = map[string]any{}
	h.previous = map[string]any{}
}

// Only returns a subset of the attributes, by key.
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

// Except returns the attributes without the named keys.
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

// Append adds names to the appended list.
func (h *HasAttributes) Append(attributes ...string) { h.appends = append(h.appends, attributes...) }

// GetAppends returns the appended list.
func (h *HasAttributes) GetAppends() []string { return h.appends }

// SetAppends replaces the appended list.
func (h *HasAttributes) SetAppends(appends []string) { h.appends = appends }

// MergeAppends adds appends to the appended list.
func (h *HasAttributes) MergeAppends(appends []string) { h.appends = append(h.appends, appends...) }

// HasAppended reports whether attribute is in the appended list.
func (h *HasAttributes) HasAppended(attribute string) bool {
	for _, appended := range h.appends {
		if appended == attribute {
			return true
		}
	}
	return false
}

// WithoutAppends clears the appended list.
func (h *HasAttributes) WithoutAppends() { h.appends = nil }

// AttributesToArray returns the attributes, filtered by the hidden and
// visible lists the model carries.
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
