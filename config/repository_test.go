package config_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/config"
)

func repo(t *testing.T) *config.Repository {
	t.Helper()
	return config.NewRepository(map[string]any{
		"app": map[string]any{
			"name":      "Arandu",
			"debug":     true,
			"providers": []any{"first", "second"},
			"threshold": 0.5,
			"workers":   4,
			"fallback":  nil,
		},
		"a.b": "literal",
	})
}

func TestNewRepositoryTreatsNilItemsAsEmpty(t *testing.T) {
	r := config.NewRepository(nil)
	if r.Has("app") {
		t.Fatal("Has on an empty repository: true, want false")
	}
	if got := r.All(); len(got) != 0 {
		t.Fatalf("All: %v, want empty", got)
	}
	if got := r.All(); got == nil {
		t.Fatal("All: nil, want an empty map")
	}
}

func TestNewRepositoryCopiesTheItems(t *testing.T) {
	items := map[string]any{"app": map[string]any{"name": "Arandu"}}
	r := config.NewRepository(items)

	items["app"].(map[string]any)["name"] = "changed"

	if got := r.Get("app.name"); got != "Arandu" {
		t.Fatalf("app.name: %v, want Arandu -- the constructor kept the caller's map", got)
	}
}

func TestHas(t *testing.T) {
	r := repo(t)

	cases := []struct {
		key  string
		want bool
	}{
		{"app", true},
		{"app.name", true},
		{"app.fallback", true}, // present with a nil value: the key exists
		{"app.missing", false},
		{"missing.name", false},
		{"app.name.deeper", false},
		{"a.b", true}, // the literal key wins over the path
		{"", false},
		{"app.providers.1", true},
		{"app.providers.9", false},
	}
	for _, c := range cases {
		if got := r.Has(c.key); got != c.want {
			t.Errorf("Has(%q): %v, want %v", c.key, got, c.want)
		}
	}
}

func TestHasIsFalseOnAnEmptyRepository(t *testing.T) {
	r := config.NewRepository(map[string]any{})
	for _, key := range []string{"", "app", "app.name"} {
		if r.Has(key) {
			t.Errorf("Has(%q): true, want false", key)
		}
	}
}

func TestGet(t *testing.T) {
	r := repo(t)

	if got := r.Get("app.name"); got != "Arandu" {
		t.Errorf("Get(app.name): %v", got)
	}
	if got := r.Get("app.missing"); got != nil {
		t.Errorf("Get(app.missing): %v, want nil", got)
	}
	if got := r.Get("app.missing", "default"); got != "default" {
		t.Errorf("Get(app.missing, default): %v", got)
	}
	if got := r.Get("app.fallback", "default"); got != nil {
		t.Errorf("Get(app.fallback, default): %v, want nil -- a key present with a nil value is not missing", got)
	}
	if got := r.Get("a.b"); got != "literal" {
		t.Errorf("Get(a.b): %v, want literal", got)
	}
	if got := r.Get("app.providers.0"); got != "first" {
		t.Errorf("Get(app.providers.0): %v, want first", got)
	}
	if got := r.Get(""); got != nil {
		t.Errorf("Get(empty key): %v, want nil", got)
	}
}

