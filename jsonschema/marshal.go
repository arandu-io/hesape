package jsonschema

import (
	"bytes"
	"encoding/json"
)

// field is one key of a rendered JSON object.
//
// The document is assembled from an ordered list rather than a map because a
// map renders its keys in alphabetical order, and a schema whose type comes
// after its properties is a schema a person reads twice.
type field struct {
	key   string
	value any
}

func marshalFields(fields []field) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		value, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// head opens a type's rendering with its kind, then its labels.
func (m *meta) head(kind string) []field {
	var value any = kind
	if m.nullable {
		value = []string{kind, "null"}
	}
	return m.labels([]field{{"type", value}})
}

func (m *meta) labels(fields []field) []field {
	if m.title != "" {
		fields = append(fields, field{"title", m.title})
	}
	if m.description != "" {
		fields = append(fields, field{"description", m.description})
	}
	return fields
}

// tail closes a type's rendering with the keywords that read best last.
func (m *meta) tail(fields []field) []field {
	if len(m.enum) > 0 {
		fields = append(fields, field{"enum", m.enum})
	}
	if m.hasDefault {
		fields = append(fields, field{"default", m.def})
	}
	return fields
}

// MarshalJSON renders the object, its properties in declaration order, and the
// names of the properties marked Required.
func (t *ObjectType) MarshalJSON() ([]byte, error) {
	properties := make([]field, 0, len(t.properties))
	var required []string
	for _, p := range t.properties {
		properties = append(properties, field{p.Name, p.Type})
		if p.Type.meta().required {
			required = append(required, p.Name)
		}
	}
	body, err := marshalFields(properties)
	if err != nil {
		return nil, err
	}

	fields := t.m.head("object")
	fields = append(fields, field{"properties", json.RawMessage(body)})
	if len(required) > 0 {
		fields = append(fields, field{"required", required})
	}
	fields = append(fields, field{"additionalProperties", false})
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the string and its constraints.
func (t *StringType) MarshalJSON() ([]byte, error) {
	fields := t.m.head("string")
	if t.hasMin {
		fields = append(fields, field{"minLength", t.minLength})
	}
	if t.hasMax {
		fields = append(fields, field{"maxLength", t.maxLength})
	}
	if t.pattern != nil {
		fields = append(fields, field{"pattern", t.pattern.String()})
	}
	if t.format != "" {
		fields = append(fields, field{"format", t.format})
	}
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the integer and its constraints.
func (t *IntegerType) MarshalJSON() ([]byte, error) {
	fields := t.m.head("integer")
	if t.hasMin {
		fields = append(fields, field{"minimum", t.minimum})
	}
	if t.hasMax {
		fields = append(fields, field{"maximum", t.maximum})
	}
	if t.hasMultipleOf {
		fields = append(fields, field{"multipleOf", t.multipleOf})
	}
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the number and its constraints.
func (t *NumberType) MarshalJSON() ([]byte, error) {
	fields := t.m.head("number")
	if t.hasMin {
		fields = append(fields, field{"minimum", t.minimum})
	}
	if t.hasMax {
		fields = append(fields, field{"maximum", t.maximum})
	}
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the boolean.
func (t *BooleanType) MarshalJSON() ([]byte, error) {
	return marshalFields(t.m.tail(t.m.head("boolean")))
}

// MarshalJSON renders the array, its constraints and its element schema.
func (t *ArrayType) MarshalJSON() ([]byte, error) {
	fields := t.m.head("array")
	if t.items != nil {
		fields = append(fields, field{"items", t.items})
	}
	if t.hasMin {
		fields = append(fields, field{"minItems", t.minItems})
	}
	if t.hasMax {
		fields = append(fields, field{"maxItems", t.maxItems})
	}
	if t.unique {
		fields = append(fields, field{"uniqueItems", true})
	}
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the union as a list of kinds.
func (t *UnionType) MarshalJSON() ([]byte, error) {
	kinds := t.Kinds()
	if t.m.nullable {
		kinds = append(kinds, "null")
	}
	fields := t.m.labels([]field{{"type", kinds}})
	return marshalFields(t.m.tail(fields))
}

// MarshalJSON renders the alternatives. A nullable AnyOf carries null as one
// more alternative, because there is no kind to add it to.
func (t *AnyOfType) MarshalJSON() ([]byte, error) {
	options := make([]any, 0, len(t.options)+1)
	for _, option := range t.options {
		options = append(options, option)
	}
	if t.m.nullable {
		options = append(options, map[string]string{"type": "null"})
	}
	fields := t.m.labels([]field{{"anyOf", options}})
	return marshalFields(t.m.tail(fields))
}
