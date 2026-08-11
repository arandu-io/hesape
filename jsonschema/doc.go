// Package jsonschema builds and checks JSON Schema documents.
//
// It is Arandu's answer to Illuminate\JsonSchema -- Serializer.php,
// JsonSchema.php, JsonSchemaTypeFactory.php and the Types directory in the
// clone at laravel_illuminate/json-schema -- and it exists for one reason: a
// schema written as JSON in a string is a schema nothing checks, and the first
// time it is wrong the caller sends a value the program ignores. Here a schema
// is a value the compiler knows the shape of, and the same value both renders
// the document and validates against it, so the two cannot disagree.
//
// The whole surface is a builder and one function:
//
//	schema := jsonschema.Object(
//		jsonschema.Prop("status", jsonschema.String().
//			Description("Which posts to list").
//			Enum("published", "draft").
//			Required()),
//		jsonschema.Prop("limit", jsonschema.Integer().Min(1).Max(100)),
//	)
//
//	if err := jsonschema.Validate(schema, decoded); err != nil {
//		// every problem, in one error
//	}
//
// # Two callers, one shape of the same need
//
// A model asked to fill in a tool call, and a model asked to write a module
// specification, are the same problem: something outside the program produces
// JSON, and the program has to refuse what it did not ask for before any of it
// reaches application code. Both callers used to carry their own schema code.
//
// # What this package is not
//
// It is not the validation package. That one checks a request or a struct
// against rule strings and produces a sentence for a person. This one checks
// decoded JSON against a schema document, before a handler runs, and produces
// a sentence for a machine. Neither is implemented in terms of the other, and
// neither grows the other's vocabulary.
//
// # Objects are closed
//
// An object refuses a property it did not declare, and says so in the rendered
// document with additionalProperties: false. JSON Schema's own default is the
// opposite, and the default is wrong for this job: a producer that invents a
// property and is never told keeps inventing it.
//
// # Reading a schema this package did not write
//
// [Deserialize] is the other direction -- Deserializer.php, reached in PHP
// through JsonSchema::fromArray, which is [FromArray] here. It reads a decoded
// document into the same values the builder produces, so a schema that arrived
// as JSON is checked by the same [Validate] as one written in Go.
//
// It reads the subset this package can represent, and every keyword outside it
// is an error naming the keyword rather than a silent drop. There is no Parse
// that takes bytes: [encoding/json.Unmarshal] into a map[string]any is the
// decoder, and a second one here would be a second answer to what the document
// says.
//
// # Where the types live
//
// Illuminate keeps its eight type classes in a Types sub-namespace and reaches
// them through the JsonSchema facade -- JsonSchema::string(). Here they are the
// package itself, so the call reads jsonschema.String(). The sub-package
// hesape/jsonschema/types holds no types and exists only to say so: splitting
// them out would put half a schema behind a second import path for no gain, and
// [Validate] switches over a closed set that has to be declared where it is
// checked.
package jsonschema
