package arr_test

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/arandu-io/hesape/collections"
	"github.com/arandu-io/hesape/collections/arr"
)

func TestAccessible(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"map", map[string]any{}, true},
		{"list", []any{}, true},
		{"typed list", []string{"a"}, true},
		{"collection", collections.Collection[int]{1}, true},
		{"array", [2]int{1, 2}, true},
		{"nil", nil, false},
		{"string", "abc", false},
		{"int", 1, false},
	}
	for _, c := range cases {
		if got := arr.Accessible(c.value); got != c.want {
			t.Errorf("Accessible(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

type arrayableStub struct{}

func (arrayableStub) ToArray() []any { return []any{1, 2} }

func TestArrayable(t *testing.T) {
	if !arr.Arrayable(map[string]any{}) {
		t.Error("a map is arrayable")
	}
	if !arr.Arrayable(arrayableStub{}) {
		t.Error("a value with ToArray is arrayable")
	}
	if arr.Arrayable("abc") {
		t.Error("a string is not arrayable")
	}
	if arr.Arrayable(nil) {
		t.Error("nil is not arrayable")
	}
}

func TestGetWalksDottedPath(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{
			"address": map[string]any{"city": "Recife"},
		},
	}
	value, ok := arr.Get(data, "user.address.city")
	if !ok || value != "Recife" {
		t.Fatalf("Get = (%v, %v), want (Recife, true)", value, ok)
	}
}

func TestGetMissingKey(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana"}}
	for _, key := range []string{"user.address.city", "absent", "user.name.deeper", ""} {
		if value, ok := arr.Get(data, key); ok {
			t.Errorf("Get(%q) = (%v, true), want a miss", key, value)
		}
	}
}

func TestGetPrefersTheExactKeyOverThePath(t *testing.T) {
	// PHP checks static::exists($array, $key) before it splits, so a key that
	// itself holds a dot is found rather than walked.
	data := map[string]any{
		"user.name": "flat",
		"user":      map[string]any{"name": "nested"},
	}
	if value, _ := arr.Get(data, "user.name"); value != "flat" {
		t.Errorf("Get = %v, want the flat key", value)
	}
}

func TestGetIndexesIntoLists(t *testing.T) {
	data := map[string]any{"users": []any{
		map[string]any{"name": "Ana"},
		map[string]any{"name": "Bia"},
	}}
	if value, _ := arr.Get(data, "users.1.name"); value != "Bia" {
		t.Errorf("Get = %v, want Bia", value)
	}
	if value, ok := arr.Get(data, "users.9.name"); ok {
		t.Errorf("Get past the end = (%v, true), want a miss", value)
	}
	if value, ok := arr.Get(data, "users.-1"); ok {
		t.Errorf("Get with a negative index = (%v, true), want a miss", value)
	}
}

func TestGetTellsNilFromAbsent(t *testing.T) {
	data := map[string]any{"here": nil}
	value, ok := arr.Get(data, "here")
	if !ok || value != nil {
		t.Errorf("Get on a key holding nil = (%v, %v), want (nil, true)", value, ok)
	}
	if _, ok := arr.Get(data, "absent"); ok {
		t.Error("an absent key must report false")
	}
}

func TestGetOnSomethingThatIsNotAnArray(t *testing.T) {
	if _, ok := arr.Get("a string", "0"); ok {
		t.Error("a string is not accessible")
	}
	if _, ok := arr.Get(nil, "any"); ok {
		t.Error("nil is not accessible")
	}
}

func TestExists(t *testing.T) {
	data := map[string]any{"a": nil}
	if !arr.Exists(data, "a") {
		t.Error("a key holding nil exists")
	}
	if arr.Exists(data, "b") {
		t.Error("an absent key does not exist")
	}
	if !arr.Exists([]any{1, 2}, "1") {
		t.Error("position 1 exists in a two element list")
	}
	if arr.Exists([]any{1, 2}, "2") {
		t.Error("position 2 does not exist in a two element list")
	}
}

func TestHasAndFriends(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana", "age": 30}}

	if !arr.Has(data, "user.name", "user.age") {
		t.Error("Has must accept several keys")
	}
	if arr.Has(data, "user.name", "user.email") {
		t.Error("Has must fail when one key is missing")
	}
	if arr.Has(data) {
		t.Error("Has with no key reports false")
	}
	if arr.Has(map[string]any{}, "a") {
		t.Error("Has on an empty array reports false")
	}
	if !arr.HasAll(data, "user.name", "user.age") {
		t.Error("HasAll must accept several keys")
	}
	if arr.HasAll(data, "user.name", "nope") {
		t.Error("HasAll must fail when one key is missing")
	}
	if !arr.HasAny(data, "nope", "user.age") {
		t.Error("HasAny must pass when one key is there")
	}
	if arr.HasAny(data, "nope", "nada") {
		t.Error("HasAny must fail when no key is there")
	}
	if arr.HasAny(data) {
		t.Error("HasAny with no key reports false")
	}
}

func TestSetCreatesThePath(t *testing.T) {
	data := map[string]any{}
	arr.Set(data, "user.address.city", "Recife")
	if value, _ := arr.Get(data, "user.address.city"); value != "Recife" {
		t.Fatalf("Set did not create the path: %v", data)
	}
}

func TestSetReplacesANonArraySegment(t *testing.T) {
	data := map[string]any{"user": "a string"}
	arr.Set(data, "user.name", "Ana")
	if value, _ := arr.Get(data, "user.name"); value != "Ana" {
		t.Fatalf("Set must replace a scalar on the path: %v", data)
	}
}

func TestSetWritesThroughAList(t *testing.T) {
	data := map[string]any{"users": []any{map[string]any{"name": "Ana"}}}
	arr.Set(data, "users.0.name", "Bia")
	if value, _ := arr.Get(data, "users.0.name"); value != "Bia" {
		t.Fatalf("Set through a list position: %v", data)
	}
	// Past the end there is no position to write into, and a Go slice cannot
	// grow a key the way a PHP array does.
	arr.Set(data, "users.5.name", "Cida")
	if _, ok := arr.Get(data, "users.5.name"); ok {
		t.Error("Set past the end of a list must not invent a position")
	}
}

func TestAddOnlyWritesWhereNothingIs(t *testing.T) {
	data := map[string]any{"name": "Ana"}
	out := arr.Add(data, "name", "Bia")
	if out["name"] != "Ana" {
		t.Errorf("Add must not overwrite: %v", out)
	}
	out = arr.Add(data, "email", "ana@example.com")
	if out["email"] != "ana@example.com" {
		t.Errorf("Add must write a missing key: %v", out)
	}
	if _, present := data["email"]; present {
		t.Error("Add takes the array by value; the argument must survive untouched")
	}
}

func TestForget(t *testing.T) {
	data := map[string]any{
		"user":      map[string]any{"name": "Ana", "age": 30},
		"user.name": "flat",
	}
	arr.Forget(data, "user.name")
	if _, present := data["user.name"]; present {
		t.Error("Forget must remove the exact key first")
	}
	if value, _ := arr.Get(data, "user.name"); value != "Ana" {
		t.Error("Forget must not have walked the path while an exact key was there")
	}
	arr.Forget(data, "user.name")
	if _, ok := arr.Get(data, "user.name"); ok {
		t.Error("Forget must remove the nested key on the second pass")
	}
	arr.Forget(data, "nothing.here", "absent")
	if len(data) != 1 {
		t.Errorf("Forget of a missing path must change nothing: %v", data)
	}
}

func TestForgetClosesTheGapInAList(t *testing.T) {
	data := map[string]any{"items": []any{"a", "b", "c"}}
	arr.Forget(data, "items.1")
	if got, _ := arr.Get(data, "items"); !reflect.DeepEqual(got, []any{"a", "c"}) {
		t.Errorf("Forget on a list = %v, want [a c]", got)
	}
}

func TestPull(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana"}}
	value, ok := arr.Pull(data, "user.name")
	if !ok || value != "Ana" {
		t.Fatalf("Pull = (%v, %v)", value, ok)
	}
	if _, ok := arr.Get(data, "user.name"); ok {
		t.Error("Pull must remove what it read")
	}
	if _, ok := arr.Pull(data, "absent"); ok {
		t.Error("Pull of a missing key reports false")
	}
}

func TestPush(t *testing.T) {
	data := map[string]any{}
	if _, err := arr.Push(data, "list", 1, 2); err != nil {
		t.Fatalf("Push into nothing: %v", err)
	}
	if got, _ := arr.Get(data, "list"); !reflect.DeepEqual(got, []any{1, 2}) {
		t.Errorf("Push = %v, want [1 2]", got)
	}
	if _, err := arr.Push(data, "list", 3); err != nil {
		t.Fatalf("Push onto a list: %v", err)
	}
	if got, _ := arr.Get(data, "list"); !reflect.DeepEqual(got, []any{1, 2, 3}) {
		t.Errorf("Push = %v, want [1 2 3]", got)
	}
	data["scalar"] = 7
	if _, err := arr.Push(data, "scalar", 1); !errors.Is(err, arr.ErrInvalidArgument) {
		t.Errorf("Push onto a scalar = %v, want ErrInvalidArgument", err)
	}
}

func TestExceptAndOnly(t *testing.T) {
	data := map[string]any{"a": 1, "b": 2, "c": 3}

	got := arr.Except(data, "b")
	if !reflect.DeepEqual(got, map[string]any{"a": 1, "c": 3}) {
		t.Errorf("Except = %v", got)
	}
	if len(data) != 3 {
		t.Error("Except takes the array by value; the argument must survive")
	}
	if got := arr.Only(data, "a", "z"); !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Errorf("Only = %v, want only the keys that are there", got)
	}
	if got := arr.Only(data); len(got) != 0 {
		t.Errorf("Only with no key = %v, want empty", got)
	}
}

func TestExceptValuesAndOnlyValues(t *testing.T) {
	if got := arr.ExceptValues([]int{1, 2, 3, 2}, 2); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("ExceptValues = %v", got)
	}
	if got := arr.OnlyValues([]int{1, 2, 3, 2}, 2); !reflect.DeepEqual(got, []int{2, 2}) {
		t.Errorf("OnlyValues = %v", got)
	}
	if got := arr.ExceptValues([]int{}, 1); len(got) != 0 {
		t.Errorf("ExceptValues on an empty slice = %v", got)
	}
	if got := arr.OnlyValues([]int{1, 2}); len(got) != 0 {
		t.Errorf("OnlyValues with no value = %v, want empty", got)
	}
}

