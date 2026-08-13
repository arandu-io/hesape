package jsonapi_test

import (
	"testing"

	"github.com/arandu-io/hesape/http/resources/jsonapi"
)

func TestAResourceObjectCarriesItsIdAndTypeThroughResolution(t *testing.T) {
	resource := jsonapi.NewJsonApiResource("7", "articles")
	resource.Attributes["title"] = "On Naming"
	resource.Links["self"] = "/articles/7"
	resource.Meta["read"] = true

	if got := resource.ToId(); got != "7" {
		t.Fatalf("ToId() = %q, want 7", got)
	}
	if got := resource.ToType(); got != "articles" {
		t.Fatalf("ToType() = %q, want articles", got)
	}
	if got := resource.ToAttributes()["title"]; got != "On Naming" {
		t.Fatalf("ToAttributes()[title] = %v, want On Naming", got)
	}
	if got := resource.ToLinks()["self"]; got != "/articles/7" {
		t.Fatalf("ToLinks()[self] = %v", got)
	}
	if got := resource.ToMeta()["read"]; got != true {
		t.Fatalf("ToMeta()[read] = %v, want true", got)
	}

	id, err := resource.ResolveResourceIdentifier(nil)
	if err != nil || id != "7" {
		t.Fatalf("ResolveResourceIdentifier() = (%q, %v), want (7, nil)", id, err)
	}
	resourceType, err := resource.ResolveResourceType(nil)
	if err != nil || resourceType != "articles" {
		t.Fatalf("ResolveResourceType() = (%q, %v), want (articles, nil)", resourceType, err)
	}
}

func TestAResourceObjectWithNothingToIdentifyItSaysSo(t *testing.T) {
	resource := jsonapi.NewJsonApiResource("", "")

	if _, err := resource.ResolveResourceIdentifier(nil); err == nil {
		t.Fatal("a resource with no id should not resolve one")
	}
	if _, err := resource.ResolveResourceType(nil); err == nil {
		t.Fatal("a resource with no type should not resolve one")
	}
}

func TestIncludedResourceObjectsAppearOncePerTypeAndId(t *testing.T) {
	resource := jsonapi.NewJsonApiResource("7", "articles")
	author := map[string]any{"type": "people", "id": "1"}
	resource.Relationships["author"] = map[string]any{"data": author}
	resource.Relationships["editor"] = map[string]any{"data": author}
	resource.Relationships["tags"] = map[string]any{"data": []map[string]any{
		{"type": "tags", "id": "a"},
		{"type": "tags", "id": "a"},
		{"type": "tags", "id": "b"},
	}}

	included := resource.ResolveIncludedResourceObjects(nil)
	if len(included) != 3 {
		t.Fatalf("included = %v, want three unique resource objects", included)
	}
}

func TestConfigureAndMaxRelationshipDepthAreForgottenByFlushState(t *testing.T) {
	t.Cleanup(jsonapi.FlushState)

	jsonapi.Configure("1.1", nil, []string{"https://example.test/profile"}, nil)
	information := jsonapi.JsonApiInformation()
	if information["version"] != "1.1" {
		t.Fatalf("version = %v, want 1.1", information["version"])
	}
	if _, present := information["ext"]; present {
		t.Fatal("an empty ext should not have been recorded")
	}

	jsonapi.MaxRelationshipDepth(-4)
	if got := jsonapi.CurrentMaxRelationshipDepth(); got != 0 {
		t.Fatalf("depth = %d, want a negative depth clamped to 0", got)
	}
	jsonapi.MaxRelationshipDepth(5)
	if got := jsonapi.CurrentMaxRelationshipDepth(); got != 5 {
		t.Fatalf("depth = %d, want 5", got)
	}

	jsonapi.FlushState()
	if len(jsonapi.JsonApiInformation()) != 0 {
		t.Fatal("FlushState should have forgotten the configuration")
	}
	if got := jsonapi.CurrentMaxRelationshipDepth(); got != jsonapi.DefaultMaxRelationshipDepth {
		t.Fatalf("depth = %d, want the default back", got)
	}
}

func TestTheQueryStringDrivesTheResourceUntilItIsToldNotTo(t *testing.T) {
	resource := jsonapi.NewJsonApiResource("7", "articles")

	if !resource.UsesRequestQueryString() {
		t.Fatal("a new resource should respect the query string")
	}
	resource.IgnoreFieldsAndIncludesInQueryString()
	if resource.UsesRequestQueryString() {
		t.Fatal("IgnoreFieldsAndIncludesInQueryString should have turned it off")
	}
	resource.RespectFieldsAndIncludesInQueryString(true)
	if !resource.UsesRequestQueryString() {
		t.Fatal("RespectFieldsAndIncludesInQueryString should have turned it back on")
	}

	if resource.IncludesPreviouslyLoadedRelationships() {
		t.Fatal("a new resource should not include previously loaded relationships")
	}
	resource.IncludePreviouslyLoadedRelationships()
	if !resource.IncludesPreviouslyLoadedRelationships() {
		t.Fatal("IncludePreviouslyLoadedRelationships should have turned it on")
	}
}
