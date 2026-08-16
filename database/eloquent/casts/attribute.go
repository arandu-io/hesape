package casts

// Attribute is an accessor, a mutator, or both, declared as one value instead of
// two methods.
//
// A model registers the Attribute under its key with
// concerns.HasAttributes.SetAttributeMutator, and the lookups --
// HasAttributeMutator, HasAttributeGetMutator, HasAttributeSetMutator -- read
// that registry.
type Attribute struct {
	// Get is the accessor: a function that takes the stored value and the
	// whole row, and returns the value the application sees.
	Get func(value any, attributes map[string]any) (any, error)

	// Set is the mutator: a function that takes the value being assigned and
	// the whole row, and returns the columns to write.
	Set func(value any, attributes map[string]any) (map[string]any, error)

	// WithCaching reports whether ShouldCache turned caching on for this
	// Attribute's accessor.
	WithCaching bool

	// WithObjectCaching reports whether an object result may be held between
	// reads. The default is true, but Go's zero value for a bool is false,
	// so every constructor here sets the field explicitly; a zero Attribute
	// built by hand is read through CachesObjects instead, which supplies
	// the default.
	WithObjectCaching bool

	// objectCachingSet records that WithObjectCaching was chosen rather than
	// left at Go's zero value, which is the only way to tell "off" from "never
	// said".
	objectCachingSet bool
}

// Make returns an Attribute with the given accessor and mutator, with object
// caching on.
func Make(
	get func(value any, attributes map[string]any) (any, error),
	set func(value any, attributes map[string]any) (map[string]any, error),
) *Attribute {
	return &Attribute{Get: get, Set: set, WithObjectCaching: true, objectCachingSet: true}
}

// Get returns an Attribute with only the accessor set.
func Get(get func(value any, attributes map[string]any) (any, error)) *Attribute {
	return Make(get, nil)
}

// Set returns an Attribute with only the mutator set.
func Set(set func(value any, attributes map[string]any) (map[string]any, error)) *Attribute {
	return Make(nil, set)
}

// WithoutObjectCaching turns object caching off and returns a for chaining.
func (a *Attribute) WithoutObjectCaching() *Attribute {
	a.WithObjectCaching = false
	a.objectCachingSet = true
	return a
}

// ShouldCache turns caching on and returns a for chaining.
func (a *Attribute) ShouldCache() *Attribute {
	a.WithCaching = true
	return a
}

// CachesObjects reports whether the accessor's result may be held between
// reads. An Attribute built as a literal has WithObjectCaching false, Go's
// zero value, but CachesObjects reports true for it unless
// WithoutObjectCaching was called.
func (a *Attribute) CachesObjects() bool {
	if a.objectCachingSet {
		return a.WithObjectCaching
	}
	return true
}

// HasGet reports whether a is non-nil and its accessor is set.
func (a *Attribute) HasGet() bool { return a != nil && a.Get != nil }

// HasSet reports whether a is non-nil and its mutator is set.
func (a *Attribute) HasSet() bool { return a != nil && a.Set != nil }