func TestPrependKeysWith(t *testing.T) {
	got := arr.PrependKeysWith(map[string]any{"name": "Ana"}, "user_")
	if !reflect.DeepEqual(got, map[string]any{"user_name": "Ana"}) {
		t.Errorf("PrependKeysWith = %v", got)
	}
}

func TestDivide(t *testing.T) {
	names, values := arr.Divide(map[string]any{"b": 2, "a": 1})
	if !reflect.DeepEqual(names, []string{"a", "b"}) || !reflect.DeepEqual(values, []any{1, 2}) {
		t.Errorf("Divide = (%v, %v), want the two paired ascending", names, values)
	}
	names, values = arr.Divide(map[string]any{})
	if len(names) != 0 || len(values) != 0 {
		t.Errorf("Divide of an empty map = (%v, %v)", names, values)
	}
}

func TestDotAndUndot(t *testing.T) {
	data := map[string]any{
		"user":  map[string]any{"name": "Ana", "tags": []any{"a", "b"}},
		"empty": map[string]any{},
	}
	got := arr.Dot(data)
	want := map[string]any{
		"user.name":   "Ana",
		"user.tags.0": "a",
		"user.tags.1": "b",
		"empty":       map[string]any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dot = %v,\nwant %v", got, want)
	}
	if prefixed := arr.Dot(map[string]any{"a": 1}, "p."); !reflect.DeepEqual(prefixed, map[string]any{"p.a": 1}) {
		t.Errorf("Dot with a prepend = %v", prefixed)
	}

	back := arr.Undot(map[string]any{"user.name": "Ana", "user.age": 30})
	if !reflect.DeepEqual(back, map[string]any{"user": map[string]any{"name": "Ana", "age": 30}}) {
		t.Errorf("Undot = %v", back)
	}
}

