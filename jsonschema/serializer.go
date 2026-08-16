package jsonschema

import (
	"encoding/json"
	"errors"
)

// indent is the indentation ToString encodes with.
const indent = "    "

// Serialize renders a type as the map form of its schema document.
//
// The document is produced by marshalling the type and decoding the result, so
// there is one renderer and not two: the map this returns and the bytes
// [encoding/json.Marshal] writes cannot disagree about what the schema says.
// The cost is that a map decoded from JSON holds every number as a float64, so
// a minLength of 3 reads back as float64(3). [Deserialize] takes it either way.
//
// The key order the types chose is lost, because a Go map has none. The bytes
// keep it; marshal the type when the order matters.
//
// [AnyOfType] renders as anyOf rather than failing: it is a schema this package
// can both write and check, and refusing to serialize what it can validate
// would be a document that disagrees with the program.
func Serialize(t Type) (map[string]any, error) {
	if t == nil {
		return nil, errors.New("jsonschema: serialize a nil type")
	}
	body, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ToArray converts the type to its map form. It delegates to [Serialize].
func (b *base[T]) ToArray() (map[string]any, error) {
	t, ok := any(b.self).(Type)
	if !ok {
		return nil, errors.New("jsonschema: the type was not built by this package's constructors")
	}
	return Serialize(t)
}

// ToString converts the type to its string representation: the schema
// document as pretty-printed JSON, indented with four spaces.
//
// It renders the type rather than the map [ToArray] returns, so the keys come
// out in the order the type declares them: type before properties, and
// properties in the order they were written. A Go map has no order, which is
// why the two differ.
func (b *base[T]) ToString() (string, error) {
	t, ok := any(b.self).(Type)
	if !ok {
		return "", errors.New("jsonschema: the type was not built by this package's constructors")
	}
	body, err := json.MarshalIndent(t, "", indent)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
