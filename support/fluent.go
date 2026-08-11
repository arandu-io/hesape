package support

import (
	"encoding/json"
	"strconv"

	"github.com/arandu-io/hesape/support/arr"
)

// Fluent answers to Illuminate\Support\Fluent: a bag of attributes read and
// written by dotted key, with the typed readers of
// Illuminate\Support\Traits\InteractsWithData on top -- String, Integer, Float,
// Boolean, Array, Date, Collect and the rest, all promoted from the embedded
// trait.
//
// The PHP also answers any undefined method by setting the attribute of that
// name, through __call. Go has no __call, so [Fluent.Set] is the one spelling.
type Fluent struct {
	dataSource
	attributes map[string]any
}

// NewFluent answers to Fluent::__construct.
func NewFluent(attributes map[string]any) *Fluent {
	f := &Fluent{attributes: map[string]any{}}
	f.dataSource = dataSource{
		allFn:  f.All,
		dataFn: func(key string, def any) any { return f.Get(key, def) },
	}
	f.Fill(attributes)
	return f
}

// Make answers to Fluent::make.
func Make(attributes map[string]any) *Fluent { return NewFluent(attributes) }

// Get answers to Fluent::get: a value by dotted key, falling back to the
// default. The variadic argument stands for the PHP $default, which is null.
func (f *Fluent) Get(key string, def ...any) any {
	return arr.Get(f.attributes, key, firstOr(def, nil))
}

// Set answers to Fluent::set: write a value under a dotted key, creating the
// levels that are missing.
func (f *Fluent) Set(key string, v any) *Fluent {
	if f.attributes == nil {
		f.attributes = map[string]any{}
	}
	arr.Set(f.attributes, key, v)
	return f
}

// Fill answers to Fluent::fill: every pair written at the top level, with no
// dot reading, which is what the PHP foreach does.
func (f *Fluent) Fill(attributes map[string]any) *Fluent {
	if f.attributes == nil {
		f.attributes = map[string]any{}
	}
	for key, v := range attributes {
		f.attributes[key] = v
	}
	return f
}

// Value answers to Fluent::value: the attribute under the exact key, with no
// dot reading, falling back to the default. A default that is a func() any is
// invoked, as PHP's value() does.
func (f *Fluent) Value(key string, def ...any) any {
	if v, ok := f.attributes[key]; ok {
		return v
	}
	return value(firstOr(def, nil))
}

// Scope answers to Fluent::scope: the value under the key as a Fluent of its
// own.
//
// The PHP casts the value to an array; a scalar becomes a one-element array
// under the key 0, and that is the key "0" here.
func (f *Fluent) Scope(key string, def ...any) *Fluent {
	switch v := f.Get(key, firstOr(def, nil)).(type) {
	case nil:
		return NewFluent(nil)
	case map[string]any:
		return NewFluent(v)
	case []any:
		attributes := make(map[string]any, len(v))
		for i, item := range v {
			attributes[strconv.Itoa(i)] = item
		}
		return NewFluent(attributes)
	default:
		return NewFluent(map[string]any{"0": v})
	}
}

// All answers to Fluent::all. With no key it is every attribute; with keys it
// is the subset under them, absent keys included as nil, which is what
// Arr::set of an Arr::get miss does in the PHP.
func (f *Fluent) All(keys ...string) map[string]any {
	if len(keys) == 0 {
		return f.ToArray()
	}
	results := map[string]any{}
	for _, key := range keys {
		arr.Set(results, key, arr.Get(f.attributes, key, nil))
	}
	return results
}

// GetAttributes answers to Fluent::getAttributes.
func (f *Fluent) GetAttributes() map[string]any { return f.ToArray() }

// ToArray answers to Fluent::toArray.
//
// The PHP hands back a copy, because a PHP array is a value; a Go map is a
// reference, so the copy is made here.
func (f *Fluent) ToArray() map[string]any {
	out := make(map[string]any, len(f.attributes))
	for k, v := range f.attributes {
		out[k] = v
	}
	return out
}

// ToJson answers to Fluent::toJson. The PHP $options flag has no counterpart in
// encoding/json, and a failed encode is the error the PHP hides behind false.
func (f *Fluent) ToJson() (string, error) {
	raw, err := json.Marshal(f.attributes)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// MarshalJSON answers to Fluent::jsonSerialize, under the name the Go encoder
// looks for.
func (f *Fluent) MarshalJSON() ([]byte, error) {
	if f.attributes == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(f.attributes)
}
