package casts

// CastsAttributes is the two halves of a custom cast, one for the way out of the
// database and one for the way in.
//
// Both halves return an error, because a value that cannot be cast is a
// condition the caller has to see. model is `any` because a cast is written
// against a column and not against a model type; a cast that needs a sibling
// attribute reads the attributes map.
type CastsAttributes interface {
	// Get returns the stored value transformed into what the application
	// wants to see.
	Get(model any, key string, value any, attributes map[string]any) (any, error)

	// Set returns the columns to write. It is always a map, because a cast
	// that writes two columns and a cast that writes one should not have two
	// shapes.
	Set(model any, key string, value any, attributes map[string]any) (map[string]any, error)
}

// Castable is a type that names the caster to use rather than being one.
//
// A cast is a value the model holds, so CastUsing is a method on that value and
// what would otherwise be arguments are its fields.
type Castable interface {
	// CastUsing returns the caster configured for arguments, and an error if
	// an argument cannot be used.
	CastUsing(arguments []string) (CastsAttributes, error)
}

// SerializesCastableAttributes is the optional third half of a cast, used when
// what the application holds is not what should appear in the serialised row.
type SerializesCastableAttributes interface {
	// Serialize returns value transformed into what belongs in the
	// serialised row.
	Serialize(model any, key string, value any, attributes map[string]any) (any, error)
}
