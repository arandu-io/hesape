package jsonschema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/jsonschema"
)

// TestAnObjectRendersItsPropertiesInDeclarationOrder.
//
// The document is what a producer reads, and a schema whose type keyword comes
// after its properties is a schema a person reads twice. A map would order the
// keys alphabetically, so the order is asserted rather than assumed.
func TestAnObjectRendersItsPropertiesInDeclarationOrder(t *testing.T) {
	schema := jsonschema.Object(
		jsonschema.Prop("status", jsonschema.String().
			Description("Which posts to list").
			Enum("published", "draft").
			Required()),
		jsonschema.Prop("limit", jsonschema.Integer().Min(1).Max(100)),
	)

	const want = `{"type":"object","properties":` +
		`{"status":{"type":"string","description":"Which posts to list","enum":["published","draft"]},` +
		`"limit":{"type":"integer","minimum":1,"maximum":100}},` +
		`"required":["status"],"additionalProperties":false}`

	if got := render(t, schema); got != want {
		t.Errorf("rendered\n %s\nwant\n %s", got, want)
	}
}

// TestAnObjectWithNoPropertiesStillDeclaresThem: a tool that takes no argument
// still has to say so, and a client reading inputSchema expects the key.
func TestAnObjectWithNoPropertiesStillDeclaresThem(t *testing.T) {
	const want = `{"type":"object","properties":{},"additionalProperties":false}`

	if got := render(t, jsonschema.Object()); got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}

// TestAnObjectClosesItself.
//
// additionalProperties is false in the document, not only in the validator: a
// producer refused for a reason the published schema does not state has been
// told nothing it can act on.
func TestAnObjectClosesItself(t *testing.T) {
	if got := render(t, jsonschema.Object()); !strings.Contains(got, `"additionalProperties":false`) {
		t.Errorf("the object does not say it is closed: %s", got)
	}
}

// TestRequiredIsHoistedIntoTheParent: it is declared on the property and it
// belongs to the object, which is where JSON Schema keeps it.
func TestRequiredIsHoistedIntoTheParent(t *testing.T) {
	schema := jsonschema.Object(
		jsonschema.Prop("a", jsonschema.String().Required()),
		jsonschema.Prop("b", jsonschema.String()),
	)

	got := render(t, schema)
	if !strings.Contains(got, `"required":["a"]`) {
		t.Errorf("required was not hoisted: %s", got)
	}
	if strings.Contains(got, `{"type":"string","required"`) {
		t.Errorf("required leaked into the property: %s", got)
	}
}

