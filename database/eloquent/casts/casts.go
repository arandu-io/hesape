package casts

// CastsAttributes answers
// Illuminate\Contracts\Database\Eloquent\CastsAttributes: the two halves of a
// custom cast, one for the way out of the database and one for the way in.
//
// The PHP throws where a value cannot be cast, so both halves return an error
// (ADR 0044, mechanical change). model is `any` because a cast is written
// against a column, not against a model type, and the PHP passes $this only so
// the rare cast can look at a sibling attribute -- which the attributes map
// already carries.
type CastsAttributes interface {
	// Get answers CastsAttributes::get: the stored value, as the application
	// wants to see it.
	Get(model any, key string, value any, attributes map[string]any) (any, error)

	// Set answers CastsAttributes::set: the columns to write. The PHP returns
	// either a bare value or a key => value array; this always returns the map,
	// because a cast that writes two columns and a cast that writes one should
	// not have two shapes (RULE 9).
	Set(model any, key string, value any, attributes map[string]any) (map[string]any, error)
}

// Castable answers Illuminate\Contracts\Database\Eloquent\Castable: a type that
// names the caster to use rather than being one.
//
// The PHP declares castUsing static and reads the arguments out of the cast
// string, "AsCollection:App\Data". There are no cast strings here -- a cast is
// a value the model holds -- so CastUsing is a method on that value and the
// arguments are its fields. The name is the PHP's.
type Castable interface {
	// CastUsing answers Castable::castUsing. It returns an error where the PHP
	// throws InvalidArgumentException for an argument it cannot use.
	CastUsing(arguments []string) (CastsAttributes, error)
}

// SerializesCastableAttributes answers
// Illuminate\Contracts\Database\Eloquent\SerializesCastableAttributes: the
// optional third half, used when what the application holds is not what should
// appear in the serialised row.
type SerializesCastableAttributes interface {
	// Serialize answers SerializesCastableAttributes::serialize.
	Serialize(model any, key string, value any, attributes map[string]any) (any, error)
}