func TestIsAssocAndIsList(t *testing.T) {
	if !arr.IsAssoc(map[string]any{"a": 1}) || arr.IsAssoc([]any{1}) || arr.IsAssoc("x") {
		t.Error("IsAssoc must be true only for a map")
	}
	if !arr.IsList([]any{1}) || arr.IsList(map[string]any{"a": 1}) || arr.IsList("x") {
		t.Error("IsList must be true only for a slice or an array")
	}
	if !arr.IsList([]any{}) {
		t.Error("an empty slice is a list")
	}
}

func TestFrom(t *testing.T) {
	if got, err := arr.From(map[string]any{"a": 1}); err != nil || !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Errorf("From(map) = (%v, %v)", got, err)
	}
	if got, err := arr.From([]string{"a"}); err != nil || !reflect.DeepEqual(got, []any{"a"}) {
		t.Errorf("From(typed slice) = (%v, %v)", got, err)
	}
	if got, err := arr.From(arrayableStub{}); err != nil || !reflect.DeepEqual(got, []any{1, 2}) {
		t.Errorf("From(Arrayable) = (%v, %v)", got, err)
	}
	type person struct {
		Name   string
		hidden int
	}
	got, err := arr.From(person{Name: "Ana", hidden: 1})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"Name": "Ana"}) {
		t.Errorf("From(struct) = (%v, %v), want only the exported fields", got, err)
	}
	for _, scalar := range []any{nil, 7, "abc"} {
		if _, err := arr.From(scalar); !errors.Is(err, arr.ErrInvalidArgument) {
			t.Errorf("From(%v) = %v, want ErrInvalidArgument", scalar, err)
		}
	}
}

