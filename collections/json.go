package collections

import "encoding/json"

// The four methods of Traits/EnumeratesValues.php that cross into JSON, plus
// the one that flattens the elements first.
//
// Illuminate spells them fromJson, toArray, toJson and toPrettyJson. Go spells
// an initialism in capitals, so Json is JSON here -- the same rule that turns
// toSql into ToSQL. Nothing else about the names changes.

// FromJSON answers to
// Illuminate\Support\Traits\EnumeratesValues::fromJson: a collection built by
// decoding a JSON string.
//
// The PHP takes json_decode's $depth and $flags. Neither has a counterpart in
// encoding/json, which imposes no depth limit and takes no flags, so they are
// absent rather than accepted and ignored. Where the PHP returns null on
// malformed input -- and builds an empty collection from it -- this returns the
// decoding error.
//
// A JSON object decodes in PHP to a keyed array and from there to a collection
// carrying those keys. A Collection[T] is a list, so an object is a decoding
// error here; decode it into a map with encoding/json directly.
func FromJSON[T any](text string) (Collection[T], error) {
	var items []T
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, err
	}
	if items == nil {
		return Collection[T]{}, nil
	}
	return Collection[T](items), nil
}

// ToArray answers to
// Illuminate\Support\Traits\EnumeratesValues::toArray: the elements as a plain
// slice, with each element that knows how to become an array turned into one.
//
// The PHP asks whether the element is an Arrayable and calls toArray on it. The
// Go counterpart is an element with a ToArray method returning []any or
// map[string]any; anything else is passed through as it is.
//
// It is a method and All is a method, and they differ: All hands back the
// elements with their own type, ToArray hands back []any with the Arrayable
// ones already converted.
func (c Collection[T]) ToArray() []any {
	out := make([]any, len(c))
	for i, item := range c {
		switch value := any(item).(type) {
		case interface{ ToArray() []any }:
			out[i] = value.ToArray()
		case interface{ ToArray() map[string]any }:
			out[i] = value.ToArray()
		default:
			out[i] = item
		}
	}
	return out
}

// ToJSON answers to
// Illuminate\Support\Traits\EnumeratesValues::toJson.
//
// The PHP takes json_encode's $options bitmask, which encoding/json does not
// have; the one option Illuminate itself passes is JSON_PRETTY_PRINT, and that
// is ToPrettyJSON. It returns the marshalling error where the PHP returns false
// and leaves the reason in json_last_error.
func (c Collection[T]) ToJSON() (string, error) {
	encoded, err := json.Marshal(c.All())
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// ToPrettyJSON answers to
// Illuminate\Support\Traits\EnumeratesValues::toPrettyJson: ToJSON with
// JSON_PRETTY_PRINT set.
//
// PHP indents with four spaces; encoding/json is asked for the same here, so
// the two produce the same shape.
func (c Collection[T]) ToPrettyJSON() (string, error) {
	encoded, err := json.MarshalIndent(c.All(), "", "    ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Head answers to the head() helper of helpers.php: the first element of a
// slice.
//
// The PHP returns false on an empty array, which is its way of saying "no
// element"; the second result is false here for the same reason. Over a
// collection, First with a nil callback is the same thing.
func Head[T any](array []T) (T, bool) {
	if len(array) == 0 {
		var zero T
		return zero, false
	}
	return array[0], true
}
