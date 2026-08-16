package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/collections"
)

// AsCollection casts a JSON array column into a collection.
//
// Go cannot reach a type from a name in a string, so what the item type would be
// named by is a field instead: Mapper is what Of sets.
type AsCollection struct {
	// Mapper is the function each decoded item is passed through before it
	// joins the collection; a nil Mapper leaves the decoded items as they
	// are.
	Mapper func(item any) (any, error)
}

// Of sets what each item in the collection is mapped to, and returns the
// configured cast.
//
// It is a method on the zero value rather than a package function because two
// casts in this package spell the same constructor -- AsCollection.Of and
// AsBinary.Of -- and only the receiver can keep both names.
func (a AsCollection) Of(mapper func(item any) (any, error)) AsCollection {
	return AsCollection{Mapper: mapper}
}

// CastUsing returns the caster configured with Mapper.
func (a AsCollection) CastUsing(arguments []string) (CastsAttributes, error) {
	return collectionCast{mapper: a.Mapper}, nil
}

type collectionCast struct {
	mapper func(item any) (any, error)
}

// Get returns the column decoded, mapped through Mapper when one is set.
//
// A decoded value that is not an array and a key the row does not have both
// come back as nil: Go has one nil for what would otherwise be two different
// kinds of absence.
func (c collectionCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	stored, ok := attributes[key]
	if !ok || stored == nil {
		return nil, nil
	}

	data, err := decodeAttribute(stored)
	if err != nil {
		return nil, fmt.Errorf("casts: decoding %s: %w", key, err)
	}

	items, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if c.mapper == nil {
		return collections.Collect(items), nil
	}

	mapped := make([]any, 0, len(items))
	for _, item := range items {
		out, err := c.mapper(item)
		if err != nil {
			return nil, fmt.Errorf("casts: mapping %s: %w", key, err)
		}
		mapped = append(mapped, out)
	}
	return collections.Collect(mapped), nil
}

// Set encodes value as JSON and returns the column holding it.
func (c collectionCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	return map[string]any{key: encoded}, nil
}

// AsArrayObject casts a JSON object column into an ArrayObject.
type AsArrayObject struct{}

// CastUsing returns the caster that reads and writes the column as an
// ArrayObject.
func (AsArrayObject) CastUsing(arguments []string) (CastsAttributes, error) {
	return arrayObjectCast{}, nil
}

type arrayObjectCast struct{}

// Get decodes the column and returns it as an *ArrayObject, or nil if the
// column is absent or does not decode to a JSON object.
func (arrayObjectCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	stored, ok := attributes[key]
	if !ok || stored == nil {
		return nil, nil
	}

	data, err := decodeAttribute(stored)
	if err != nil {
		return nil, fmt.Errorf("casts: decoding %s: %w", key, err)
	}

	items, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	return NewArrayObject(items), nil
}

// Set encodes value as JSON and returns the column holding it.
func (arrayObjectCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	return map[string]any{key: encoded}, nil
}

// Serialize returns value's plain map form when value is an *ArrayObject, so
// the serialised row holds the map rather than the ArrayObject itself.
func (arrayObjectCast) Serialize(model any, key string, value any, attributes map[string]any) (any, error) {
	if bag, ok := value.(*ArrayObject[any]); ok {
		return bag.GetArrayCopy(), nil
	}
	return value, nil
}
