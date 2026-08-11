package arr_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/collections/arr"
)

func users() map[string]any {
	return map[string]any{
		"users": []any{
			map[string]any{"name": "Ana", "roles": []any{
				map[string]any{"name": "admin"},
				map[string]any{"name": "editor"},
			}},
			map[string]any{"name": "Bia", "roles": []any{
				map[string]any{"name": "viewer"},
			}},
		},
	}
}

func TestDataGetPlainPath(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana"}}
	if got, ok := arr.DataGet(data, "user.name"); !ok || got != "Ana" {
		t.Errorf("DataGet = (%v, %v)", got, ok)
	}
	if _, ok := arr.DataGet(data, "user.email"); ok {
		t.Error("a missing segment must report false")
	}
	if _, ok := arr.DataGet(nil, "anything"); ok {
		t.Error("DataGet over nil must report false")
	}
}

func TestDataGetAlwaysSplitsWhereArrGetDoesNot(t *testing.T) {
	// Arr::get checks the exact key first; data_get never does.
	data := map[string]any{"user.name": "flat", "user": map[string]any{"name": "nested"}}
	if got, _ := arr.DataGet(data, "user.name"); got != "nested" {
		t.Errorf("DataGet = %v, want the walked path", got)
	}
	if got, _ := arr.Get(data, "user.name"); got != "flat" {
		t.Errorf("Get = %v, want the exact key", got)
	}
}

func TestDataGetWildcard(t *testing.T) {
	got, ok := arr.DataGet(users(), "users.*.name")
	if !ok || !reflect.DeepEqual(got, []any{"Ana", "Bia"}) {
		t.Errorf("DataGet(*) = (%v, %v)", got, ok)
	}
}

func TestDataGetNestedWildcardCollapses(t *testing.T) {
	// The PHP collapses when a further '*' is left in the remaining key, so
	// this is a flat list of names and not a list of lists.
	got, ok := arr.DataGet(users(), "users.*.roles.*.name")
	if !ok || !reflect.DeepEqual(got, []any{"admin", "editor", "viewer"}) {
		t.Errorf("DataGet(nested *) = (%v, %v)", got, ok)
	}
}

func TestDataGetWildcardKeepsMissesAsNil(t *testing.T) {
	data := map[string]any{"users": []any{
		map[string]any{"name": "Ana"},
		map[string]any{},
	}}
	got, _ := arr.DataGet(data, "users.*.name")
	if !reflect.DeepEqual(got, []any{"Ana", nil}) {
		t.Errorf("DataGet(*) = %v, want the miss kept as nil", got)
	}
}

func TestDataGetWildcardOverSomethingThatIsNotIterable(t *testing.T) {
	if _, ok := arr.DataGet(map[string]any{"users": 7}, "users.*.name"); ok {
		t.Error("a wildcard over a scalar must report false")
	}
}

func TestDataGetFirstAndLast(t *testing.T) {
	data := map[string]any{"list": []any{"a", "b", "c"}}
	if got, _ := arr.DataGet(data, "list.{first}"); got != "a" {
		t.Errorf("{first} = %v", got)
	}
	if got, _ := arr.DataGet(data, "list.{last}"); got != "c" {
		t.Errorf("{last} = %v", got)
	}
	if _, ok := arr.DataGet(map[string]any{"list": []any{}}, "list.{first}"); ok {
		t.Error("{first} over an empty list must report false")
	}
}

func TestDataGetEscapedSegments(t *testing.T) {
	data := map[string]any{"*": "star", "{first}": "literal"}
	if got, _ := arr.DataGet(data, `\*`); got != "star" {
		t.Errorf(`\* = %v`, got)
	}
	if got, _ := arr.DataGet(data, `\{first}`); got != "literal" {
		t.Errorf(`\{first} = %v`, got)
	}
}

func TestDataGetReadsStructFields(t *testing.T) {
	type role struct{ Name string }
	type person struct {
		Name string
		Role role
	}
	if got, _ := arr.DataGet(person{Name: "Ana", Role: role{Name: "admin"}}, "Role.Name"); got != "admin" {
		t.Errorf("DataGet over a struct = %v", got)
	}
}

