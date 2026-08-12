package view

import (
	"fmt"
	"html"
	"iter"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/str"
	"github.com/arandu-io/hesape/support/arr"
)

// ComponentAttributeBag is Illuminate\View\ComponentAttributeBag.
//
// It is the bag a component receives every attribute it was not declared to
// take: the HTML that the caller wrote on the tag and the component did not
// name. Merge is what makes a component's own class list and the caller's
// coexist instead of one overwriting the other.
//
// One divergence, and it is forced: a PHP array remembers insertion order and a
// Go map does not, so the bag iterates its keys sorted. Every method that
// returns a string -- String, ToHTML -- is therefore deterministic, which is
// what a test can assert against. It is the same choice hesape/html made for
// its attribute writer.
type ComponentAttributeBag struct {
	attributes map[string]any
}

// NewComponentAttributeBag is ComponentAttributeBag::__construct.
func NewComponentAttributeBag(attributes map[string]any) *ComponentAttributeBag {
	bag := &ComponentAttributeBag{attributes: map[string]any{}}
	bag.SetAttributes(attributes)
	return bag
}

// All is ComponentAttributeBag::all.
func (b *ComponentAttributeBag) All() map[string]any {
	out := make(map[string]any, len(b.attributes))
	for k, v := range b.attributes {
		out[k] = v
	}
	return out
}

