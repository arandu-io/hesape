package jsonschema_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/jsonschema"
)

// tool is the shape both callers of this package start from: a flat object
// with a required enum and an optional bounded integer.
func tool() jsonschema.Type {
	return jsonschema.Object(
		jsonschema.Prop("status", jsonschema.String().
			Description("Which posts to list").
			Enum("published", "draft").
			Required()),
		jsonschema.Prop("limit", jsonschema.Integer().Min(1).Max(100)),
	)
}

// TestAValueThatMatchesIsAccepted, including the property that was left out.
func TestAValueThatMatchesIsAccepted(t *testing.T) {
	if err := jsonschema.Validate(tool(), decode(t, `{"status":"draft"}`)); err != nil {
		t.Fatalf("a valid value was refused: %v", err)
	}
	if err := jsonschema.Validate(tool(), decode(t, `{"status":"draft","limit":10}`)); err != nil {
		t.Fatalf("a valid value was refused: %v", err)
	}
}

// TestAMissingRequiredPropertyIsNamed: the point of validating before the
// handler runs is that the handler may then read the field without checking.
func TestAMissingRequiredPropertyIsNamed(t *testing.T) {
	err := jsonschema.Validate(tool(), decode(t, `{"limit":10}`))
	if err == nil {
		t.Fatal("a value missing a required property was accepted")
	}
	if !strings.Contains(err.Error(), "status is required") {
		t.Errorf("the refusal does not name the property: %v", err)
	}
}

// TestAWrongTypeAndAWrongEnumAreBothReported, in one answer.
//
// A producer told one mistake per attempt spends three attempts on a form it
// could have fixed on the second.
func TestAWrongTypeAndAWrongEnumAreBothReported(t *testing.T) {
	err := jsonschema.Validate(tool(), decode(t, `{"status":"archived","limit":"ten"}`))
	if err == nil {
		t.Fatal("a value with two bad properties was accepted")
	}
	for _, want := range []string{"status", "limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s is not mentioned: %v", want, err)
		}
	}

	var invalid *jsonschema.Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("the error is not an *Invalid: %T", err)
	}
	if len(invalid.Problems) != 2 {
		t.Fatalf("problems came back as %+v", invalid.Problems)
	}
	if invalid.Problems[0].Path != "status" || invalid.Problems[1].Path != "limit" {
		t.Errorf("the problems are not in declaration order: %+v", invalid.Problems)
	}
}

// TestAnUndeclaredPropertyIsRefusedRatherThanIgnored: a producer that invents
// one and is never told keeps inventing it.
func TestAnUndeclaredPropertyIsRefusedRatherThanIgnored(t *testing.T) {
	err := jsonschema.Validate(tool(), decode(t, `{"status":"draft","tenant":"acme"}`))
	if err == nil {
		t.Fatal("an undeclared property was accepted")
	}
	if !strings.Contains(err.Error(), "tenant is not a declared property") {
		t.Errorf("the refusal does not name the property: %v", err)
	}
}

// TestUndeclaredPropertiesAreReportedInAStableOrder.
//
// They come out of a map, whose iteration order changes between runs, and an
// error message that reads differently on two runs is one nobody can test.
func TestUndeclaredPropertiesAreReportedInAStableOrder(t *testing.T) {
	value := decode(t, `{"status":"draft","z":1,"a":1,"m":1}`)

	first := jsonschema.Validate(tool(), value)
	if first == nil {
		t.Fatal("undeclared properties were accepted")
	}
	for i := 0; i < 20; i++ {
		if again := jsonschema.Validate(tool(), value); again.Error() != first.Error() {
			t.Fatalf("the message changed between runs:\n %v\n %v", first, again)
		}
	}
	if want := "a is not a declared property"; !strings.HasPrefix(first.Error(), want) {
		t.Errorf("the names are not sorted: %v", first)
	}
}

// TestAProblemInsideANestedValueCarriesItsPath: "the object is wrong" is not
// something a producer can act on.
func TestAProblemInsideANestedValueCarriesItsPath(t *testing.T) {
	schema := jsonschema.Object(
		jsonschema.Prop("filters", jsonschema.Object(
			jsonschema.Prop("tag", jsonschema.String().Required()),
		)),
		jsonschema.Prop("tags", jsonschema.Array().Items(jsonschema.String())),
	)

	err := jsonschema.Validate(schema, decode(t, `{"filters":{},"tags":["a",2]}`))
	if err == nil {
		t.Fatal("a value with two nested problems was accepted")
	}
	for _, want := range []string{"filters.tag is required", "tags[1] must be a string"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q is missing from: %v", want, err)
		}
	}
}

// TestNullIsRefusedUnlessTheSchemaAllowsIt: a null that reaches a handler as a
// missing field is the same bug as a missing field nobody checked.
func TestNullIsRefusedUnlessTheSchemaAllowsIt(t *testing.T) {
	strict := jsonschema.Object(jsonschema.Prop("note", jsonschema.String()))
	if err := jsonschema.Validate(strict, decode(t, `{"note":null}`)); err == nil {
		t.Fatal("a null was accepted by a schema that does not allow it")
	}

	loose := jsonschema.Object(jsonschema.Prop("note", jsonschema.String().Nullable()))
	if err := jsonschema.Validate(loose, decode(t, `{"note":null}`)); err != nil {
		t.Fatalf("a null was refused by a schema that allows it: %v", err)
	}
}