func TestDataHas(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana", "nothing": nil}}
	if !arr.DataHas(data, "user.name") {
		t.Error("DataHas over a present path")
	}
	if !arr.DataHas(data, "user.nothing") {
		t.Error("a key holding nil is still a key")
	}
	if arr.DataHas(data, "user.email") {
		t.Error("DataHas over a missing path")
	}
	if arr.DataHas(data, "") {
		t.Error("DataHas with an empty key reports false")
	}
}

func TestDataSet(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana"}}
	arr.DataSet(data, "user.name", "Bia")
	if got, _ := arr.DataGet(data, "user.name"); got != "Bia" {
		t.Errorf("DataSet must overwrite: %v", got)
	}
	arr.DataSet(data, "user.address.city", "Recife")
	if got, _ := arr.DataGet(data, "user.address.city"); got != "Recife" {
		t.Errorf("DataSet must build the path: %v", data)
	}
}

func TestDataSetWildcard(t *testing.T) {
	data := users()
	arr.DataSet(data, "users.*.name", "same")
	got, _ := arr.DataGet(data, "users.*.name")
	if !reflect.DeepEqual(got, []any{"same", "same"}) {
		t.Errorf("DataSet(*) = %v", got)
	}
}

func TestDataSetReplacesAScalarOnThePath(t *testing.T) {
	data := map[string]any{"user": "a string"}
	arr.DataSet(data, "user.name", "Ana")
	if got, _ := arr.DataGet(data, "user.name"); got != "Ana" {
		t.Errorf("DataSet over a scalar = %v", got)
	}
}

func TestDataSetBuildsARootWhenThereIsNone(t *testing.T) {
	got := arr.DataSet("not an array", "a.b", 1)
	want := map[string]any{"a": map[string]any{"b": 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DataSet over a scalar root = %v, want %v", got, want)
	}
}

func TestDataFillLeavesWhatIsThere(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana"}}
	arr.DataFill(data, "user.name", "Bia")
	arr.DataFill(data, "user.email", "ana@example.com")
	if got, _ := arr.DataGet(data, "user.name"); got != "Ana" {
		t.Errorf("DataFill must not overwrite: %v", got)
	}
	if got, _ := arr.DataGet(data, "user.email"); got != "ana@example.com" {
		t.Errorf("DataFill must write what is missing: %v", got)
	}
}

func TestDataFillThroughAWildcard(t *testing.T) {
	data := map[string]any{"users": []any{
		map[string]any{"name": "Ana"},
		map[string]any{},
	}}
	arr.DataFill(data, "users.*.name", "unknown")
	got, _ := arr.DataGet(data, "users.*.name")
	if !reflect.DeepEqual(got, []any{"Ana", "unknown"}) {
		t.Errorf("DataFill(*) = %v", got)
	}
}

func TestDataForget(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "Ana", "age": 30}}
	arr.DataForget(data, "user.name")
	if _, ok := arr.DataGet(data, "user.name"); ok {
		t.Error("DataForget must remove the key")
	}
	if _, ok := arr.DataGet(data, "user.age"); !ok {
		t.Error("DataForget must leave the siblings alone")
	}
	arr.DataForget(data, "user.absent")
	arr.DataForget(data, "nothing.here")
	if _, ok := arr.DataGet(data, "user.age"); !ok {
		t.Error("DataForget of a missing path must change nothing")
	}
}

func TestDataForgetWildcard(t *testing.T) {
	data := users()
	arr.DataForget(data, "users.*.name")
	got, _ := arr.DataGet(data, "users.*.name")
	if !reflect.DeepEqual(got, []any{nil, nil}) {
		t.Errorf("DataForget(*) = %v, want every name gone", got)
	}
}

func TestDataForgetClosesTheGapInAList(t *testing.T) {
	data := map[string]any{"items": []any{"a", "b", "c"}}
	arr.DataForget(data, "items.1")
	got, _ := arr.DataGet(data, "items")
	if !reflect.DeepEqual(got, []any{"a", "c"}) {
		t.Errorf("DataForget on a list = %v, want [a c]", got)
	}
}
