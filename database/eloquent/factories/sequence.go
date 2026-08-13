package factories

import (
	"github.com/arandu-io/hesape/support/arr"
)

// Sequence answers Illuminate\Database\Eloquent\Factories\Sequence.
//
// It is the state that changes on every model the factory makes: the first
// gets the first element, the second the second, and the run wraps round when
// the elements are exhausted. That wrap is the whole behaviour -- a sequence of
// two applied to a count of five gives 1, 2, 1, 2, 1 -- and it is what a test
// that seeds "one admin and the rest members" relies on.
//
// An element is either a map of attributes or a func(*Sequence, map[string]any,
// Model) any that returns one, which is PHP's value() helper applied to the
// element with the sequence, the attributes so far and the parent.
type Sequence struct {
	// sequence is Sequence::$sequence. Unexported because PHP declares both a
	// $count property and a count() method, and Go has one namespace for the
	// two: count() keeps the name and the property loses it.
	sequence []any

	// count is Sequence::$count.
	count int

	// Index is Sequence::$index, the public property PHP increments on every
	// invocation.
	Index int
}

// NewSequence answers Sequence::__construct.
func NewSequence(sequence ...any) *Sequence {
	return &Sequence{sequence: sequence, count: len(sequence)}
}

// Count answers Sequence::count, the Countable implementation: how many
// elements the sequence holds, not how many have been handed out.
func (s *Sequence) Count() int { return s.count }

// Invoke answers Sequence::__invoke.
//
// PHP makes the sequence callable and passes it straight to state(); Go has no
// callable object, so the body carries the name PHP would print for it. It
// answers the element for the current index and advances, wrapping at the end.
//
// An empty sequence answers nil rather than dividing by zero, which is the one
// place this differs from the PHP -- there a modulo by zero is a fatal error.
func (s *Sequence) Invoke(attributes map[string]any, parent Model) any {
	if s.count == 0 {
		return nil
	}

	value := s.sequence[s.Index%s.count]
	s.Index++

	if fn, ok := value.(func(*Sequence, map[string]any, Model) any); ok {
		return fn(s, attributes, parent)
	}

	return value
}

// CrossJoinSequence answers
// Illuminate\Database\Eloquent\Factories\CrossJoinSequence.
//
// It adds a constructor to Sequence and nothing else: the cross join of two
// sequences of two is a sequence of four, each element the two merged. Three
// states crossed with two roles is six models, and writing the six out by hand
// is how one gets forgotten.
type CrossJoinSequence struct {
	Sequence
}

// NewCrossJoinSequence answers CrossJoinSequence::__construct.
//
// Each argument is one sequence, and each element of a sequence is a map of
// attributes. PHP array_merges the combined elements; the merge here is
// left-to-right over maps, so a key present in two sequences takes the value of
// the later one, as it does there.
func NewCrossJoinSequence(sequences ...[]any) *CrossJoinSequence {
	crossJoined := arr.CrossJoin(sequences...)

	merged := make([]any, 0, len(crossJoined))
	for _, combination := range crossJoined {
		attributes := map[string]any{}
		for _, element := range combination {
			for key, value := range toAttributes(element) {
				attributes[key] = value
			}
		}
		merged = append(merged, attributes)
	}

	return &CrossJoinSequence{Sequence: *NewSequence(merged...)}
}

// toAttributes reads an element of a sequence as an attribute map. PHP does not
// need this: everything there is an array already.
func toAttributes(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case nil:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}