// keys returns the attribute names in the bag's iteration order.
func (b *ComponentAttributeBag) keys() []string {
	out := make([]string, 0, len(b.attributes))
	for k := range b.attributes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// First is ComponentAttributeBag::first.
//
// The default is variadic where PHP takes a nullable argument, which is the
// mechanical form of an optional parameter in Go.
func (b *ComponentAttributeBag) First(fallback ...any) any {
	for _, k := range b.keys() {
		return b.attributes[k]
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return nil
}

// Get is ComponentAttributeBag::get.
func (b *ComponentAttributeBag) Get(key string, fallback ...any) any {
	if v, ok := b.attributes[key]; ok {
		return v
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return nil
}

// Has is ComponentAttributeBag::has. Every key has to be present.
func (b *ComponentAttributeBag) Has(keys ...string) bool {
	for _, key := range keys {
		if _, ok := b.attributes[key]; !ok {
			return false
		}
	}
	return true
}

// HasAny is ComponentAttributeBag::hasAny. One key present is enough.
func (b *ComponentAttributeBag) HasAny(keys ...string) bool {
	if len(b.attributes) == 0 {
		return false
	}
	for _, key := range keys {
		if b.Has(key) {
			return true
		}
	}
	return false
}

// Missing is ComponentAttributeBag::missing.
func (b *ComponentAttributeBag) Missing(key string) bool { return !b.Has(key) }

// Only is ComponentAttributeBag::only.
func (b *ComponentAttributeBag) Only(keys ...string) *ComponentAttributeBag {
	if keys == nil {
		return NewComponentAttributeBag(b.All())
	}
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[k] = true
	}
	values := map[string]any{}
	for k, v := range b.attributes {
		if wanted[k] {
			values[k] = v
		}
	}
	return NewComponentAttributeBag(values)
}

// Except is ComponentAttributeBag::except.
func (b *ComponentAttributeBag) Except(keys ...string) *ComponentAttributeBag {
	if keys == nil {
		return NewComponentAttributeBag(b.All())
	}
	unwanted := map[string]bool{}
	for _, k := range keys {
		unwanted[k] = true
	}
	values := map[string]any{}
	for k, v := range b.attributes {
		if !unwanted[k] {
			values[k] = v
		}
	}
	return NewComponentAttributeBag(values)
}

// Filter is ComponentAttributeBag::filter.
//
// The callback takes the value and the key, in that order, the way the PHP
// closure does.
func (b *ComponentAttributeBag) Filter(callback func(value any, key string) bool) *ComponentAttributeBag {
	values := map[string]any{}
	for k, v := range b.attributes {
		if callback(v, k) {
			values[k] = v
		}
	}
	return NewComponentAttributeBag(values)
}

// WhereStartsWith is ComponentAttributeBag::whereStartsWith.
func (b *ComponentAttributeBag) WhereStartsWith(needles ...string) *ComponentAttributeBag {
	return b.Filter(func(_ any, key string) bool { return str.StartsWith(key, needles...) })
}

// WhereDoesntStartWith is ComponentAttributeBag::whereDoesntStartWith.
func (b *ComponentAttributeBag) WhereDoesntStartWith(needles ...string) *ComponentAttributeBag {
	return b.Filter(func(_ any, key string) bool { return !str.StartsWith(key, needles...) })
}

// ThatStartWith is ComponentAttributeBag::thatStartWith.
func (b *ComponentAttributeBag) ThatStartWith(needles ...string) *ComponentAttributeBag {
	return b.WhereStartsWith(needles...)
}

// OnlyProps is ComponentAttributeBag::onlyProps.
func (b *ComponentAttributeBag) OnlyProps(keys ...string) *ComponentAttributeBag {
	return b.Only(ExtractPropNames(keys)...)
}

// ExceptProps is ComponentAttributeBag::exceptProps.
func (b *ComponentAttributeBag) ExceptProps(keys ...string) *ComponentAttributeBag {
	return b.Except(ExtractPropNames(keys)...)
}

// Class is ComponentAttributeBag::class.
//
// The name carries the Go suffix that a reserved word forces: class is not
// reserved in Go, but the method has to answer the PHP name, and it does.
func (b *ComponentAttributeBag) Class(classList any) *ComponentAttributeBag {
	return b.Merge(map[string]any{"class": arr.ToCssClasses(classList)})
}

// Style is ComponentAttributeBag::style.
func (b *ComponentAttributeBag) Style(styleList any) *ComponentAttributeBag {
	return b.Merge(map[string]any{"style": arr.ToCssStyles(styleList)})
}

// Merge is ComponentAttributeBag::merge.
//
// The component's defaults lose to what the caller wrote, except for class and
// style and for anything the component marked with Prepends: those two are
// appended rather than replaced, which is why a button can carry its own
// padding and still take a colour from the call site.
//
// escape is variadic where PHP has a defaulted parameter; it defaults to true.
func (b *ComponentAttributeBag) Merge(attributeDefaults map[string]any, escape ...bool) *ComponentAttributeBag {
	shouldEscape := true
	if len(escape) > 0 {
		shouldEscape = escape[0]
	}

	defaults := make(map[string]any, len(attributeDefaults))
	for k, v := range attributeDefaults {
		if shouldEscapeAttributeValue(shouldEscape, v) {
			defaults[k] = html.EscapeString(attributeToString(v))
			continue
		}
		defaults[k] = v
	}

	appendable := map[string]any{}
	nonAppendable := map[string]any{}
	for k, v := range b.attributes {
		_, prepending := defaults[k].(AppendableAttributeValue)
		if k == "class" || k == "style" || prepending {
			appendable[k] = v
			continue
		}
		nonAppendable[k] = v
	}

	attributes := map[string]any{}
	for k, v := range appendable {
		var defaultsValue any
		if appendableDefault, ok := defaults[k].(AppendableAttributeValue); ok {
			defaultsValue = resolveAppendableAttributeDefault(appendableDefault, shouldEscape)
		} else {
			defaultsValue = defaults[k]
		}

		value := attributeToString(v)
		if k == "style" {
			value = str.Finish(value, ";")
		}

		attributes[k] = joinUnique(attributeToString(defaultsValue), value)
	}
	for k, v := range nonAppendable {
		attributes[k] = v
	}

	merged := make(map[string]any, len(defaults)+len(attributes))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range attributes {
		merged[k] = v
	}
	return NewComponentAttributeBag(merged)
}

// joinUnique is the array_unique(array_filter([...])) of the PHP merge: empty
// pieces drop out and a piece repeated twice is written once.
func joinUnique(values ...string) string {
	seen := map[string]bool{}
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		kept = append(kept, v)
	}
	return strings.Join(kept, " ")
}

// shouldEscapeAttributeValue is ComponentAttributeBag::shouldEscapeAttributeValue.
//
// PHP escapes anything that is not an object, not null and not a boolean. The
// Go reading of "not an object" is "a scalar": a string or a number. Anything
// with a String method is the Stringable object PHP leaves alone.
func shouldEscapeAttributeValue(escape bool, value any) bool {
	if !escape {
		return false
	}
	switch value.(type) {
	case string, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// resolveAppendableAttributeDefault is
// ComponentAttributeBag::resolveAppendableAttributeDefault.
func resolveAppendableAttributeDefault(value AppendableAttributeValue, escape bool) any {
	if shouldEscapeAttributeValue(escape, value.Value) {
		return html.EscapeString(attributeToString(value.Value))
	}
	return value.Value
}

// Prepends is ComponentAttributeBag::prepends.
func (b *ComponentAttributeBag) Prepends(value any) AppendableAttributeValue {
	return NewAppendableAttributeValue(value)
}

// IsEmpty is ComponentAttributeBag::isEmpty.
func (b *ComponentAttributeBag) IsEmpty() bool {
	return strings.TrimSpace(b.String()) == ""
}

// IsNotEmpty is ComponentAttributeBag::isNotEmpty.
func (b *ComponentAttributeBag) IsNotEmpty() bool { return !b.IsEmpty() }

// GetAttributes is ComponentAttributeBag::getAttributes.
func (b *ComponentAttributeBag) GetAttributes() map[string]any { return b.All() }

// SetAttributes is ComponentAttributeBag::setAttributes.
//
// A bag handed in under the "attributes" key is the parent's bag: it is merged
// away rather than stored, so a component that forwards its attributes to a
// child does not nest one bag inside another.
func (b *ComponentAttributeBag) SetAttributes(attributes map[string]any) {
	copied := make(map[string]any, len(attributes))
	for k, v := range attributes {
		copied[k] = v
	}

	if parent, ok := copied["attributes"].(*ComponentAttributeBag); ok {
		delete(copied, "attributes")
		copied = parent.Merge(copied, false).GetAttributes()
	}

	b.attributes = copied
}

// ExtractPropNames is ComponentAttributeBag::extractPropNames.
//
// Each name is kept as written and again in kebab case, because a prop declared
// as userName is written on the tag as user-name.
func ExtractPropNames(keys []string) []string {
	props := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		props = append(props, key, str.Kebab(key))
	}
	return props
}

// ToHTML is ComponentAttributeBag::toHtml.
func (b *ComponentAttributeBag) ToHTML() string { return b.String() }

// OffsetExists is ComponentAttributeBag::offsetExists.
func (b *ComponentAttributeBag) OffsetExists(offset string) bool {
	v, ok := b.attributes[offset]
	return ok && v != nil
}

// OffsetGet is ComponentAttributeBag::offsetGet.
func (b *ComponentAttributeBag) OffsetGet(offset string) any { return b.Get(offset) }

// OffsetSet is ComponentAttributeBag::offsetSet.
func (b *ComponentAttributeBag) OffsetSet(offset string, value any) {
	if b.attributes == nil {
		b.attributes = map[string]any{}
	}
	b.attributes[offset] = value
}

// OffsetUnset is ComponentAttributeBag::offsetUnset.
func (b *ComponentAttributeBag) OffsetUnset(offset string) { delete(b.attributes, offset) }

// GetIterator is ComponentAttributeBag::getIterator.
//
// PHP returns an ArrayIterator; Go returns a range-over-func sequence, which is
// what a for range takes.
func (b *ComponentAttributeBag) GetIterator() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for _, k := range b.keys() {
			if !yield(k, b.attributes[k]) {
				return
			}
		}
	}
}

