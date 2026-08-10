package jsonschema

import (
	"encoding/json"
	"errors"
)

// Draft is the JSON Schema dialect every Document declares.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// Document is a schema published on its own: a root type, plus the keywords
// that let a reader say which dialect it is written in and where it came from.
//
// It renders as one object -- the identity keywords and the root's own
// keywords side by side -- which is the shape a schema file has:
//
//	{
//	  "$schema": "https://json-schema.org/draft/2020-12/schema",
//	  "$id": "https://example.test/module.schema.json",
//	  "type": "object",
//	  "properties": { ... }
//	}
//
// Title and description belong to Root. There is one place to write them.
type Document struct {
	// ID is the address the document is published at. It renders as $id.
	ID string

	// Root is the schema itself. A Document without one is an error.
	Root Type

	// Examples are whole values that satisfy Root. They are the cheapest
	// instruction a producer gets, and worth one.
	Examples []any
}

// MarshalJSON renders the document. Use [encoding/json.MarshalIndent] to write
// it to a file; the indenter reads this output like any other.
func (d Document) MarshalJSON() ([]byte, error) {
	if d.Root == nil {
		return nil, errors.New("jsonschema: document has no root type")
	}

	root, err := json.Marshal(d.Root)
	if err != nil {
		return nil, err
	}
	// The root always renders as an object, so its body can be spliced
	// between the identity keywords and the examples without decoding it --
	// which is what keeps the key order the types chose.
	if len(root) < 2 || root[0] != '{' || root[len(root)-1] != '}' {
		return nil, errors.New("jsonschema: root type did not render as an object")
	}
	body := root[1 : len(root)-1]

	head := []field{{"$schema", Draft}}
	if d.ID != "" {
		head = append(head, field{"$id", d.ID})
	}
	var tail []field
	if len(d.Examples) > 0 {
		tail = append(tail, field{"examples", d.Examples})
	}

	out, err := marshalFields(head)
	if err != nil {
		return nil, err
	}
	out = out[:len(out)-1] // drop the closing brace
	if len(body) > 0 {
		out = append(out, ',')
		out = append(out, body...)
	}
	if len(tail) > 0 {
		rest, err := marshalFields(tail)
		if err != nil {
			return nil, err
		}
		out = append(out, ',')
		out = append(out, rest[1:len(rest)-1]...)
	}
	return append(out, '}'), nil
}