func TestGetCallsAFunctionDefault(t *testing.T) {
	r := repo(t)

	called := 0
	def := func() any {
		called++
		return "computed"
	}

	if got := r.Get("app.missing", any(def)); got != "computed" {
		t.Fatalf("Get with a function default: %v", got)
	}
	if got := r.Get("app.name", any(def)); got != "Arandu" {
		t.Fatalf("Get with a function default on a present key: %v", got)
	}
	if called != 1 {
		t.Fatalf("the default was called %d times, want 1 -- a present key must not pay for it", called)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	r := repo(t)

	got, ok := r.Get("app").(map[string]any)
	if !ok {
		t.Fatalf("Get(app): %T, want a map", r.Get("app"))
	}
	got["name"] = "changed"

	if again := r.Get("app.name"); again != "Arandu" {
		t.Fatalf("app.name: %v, want Arandu -- Get handed out the live map", again)
	}
}

func TestGetMany(t *testing.T) {
	r := repo(t)

	got := r.GetMany(map[string]any{
		"app.name":    nil,
		"app.missing": nil,
		"app.absent":  "fallback",
	})
	want := map[string]any{
		"app.name":    "Arandu",
		"app.missing": nil,
		"app.absent":  "fallback",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetMany: %v, want %v", got, want)
	}
}

func TestGetManyWithNoKeys(t *testing.T) {
	got := repo(t).GetMany(nil)
	if got == nil {
		t.Fatal("GetMany(nil): nil, want an empty map")
	}
	if len(got) != 0 {
		t.Fatalf("GetMany(nil): %v, want empty", got)
	}
}

func TestString(t *testing.T) {
	r := repo(t)

	got, err := r.String("app.name")
	if err != nil || got != "Arandu" {
		t.Fatalf("String(app.name): %q, %v", got, err)
	}

	if _, err := r.String("app.workers"); err == nil {
		t.Fatal("String on an int: no error, want one -- the check is is_string, not a cast")
	}

	_, err = r.String("app.missing")
	if err == nil {
		t.Fatal("String on a missing key: no error, want one")
	}
	const want = "configuration value for key [app.missing] must be a string, NULL given"
	if err.Error() != want {
		t.Fatalf("String error: %q, want %q", err, want)
	}

	if got, err := r.String("app.missing", "default"); err != nil || got != "default" {
		t.Fatalf("String with a default: %q, %v", got, err)
	}
}

func TestInteger(t *testing.T) {
	r := repo(t)

	got, err := r.Integer("app.workers")
	if err != nil || got != 4 {
		t.Fatalf("Integer(app.workers): %d, %v", got, err)
	}

	if _, err := r.Integer("app.threshold"); err == nil {
		t.Fatal("Integer on a float: no error, want one -- is_float is not is_int")
	}
	if _, err := r.Integer("app.name"); err == nil {
		t.Fatal("Integer on a string: no error, want one")
	}

	_, err = r.Integer("app.missing")
	if err == nil || err.Error() != "configuration value for key [app.missing] must be an integer, NULL given" {
		t.Fatalf("Integer error: %v", err)
	}

	// PHP has one integer type. Every Go integer kind answers to it.
	wide := config.NewRepository(map[string]any{"n": int64(9), "u": uint8(3)})
	if got, err := wide.Integer("n"); err != nil || got != 9 {
		t.Fatalf("Integer on an int64: %d, %v", got, err)
	}
	if got, err := wide.Integer("u"); err != nil || got != 3 {
		t.Fatalf("Integer on a uint8: %d, %v", got, err)
	}

	over := config.NewRepository(map[string]any{"n": uint64(1) << 63})
	if _, err := over.Integer("n"); err == nil {
		t.Fatal("Integer on a value too large for int: no error, want one rather than a truncation")
	}

	if got, err := r.Integer("app.missing", 7); err != nil || got != 7 {
		t.Fatalf("Integer with a default: %d, %v", got, err)
	}
}

func TestFloat(t *testing.T) {
	r := repo(t)

	got, err := r.Float("app.threshold")
	if err != nil || got != 0.5 {
		t.Fatalf("Float(app.threshold): %v, %v", got, err)
	}

	if _, err := r.Float("app.workers"); err == nil {
		t.Fatal("Float on an int: no error, want one -- is_float rejects it in PHP too")
	}

	_, err = r.Float("app.missing")
	if err == nil || err.Error() != "configuration value for key [app.missing] must be a float, NULL given" {
		t.Fatalf("Float error: %v", err)
	}

	if got, err := r.Float("app.missing", float32(1.5)); err != nil || got != 1.5 {
		t.Fatalf("Float with a float32 default: %v, %v", got, err)
	}
}

func TestBoolean(t *testing.T) {
	r := repo(t)

	got, err := r.Boolean("app.debug")
	if err != nil || got != true {
		t.Fatalf("Boolean(app.debug): %v, %v", got, err)
	}

	truthy := config.NewRepository(map[string]any{"a": "true", "b": 1})
	for _, key := range []string{"a", "b"} {
		if _, err := truthy.Boolean(key); err == nil {
			t.Errorf("Boolean(%q): no error, want one -- is_bool is not a cast", key)
		}
	}

	_, err = r.Boolean("app.missing")
	if err == nil || err.Error() != "configuration value for key [app.missing] must be a boolean, NULL given" {
		t.Fatalf("Boolean error: %v", err)
	}

	if got, err := r.Boolean("app.missing", false); err != nil || got != false {
		t.Fatalf("Boolean with a default: %v, %v", got, err)
	}
}

func TestArray(t *testing.T) {
	r := repo(t)

	got, err := r.Array("app.providers")
	if err != nil {
		t.Fatalf("Array(app.providers): %v", err)
	}
	if !reflect.DeepEqual(got, []any{"first", "second"}) {
		t.Fatalf("Array(app.providers): %v", got)
	}

	if _, err := r.Array("app.name"); err == nil {
		t.Fatal("Array on a string: no error, want one")
	}

	_, err = r.Array("app.missing")
	if err == nil || err.Error() != "configuration value for key [app.missing] must be an array, NULL given" {
		t.Fatalf("Array error: %v", err)
	}

	// A section with string keys is an array in PHP and a map here. The error
	// says where to read it rather than dropping the keys.
	if _, err := r.Array("app"); err == nil {
		t.Fatal("Array on a section with string keys: no error, want one")
	}

	if got, err := r.Array("app.missing", []any{"x"}); err != nil || !reflect.DeepEqual(got, []any{"x"}) {
		t.Fatalf("Array with a default: %v, %v", got, err)
	}
}

func TestArrayReturnsACopy(t *testing.T) {
	r := repo(t)

	got, err := r.Array("app.providers")
	if err != nil {
		t.Fatal(err)
	}
	got[0] = "changed"

	again, err := r.Array("app.providers")
	if err != nil {
		t.Fatal(err)
	}
	if again[0] != "first" {
		t.Fatalf("app.providers[0]: %v, want first", again[0])
	}
}

func TestCollection(t *testing.T) {
	r := repo(t)

	got, err := r.Collection("app.providers")
	if err != nil {
		t.Fatalf("Collection(app.providers): %v", err)
	}
	// It is Illuminate\Support\Collection, so the result carries that type's
	// methods rather than being a bare slice.
	if !reflect.DeepEqual([]any(got), []any{"first", "second"}) {
		t.Fatalf("Collection(app.providers): %v", got)
	}
	if got.Count() != 2 {
		t.Fatalf("Collection(app.providers).Count() = %d, want 2", got.Count())
	}

	if _, err := r.Collection("app.missing"); err == nil {
		t.Fatal("Collection on a missing key: no error, want the one Array gives")
	}
}

func TestSet(t *testing.T) {
	r := repo(t)

	r.Set("app.name", "Renamed")
	if got := r.Get("app.name"); got != "Renamed" {
		t.Errorf("app.name after Set: %v", got)
	}

	r.Set("mail.mailers.ses.region", "sa-east-1")
	if got := r.Get("mail.mailers.ses.region"); got != "sa-east-1" {
		t.Errorf("a path that did not exist: %v", got)
	}

	// A level that exists but is not a map is replaced by one, as Arr::set does.
	r.Set("app.name.first", "nested")
	if got := r.Get("app.name.first"); got != "nested" {
		t.Errorf("after nesting under a scalar: %v", got)
	}

	r.Set("", "empty")
	if got := r.Get(""); got != "empty" {
		t.Errorf("Get(empty key) after Set: %v", got)
	}
}

func TestSetDoesNotCreateALiteralDottedKey(t *testing.T) {
	r := config.NewRepository(nil)
	r.Set("a.b", 1)

	all := r.All()
	if _, literal := all["a.b"]; literal {
		t.Fatal("Set created a top-level \"a.b\": Arr::set always explodes on the dot")
	}
	nested, ok := all["a"].(map[string]any)
	if !ok || nested["b"] != 1 {
		t.Fatalf("All: %v, want a nested a.b", all)
	}
}

func TestSetCopiesTheValue(t *testing.T) {
	r := config.NewRepository(nil)
	val := map[string]any{"host": "localhost"}
	r.Set("db", val)

	val["host"] = "changed"

	if got := r.Get("db.host"); got != "localhost" {
		t.Fatalf("db.host: %v, want localhost -- Set kept the caller's map", got)
	}
}

func TestPush(t *testing.T) {
	r := repo(t)

	if err := r.Push("app.providers", "third"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Array("app.providers")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"first", "second", "third"}) {
		t.Fatalf("after Push: %v", got)
	}

	// A missing key starts an empty list, as get($key, []) does.
	if err := r.Push("app.listeners", "one"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Array("app.listeners"); !reflect.DeepEqual(got, []any{"one"}) {
		t.Fatalf("Push onto a missing key: %v", got)
	}

	if err := r.Push("app.name", "x"); err == nil {
		t.Error("Push onto a string: no error, want one")
	}
	// A key present with a nil value returns nil, not the default: PHP fatals
	// on the array_unshift that follows, so this is an error.
	if err := r.Push("app.fallback", "x"); err == nil {
		t.Error("Push onto a nil value: no error, want one")
	}
}

func TestPrepend(t *testing.T) {
	r := repo(t)

	if err := r.Prepend("app.providers", "zeroth"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Array("app.providers")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"zeroth", "first", "second"}) {
		t.Fatalf("after Prepend: %v", got)
	}

	if err := r.Prepend("app.listeners", "one"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Array("app.listeners"); !reflect.DeepEqual(got, []any{"one"}) {
		t.Fatalf("Prepend onto a missing key: %v", got)
	}

	if err := r.Prepend("app.workers", "x"); err == nil {
		t.Error("Prepend onto an int: no error, want one")
	}
}

func TestPushDoesNotWriteIntoAValueAlreadyHandedOut(t *testing.T) {
	r := repo(t)

	before, err := r.Array("app.providers")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push("app.providers", "third"); err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("the slice read before the Push grew to %v", before)
	}
}

func TestAllReturnsACopy(t *testing.T) {
	r := repo(t)

	all := r.All()
	all["app"].(map[string]any)["name"] = "changed"
	delete(all, "a.b")

	if got := r.Get("app.name"); got != "Arandu" {
		t.Errorf("app.name: %v, want Arandu -- All handed out the live tree", got)
	}
	if !r.Has("a.b") {
		t.Error("a.b: gone -- All handed out the live tree")
	}
}

func TestRepositoryIsSafeForConcurrentUse(t *testing.T) {
	r := repo(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Set("app.workers", i)
			_ = r.Get("app.name")
			_, _ = r.Integer("app.workers")
			_ = r.All()
			_ = r.Has("app.providers")
			_ = r.Push("app.providers", i)
		}(i)
	}
	wg.Wait()
}
