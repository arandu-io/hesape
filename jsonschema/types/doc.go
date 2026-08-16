// Package types is empty on purpose, and holds no types.
//
// The eight schema types -- ArrayType, BooleanType, IntegerType, NumberType,
// ObjectType, StringType, Type and UnionType -- are declared in the parent
// package, github.com/arandu-io/hesape/jsonschema, so the call reads
// jsonschema.String() beside jsonschema.Validate rather than splitting one
// schema across two import paths.
//
// The set is closed: jsonschema.Validate switches over every implementation,
// and a ninth declared elsewhere would be a schema it cannot check. So the
// types are declared in the package that checks them, and nothing will be added
// here.
package types
