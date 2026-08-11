package resources

import (
	"encoding/json"
	"testing"
)

func assertHelper(t *testing.T) {
	t.Helper()
}

func assertEqual(t *testing.T, got, want any, msg string) {
	assertHelper(t)
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func assertNotNil(t *testing.T, v any, msg string) {
	assertHelper(t)
	if v == nil {
		t.Fatalf("%s: expected non-nil value", msg)
	}
}

// --- When / Conditional tests ---

func TestWhenTrueIncludesField(t *testing.T) {
	result := When(true, "present")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "When(true) should not be MissingValue")
	assertEqual(t, result, "present", "When(true) should return the value")
}

func TestWhenFalseOmitsField(t *testing.T) {
	result := When(false, "absent")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "When(false) should be MissingValue")
}

func TestUnlessTrueOmitsField(t *testing.T) {
	result := Unless(true, "absent")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "Unless(true) should be MissingValue")
}

func TestUnlessFalseIncludesField(t *testing.T) {
	result := Unless(false, "present")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "Unless(false) should not be MissingValue")
}

func TestWhenNotNullIncludesValue(t *testing.T) {
	result := WhenNotNull("hello")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenNotNull(hello) should not be MissingValue")
	assertEqual(t, result, "hello", "WhenNotNull should return value")
}

func TestWhenNotNullOmitsNil(t *testing.T) {
	result := WhenNotNull(nil)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "WhenNotNull(nil) should be MissingValue")
}

func TestWhenHasFound(t *testing.T) {
	data := map[string]any{"name": "Alice"}
	result := WhenHas(data, "name", nil)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenHas found should not be MissingValue")
}

func TestWhenHasNotFound(t *testing.T) {
	data := map[string]any{"name": "Alice"}
	result := WhenHas(data, "email", nil)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "WhenHas not found should be MissingValue")
}

func TestWhenLoadedTrue(t *testing.T) {
	result := WhenLoaded("posts", true, map[string]any{"count": 5})
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenLoaded true should not be MissingValue")
}

func TestWhenLoadedFalse(t *testing.T) {
	result := WhenLoaded("posts", false, map[string]any{"count": 5})
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "WhenLoaded false should be MissingValue")
}

func TestWhenCountedTrue(t *testing.T) {
	result := WhenCounted("replies", true, 42)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenCounted true should not be MissingValue")
}

func TestWhenCountedFalse(t *testing.T) {
	result := WhenCounted("replies", false, 42)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "WhenCounted false should be MissingValue")
}

func TestWhenAggregatedLoaded(t *testing.T) {
	result := WhenAggregated("orders", "amount", "sum", true, 150.0)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenAggregated loaded should not be MissingValue")
}

func TestWhenAggregatedNotLoaded(t *testing.T) {
	result := WhenAggregated("orders", "amount", "sum", false, 150.0)
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "WhenAggregated not loaded should be MissingValue")
}

func TestWhenAppended(t *testing.T) {
	result := WhenAppended("full_name", true, "Alice Smith")
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, false, "WhenAppended true should not be MissingValue")

	result2 := WhenAppended("full_name", false, "Alice Smith")
	_, isMissing2 := result2.(MissingValue)
	assertEqual(t, isMissing2, true, "WhenAppended false should be MissingValue")
}

// --- MergeValue tests ---

func TestMergeValue(t *testing.T) {
	mv := NewMergeValue(map[string]any{"extra": 1})
	assertNotNil(t, mv, "MergeValue")
	assertNotNil(t, mv.Data, "MergeValue data")
}

func TestMergeWhenTrue(t *testing.T) {
	result := MergeWhen(true, map[string]any{"extra": 1})
	mv, ok := result.(*MergeValue)
	assertEqual(t, ok, true, "MergeWhen true should return MergeValue")
	assertNotNil(t, mv, "MergeValue")
}

func TestMergeWhenFalse(t *testing.T) {
	result := MergeWhen(false, map[string]any{"extra": 1})
	_, ok := result.(MissingValue)
	assertEqual(t, ok, true, "MergeWhen false should return MissingValue")
}

// --- Filter tests ---

func TestFilterRemovesMissingValues(t *testing.T) {
	data := map[string]any{
		"name":  "Alice",
		"email": MissingValue{},
		"age":   When(false, 30),
	}
	result := Filter(data)

	_, hasName := result["name"]
	assertEqual(t, hasName, true, "name should be present")
	_, hasEmail := result["email"]
	assertEqual(t, hasEmail, false, "MissingValue should be removed")
	_, hasAge := result["age"]
	assertEqual(t, hasAge, false, "When(false) should be removed")
}

func TestFilterMergesMergeValues(t *testing.T) {
	data := map[string]any{
		"name": "Bob",
		"merge": NewMergeValue(map[string]any{"city": "NYC", "state": "NY"}),
	}
	result := Filter(data)

	assertEqual(t, result["name"], "Bob", "name")
	assertEqual(t, result["city"], "NYC", "merged city")
	assertEqual(t, result["state"], "NY", "merged state")
}

// --- JsonResource tests ---

type testUserResource struct {
	Name  string
	Email string
	Age   int
}

func (r *testUserResource) ToArray() map[string]any {
	return map[string]any{
		"name":  r.Name,
		"email": r.Email,
		"age":   When(r.Age > 0, r.Age),
	}
}

func (r *testUserResource) With() map[string]any {
	return nil
}

