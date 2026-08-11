package jsonschema

import (
	"encoding/json"
	"strings"
	"testing"
)

// decode reads a schema document the way a caller does, so the tests below run
// against exactly what encoding/json produces: every number a float64.
func decode(t *testing.T, document string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(document), &out); err != nil {
		t.Fatalf("the test document is not JSON: %v", err)
	}
	return out
}

// render marshals a type back to compact JSON, which is what the round-trip
// tests compare.
func render(t *testing.T, schema Type) string {
	t.Helper()
	body, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

func mustDeserialize(t *testing.T, document string) Type {
	t.Helper()
	schema, err := Deserialize(decode(t, document))
	if err != nil {
		t.Fatalf("deserialize %s: %v", document, err)
	}
	return schema
}

func TestDeserializeRoundTripsEveryKind(t *testing.T) {
	cases := []string{
		`{"type":"boolean"}`,
		`{"type":"string","minLength":3,"maxLength":9,"pattern":"^a+$","format":"email"}`,
		`{"type":"integer","minimum":1,"maximum":100,"multipleOf":5}`,
		`{"type":"number","minimum":0.5,"maximum":9.5,"multipleOf":0.25}`,
		`{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":4,"uniqueItems":true}`,
		`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`,
		`{"type":["string","integer"]}`,
		`{"type":["string","null"]}`,
		`{"type":["string","integer","null"]}`,
		`{"type":"string","title":"Tag","description":"A label","enum":["a","b"],"default":"a"}`,
		`{"type":"object","properties":{}}`,
	}

	for _, document := range cases {
		schema := mustDeserialize(t, document)
		if got := render(t, schema); got != document {
			t.Errorf("round trip\n got %s\nwant %s", got, document)
		}
	}
}

func TestDeserializeRoundTripsWhatSerializeWrote(t *testing.T) {
	original := Object(
		Prop("limit", Integer().Min(1).Max(100).Default(10)),
		Prop("status", String().Enum("draft", "published").Required()),
		Prop("tags", Array().Items(String()).Unique().Min(1)),
		Prop("score", Number().Min(0.5).Nullable()),
	).Description("A query")

	document, err := Serialize(original)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	back, err := Deserialize(document)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	// The properties come back ordered by name rather than in declaration
	// order, so the two renderings are compared as decoded documents.
	again, err := Serialize(back)
	if err != nil {
		t.Fatalf("serialize again: %v", err)
	}
	first, _ := json.Marshal(document)
	second, _ := json.Marshal(again)
	if string(first) != string(second) {
		t.Errorf("round trip changed the document\n got %s\nwant %s", second, first)
	}
}

func TestDeserializeSortsProperties(t *testing.T) {
	schema := mustDeserialize(t, `{"type":"object","properties":{"c":{"type":"string"},"a":{"type":"string"},"b":{"type":"string"}}}`)
	object, ok := schema.(*ObjectType)
	if !ok {
		t.Fatalf("got %T, want *ObjectType", schema)
	}
	var names []string
	for _, p := range object.Properties() {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "a,b,c" {
		t.Errorf("properties are %v, want them ordered by name", names)
	}
}

func TestDeserializeKeepsObjectsOpenUnlessTheDocumentClosesThem(t *testing.T) {
	// Object() closes by default; a document that never said so must not.
	open := mustDeserialize(t, `{"type":"object","properties":{"a":{"type":"string"}}}`)
	if open.(*ObjectType).closed() {
		t.Error("an object with no additionalProperties keyword came back closed")
	}
	if err := Validate(open, map[string]any{"a": "x", "extra": 1}); err != nil {
		t.Errorf("an open object refused an undeclared property: %v", err)
	}

	closed := mustDeserialize(t, `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`)
	if !closed.(*ObjectType).closed() {
		t.Error("additionalProperties:false did not close the object")
	}
	if err := Validate(closed, map[string]any{"a": "x", "extra": 1}); err == nil {
		t.Error("a closed object accepted an undeclared property")
	}

	// true is not false, so the object stays open and the keyword is dropped.
	permissive := mustDeserialize(t, `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":true}`)
	if permissive.(*ObjectType).closed() {
		t.Error("additionalProperties:true closed the object")
	}
}

func TestDeserializeMarksRequiredProperties(t *testing.T) {
	schema := mustDeserialize(t, `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["b"]}`)
	if err := Validate(schema, map[string]any{"a": "x"}); err == nil {
		t.Fatal("a missing required property was accepted")
	} else if !strings.Contains(err.Error(), "b is required") {
		t.Errorf("got %q, want it to name b", err)
	}
	if err := Validate(schema, map[string]any{"b": "x"}); err != nil {
		t.Errorf("an absent optional property was refused: %v", err)
	}
}

func TestDeserializeInfersTypeFromShape(t *testing.T) {
	cases := map[string]string{
		`{"properties":{"a":{"type":"string"}}}`: "object",
		`{"additionalProperties":false}`:         "object",
		`{"required":["a"]}`:                     "object",
		`{"items":{"type":"string"}}`:            "array",
		`{"minItems":1}`:                         "array",
		`{"uniqueItems":true}`:                   "array",
		`{"minLength":1}`:                        "string",
		`{"pattern":"^a$"}`:                      "string",
		`{"format":"email"}`:                     "string",
		`{"minimum":1}`:                          "number",
		`{"multipleOf":2}`:                       "number",
		`{"enum":["a","b"]}`:                     "string",
		`{"enum":[true,false]}`:                  "boolean",
		`{"enum":[1,2]}`:                         "integer",
		// Whole and fractional together are numeric, which is the one widening
		// the PHP allows.
		`{"enum":[1,2.5]}`: "number",
		`{"enum":[1.5]}`:   "number",
	}

	for document, want := range cases {
		schema := mustDeserialize(t, document)
		body := render(t, schema)
		if !strings.HasPrefix(body, `{"type":"`+want+`"`) {
			t.Errorf("%s inferred %s, want type %s", document, body, want)
		}
	}
}

func TestDeserializeRefusesASchemaWithNoTypeToInfer(t *testing.T) {
	for _, document := range []string{
		`{}`,
		`{"title":"Nameless"}`,
		`{"type":null}`,
		`{"type":5}`,
		`{"type":["null"]}`,
		`{"enum":["a",1]}`,
		`{"enum":[{"a":1}]}`,
		`{"enum":[]}`,
	} {
		if _, err := Deserialize(decode(t, document)); err == nil {
			t.Errorf("%s deserialized, want an error", document)
		} else if !strings.Contains(err.Error(), "unable to determine") {
			t.Errorf("%s: got %q, want the undetermined-type message", document, err)
		}
	}
}

func TestDeserializeNilAndEmptyAreTheSameError(t *testing.T) {
	if _, err := Deserialize(nil); err == nil {
		t.Error("a nil schema deserialized")
	}
	if _, err := FromArray(nil); err == nil {
		t.Error("FromArray accepted a nil schema")
	}
}

func TestFromArrayIsDeserialize(t *testing.T) {
	document := decode(t, `{"type":"string","minLength":2}`)
	schema, err := FromArray(document)
	if err != nil {
		t.Fatalf("FromArray: %v", err)
	}
	if got := render(t, schema); got != `{"type":"string","minLength":2}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeUnionRefusesTypeSpecificKeywords(t *testing.T) {
	_, err := Deserialize(decode(t, `{"type":["string","integer"],"minLength":3,"minimum":1}`))
	if err == nil {
		t.Fatal("a union carrying minLength deserialized")
	}
	// The keywords are named in the order the PHP lists them, not in map order.
	if !strings.Contains(err.Error(), "[minLength, minimum]") {
		t.Errorf("got %q, want both keywords in list order", err)
	}
}

func TestDeserializeUnionDropsDuplicatesAndRefusesUnknownNames(t *testing.T) {
	schema := mustDeserialize(t, `{"type":["string","integer","string"]}`)
	if got := render(t, schema); got != `{"type":["string","integer"]}` {
		t.Errorf("got %s, want the duplicate dropped", got)
	}

	if _, err := Deserialize(decode(t, `{"type":["string","date"]}`)); err == nil {
		t.Error("a union naming an unsupported kind deserialized")
	}
}

func TestDeserializeCollapsesANullableAnyOf(t *testing.T) {
	for _, key := range []string{"anyOf", "oneOf"} {
		document := `{"` + key + `":[{"type":"string","minLength":2},{"type":"null"}]}`
		schema := mustDeserialize(t, document)
		if got := render(t, schema); got != `{"type":["string","null"],"minLength":2}` {
			t.Errorf("%s collapsed to %s", key, got)
		}
		if err := Validate(schema, nil); err != nil {
			t.Errorf("%s: the collapsed type refused null: %v", key, err)
		}
	}

	// The null branch may also be written as a one-element list.
	schema := mustDeserialize(t, `{"anyOf":[{"type":"integer"},{"type":["null"]}]}`)
	if got := render(t, schema); got != `{"type":["integer","null"]}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeRefusesAnAnyOfThatIsNotJustNullable(t *testing.T) {
	for _, document := range []string{
		`{"anyOf":[{"type":"string"},{"type":"integer"}]}`,
		`{"anyOf":[{"type":"string"}]}`,
		`{"anyOf":[{"type":"null"}]}`,
		`{"anyOf":[{"type":"string"},{"type":"integer"},{"type":"null"}]}`,
	} {
		if _, err := Deserialize(decode(t, document)); err == nil {
			t.Errorf("%s deserialized, want an error", document)
		}
	}
}

func TestDeserializeMergesAnyOfSiblingsAndRefusesConflicts(t *testing.T) {
	schema := mustDeserialize(t, `{"description":"A tag","anyOf":[{"type":"string"},{"type":"null"}]}`)
	if got := render(t, schema); got != `{"type":["string","null"],"description":"A tag"}` {
		t.Errorf("got %s, want the sibling description kept", got)
	}

	// A sibling that agrees with the branch is not a conflict.
	if _, err := Deserialize(decode(t, `{"title":"T","anyOf":[{"type":"string","title":"T"},{"type":"null"}]}`)); err != nil {
		t.Errorf("an agreeing sibling was refused: %v", err)
	}

	_, err := Deserialize(decode(t, `{"title":"Outer","anyOf":[{"type":"string","title":"Inner"},{"type":"null"}]}`))
	if err == nil {
		t.Fatal("a conflicting sibling deserialized")
	}
	if !strings.Contains(err.Error(), "conflicting [title]") {
		t.Errorf("got %q, want it to name title", err)
	}
}

func TestDeserializeResolvesLocalRefs(t *testing.T) {
	document := `{
		"$defs": {"tag": {"type":"string","minLength":2}},
		"type":"object",
		"properties": {"a": {"$ref":"#/$defs/tag"}}
	}`
	schema := mustDeserialize(t, document)
	if got := render(t, schema); !strings.Contains(got, `"a":{"type":"string","minLength":2}`) {
		t.Errorf("got %s, want the reference expanded", got)
	}
}

func TestDeserializeLetsRefSiblingsWinOverTheTarget(t *testing.T) {
	document := `{
		"$defs": {"tag": {"type":"string","description":"from the definition","minLength":2}},
		"$ref": "#/$defs/tag",
		"description": "from the use site"
	}`
	schema := mustDeserialize(t, document)
	if got := render(t, schema); got != `{"type":"string","description":"from the use site","minLength":2}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeFollowsAChainOfRefs(t *testing.T) {
	document := `{
		"$defs": {"a": {"$ref":"#/$defs/b"}, "b": {"type":"integer","minimum":3}},
		"$ref": "#/$defs/a"
	}`
	schema := mustDeserialize(t, document)
	if got := render(t, schema); got != `{"type":"integer","minimum":3}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeRefusesACircularRef(t *testing.T) {
	document := `{"$defs":{"a":{"$ref":"#/$defs/b"},"b":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`
	_, err := Deserialize(decode(t, document))
	if err == nil {
		t.Fatal("a circular reference deserialized")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("got %q, want the circular message", err)
	}

	// A reference to the whole document is circular too, and must not recurse
	// forever before saying so.
	if _, err := Deserialize(decode(t, `{"$ref":"#"}`)); err == nil {
		t.Error("a self reference deserialized")
	}
}

func TestDeserializeRefusesUnreachableRefs(t *testing.T) {
	cases := map[string]string{
		`{"$ref":"https://example.test/s.json"}`:         "non-local",
		`{"$ref":"tag.json"}`:                            "non-local",
		`{"$defs":{},"$ref":"#/$defs/missing"}`:          "unable to resolve",
		`{"$defs":{"a":"text"},"$ref":"#/$defs/a"}`:      "does not point to a schema",
		`{"$defs":{"a":{"b":1}},"$ref":"#/$defs/a/b"}`:   "does not point to a schema",
		`{"$defs":{"a":{}},"$ref":"#/$defs/a/deeper"}`:   "unable to resolve",
		`{"$defs":[{"type":"string"}],"$ref":"#/$defs"}`: "does not point to a schema",
	}
	for document, want := range cases {
		_, err := Deserialize(decode(t, document))
		if err == nil {
			t.Errorf("%s deserialized, want an error", document)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %q, want %q", document, err, want)
		}
	}
}

func TestDeserializeIndexesListsAndUnescapesPointerSegments(t *testing.T) {
	// A pointer may walk into a list, because a PHP array is both shapes.
	schema := mustDeserialize(t, `{"$defs":{"all":[{"type":"boolean"},{"type":"integer"}]},"$ref":"#/$defs/all/1"}`)
	if got := render(t, schema); got != `{"type":"integer"}` {
		t.Errorf("list pointer got %s", got)
	}

	// ~1 is a slash, ~0 is a tilde, and %25 is a percent -- decoded in that
	// order, so ~01 reads as the literal ~1 rather than as a slash.
	cases := map[string]string{
		`{"a/b":{"type":"string"}}`:  "#/a~1b",
		`{"a~b":{"type":"string"}}`:  "#/a~0b",
		`{"a~1b":{"type":"string"}}`: "#/a~01b",
		`{"a b":{"type":"string"}}`:  "#/a%20b",
	}
	for defs, ref := range cases {
		document := `{"$defs":` + defs + `,"$ref":"` + strings.Replace(ref, "#/", "#/$defs/", 1) + `"}`
		schema, err := Deserialize(decode(t, document))
		if err != nil {
			t.Errorf("%s: %v", document, err)
			continue
		}
		if got := render(t, schema); got != `{"type":"string"}` {
			t.Errorf("%s got %s", document, got)
		}
	}
}

func TestDeserializeRefusesADocumentThatExpandsPastTheNodeLimit(t *testing.T) {
	// A reference bomb: each definition expands the one below it twice, so the
	// document is small and its expansion is not. The reference cache does not
	// help, because every expansion is its own node.
	var defs strings.Builder
	defs.WriteString(`{"$defs":{"d0":{"type":"string"}`)
	for i := 1; i <= 20; i++ {
		prev := i - 1
		defs.WriteString(`,"d`)
		defs.WriteString(itoa(i))
		defs.WriteString(`":{"type":"object","properties":{"a":{"$ref":"#/$defs/d`)
		defs.WriteString(itoa(prev))
		defs.WriteString(`"},"b":{"$ref":"#/$defs/d`)
		defs.WriteString(itoa(prev))
		defs.WriteString(`"}}}`)
	}
	defs.WriteString(`},"$ref":"#/$defs/d20"}`)

	_, err := Deserialize(decode(t, defs.String()))
	if err == nil {
		t.Fatal("the reference bomb deserialized")
	}
	if !strings.Contains(err.Error(), "too large to deserialize") {
		t.Errorf("got %q, want the node-limit message", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestDeserializeRefusesABooleanPropertySchema(t *testing.T) {
	_, err := Deserialize(decode(t, `{"type":"object","properties":{"a":true}}`))
	if err == nil {
		t.Fatal("a boolean property schema deserialized")
	}
	if !strings.Contains(err.Error(), "[a]") || !strings.Contains(err.Error(), "boolean schemas") {
		t.Errorf("got %q, want it to name the property", err)
	}
}

func TestDeserializeRefusesTupleAndBooleanItems(t *testing.T) {
	for _, document := range []string{
		`{"type":"array","items":[{"type":"string"}]}`,
		`{"type":"array","items":true}`,
		`{"type":"array","items":"string"}`,
	} {
		if _, err := Deserialize(decode(t, document)); err == nil {
			t.Errorf("%s deserialized, want an error", document)
		}
	}
}

func TestDeserializeSkipsEmptyItems(t *testing.T) {
	// json_decode gives PHP the same empty array for [] and {}, and the guard
	// compares against it before deciding the value is a tuple.
	for _, document := range []string{
		`{"type":"array","items":[]}`,
		`{"type":"array","items":{}}`,
		`{"type":"array","items":null}`,
	} {
		schema := mustDeserialize(t, document)
		if got := render(t, schema); got != `{"type":"array"}` {
			t.Errorf("%s got %s, want no items", document, got)
		}
	}
}

func TestDeserializeReadsUniqueItemsAsABoolCast(t *testing.T) {
	cases := map[string]bool{
		`{"type":"array","uniqueItems":true}`:  true,
		`{"type":"array","uniqueItems":false}`: false,
		`{"type":"array","uniqueItems":1}`:     true,
		`{"type":"array","uniqueItems":0}`:     false,
		`{"type":"array","uniqueItems":""}`:    false,
		`{"type":"array","uniqueItems":"0"}`:   false,
		`{"type":"array","uniqueItems":"yes"}`: true,
	}
	for document, want := range cases {
		schema := mustDeserialize(t, document)
		if got := schema.(*ArrayType).unique; got != want {
			t.Errorf("%s: unique is %v, want %v", document, got, want)
		}
	}
}

func TestDeserializeRefusesANullDefault(t *testing.T) {
	_, err := Deserialize(decode(t, `{"type":"string","default":null}`))
	if err == nil {
		t.Fatal(`"default": null deserialized`)
	}
	if !strings.Contains(err.Error(), "null JSON Schema [default]") {
		t.Errorf("got %q", err)
	}

	// An absent default is not the same thing and must not fail.
	if _, err := Deserialize(decode(t, `{"type":"string"}`)); err != nil {
		t.Errorf("an absent default failed: %v", err)
	}
	// A falsy default is a default.
	schema := mustDeserialize(t, `{"type":"boolean","default":false}`)
	if got := render(t, schema); got != `{"type":"boolean","default":false}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeNumericBounds(t *testing.T) {
	// A quoted bound counts, which is the is_numeric branch.
	schema := mustDeserialize(t, `{"type":"integer","minimum":"3","maximum":"10"}`)
	if got := render(t, schema); got != `{"type":"integer","minimum":3,"maximum":10}` {
		t.Errorf("got %s", got)
	}

	// A whole-number float is a whole number, which is what a JSON decoder
	// hands over for every integer bound.
	if _, err := Deserialize(decode(t, `{"type":"integer","minimum":3.0}`)); err != nil {
		t.Errorf("a whole float bound failed: %v", err)
	}

	_, err := Deserialize(decode(t, `{"type":"integer","minimum":1.5}`))
	if err == nil {
		t.Fatal("a fractional integer bound deserialized")
	}
	if !strings.Contains(err.Error(), "[1.5]") {
		t.Errorf("got %q, want it to name the value", err)
	}

	// A number keeps its fraction.
	if got := render(t, mustDeserialize(t, `{"type":"number","minimum":1.5}`)); got != `{"type":"number","minimum":1.5}` {
		t.Errorf("got %s", got)
	}

	for _, document := range []string{
		`{"type":"integer","minimum":true}`,
		`{"type":"number","maximum":"many"}`,
		`{"type":"number","multipleOf":{"a":1}}`,
	} {
		if _, err := Deserialize(decode(t, document)); err == nil {
			t.Errorf("%s deserialized, want an error", document)
		} else if !strings.Contains(err.Error(), "must be a number") {
			t.Errorf("%s: got %q", document, err)
		}
	}

	// A bound past the width of an int is refused rather than truncated into
	// whichever number the conversion happens to produce.
	if _, err := Deserialize(decode(t, `{"type":"integer","maximum":1e300}`)); err == nil {
		t.Error("an out-of-range integer bound deserialized")
	}
}

func TestDeserializeRefusesAPatternItCannotCompile(t *testing.T) {
	_, err := Deserialize(decode(t, `{"type":"string","pattern":"a(b"}`))
	if err == nil {
		t.Fatal("a broken pattern deserialized")
	}
	if !strings.Contains(err.Error(), "[pattern]") {
		t.Errorf("got %q", err)
	}
}

func TestDeserializeCastsLabelsAndBounds(t *testing.T) {
	// PHP casts these rather than requiring a string or an int, and a schema
	// written by hand quotes numbers and numbers strings often enough.
	schema := mustDeserialize(t, `{"type":"string","title":2,"description":true,"minLength":"4","maxLength":9.7}`)
	if got := render(t, schema); got != `{"type":"string","title":"2","description":"1","minLength":4,"maxLength":9}` {
		t.Errorf("got %s", got)
	}
}

func TestDeserializeKeepsEnumAndValidatesAgainstIt(t *testing.T) {
	schema := mustDeserialize(t, `{"type":"string","enum":["draft","published"]}`)
	if err := Validate(schema, "draft"); err != nil {
		t.Errorf("an allowed value was refused: %v", err)
	}
	if err := Validate(schema, "archived"); err == nil {
		t.Error("a value outside the enum was accepted")
	}
}

func TestDeserializeAppliesNullabilityThroughRefsAndUnions(t *testing.T) {
	document := `{
		"$defs": {"tag": {"anyOf": [{"type":"string"}, {"type":"null"}]}},
		"type": "object",
		"properties": {"a": {"$ref": "#/$defs/tag"}},
		"additionalProperties": false
	}`
	schema := mustDeserialize(t, document)
	if err := Validate(schema, map[string]any{"a": nil}); err != nil {
		t.Errorf("a null was refused by a nullable property: %v", err)
	}
	if err := Validate(schema, map[string]any{"a": 5}); err == nil {
		t.Error("a number was accepted by a nullable string")
	}
}

func TestDeserializeReusesARefWithoutReportingACycle(t *testing.T) {
	// The same definition used twice as siblings is not a cycle: each branch
	// carries its own trail.
	document := `{
		"$defs": {"tag": {"type":"string"}},
		"type":"object",
		"properties": {"a": {"$ref":"#/$defs/tag"}, "b": {"$ref":"#/$defs/tag"}}
	}`
	schema := mustDeserialize(t, document)
	if err := Validate(schema, map[string]any{"a": "x", "b": "y"}); err != nil {
		t.Errorf("a reused definition failed: %v", err)
	}
}

func TestDeserializeValidatesWhatTheDocumentDescribed(t *testing.T) {
	document := `{
		"type": "object",
		"properties": {
			"limit": {"type":"integer","minimum":1,"maximum":100},
			"status": {"type":"string","enum":["draft","published"]},
			"tags": {"type":"array","items":{"type":"string"},"minItems":1}
		},
		"required": ["status"],
		"additionalProperties": false
	}`
	schema := mustDeserialize(t, document)

	if err := Validate(schema, map[string]any{"status": "draft", "limit": 5.0, "tags": []any{"a"}}); err != nil {
		t.Errorf("a good value was refused: %v", err)
	}

	err := Validate(schema, map[string]any{"limit": 0.0, "tags": []any{}, "nope": 1})
	if err == nil {
		t.Fatal("a bad value was accepted")
	}
	for _, want := range []string{"status is required", "limit must be at least 1", "tags must have at least 1 item", "nope is not a declared property"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("got %q, want it to contain %q", err, want)
		}
	}
}