func TestTypedReaders(t *testing.T) {
	data := map[string]any{
		"s": "text",
		"i": 7,
		"f": 1.5,
		"b": true,
		"a": []any{1},
	}

	if got, err := arr.String(data, "s"); err != nil || got != "text" {
		t.Errorf("String = (%v, %v)", got, err)
	}
	if got, err := arr.Integer(data, "i"); err != nil || got != 7 {
		t.Errorf("Integer = (%v, %v)", got, err)
	}
	if got, err := arr.Float(data, "f"); err != nil || got != 1.5 {
		t.Errorf("Float = (%v, %v)", got, err)
	}
	if got, err := arr.Boolean(data, "b"); err != nil || got != true {
		t.Errorf("Boolean = (%v, %v)", got, err)
	}
	if got, err := arr.Array(data, "a"); err != nil || !reflect.DeepEqual(got, []any{1}) {
		t.Errorf("Array = (%v, %v)", got, err)
	}

	// The PHP asks is_float, which an integer fails, and is_int, which a float
	// fails.
	if _, err := arr.Float(data, "i"); !errors.Is(err, arr.ErrInvalidArgument) {
		t.Error("Float over an int must report the type error")
	}
	if _, err := arr.Integer(data, "f"); !errors.Is(err, arr.ErrInvalidArgument) {
		t.Error("Integer over a float must report the type error")
	}
	if _, err := arr.String(data, "absent"); !errors.Is(err, arr.ErrInvalidArgument) {
		t.Error("a missing key with no default must report the type error")
	}
	if got, err := arr.String(data, "absent", "fallback"); err != nil || got != "fallback" {
		t.Errorf("String with a default = (%v, %v)", got, err)
	}
	if got, err := arr.Integer(data, "absent", 3); err != nil || got != 3 {
		t.Errorf("Integer with a default = (%v, %v)", got, err)
	}
}

