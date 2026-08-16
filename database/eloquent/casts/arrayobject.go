package casts

import (
	"encoding/json"
	"sort"

	"github.com/arandu-io/hesape/collections"
)

// ArrayObject is the keyed bag a JSON object column becomes.
//
// The bag is a map behind accessors, with Collect, ToArray and JSON encoding on
// top of it.
//
// The key is a string because that is what a JSON object has. A JSON array is a
// Collection rather than an ArrayObject.
type ArrayObject[TItem any] struct {
	items map[string]TItem
}

// NewArrayObject returns an ArrayObject holding a copy of items.
func NewArrayObject[TItem any](items map[string]TItem) *ArrayObject[TItem] {
	out := &ArrayObject[TItem]{items: make(map[string]TItem, len(items))}
	for key, item := range items {
		out.items[key] = item
	}
	return out
}

// GetArrayCopy returns a copy of the underlying map.
func (a *ArrayObject[TItem]) GetArrayCopy() map[string]TItem {
	out := make(map[string]TItem, len(a.items))
	for key, item := range a.items {
		out[key] = item
	}
	return out
}

// Count returns the number of items in the bag.
func (a *ArrayObject[TItem]) Count() int { return len(a.items) }

// OffsetGet returns the item stored under key, and whether it exists.
func (a *ArrayObject[TItem]) OffsetGet(key string) (TItem, bool) {
	item, ok := a.items[key]
	return item, ok
}

// OffsetSet stores item under key, allocating the backing map if needed.
func (a *ArrayObject[TItem]) OffsetSet(key string, item TItem) {
	if a.items == nil {
		a.items = map[string]TItem{}
	}
	a.items[key] = item
}

// OffsetExists reports whether key is present in the bag.
func (a *ArrayObject[TItem]) OffsetExists(key string) bool {
	_, ok := a.items[key]
	return ok
}

// OffsetUnset removes key from the bag, if present.
func (a *ArrayObject[TItem]) OffsetUnset(key string) { delete(a.items, key) }

// Keys returns the keys in a stable order.
//
// A Go map has no defined iteration order, so everything here that has to
// walk the bag in order walks this instead, and two runs produce the same
// sequence.
func (a *ArrayObject[TItem]) Keys() []string {
	keys := make([]string, 0, len(a.items))
	for key := range a.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Collect returns the bag's values as a collections.Collection, in key order.
//
// collections.Collection is a slice, so only the values travel; the keys stay
// behind in the bag.
func (a *ArrayObject[TItem]) Collect() collections.Collection[TItem] {
	out := make([]TItem, 0, len(a.items))
	for _, key := range a.Keys() {
		out = append(out, a.items[key])
	}
	return collections.Collect(out)
}

// ToArray returns a copy of the bag as a map.
func (a *ArrayObject[TItem]) ToArray() map[string]TItem { return a.GetArrayCopy() }

// JSONSerialize returns a copy of the bag for encoding as a JSON object.
func (a *ArrayObject[TItem]) JSONSerialize() map[string]TItem { return a.GetArrayCopy() }

// MarshalJSON is what makes JSONSerialize reachable from encoding/json.
func (a *ArrayObject[TItem]) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.JSONSerialize())
}

// UnmarshalJSON fills the bag from a JSON object.
func (a *ArrayObject[TItem]) UnmarshalJSON(data []byte) error {
	var items map[string]TItem
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	a.items = items
	if a.items == nil {
		a.items = map[string]TItem{}
	}
	return nil
}
