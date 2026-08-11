package support

import (
	"sort"

	"github.com/arandu-io/hesape/support/arr"
)

// ValidatedInput answers to Illuminate\Support\ValidatedInput: what
// $request->validate() hands back, which is the input that passed the rules and
// nothing else.
//
// The typed readers of Illuminate\Support\Traits\InteractsWithData -- String,
// Integer, Float, Boolean, Array, Date, Only, Except, Collect, Has, Missing --
// are promoted from the embedded trait, exactly as the PHP class gets them.
type ValidatedInput struct {
	dataSource
	input map[string]any
}

// NewValidatedInput answers to ValidatedInput::__construct.
func NewValidatedInput(input map[string]any) *ValidatedInput {
	v := &ValidatedInput{input: map[string]any{}}
	for key, held := range input {
		v.input[key] = held
	}
	v.dataSource = dataSource{
		allFn:  v.All,
		dataFn: func(key string, def any) any { return v.Input(key, def) },
	}
	return v
}

// Merge answers to ValidatedInput::merge: a new instance carrying this input
// with the given items written over it.
func (v *ValidatedInput) Merge(items map[string]any) *ValidatedInput {
	merged := v.All()
	for key, held := range items {
		merged[key] = held
	}
	return NewValidatedInput(merged)
}

// All answers to ValidatedInput::all. With no key it is the whole input; with
// keys it is the subset under them, read by dotted key.
func (v *ValidatedInput) All(keys ...string) map[string]any {
	if len(keys) == 0 {
		out := make(map[string]any, len(v.input))
		for key, held := range v.input {
			out[key] = held
		}
		return out
	}
	results := map[string]any{}
	for _, key := range keys {
		arr.Set(results, key, arr.Get(v.input, key, nil))
	}
	return results
}

// Keys answers to ValidatedInput::keys.
//
// A PHP array keeps insertion order and a Go map has none, so the keys come
// back sorted, which is the only order a map can promise.
func (v *ValidatedInput) Keys() []string {
	keys := make([]string, 0, len(v.input))
	for key := range v.input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Input answers to ValidatedInput::input: one item by dotted key. The variadic
// argument stands for the PHP $default, which is null; an empty key stands for
// the PHP null key and gives back everything.
func (v *ValidatedInput) Input(key string, def ...any) any {
	return arr.Get(v.All(), key, firstOr(def, nil))
}

// ToArray answers to ValidatedInput::toArray.
func (v *ValidatedInput) ToArray() map[string]any { return v.All() }

// Dump answers to ValidatedInput::dump: write the input out where the process
// can see it. With keys it is only those.
//
// The PHP uses Symfony's VarDumper; there is no such thing in the standard
// library, so this is [Dump], which writes to standard error.
func (v *ValidatedInput) Dump(keys ...string) *ValidatedInput {
	if len(keys) > 0 {
		Dump(v.Only(keys...))
		return v
	}
	Dump(v.All())
	return v
}

// Dd answers to ValidatedInput::dd: dump, then end the process with status 1.
func (v *ValidatedInput) Dd(keys ...string) {
	v.Dump(keys...)
	exit(1)
}