func TestJsonResourceToArray(t *testing.T) {
	user := &testUserResource{Name: "Alice", Email: "alice@example.com", Age: 30}
	data := user.ToArray()
	assertEqual(t, data["name"], "Alice", "name")
	assertEqual(t, data["email"], "alice@example.com", "email")
}

func TestJsonResourceConditionalFields(t *testing.T) {
	// Age 0 should be omitted.
	user := &testUserResource{Name: "Bob", Email: "bob@example.com", Age: 0}
	data := user.ToArray()
	_, hasAge := data["age"]
	// When(false) wraps in MissingValue, not directly in data.
	// The raw ToArray still has the key with a MissingValue.
	// Filter removes it.
	assertEqual(t, hasAge, true, "age key exists in raw ToArray")

	filtered := Filter(data)
	_, hasAgeFiltered := filtered["age"]
	assertEqual(t, hasAgeFiltered, false, "age should be filtered out")
}

func TestJsonResponseBuilder(t *testing.T) {
	user := &testUserResource{Name: "Alice", Email: "alice@example.com", Age: 30}
	builder := NewJsonResponse(user)
	builder.Additional(map[string]any{"version": "1.0"})

	body, err := builder.Build()
	assertNotNil(t, body, "build result")
	assertNotNil(t, err, "build error")
	assertEqual(t, err, nil, "build should not error")

	var parsed map[string]any
	json.Unmarshal(body, &parsed)

	data, ok := parsed["data"].(map[string]any)
	assertEqual(t, ok, true, "should have data key")
	assertEqual(t, data["name"], "Alice", "data.name")
	assertEqual(t, data["email"], "alice@example.com", "data.email")
	assertEqual(t, parsed["version"], "1.0", "additional version")
}

func TestJsonResponseBuilderWithoutWrapping(t *testing.T) {
	user := &testUserResource{Name: "Alice", Email: "alice@example.com", Age: 30}
	builder := NewJsonResponse(user)
	builder.WithoutWrapping()

	body, err := builder.Build()
	assertNotNil(t, body, "build result")
	assertEqual(t, err, nil, "build should not error")

	var parsed map[string]any
	json.Unmarshal(body, &parsed)

	// Without wrapping, data goes directly at top level.
	assertEqual(t, parsed["name"], "Alice", "name at top level")
	_, hasData := parsed["data"]
	assertEqual(t, hasData, false, "no data wrapper")
}

// --- ResourceCollection tests ---

func TestResourceCollection(t *testing.T) {
	users := []JsonResource{
		&testUserResource{Name: "Alice", Email: "a@example.com", Age: 30},
		&testUserResource{Name: "Bob", Email: "b@example.com", Age: 25},
	}

	collection := NewResourceCollection(users)
	assertEqual(t, collection.Count(), 2, "count")

	builder := NewJsonResponse(collection)
	body, err := builder.Build()
	assertNotNil(t, body, "build result")
	assertEqual(t, err, nil, "build should not error")

	var parsed map[string]any
	json.Unmarshal(body, &parsed)
	// ResourceCollection.ToArray returns {"data": [...]}, builder wraps again as {"data": {"data": [...]}}
	outer, ok := parsed["data"].(map[string]any)
	assertEqual(t, ok, true, "should have outer data wrapper")
	items, ok2 := outer["data"].([]any)
	assertEqual(t, ok2, true, "should have inner data array")
	assertEqual(t, len(items), 2, "items count")
}

// --- PaginatedResourceResponse tests ---

func TestPaginatedResourceResponse(t *testing.T) {
	users := []JsonResource{
		&testUserResource{Name: "Alice", Email: "a@example.com", Age: 30},
		&testUserResource{Name: "Bob", Email: "b@example.com", Age: 25},
	}
	collection := NewResourceCollection(users)
	paginated := NewPaginatedResourceResponse(collection, 1, 2, 10)

	result := paginated.ToArray()

	// Check meta.
	meta, ok := result["meta"].(map[string]any)
	assertEqual(t, ok, true, "should have meta")
	assertEqual(t, meta["current_page"], 1, "current_page")
	assertEqual(t, meta["per_page"], 2, "per_page")
	assertEqual(t, meta["total"], 10, "total")
	assertEqual(t, meta["last_page"], 5, "last_page")

	// Check links.
	links, ok := result["links"].(map[string]any)
	assertEqual(t, ok, true, "should have links")
	assertEqual(t, links["first"], nil, "first link")
	assertEqual(t, links["last"], nil, "last link")
}

func TestPaginatedResourceResponseLastPage(t *testing.T) {
	collection := NewResourceCollection(nil)
	// 11 items at 3 per page = 4 pages (3 full + 1 partial).
	paginated := NewPaginatedResourceResponse(collection, 1, 3, 11)

	result := paginated.ToArray()
	meta := result["meta"].(map[string]any)
	assertEqual(t, meta["last_page"], 4, "last_page with remainder")
}

// --- Transform tests ---

func TestTransform(t *testing.T) {
	result := Transform("hello", func(v any) any {
		return v.(string) + " world"
	})
	assertEqual(t, result, "hello world", "transform should apply callback")
}

func TestTransformMissingValue(t *testing.T) {
	result := Transform(MissingValue{}, func(v any) any {
		return "transformed"
	})
	_, isMissing := result.(MissingValue)
	assertEqual(t, isMissing, true, "transform MissingValue should stay MissingValue")
}