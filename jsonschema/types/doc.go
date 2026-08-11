// Package types is the address Illuminate\JsonSchema\Types would have, and it
// is empty on purpose.
//
// The files it would answer to, in the clone at
// laravel_illuminate/json-schema/Types:
//
//	ArrayType.php
//	BooleanType.php
//	IntegerType.php
//	NumberType.php
//	ObjectType.php
//	StringType.php
//	Type.php
//	UnionType.php
//
// All eight are implemented, in the parent package
// github.com/arandu-io/hesape/jsonschema: ArrayType, BooleanType, IntegerType,
// NumberType, ObjectType, StringType, Type and UnionType, with their methods
// under the names their PHP has -- Min, Max, Pattern, Format, Items, Unique,
// MultipleOf, WithoutAdditionalProperties, Types, Required, Nullable, Title,
// Description, Enum, Default, ToArray and ToString.
//
// PHP puts them in a sub-namespace because it reaches them through a facade,
// and JsonSchema::string() reads the same wherever StringType lives. Go has no
// facade (ADR 0001, ADR 0002), so the import path is the name the caller
// types: jsonschema.String() beside jsonschema.Validate, rather than
// types.String() beside jsonschema.Validate for one half of the same schema.
//
// The set is also closed on purpose -- jsonschema.Validate switches over every
// implementation, and a ninth declared elsewhere would be a schema it cannot
// check -- so the types are declared in the package that checks them.
//
// Nothing will be added here. It stays as a signpost, because the mapping from
// an Illuminate namespace to a Go package is otherwise one directory per
// namespace and this is the one exception.
package types
