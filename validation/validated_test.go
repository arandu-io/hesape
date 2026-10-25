package validation_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/validation"
)

// TestValidatedIsNestedTheWayTheNamesDeclareIt. items.*.name is a list of
// objects, and an object holds only what a rule declared: the price nobody wrote
// a rule for is not in it.
func TestValidatedIsNestedTheWayTheNamesDeclareIt(t *testing.T) {
	set := mustCompile(t, validation.Rules{
		"title":        "required",
		"items":        "required|array",
		"items.*.name": "required|string",
		"items.*.qty":  "required|integer",
		"address.city": "required",
	})
	data := validation.Data{
		"title":    "Order",
		"smuggled": "x",
		"items": list(
			keyed("name", "a", "qty", "1", "price", "0.01"),
			keyed("name", "b", "qty", "2", "is_admin", "yes"),
		),
		"address": keyed("city", "Recife", "street", "unvalidated"),
	}
	v := validation.Make(data, set)
	if !v.Passes() {
		t.Fatalf("failed: %v", v.Errors())
	}
	got := v.Validated().Tree()
	want := validation.Data{
		"title": "Order",
		"items": []any{
			validation.Data{"name": "a", "qty": "1"},
			validation.Data{"name": "b", "qty": "2"},
		},
		"address": validation.Data{"city": "Recife"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tree() =\n%#v\nwant\n%#v", got, want)
	}
}

// TestValidatedKeepsFlatAccessWorking. The names a rule declared are still how a
// value is read, and the list a wildcard rebuilt is still read by its parent.
func TestValidatedKeepsFlatAccessWorking(t *testing.T) {
	set := mustCompile(t, validation.Rules{
		"tags":         "array",
		"tags.*":       "string|max:5",
		"items":        "array",
		"items.*.name": "required",
		"user.email":   "required|email",
	})
	v := validation.Make(validation.Data{
		"tags":  list("a", "b"),
		"items": list(keyed("name", "n0"), keyed("name", "n1")),
		"user":  keyed("email", "a@b.co"),
	}, set)
	in, err := v.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got := in.Strings("tags"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Strings(tags) = %v", got)
	}
	if got := in.String("items.1.name"); got != "n1" {
		t.Errorf("String(items.1.name) = %q", got)
	}
	if got := in.String("user.email"); got != "a@b.co" {
		t.Errorf("String(user.email) = %q", got)
	}
	if !in.Has("tags.0") || in.Has("tags.2") {
		t.Error("Has() disagrees with what was sent")
	}
	if got := in.Data()["items.0.name"]; got != "n0" {
		t.Errorf("Data()[items.0.name] = %v", got)
	}
	values := in.Values()
	if !reflect.DeepEqual(values["tags"], []string{"a", "b"}) || values.Get("items.0.name") != "n0" || values.Get("user.email") != "a@b.co" {
		t.Errorf("Values() = %v", values)
	}
	if _, whole := in.Data()["items"]; whole {
		t.Error("the array was returned whole although its members are declared")
	}
}

// TestAnUndeclaredArrayIsReturnedWhole is the case the rule is not about: an
// array with no member declared has nothing to narrow it to.
func TestAnUndeclaredArrayIsReturnedWhole(t *testing.T) {
	set := mustCompile(t, validation.Rules{"tags": "array"})
	in, err := validation.Make(validation.Data{"tags": list("a", "b")}, set).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got := in.Strings("tags"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Strings(tags) = %v", got)
	}
}

// TestFactoryCanKeepTheWholeArray.
func TestFactoryCanKeepTheWholeArray(t *testing.T) {
	set := mustCompile(t, validation.Rules{"items": "array", "items.*.name": "required"})
	data := validation.Data{"items": list(keyed("name", "a", "price", "1"))}

	whole := validation.NewFactory(nil)
	whole.IncludeUnvalidatedArrayKeys()
	in, err := whole.Validate(data, set)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := in.Get("items").([]any)
	if len(items) != 1 || items[0].(validation.Data)["price"] != "1" {
		t.Errorf("items = %v", in.Get("items"))
	}

	narrow, err := validation.NewFactory(nil).Validate(data, set)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := narrow.Get("items").([]any); got[0].(validation.Data)["price"] != nil {
		t.Errorf("items = %v", narrow.Get("items"))
	}
}

// TestAnExcludedFieldIsNotValidated.
func TestAnExcludedFieldIsNotValidated(t *testing.T) {
	set := mustCompile(t, validation.Rules{"kind": "required", "extra": "exclude_if:kind,a|string"})
	in, err := validation.Make(validation.Data{"kind": "a", "extra": "x"}, set).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if in.Has("extra") {
		t.Error("an excluded field was returned")
	}
}