func TestFirstAndLast(t *testing.T) {
	list := []int{1, 2, 3, 4}

	if got, ok := arr.First(list, nil); !ok || got != 1 {
		t.Errorf("First(nil callback) = (%v, %v)", got, ok)
	}
	if got, ok := arr.Last(list, nil); !ok || got != 4 {
		t.Errorf("Last(nil callback) = (%v, %v)", got, ok)
	}
	even := func(v int, _ int) bool { return v%2 == 0 }
	if got, _ := arr.First(list, even); got != 2 {
		t.Errorf("First(even) = %v", got)
	}
	if got, _ := arr.Last(list, even); got != 4 {
		t.Errorf("Last(even) = %v", got)
	}
	never := func(int, int) bool { return false }
	if got, ok := arr.First(list, never); ok || got != 0 {
		t.Errorf("First with a callback that never matches = (%v, %v)", got, ok)
	}
	if _, ok := arr.Last([]int{}, nil); ok {
		t.Error("Last of an empty slice reports false")
	}
	if _, ok := arr.First([]int{}, nil); ok {
		t.Error("First of an empty slice reports false")
	}
}

func TestTake(t *testing.T) {
	list := []int{1, 2, 3, 4}
	cases := []struct {
		limit int
		want  []int
	}{
		{2, []int{1, 2}},
		{0, []int{}},
		{9, []int{1, 2, 3, 4}},
		{-2, []int{3, 4}},
		{-9, []int{1, 2, 3, 4}},
	}
	for _, c := range cases {
		if got := arr.Take(list, c.limit); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Take(%d) = %v, want %v", c.limit, got, c.want)
		}
	}
	if got := arr.Take([]int{}, 3); len(got) != 0 {
		t.Errorf("Take from an empty slice = %v", got)
	}
}

func TestFlatten(t *testing.T) {
	nested := []any{1, []any{2, []any{3, []any{4}}}}
	if got := arr.Flatten(nested); !reflect.DeepEqual(got, []any{1, 2, 3, 4}) {
		t.Errorf("Flatten = %v", got)
	}
	if got := arr.Flatten(nested, 1); !reflect.DeepEqual(got, []any{1, 2, []any{3, []any{4}}}) {
		t.Errorf("Flatten(depth 1) = %v", got)
	}
	withMap := []any{map[string]any{"b": 2, "a": 1}}
	if got := arr.Flatten(withMap); !reflect.DeepEqual(got, []any{1, 2}) {
		t.Errorf("Flatten of a map takes its values ascending: %v", got)
	}
	if got := arr.Flatten([]any{}); len(got) != 0 {
		t.Errorf("Flatten of an empty slice = %v", got)
	}
}

func TestCollapse(t *testing.T) {
	got := arr.Collapse([]any{[]any{1, 2}, "dropped", []any{3}, collections.Collection[any]{4}})
	if !reflect.DeepEqual(got, []any{1, 2, 3, 4}) {
		t.Errorf("Collapse = %v, want the non-arrays dropped", got)
	}
	if got := arr.Collapse(nil); len(got) != 0 {
		t.Errorf("Collapse(nil) = %v", got)
	}
}

