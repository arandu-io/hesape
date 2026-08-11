package support

import "reflect"

// Optional answers to Illuminate\Support\Optional: a wrapper that answers nil
// for anything the value underneath does not have, instead of blowing up.
//
// In PHP the whole class is magic methods -- __get, __isset, __call and the
// four ArrayAccess offsets. Go has neither property access nor dynamic calls,
// so each one is a method here, named after the PHP method it answers to.
type Optional struct {
	value any
}

// NewOptional answers to Optional::__construct, and to the optional() helper of
// helpers.php.
func NewOptional(value any) *Optional { return &Optional{value: value} }

// Value hands back the value being wrapped. The PHP reads the protected
// property directly, which Go cannot do from outside the package.
func (o *Optional) Value() any { return o.value }

// Get answers to Optional::__get: a property of the value underneath, or nil.
// A map is read by key and a struct by field name, both of which are what a
// PHP property access reaches.
func (o *Optional) Get(key string) any {
	if o == nil || o.value == nil {
		return nil
	}
	if m, ok := o.value.(map[string]any); ok {
		return m[key]
	}
	rv := reflect.ValueOf(o.value)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		field := rv.FieldByName(key)
		if !field.IsValid() || !field.CanInterface() {
			return nil
		}
		return field.Interface()
	case reflect.Map:
		v := rv.MapIndex(reflect.ValueOf(key))
		if !v.IsValid() {
			return nil
		}
		return v.Interface()
	default:
		return nil
	}
}

// IsSet answers to Optional::__isset: whether the value underneath carries the
// name at all.
func (o *Optional) IsSet(key string) bool { return o.Get(key) != nil }

// OffsetExists answers to Optional::offsetExists: whether the value is
// accessible as an array and holds the key.
func (o *Optional) OffsetExists(key string) bool {
	if o == nil || o.value == nil {
		return false
	}
	m, ok := o.value.(map[string]any)
	if !ok {
		return false
	}
	_, exists := m[key]
	return exists
}

// OffsetGet answers to Optional::offsetGet.
func (o *Optional) OffsetGet(key string) any { return o.Get(key) }

// OffsetSet answers to Optional::offsetSet: a write that happens only when the
// value underneath is accessible as an array, and is dropped otherwise.
func (o *Optional) OffsetSet(key string, v any) {
	if m, ok := o.value.(map[string]any); ok {
		m[key] = v
	}
}

// OffsetUnset answers to Optional::offsetUnset.
func (o *Optional) OffsetUnset(key string) {
	if m, ok := o.value.(map[string]any); ok {
		delete(m, key)
	}
}
