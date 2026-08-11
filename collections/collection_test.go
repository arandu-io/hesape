package collections_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/collections"
)

// The edges Illuminate handles and a Go port is likely to drop: the empty
// collection, the key that is not there, the negative count, the index past the
// end and the callback that never matches.

func TestGetOutOfRange(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3})
	for _, key := range []int{-1, 3, 99} {
		if value, ok := c.Get(key); ok {
			t.Errorf("Get(%d) = (%v, true), want a miss", key, value)
		}
	}
	if value, ok := c.Get(0); !ok || value != 1 {
		t.Errorf("Get(0) = (%v, %v)", value, ok)
	}
}

func TestEmptyCollection(t *testing.T) {
	var c collections.Collection[int]

	if !c.IsEmpty() || c.IsNotEmpty() {
		t.Error("a nil collection is empty")
	}
	if all := c.All(); all == nil || len(all) != 0 {
		t.Errorf("All of an empty collection = %v, want an empty slice and never nil", all)
	}
	if _, ok := c.First(nil); ok {
		t.Error("First of an empty collection reports false")
	}
	if _, ok := c.Last(nil); ok {
		t.Error("Last of an empty collection reports false")
	}
	if _, err := c.FirstOrFail(nil); !errors.Is(err, collections.ErrItemNotFound) {
		t.Error("FirstOrFail of an empty collection reports ErrItemNotFound")
	}
	if _, err := c.Sole(nil); !errors.Is(err, collections.ErrItemNotFound) {
		t.Error("Sole of an empty collection reports ErrItemNotFound")
	}
	if _, ok := c.Percentage(func(int, int) bool { return true }, 2); ok {
		t.Error("Percentage of an empty collection reports false")
	}
	if _, ok := collections.Avg(c, func(v int) int { return v }); ok {
		t.Error("Avg of an empty collection reports false")
	}
	if _, ok := collections.Median(c, func(v int) int { return v }); ok {
		t.Error("Median of an empty collection reports false")
	}
	if collections.Mode(c, func(v int) int { return v }) != nil {
		t.Error("Mode of an empty collection is nil, as PHP returns null")
	}
	if total := collections.Sum(c, func(v int) int { return v }); total != 0 {
		t.Errorf("Sum of an empty collection = %v, want the zero", total)
	}
}

func TestCallbackThatNeverMatches(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3})
	never := func(int, int) bool { return false }

	if got := c.Filter(never); len(got) != 0 {
		t.Errorf("Filter = %v, want empty", got)
	}
	if _, ok := c.First(never); ok {
		t.Error("First reports false")
	}
	if c.Contains(never) || c.Some(never) {
		t.Error("Contains reports false")
	}
	if !c.DoesntContain(never) {
		t.Error("DoesntContain reports true")
	}
	if c.HasMany(never) || c.HasSole(never) {
		t.Error("HasMany and HasSole report false")
	}
	if got := c.TakeWhile(never); len(got) != 0 {
		t.Errorf("TakeWhile = %v, want empty", got)
	}
	if got := c.SkipWhile(never); len(got) != 3 {
		t.Errorf("SkipWhile = %v, want everything", got)
	}
	passed, failed := c.Partition(never)
	if len(passed) != 0 || len(failed) != 3 {
		t.Errorf("Partition = (%v, %v)", passed, failed)
	}
}

func TestNegativeAndZeroCounts(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3})

	if got := collections.Times(-1, func(i int) int { return i }); len(got) != 0 {
		t.Errorf("Times(-1) = %v, want empty", got)
	}
	if got := c.Multiply(-1); len(got) != 0 {
		t.Errorf("Multiply(-1) = %v, want empty", got)
	}
	if got := c.Multiply(0); len(got) != 0 {
		t.Errorf("Multiply(0) = %v, want empty", got)
	}
	if got := collections.Chunk(c, 0); len(got) != 0 {
		t.Errorf("Chunk(0) = %v, want empty", got)
	}
	if _, err := collections.Split(c, 0); !errors.Is(err, collections.ErrInvalidArgument) {
		t.Error("Split(0) reports ErrInvalidArgument")
	}
	if _, err := collections.Sliding(c, 0, 1); !errors.Is(err, collections.ErrInvalidArgument) {
		t.Error("Sliding with size 0 reports ErrInvalidArgument")
	}
	if _, err := c.Nth(0, 0); !errors.Is(err, collections.ErrInvalidArgument) {
		t.Error("Nth(0) reports ErrInvalidArgument")
	}
	if _, err := c.Random(4); !errors.Is(err, collections.ErrInvalidArgument) {
		t.Error("Random of more than there is reports ErrInvalidArgument")
	}
	if got := c.Pop(-1); len(got) != 0 {
		t.Errorf("Pop(-1) = %v, want empty", got)
	}
	if got := c.Shift(-1); len(got) != 0 {
		t.Errorf("Shift(-1) = %v, want empty", got)
	}
	if got := c.Take(-2); !reflect.DeepEqual([]int(got), []int{2, 3}) {
		t.Errorf("Take(-2) = %v, want the last two", got)
	}
}