func TestCrossJoin(t *testing.T) {
	if got := arr.CrossJoin[int](); !reflect.DeepEqual(got, [][]int{{}}) {
		t.Errorf("CrossJoin of nothing = %v, want one empty tuple", got)
	}
	if got := arr.CrossJoin([]int{1, 2}); !reflect.DeepEqual(got, [][]int{{1}, {2}}) {
		t.Errorf("CrossJoin of one list = %v", got)
	}
	if got := arr.CrossJoin([]int{1, 2}, []int{}); len(got) != 0 {
		t.Errorf("CrossJoin with an empty list = %v, want nothing", got)
	}
	got := arr.CrossJoin([]int{1, 2}, []int{3, 4})
	want := [][]int{{1, 3}, {1, 4}, {2, 3}, {2, 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CrossJoin = %v, want %v", got, want)
	}
}

func TestEveryAndSome(t *testing.T) {
	positive := func(v int, _ int) bool { return v > 0 }
	if !arr.Every([]int{1, 2}, positive) {
		t.Error("Every over a passing slice")
	}
	if !arr.Every([]int{}, positive) {
		t.Error("Every over an empty slice is true: there is nothing to fail")
	}
	if arr.Every([]int{1, -1}, positive) {
		t.Error("Every must fail on one failing element")
	}
	if arr.Some([]int{}, positive) {
		t.Error("Some over an empty slice is false")
	}
	if !arr.Some([]int{-1, 1}, positive) {
		t.Error("Some must pass on one passing element")
	}
}

func TestJoin(t *testing.T) {
	cases := []struct {
		list      []string
		glue      string
		finalGlue string
		want      string
	}{
		{[]string{"a", "b", "c"}, ", ", " and ", "a, b and c"},
		{[]string{"a", "b"}, ", ", "", "a, b"},
		{[]string{"a"}, ", ", " and ", "a"},
		{nil, ", ", " and ", ""},
		{nil, ", ", "", ""},
	}
	for _, c := range cases {
		if got := arr.Join(c.list, c.glue, c.finalGlue); got != c.want {
			t.Errorf("Join(%v, %q, %q) = %q, want %q", c.list, c.glue, c.finalGlue, got, c.want)
		}
	}
}

func TestKeyBy(t *testing.T) {
	got := arr.KeyBy([]string{"ana", "bia", "ana"}, func(v string, _ int) string { return v })
	if len(got) != 2 || got["ana"] != "ana" {
		t.Errorf("KeyBy = %v", got)
	}
}

func TestMapFamily(t *testing.T) {
	if got := arr.Map([]int{1, 2}, func(v, k int) int { return v * 10 * (k + 1) }); !reflect.DeepEqual(got, []int{10, 40}) {
		t.Errorf("Map = %v", got)
	}
	if got := arr.MapWithKeys([]int{1, 2}, func(v, k int) (string, int) {
		return string(rune('a' + k)), v
	}); !reflect.DeepEqual(got, map[string]int{"a": 1, "b": 2}) {
		t.Errorf("MapWithKeys = %v", got)
	}
	// The PHP appends the key to the chunk before spreading it.
	got := arr.MapSpread([][]any{{"a", "b"}, {"c", "d"}}, func(values ...any) []any { return values })
	want := [][]any{{"a", "b", 0}, {"c", "d", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapSpread = %v, want %v", got, want)
	}
}

func TestPrepend(t *testing.T) {
	original := []int{2, 3}
	if got := arr.Prepend(original, 1); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Prepend = %v", got)
	}
	if !reflect.DeepEqual(original, []int{2, 3}) {
		t.Error("Prepend must leave the argument alone")
	}
	if got := arr.Prepend([]int(nil), 1); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("Prepend onto nil = %v", got)
	}
}

func TestPluck(t *testing.T) {
	people := []any{
		map[string]any{"name": "Ana", "role": map[string]any{"id": "a"}},
		map[string]any{"name": "Bia", "role": map[string]any{"id": "b"}},
	}
	if got := arr.Pluck(people, "name"); !reflect.DeepEqual(got, []any{"Ana", "Bia"}) {
		t.Errorf("Pluck = %v", got)
	}
	if got := arr.Pluck(people, "role.id"); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("Pluck through a dotted path = %v", got)
	}
	keyed, ok := arr.Pluck(people, "name", "role.id").(map[string]any)
	if !ok || keyed["a"] != "Ana" || keyed["b"] != "Bia" {
		t.Errorf("Pluck with a key = %v", keyed)
	}
	if got := arr.Pluck([]any{map[string]any{}}, "absent"); !reflect.DeepEqual(got, []any{nil}) {
		t.Errorf("Pluck of a missing field = %v, want one nil", got)
	}
}

func TestSelect(t *testing.T) {
	type person struct {
		Name string
		Age  int
	}
	got := arr.Select([]any{
		map[string]any{"name": "Ana", "age": 30, "secret": 1},
		person{Name: "Bia", Age: 20},
	}, "name", "Name", "Age")
	if !reflect.DeepEqual(got[0], map[string]any{"name": "Ana"}) {
		t.Errorf("Select over a map = %v", got[0])
	}
	if !reflect.DeepEqual(got[1], map[string]any{"Name": "Bia", "Age": 20}) {
		t.Errorf("Select over a struct = %v", got[1])
	}
}

