package casts

// Attribute answers Illuminate\Database\Eloquent\Casts\Attribute: an accessor,
// a mutator, or both, declared as one value instead of two methods.
//
// The PHP finds it by reflection -- a method whose return type is Attribute is
// a mutator for the attribute of that name. Go has no return-type reflection
// over methods, so a model registers the Attribute under its key
// (concerns.HasAttributes.SetAttributeMutator) and the lookups --
// HasAttributeMutator, HasAttributeGetMutator, HasAttributeSetMutator -- read
// that registry. What is discovered there is written down here; nothing else
// changes.
type Attribute struct {
	// Get answers $attribute->get: the accessor. It takes the stored value and
	// the whole row, as the PHP's closure does.
	Get func(value any, attributes map[string]any) (any, error)

	// Set answers $attribute->set: the mutator. It returns the columns to
	// write, which is the PHP's `[$key => $value]` return.
	Set func(value any, attributes map[string]any) (map[string]any, error)

	// WithCaching answers $attribute->withCaching.
	WithCaching bool

	// WithObjectCaching answers $attribute->withObjectCaching. The PHP defaults
	// it to true and Go's zero value for a bool is false, so every constructor
	// here sets it -- and a zero Attribute built by hand is read through
	// CachesObjects rather than the field.
	WithObjectCaching bool

	// objectCachingSet records that WithObjectCaching was chosen rather than
	// left at Go's zero value, which is the only way to tell "off" from "never
	// said".
	objectCachingSet bool
}

// Make answers Attribute::make.
func Make(
	get func(value any, attributes map[string]any) (any, error),
	set func(value any, attributes map[string]any) (map[string]any, error),
) *Attribute {
	return &Attribute{Get: get, Set: set, WithObjectCaching: true, objectCachingSet: true}
}

// Get answers Attribute::get, the static: an attribute that is read only.
func Get(get func(value any, attributes map[string]any) (any, error)) *Attribute {
	return Make(get, nil)
}

// Set answers Attribute::set, the static: an attribute that is written only.
func Set(set func(value any, attributes map[string]any) (map[string]any, error)) *Attribute {
	return Make(nil, set)
}

// WithoutObjectCaching answers Attribute::withoutObjectCaching.
func (a *Attribute) WithoutObjectCaching() *Attribute {
	a.WithObjectCaching = false
	a.objectCachingSet = true
	return a
}

// ShouldCache answers Attribute::shouldCache.
func (a *Attribute) ShouldCache() *Attribute {
	a.WithCaching = true
	return a
}

// CachesObjects reports whether the accessor's result may be held between
// reads. It is the field with the PHP's default applied: an Attribute somebody
// built as a literal has WithObjectCaching false because that is Go's zero
// value, and the PHP's default is true.
func (a *Attribute) CachesObjects() bool {
	if a.objectCachingSet {
		return a.WithObjectCaching
	}
	return true
}

// HasGet reports whether the accessor half is set, which is the PHP's
// is_callable($attribute->get).
func (a *Attribute) HasGet() bool { return a != nil && a.Get != nil }

// HasSet reports whether the mutator half is set, which is the PHP's
// is_callable($attribute->set).
func (a *Attribute) HasSet() bool { return a != nil && a.Set != nil }