func TestMapSpreadAppendsTheKey(t *testing.T) {
	// $chunk[] = $key before the spread, which is the half of the PHP that
	// reading the signature alone does not show.
	c := collections.Collect([][]any{{"a", "b"}, {"c", "d"}})
	got := collections.MapSpread(c, func(values ...any) []any { return values })
	want := collections.Collection[[]any]{{"a", "b", 0}, {"c", "d", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapSpread = %v, want %v", got, want)
	}
	if !reflect.DeepEqual([]any(c[0]), []any{"a", "b"}) {
		t.Error("spreading must not grow the collection's own chunk")
	}
}

func TestEachSpreadAppendsTheKeyAndStops(t *testing.T) {
	c := collections.Collect([][]any{{"a"}, {"b"}, {"c"}})
	seen := [][]any{}
	collections.EachSpread(c, func(values ...any) bool {
		seen = append(seen, values)
		return len(seen) < 2
	})
	want := [][]any{{"a", 0}, {"b", 1}}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("EachSpread saw %v, want %v", seen, want)
	}
}

func TestFromJSON(t *testing.T) {
	got, err := collections.FromJSON[int](`[1,2,3]`)
	if err != nil || !reflect.DeepEqual([]int(got), []int{1, 2, 3}) {
		t.Errorf("FromJSON = (%v, %v)", got, err)
	}
	if got, err := collections.FromJSON[int](`[]`); err != nil || len(got) != 0 {
		t.Errorf("FromJSON of an empty array = (%v, %v)", got, err)
	}
	if got, err := collections.FromJSON[int](`null`); err != nil || got == nil || len(got) != 0 {
		t.Errorf("FromJSON of null = (%v, %v), want an empty collection and never nil", got, err)
	}
	if _, err := collections.FromJSON[int](`{`); err == nil {
		t.Error("FromJSON of malformed input must report the error")
	}
}

type stubRow struct{ name string }

func (r stubRow) ToArray() map[string]any { return map[string]any{"name": r.name} }

func TestToArray(t *testing.T) {
	plain := collections.Collect([]int{1, 2})
	if got := plain.ToArray(); !reflect.DeepEqual(got, []any{1, 2}) {
		t.Errorf("ToArray = %v", got)
	}
	rows := collections.Collect([]stubRow{{name: "Ana"}})
	if got := rows.ToArray(); !reflect.DeepEqual(got, []any{map[string]any{"name": "Ana"}}) {
		t.Errorf("ToArray must convert an Arrayable element: %v", got)
	}
	var empty collections.Collection[int]
	if got := empty.ToArray(); got == nil || len(got) != 0 {
		t.Errorf("ToArray of an empty collection = %v", got)
	}
}

func TestToJSONAndToPrettyJSON(t *testing.T) {
	c := collections.Collect([]int{1, 2})
	if got, err := c.ToJSON(); err != nil || got != "[1,2]" {
		t.Errorf("ToJSON = (%q, %v)", got, err)
	}
	pretty, err := c.ToPrettyJSON()
	if err != nil || pretty != "[\n    1,\n    2\n]" {
		t.Errorf("ToPrettyJSON = (%q, %v), want four space indentation", pretty, err)
	}
	var empty collections.Collection[int]
	if got, err := empty.ToJSON(); err != nil || got != "[]" {
		t.Errorf("ToJSON of an empty collection = (%q, %v)", got, err)
	}
}

func TestHead(t *testing.T) {
	if got, ok := collections.Head([]int{1, 2}); !ok || got != 1 {
		t.Errorf("Head = (%v, %v)", got, ok)
	}
	if got, ok := collections.Head([]int{}); ok || got != 0 {
		t.Errorf("Head of an empty slice = (%v, %v), want the false PHP returns", got, ok)
	}
}

func TestSliceEdges(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3, 4, 5})
	cases := []struct {
		offset int
		length []int
		want   []int
	}{
		{1, []int{2}, []int{2, 3}},
		{-2, nil, []int{4, 5}},
		{-2, []int{1}, []int{4}},
		{0, []int{-2}, []int{1, 2, 3}},
		{9, nil, []int{}},
		{-99, []int{2}, []int{1, 2}},
	}
	for _, c2 := range cases {
		got := c.Slice(c2.offset, c2.length...)
		if !reflect.DeepEqual([]int(got), c2.want) {
			t.Errorf("Slice(%d, %v) = %v, want %v", c2.offset, c2.length, got, c2.want)
		}
	}
}

func TestSoleCarriesTheCount(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3})
	_, err := c.Sole(func(v int, _ int) bool { return v > 1 })
	var found *collections.MultipleItemsFoundError
	if !errors.As(err, &found) || found.GetCount() != 2 {
		t.Errorf("Sole = %v, want the count carried", err)
	}
	if !errors.Is(err, collections.ErrMultipleItemsFound) {
		t.Error("the error must match the sentinel")
	}
}