// TestAWholeNumberIsAnIntegerAndAFractionIsNot.
//
// JSON has one number type, so 3 arrives as 3.0 and refusing it would refuse
// every integer ever sent.
func TestAWholeNumberIsAnIntegerAndAFractionIsNot(t *testing.T) {
	schema := jsonschema.Object(jsonschema.Prop("limit", jsonschema.Integer()))

	if err := jsonschema.Validate(schema, decode(t, `{"limit":3}`)); err != nil {
		t.Fatalf("a whole number was refused: %v", err)
	}
	err := jsonschema.Validate(schema, decode(t, `{"limit":3.5}`))
	if err == nil {
		t.Fatal("a fraction was accepted as an integer")
	}
	if !strings.Contains(err.Error(), "whole number") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// TestTheConstraintsRefuseWhatTheyDeclare, one case per keyword that Validate
// enforces, so a keyword that stops being checked is a failure with a name.
func TestTheConstraintsRefuseWhatTheyDeclare(t *testing.T) {
	cases := []struct {
		name   string
		schema jsonschema.Type
		value  string
		want   string
	}{
		{"too short", jsonschema.String().Min(2), `"a"`, "at least 2 characters"},
		{"long enough", jsonschema.String().Min(1), `"a"`, ""},
		{"too long", jsonschema.String().Max(1), `"ab"`, "at most 1 character"},
		{"counted in characters", jsonschema.String().Max(2), `"çã"`, ""},
		{"pattern", jsonschema.String().Pattern(`^[a-z]+$`), `"A1"`, "must match"},
		{"integer floor", jsonschema.Integer().Min(1), `0`, "at least 1"},
		{"integer ceiling", jsonschema.Integer().Max(10), `11`, "at most 10"},
		{"multiple of", jsonschema.Integer().MultipleOf(5), `7`, "multiple of 5"},
		{"number floor", jsonschema.Number().Min(0.5), `0.25`, "at least 0.5"},
		{"number ceiling", jsonschema.Number().Max(1.5), `2`, "at most 1.5"},
		{"boolean", jsonschema.Boolean(), `"yes"`, "true or false"},
		{"list", jsonschema.Array(), `{}`, "must be a list"},
		{"too few items", jsonschema.Array().Min(1), `[]`, "at least 1 item"},
		{"too many items", jsonschema.Array().Max(1), `[1,2]`, "at most 1 item"},
		{"repeated item", jsonschema.Array().Unique(), `["a","a"]`, "repeats an earlier item"},
		{"distinct items", jsonschema.Array().Unique(), `["a","b"]`, ""},
		{"object", jsonschema.Object(), `[]`, "must be an object"},
		{"integer enum", jsonschema.Integer().Enum(1, 2), `3`, "must be one of 1, 2"},
		{"number enum", jsonschema.Number().Enum(0.5), `0.5`, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := jsonschema.Validate(c.schema, decode(t, c.value))
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("a valid value was refused: %v", err)
			case c.want == "":
				return
			case err == nil:
				t.Fatalf("%s was accepted", c.value)
			case !strings.Contains(err.Error(), c.want):
				t.Errorf("the refusal reads %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestAUnionAcceptsAnyOfItsKinds and names them all when it refuses.
func TestAUnionAcceptsAnyOfItsKinds(t *testing.T) {
	schema := jsonschema.Union("string", "integer")

	for _, value := range []string{`"a"`, `3`} {
		if err := jsonschema.Validate(schema, decode(t, value)); err != nil {
			t.Errorf("%s was refused by the union: %v", value, err)
		}
	}

	err := jsonschema.Validate(schema, decode(t, `true`))
	if err == nil {
		t.Fatal("a boolean was accepted by a union of string and integer")
	}
	if !strings.Contains(err.Error(), "a string or an integer") {
		t.Errorf("the refusal does not name the kinds: %v", err)
	}
}

// TestAnyOfTakesTheFirstAlternativeThatMatches, and reports nothing from the
// ones that did not: a producer told why each of four shapes failed learns
// less than one told the value matched none of them.
func TestAnyOfTakesTheFirstAlternativeThatMatches(t *testing.T) {
	schema := jsonschema.AnyOf(
		jsonschema.String().Min(3),
		jsonschema.Integer().Min(10),
	)

	for _, value := range []string{`"abc"`, `12`} {
		if err := jsonschema.Validate(schema, decode(t, value)); err != nil {
			t.Errorf("%s was refused: %v", value, err)
		}
	}

	err := jsonschema.Validate(schema, decode(t, `"ab"`))
	if err == nil {
		t.Fatal("a value matching no alternative was accepted")
	}
	if got := err.Error(); got != "the value must match one of the allowed shapes" {
		t.Errorf("the refusal reads %q", got)
	}
}

// TestANilSchemaIsAnErrorAndNotAnAcceptance.
//
// A missing schema that validated everything would turn a wiring mistake into
// an open door, silently.
func TestANilSchemaIsAnErrorAndNotAnAcceptance(t *testing.T) {
	if err := jsonschema.Validate(nil, decode(t, `{"anything":1}`)); err == nil {
		t.Fatal("a nil schema accepted a value")
	}
}

// TestAProblemReadsAsASentenceEvenAtTheRoot, where there is no path to name.
func TestAProblemReadsAsASentenceEvenAtTheRoot(t *testing.T) {
	err := jsonschema.Validate(jsonschema.String(), decode(t, `1`))
	if err == nil {
		t.Fatal("a number was accepted as a string")
	}
	if got := err.Error(); got != "the value must be a string" {
		t.Errorf("the refusal reads %q", got)
	}
}

func decode(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return v
}