func TestRandom(t *testing.T) {
	list := []int{1, 2, 3}
	got, err := arr.Random(list, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("Random = (%v, %v)", got, err)
	}
	if _, err := arr.Random(list, 4); !errors.Is(err, arr.ErrInvalidArgument) {
		t.Errorf("Random of more than there is = %v, want ErrInvalidArgument", err)
	}
	for _, number := range []int{0, -1} {
		got, err := arr.Random(list, number)
		if err != nil || len(got) != 0 {
			t.Errorf("Random(%d) = (%v, %v), want an empty slice", number, got, err)
		}
	}
	if got, err := arr.Random([]int{}, 0); err != nil || len(got) != 0 {
		t.Errorf("Random of an empty slice = (%v, %v)", got, err)
	}
}

func TestShuffle(t *testing.T) {
	original := []int{1, 2, 3, 4, 5}
	got := arr.Shuffle(original)
	if len(got) != len(original) {
		t.Fatalf("Shuffle changed the length: %v", got)
	}
	sorted := append([]int{}, got...)
	sort.Ints(sorted)
	if !reflect.DeepEqual(sorted, original) {
		t.Errorf("Shuffle must keep the same elements: %v", got)
	}
	if !reflect.DeepEqual(original, []int{1, 2, 3, 4, 5}) {
		t.Error("Shuffle must leave the argument alone")
	}
	if got := arr.Shuffle([]int{}); len(got) != 0 {
		t.Errorf("Shuffle of an empty slice = %v", got)
	}
}

func TestSole(t *testing.T) {
	if got, err := arr.Sole([]int{7}, nil); err != nil || got != 7 {
		t.Errorf("Sole of one element = (%v, %v)", got, err)
	}
	if _, err := arr.Sole([]int{}, nil); !errors.Is(err, arr.ErrItemNotFound) {
		t.Errorf("Sole of nothing = %v, want ErrItemNotFound", err)
	}
	_, err := arr.Sole([]int{1, 2}, nil)
	if !errors.Is(err, arr.ErrMultipleItemsFound) {
		t.Fatalf("Sole of two = %v, want ErrMultipleItemsFound", err)
	}
	var found *collections.MultipleItemsFoundError
	if !errors.As(err, &found) || found.GetCount() != 2 {
		t.Errorf("the error must carry the count, got %v", err)
	}
	if _, err := arr.Sole([]int{1, 2, 3}, func(v, _ int) bool { return v == 2 }); err != nil {
		t.Errorf("Sole with a filter = %v", err)
	}
	if _, err := arr.Sole([]int{1, 2}, func(int, int) bool { return false }); !errors.Is(err, arr.ErrItemNotFound) {
		t.Errorf("Sole with a filter that never matches = %v", err)
	}
}

func TestSortAndSortDesc(t *testing.T) {
	list := []int{3, 1, 2}
	identity := func(v, _ int) int { return v }
	if got := arr.Sort(list, identity); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Sort = %v", got)
	}
	if got := arr.SortDesc(list, identity); !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Errorf("SortDesc = %v", got)
	}
	if !reflect.DeepEqual(list, []int{3, 1, 2}) {
		t.Error("Sort must leave the argument alone")
	}
	if got := arr.Sort([]int{}, identity); len(got) != 0 {
		t.Errorf("Sort of an empty slice = %v", got)
	}
}

func TestSortRecursive(t *testing.T) {
	data := map[string]any{
		"letters": []any{"c", "a", "b"},
		"nested":  map[string]any{"numbers": []any{3, 1, 2}},
	}
	got, ok := arr.SortRecursive(data).(map[string]any)
	if !ok {
		t.Fatalf("SortRecursive = %T, want a map", got)
	}
	if !reflect.DeepEqual(got["letters"], []any{"a", "b", "c"}) {
		t.Errorf("the nested list must be sorted: %v", got["letters"])
	}
	nested := got["nested"].(map[string]any)
	if !reflect.DeepEqual(nested["numbers"], []any{1, 2, 3}) {
		t.Errorf("the list two levels down must be sorted: %v", nested["numbers"])
	}
	if !reflect.DeepEqual(data["letters"], []any{"c", "a", "b"}) {
		t.Error("SortRecursive takes the array by value; the argument must survive")
	}

	desc := arr.SortRecursiveDesc([]any{1, 3, 2})
	if !reflect.DeepEqual(desc, []any{3, 2, 1}) {
		t.Errorf("SortRecursiveDesc = %v", desc)
	}
	if got := arr.SortRecursive("scalar"); got != "scalar" {
		t.Errorf("SortRecursive of a scalar = %v", got)
	}
}

