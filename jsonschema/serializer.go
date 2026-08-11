package jsonschema

import (
	"encoding/json"
	"errors"
)

// indent is the indentation JSON_PRETTY_PRINT uses, which is what
// Type::toString encodes with.
const indent = "    "

// Serialize renders a type as the map form of its schema document. It answers
// the static Serializer::serialize(Types\Type).
//
// PHP builds the array by reading the type's own properties and mapping the
// class to a "type" keyword. Here the same document is produced by marshalling
// the type and decoding the result, so there is one renderer and not two: the
// map this returns and the bytes [encoding/json.Marshal] writes cannot disagree
// about what the schema says, which is the whole reason to have a single one
// (RULE 9). The cost is that a map decoded from JSON holds every number as a
// float64, so a minLength of 3 reads back as float64(3). [Deserialize] takes it
// either way.
//
// The key order the types chose is lost, because a Go map has none. The bytes
// keep it; marshal the type when the order matters.
//
// PHP throws a RuntimeException for a class it has no "type" keyword for.
// [AnyOfType] is the one type here with no Illuminate counterpart, and it
// renders as anyOf rather than failing: it is a schema this package can both
// write and check, and refusing to serialize what it can validate would be a
// document that disagrees with the program.
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

// ToArray converts the type to its map form. It answers Type::toArray, which
// delegates to [Serialize] exactly as this does.
func (b *base[T]) ToArray() (map[string]any, error) {
	t, ok := any(b.self).(Type)
	if !ok {
		return nil, errors.New("jsonschema: the type was not built by this package's constructors")
	}
	return Serialize(t)
}

// ToString converts the type to its string representation. It answers
// Type::toString, which is json_encode(toArray(), JSON_PRETTY_PRINT) -- four
// spaces of indentation, and the same here.
//
// It renders the type rather than the map [ToArray] returns, so the keys come
// out in the order the type declares them: type before properties, and
// properties in the order they were written. PHP has no such distinction,
// because a PHP array keeps its insertion order and a Go map does not.
//
// PHP's __toString is not ported. It is a language interface -- the one behind
// (string) $type -- and Go has no operator to give it.
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
