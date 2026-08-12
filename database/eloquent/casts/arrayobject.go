package casts

import (
	"encoding/json"
	"sort"

	"github.com/arandu-io/hesape/collections"
)

// ArrayObject answers Illuminate\Database\Eloquent\Casts\ArrayObject: the
// keyed bag a JSON object column becomes.
//
// The PHP extends SPL's ArrayObject, which is an array you can hand around as
// an object and reach with $bag['key']. Go has no operator to overload, so the
// bag is a map behind accessors; what stays is the shape of the Illuminate
// class, which adds collect, toArray and jsonSerialize to it.
//
// The key is a string because that is what a JSON object has. PHP's template
// says TKey of array-key, which covers int keys too, and a JSON array with int
// keys is a Collection here rather than an ArrayObject.
type ArrayObject[TItem any] struct {
	items map[string]TItem
}

// NewArrayObject answers `new ArrayObject($data)`.
func NewArrayObject[TItem any](items map[string]TItem) *ArrayObject[TItem] {
	out := &ArrayObject[TItem]{items: make(map[string]TItem, len(items))}
	for key, item := range items {
		out.items[key] = item
	}
	return out
}

// GetArrayCopy answers ArrayObject::getArrayCopy, which the Illuminate class
// inherits and calls from all three of its own methods.
func (a *ArrayObject[TItem]) GetArrayCopy() map[string]TItem {
	out := make(map[string]TItem, len(a.items))
	for key, item := range a.items {
		out[key] = item
	}
	return out
}

// Count answers ArrayObject::count.
func (a *ArrayObject[TItem]) Count() int { return len(a.items) }

// OffsetGet answers ArrayObject::offsetGet, which PHP spells $bag['key'].
func (a *ArrayObject[TItem]) OffsetGet(key string) (TItem, bool) {
	item, ok := a.items[key]
	return item, ok
}

// OffsetSet answers ArrayObject::offsetSet.
func (a *ArrayObject[TItem]) OffsetSet(key string, item TItem) {
	if a.items == nil {
		a.items = map[string]TItem{}
	}
	a.items[key] = item
}

// OffsetExists answers ArrayObject::offsetExists.
func (a *ArrayObject[TItem]) OffsetExists(key string) bool {
	_, ok := a.items[key]
	return ok
}

// OffsetUnset answers ArrayObject::offsetUnset.
func (a *ArrayObject[TItem]) OffsetUnset(key string) { delete(a.items, key) }

// Keys returns the keys in a stable order.
//
// The PHP has no such method because a PHP array remembers insertion order and
// a Go map does not. Everything here that has to walk the bag in order walks
// this, so two runs produce the same sequence.
func (a *ArrayObject[TItem]) Keys() []string {
	keys := make([]string, 0, len(a.items))
	for key := range a.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Collect answers ArrayObject::collect.
//
// The PHP wraps the underlying array keys and all. collections.Collection is a
// slice, so what comes back are the values, in key order.
func (a *ArrayObject[TItem]) Collect() collections.Collection[TItem] {
	out := make([]TItem, 0, len(a.items))
	for _, key := range a.Keys() {
		out = append(out, a.items[key])
	}
	return collections.Collect(out)
}

// ToArray answers ArrayObject::toArray, the Arrayable half.
func (a *ArrayObject[TItem]) ToArray() map[string]TItem { return a.GetArrayCopy() }

// JSONSerialize answers ArrayObject::jsonSerialize. The PHP spells it
// jsonSerialize; a Go initialism is upper case.
func (a *ArrayObject[TItem]) JSONSerialize() map[string]TItem { return a.GetArrayCopy() }

// MarshalJSON is what makes JSONSerialize reachable from encoding/json, which
// is the job PHP's JsonSerializable interface does for json_encode.
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