func TestWhereRejectPartition(t *testing.T) {
	list := []int{1, 2, 3, 4}
	even := func(v, _ int) bool { return v%2 == 0 }

	if got := arr.Where(list, even); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Errorf("Where = %v", got)
	}
	if got := arr.Reject(list, even); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("Reject = %v", got)
	}
	passed, failed := arr.Partition(list, even)
	if !reflect.DeepEqual(passed, []int{2, 4}) || !reflect.DeepEqual(failed, []int{1, 3}) {
		t.Errorf("Partition = (%v, %v)", passed, failed)
	}
	never := func(int, int) bool { return false }
	passed, failed = arr.Partition(list, never)
	if len(passed) != 0 || len(failed) != 4 {
		t.Errorf("Partition with a callback that never matches = (%v, %v)", passed, failed)
	}
	if got := arr.Where([]int{}, even); len(got) != 0 {
		t.Errorf("Where over an empty slice = %v", got)
	}
}

func TestWhereNotNull(t *testing.T) {
	got := arr.WhereNotNull([]any{1, nil, "a", nil})
	if !reflect.DeepEqual(got, []any{1, "a"}) {
		t.Errorf("WhereNotNull = %v", got)
	}
}

func TestWrap(t *testing.T) {
	if got := arr.Wrap(nil); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("Wrap(nil) = %v, want an empty list", got)
	}
	if got := arr.Wrap("a"); !reflect.DeepEqual(got, []any{"a"}) {
		t.Errorf("Wrap of a scalar = %v", got)
	}
	list := []any{1, 2}
	if got := arr.Wrap(list); !reflect.DeepEqual(got, list) {
		t.Errorf("Wrap of a list = %v", got)
	}
	if got := arr.Wrap([]string{"a"}); !reflect.DeepEqual(got, []any{"a"}) {
		t.Errorf("Wrap of a typed slice = %v", got)
	}
	assoc := map[string]any{"a": 1}
	if got := arr.Wrap(assoc); !reflect.DeepEqual(got, []any{assoc}) {
		t.Errorf("Wrap of a map = %v, want it wrapped whole", got)
	}
}

func TestQuery(t *testing.T) {
	got := arr.Query(map[string]any{
		"name":   "Ana Maria",
		"nested": map[string]any{"a": 1},
		"list":   []any{"x", "y"},
		"flag":   true,
		"off":    false,
		"absent": nil,
	})
	want := "flag=1&list%5B0%5D=x&list%5B1%5D=y&name=Ana%20Maria&nested%5Ba%5D=1&off=0"
	if got != want {
		t.Errorf("Query = %q,\nwant       %q", got, want)
	}
	if got := arr.Query(map[string]any{}); got != "" {
		t.Errorf("Query of an empty map = %q", got)
	}
}

func TestToCssClassesAndStyles(t *testing.T) {
	got := arr.ToCssClasses("p-4", map[string]bool{"font-bold": true, "hidden": false})
	if got != "p-4 font-bold" {
		t.Errorf("ToCssClasses = %q", got)
	}
	if got := arr.ToCssClasses(); got != "" {
		t.Errorf("ToCssClasses with nothing = %q", got)
	}
	if got := arr.ToCssClasses(map[string]bool{"a": false}); got != "" {
		t.Errorf("ToCssClasses with nothing true = %q", got)
	}
	styles := arr.ToCssStyles("background-color: red", map[string]bool{"font-weight: bold;": true})
	if styles != "background-color: red; font-weight: bold;" {
		t.Errorf("ToCssStyles = %q", styles)
	}
}
