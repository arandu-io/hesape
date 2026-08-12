package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/collections"
)

// AsCollection answers Illuminate\Database\Eloquent\Casts\AsCollection: a JSON
// array column read as a collection.
//
// The PHP's castUsing is static and its arguments arrive as strings parsed out
// of "AsCollection:App\Data" -- a class name resolved at runtime. Go resolves
// no class from a string, so what the arguments configured is a field: Mapper
// is what `of` set.
type AsCollection struct {
	// Mapper answers the second cast argument: what each item is turned into.
	// The PHP accepts a callable or a class to map into; a class name is not a
	// value here, so it is the function either of those became.
	Mapper func(item any) (any, error)
}

// Of answers AsCollection::of: the type each item in the collection is mapped
// to.
//
// The PHP takes a class name or a [class, method] pair and returns the cast
// string that names it. Here it takes the function that class name stood for
// and returns the configured cast, because there is no cast string to build.
//
// It is a method on the zero value rather than a package function because two
// classes in this package spell the same static -- AsCollection::of and
// AsBinary::of -- and only the receiver can keep both names (ADR 0044).
func (a AsCollection) Of(mapper func(item any) (any, error)) AsCollection {
	return AsCollection{Mapper: mapper}
}

// CastUsing answers AsCollection::castUsing.
func (a AsCollection) CastUsing(arguments []string) (CastsAttributes, error) {
	return collectionCast{mapper: a.Mapper}, nil
}

type collectionCast struct {
	mapper func(item any) (any, error)
}

// Get answers the anonymous caster's get: the column decoded, and mapped when
// a mapper was given.
//
// The PHP returns null for a decoded value that is not an array, and returns
// nothing at all -- which reads as null -- for a key the row does not have. The
// two are one answer here, because Go has one nil.
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

// Set answers the anonymous caster's set.
func (c collectionCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	return map[string]any{key: encoded}, nil
}

// AsArrayObject answers Illuminate\Database\Eloquent\Casts\AsArrayObject: a
// JSON object column read as an ArrayObject.
type AsArrayObject struct{}

// CastUsing answers AsArrayObject::castUsing.
func (AsArrayObject) CastUsing(arguments []string) (CastsAttributes, error) {
	return arrayObjectCast{}, nil
}

type arrayObjectCast struct{}

// Get answers the anonymous caster's get.
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

// Set answers the anonymous caster's set.
func (arrayObjectCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	return map[string]any{key: encoded}, nil
}

// Serialize answers the anonymous caster's serialize: what appears in the
// serialised row is the plain map, not the ArrayObject.
func (arrayObjectCast) Serialize(model any, key string, value any, attributes map[string]any) (any, error) {
	if bag, ok := value.(*ArrayObject[any]); ok {
		return bag.GetArrayCopy(), nil
	}
	return value, nil
}