// TestEveryTypeRendersItsKeywords, one case per type, so a keyword that stops
// being written is a failure with a name.
func TestEveryTypeRendersItsKeywords(t *testing.T) {
	cases := []struct {
		name   string
		schema jsonschema.Type
		want   string
	}{
		{
			"string",
			jsonschema.String().Min(1).Max(8).Pattern("^[a-z]+$").Format("email").Title("Name"),
			`{"type":"string","title":"Name","minLength":1,"maxLength":8,"pattern":"^[a-z]+$","format":"email"}`,
		},
		{
			"integer",
			jsonschema.Integer().Min(2).Max(10).MultipleOf(2).Default(4),
			`{"type":"integer","minimum":2,"maximum":10,"multipleOf":2,"default":4}`,
		},
		{
			"number",
			jsonschema.Number().Min(0.5).Max(1.5),
			`{"type":"number","minimum":0.5,"maximum":1.5}`,
		},
		{
			"boolean",
			jsonschema.Boolean().Default(true),
			`{"type":"boolean","default":true}`,
		},
		{
			"array",
			jsonschema.Array().Items(jsonschema.String()).Min(1).Max(3).Unique(),
			`{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":3,"uniqueItems":true}`,
		},
		{
			"union",
			jsonschema.Union("string", "integer"),
			`{"type":["string","integer"]}`,
		},
		{
			"anyOf",
			jsonschema.AnyOf(jsonschema.String().Min(1), jsonschema.Integer()),
			`{"anyOf":[{"type":"string","minLength":1},{"type":"integer"}]}`,
		},
		{
			"nullable string",
			jsonschema.String().Nullable(),
			`{"type":["string","null"]}`,
		},
		{
			"nullable union",
			jsonschema.Union("string", "null"),
			`{"type":["string","null"]}`,
		},
		{
			"nullable anyOf",
			jsonschema.AnyOf(jsonschema.String()).Nullable(),
			`{"anyOf":[{"type":"string"},{"type":"null"}]}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := render(t, c.schema); got != c.want {
				t.Errorf("rendered\n %s\nwant\n %s", got, c.want)
			}
		})
	}
}

// TestAUnionDropsARepeatedKind: ["string","string"] is not a wider type, it is
// a typo, and it would be published as one.
func TestAUnionDropsARepeatedKind(t *testing.T) {
	if got := render(t, jsonschema.Union("string", "string")); got != `{"type":["string"]}` {
		t.Errorf("rendered %s", got)
	}
}

// TestAUnionOfAnUnsupportedKindPanics.
//
// A schema is written once, at start-up, from a literal. A kind that does not
// exist is a mistake in the program, and the loud failure is the cheap one.
func TestAUnionOfAnUnsupportedKindPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unsupported kind was accepted")
		}
	}()
	jsonschema.Union("stirng")
}

// TestPropertiesCanBeWalkedWithoutDecodingTheDocument: a caller that prints a
// schema should not have to render it and parse it back.
func TestPropertiesCanBeWalkedWithoutDecodingTheDocument(t *testing.T) {
	schema := jsonschema.Object(
		jsonschema.Prop("status", jsonschema.String()),
		jsonschema.Prop("limit", jsonschema.Integer()),
	)

	got := schema.Properties()
	if len(got) != 2 || got[0].Name != "status" || got[1].Name != "limit" {
		t.Fatalf("properties came back as %+v", got)
	}
	if _, ok := got[0].Type.(*jsonschema.StringType); !ok {
		t.Errorf("the first property is not the string it was declared as")
	}

	// The copy is what stops a reader from editing the schema by accident.
	got[0].Name = "edited"
	if schema.Properties()[0].Name != "status" {
		t.Error("editing the returned slice changed the schema")
	}
}

// TestADocumentRendersAsOneObject: the identity keywords and the root's own
// keywords side by side, which is the shape a schema file has.
func TestADocumentRendersAsOneObject(t *testing.T) {
	doc := jsonschema.Document{
		ID:   "https://example.test/module.schema.json",
		Root: jsonschema.Object(jsonschema.Prop("name", jsonschema.String().Required())),
		Examples: []any{
			map[string]any{"name": "invoice"},
		},
	}

	const want = `{"$schema":"` + jsonschema.Draft + `",` +
		`"$id":"https://example.test/module.schema.json",` +
		`"type":"object","properties":{"name":{"type":"string"}},` +
		`"required":["name"],"additionalProperties":false,` +
		`"examples":[{"name":"invoice"}]}`

	got := render(t, doc)
	if got != want {
		t.Errorf("rendered\n %s\nwant\n %s", got, want)
	}

	// It has to survive the indenter, because that is how it reaches a file.
	if _, err := json.MarshalIndent(doc, "", "  "); err != nil {
		t.Fatalf("indenting the document: %v", err)
	}
}

// TestADocumentWithoutARootIsAnError: an empty schema accepts everything, and
// publishing one by omission is the failure this refuses.
func TestADocumentWithoutARootIsAnError(t *testing.T) {
	if _, err := json.Marshal(jsonschema.Document{}); err == nil {
		t.Fatal("a document with no root type was rendered")
	}
}

// TestARenderedSchemaIsValidJSON, for every type, whatever the assembly does
// with braces.
func TestARenderedSchemaIsValidJSON(t *testing.T) {
	doc := jsonschema.Document{
		Root: jsonschema.Object(
			jsonschema.Prop("tags", jsonschema.Array().Items(jsonschema.String()).Required()),
			jsonschema.Prop("either", jsonschema.AnyOf(jsonschema.String(), jsonschema.Integer())),
			jsonschema.Prop("nested", jsonschema.Object(
				jsonschema.Prop("deep", jsonschema.Boolean()),
			)),
		),
	}

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, body)
	}
	if back["$schema"] != jsonschema.Draft {
		t.Errorf("the dialect is %v", back["$schema"])
	}
}

func render(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return string(body)
}
