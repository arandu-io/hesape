package validation_test

import (
	"encoding/json"
	"testing"

	"github.com/arandu-io/hesape/internal/enums"
	"github.com/arandu-io/hesape/jsonschema"
	"github.com/arandu-io/hesape/validation"
)

// TestTheRulesOfASetAreItsJSONSchema is one declaration read twice.
func TestTheRulesOfASetAreItsJSONSchema(t *testing.T) {
	set := mustCompile(t, validation.Rules{
		"name":         "required|string|min:2|max:80",
		"email":        []any{"required", "email", "max:255"},
		"age":          "nullable|integer|between:18,120",
		"price":        "required|numeric|min:0",
		"role":         "required|in:admin,member",
		"status":       []any{validation.EnumFrom(enums.InvoiceStatusValues)},
		"accepted":     "required|boolean",
		"zip":          "required|digits:8",
		"code":         "regex:^[A-Z]{3}$",
		"born":         "date_format:2006-01-02",
		"tags":         "array|max:5|distinct",
		"tags.*":       "string|max:10",
		"items":        "required|array|min:1",
		"items.*.id":   "required|uuid",
		"items.*.qty":  "required|integer|min:1",
		"address.city": "required|string",
		"avatar":       "image|max:2048",
		"other_field":  "required_with:name",
	})
	document := jsonschema.Document{Root: set.JSONSchema()}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	props := got["properties"].(map[string]any)

	field := func(name string) map[string]any { return props[name].(map[string]any) }

	for name, want := range map[string]map[string]any{
		"name":     {"type": "string", "minLength": 2.0, "maxLength": 80.0},
		"email":    {"type": "string", "format": "email", "maxLength": 255.0},
		"age":      {"type": []any{"integer", "null"}, "minimum": 18.0, "maximum": 120.0},
		"price":    {"type": "number", "minimum": 0.0},
		"role":     {"type": "string", "enum": []any{"admin", "member"}},
		"status":   {"type": "string"},
		"accepted": {"type": "boolean"},
		"zip":      {"type": "string", "pattern": "^[0-9]{8}$"},
		"code":     {"type": "string", "pattern": "^[A-Z]{3}$"},
		"born":     {"type": "string", "format": "date"},
		"avatar":   {"type": "string", "format": "binary"},
	} {
		for key, value := range want {
			if !equalJSON(field(name)[key], value) {
				t.Errorf("%s.%s = %v, want %v (%v)", name, key, field(name)[key], value, field(name))
			}
		}
	}

	if cases, _ := field("status")["enum"].([]any); len(cases) != 4 {
		t.Errorf("status = %v, want the four cases of the enum type", field("status"))
	}

	tags := field("tags")
	if tags["type"] != "array" || tags["maxItems"] != 5.0 || tags["uniqueItems"] != true {
		t.Errorf("tags = %v", tags)
	}
	if item := tags["items"].(map[string]any); item["type"] != "string" || item["maxLength"] != 10.0 {
		t.Errorf("tags.items = %v", tags["items"])
	}

	items := field("items")
	row := items["items"].(map[string]any)
	if items["minItems"] != 1.0 || row["type"] != "object" {
		t.Errorf("items = %v", items)
	}
	rowProps := row["properties"].(map[string]any)
	if rowProps["id"].(map[string]any)["format"] != "uuid" || rowProps["qty"].(map[string]any)["minimum"] != 1.0 {
		t.Errorf("items.* = %v", row)
	}
	if city := field("address")["properties"].(map[string]any)["city"].(map[string]any); city["type"] != "string" {
		t.Errorf("address.city = %v", city)
	}

	required := map[string]bool{}
	for _, name := range got["required"].([]any) {
		required[name.(string)] = true
	}
	for _, name := range []string{"name", "email", "price", "role", "accepted", "zip", "items"} {
		if !required[name] {
			t.Errorf("%s is not required in %v", name, got["required"])
		}
	}
	if required["age"] || required["tags"] || required["status"] || required["other_field"] {
		t.Errorf("required = %v", got["required"])
	}
}

func equalJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