// JSONSerialize is ComponentAttributeBag::jsonSerialize.
func (b *ComponentAttributeBag) JSONSerialize() map[string]any { return b.All() }

// ToArray is ComponentAttributeBag::toArray.
func (b *ComponentAttributeBag) ToArray() map[string]any { return b.All() }

// String is ComponentAttributeBag::__toString.
//
// false and nil drop the attribute entirely; true writes the bare name, which
// is what checked and disabled need -- except for x-data and the wire: family,
// where true means an empty value.
func (b *ComponentAttributeBag) String() string {
	var out strings.Builder
	for _, key := range b.keys() {
		value := b.attributes[key]

		if value == nil {
			continue
		}
		if boolean, ok := value.(bool); ok {
			if !boolean {
				continue
			}
			if key == "x-data" || strings.HasPrefix(key, "wire:") {
				value = ""
			} else {
				value = key
			}
		}

		text := strings.TrimSpace(attributeToString(value))
		out.WriteString(" " + key + `="` + strings.ReplaceAll(text, `"`, `\"`) + `"`)
	}
	return strings.TrimSpace(out.String())
}

// attributeToString is the string cast PHP applies to an attribute value.
func attributeToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case AppendableAttributeValue:
		return v.String()
	case fmt.Stringer:
		return v.String()
	case bool:
		if v {
			return "1"
		}
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// AppendableAttributeValue is Illuminate\View\AppendableAttributeValue.
//
// It marks a default that is appended to what the caller wrote rather than
// replaced by it. Merge is the only thing that reads the marker.
type AppendableAttributeValue struct {
	// Value is the attribute value being prepended.
	Value any
}

// NewAppendableAttributeValue is AppendableAttributeValue::__construct.
func NewAppendableAttributeValue(value any) AppendableAttributeValue {
	return AppendableAttributeValue{Value: value}
}

// String is AppendableAttributeValue::__toString.
func (v AppendableAttributeValue) String() string { return attributeToString(v.Value) }
