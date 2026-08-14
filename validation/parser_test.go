package validation

import (
	"reflect"
	"slices"
	"testing"
)

func TestParseSplitsARuleIntoItsNameAndParameters(t *testing.T) {
	cases := []struct {
		rule       string
		name       string
		parameters []string
	}{
		{"required", "required", nil},
		{"max:255", "max", []string{"255"}},
		{"between:2,30", "between", []string{"2", "30"}},
		{`in:"a,b",c`, "in", []string{"a,b", "c"}},
		{`regex:^a|b$`, "regex", []string{"^a|b$"}},
		{"int", "integer", nil},
		{"bool", "boolean", nil},
	}

	for _, c := range cases {
		name, parameters := Parse(c.rule)
		if name != c.name || !reflect.DeepEqual(parameters, c.parameters) {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", c.rule, name, parameters, c.name, c.parameters)
		}
	}
}

func TestExplodeSplitsEachChain(t *testing.T) {
	exploded := NewValidationRuleParser(Data{}).Explode(Rules{
		"name":  "required|max:255",
		"email": "required|email",
	})

	if got := exploded.Rules["name"]; !slices.Equal(got, []string{"required", "max:255"}) {
		t.Fatalf("got %v", got)
	}
	if !slices.Equal(exploded.Order, []string{"email", "name"}) {
		t.Fatalf("order: got %v", exploded.Order)
	}
}

func TestExplodeExpandsAWildcardAgainstTheDataThatWasSent(t *testing.T) {
	data := Data{"items": []any{
		Data{"price": "10"},
		Data{"price": "20"},
	}}

	exploded := NewValidationRuleParser(data).Explode(Rules{"items.*.price": "required|numeric"})

	if !slices.Equal(exploded.Order, []string{"items.0.price", "items.1.price"}) {
		t.Fatalf("order: got %v", exploded.Order)
	}
	if got := exploded.ImplicitAttributes["items.*.price"]; len(got) != 2 {
		t.Fatalf("implicit attributes: got %v", got)
	}
	if got := exploded.Rules["items.0.price"]; !slices.Equal(got, []string{"required", "numeric"}) {
		t.Fatalf("got %v", got)
	}
}

func TestGetLeadingExplicitAttributePathStopsAtTheFirstStar(t *testing.T) {
	cases := map[string]string{
		"foo.bar.*.baz": "foo.bar",
		"foo.*":         "foo",
		"foo":           "foo",
		"*":             "",
	}
	for attribute, want := range cases {
		if got := GetLeadingExplicitAttributePath(attribute); got != want {
			t.Errorf("%q: got %q, want %q", attribute, got, want)
		}
	}
}

func TestExtractDataFromPathTakesOnlyTheSliceItNames(t *testing.T) {
	data := Data{
		"user":  Data{"name": "Ana", "email": "ana@example.com"},
		"other": "left alone",
	}

	got := ExtractDataFromPath("user.name", data)

	nested, isNested := asData(got["user"])
	if !isNested || nested["name"] != "Ana" {
		t.Fatalf("got %v", got)
	}
	if _, taken := got["other"]; taken {
		t.Fatal("the rest of the request must not be walked")
	}
}

func TestInitializeAndGatherDataFlattensTheSliceAndTheWildcardKeys(t *testing.T) {
	data := Data{"items": []any{Data{"price": "10"}, Data{"price": "20"}}}

	gathered := InitializeAndGatherData("items.*.price", data)

	if gathered["items.0.price"] != "10" || gathered["items.1.price"] != "20" {
		t.Fatalf("got %v", gathered)
	}
}

func TestFilterConditionalRulesPicksTheBranchTheConditionChose(t *testing.T) {
	data := Data{"kind": "company"}

	filtered := FilterConditionalRules(map[string]any{
		"name": "required",
		"vat": When(
			func(d Data) bool { return d.Get("kind") == "company" },
			"required|digits:14",
			"nullable",
		),
	}, data)

	if filtered["vat"] != "required|digits:14" {
		t.Fatalf("got %q", filtered["vat"])
	}

	other := FilterConditionalRules(map[string]any{
		"vat": Unless(func(d Data) bool { return d.Get("kind") == "company" }, "required", "nullable"),
	}, data)

	if other["vat"] != "nullable" {
		t.Fatalf("unless: got %q", other["vat"])
	}
}

func TestForEachCompilesTheRulesOneMemberNeeds(t *testing.T) {
	nested := ForEach(func(value any, attribute string, data Data) Rules {
		return Rules{attribute: "required|numeric"}
	})

	exploded := nested.Compile("items.0.price", "10", Data{})

	if got := exploded.Rules["items.0.price"]; !slices.Equal(got, []string{"required", "numeric"}) {
		t.Fatalf("got %v", got)
	}
}

func TestMergeRulesJoinsChainsOnOneAttribute(t *testing.T) {
	parser := NewValidationRuleParser(Data{})

	merged := parser.MergeRules(Rules{"name": "required"}, "name", "max:255")

	if merged["name"] != "required|max:255" {
		t.Fatalf("got %q", merged["name"])
	}
}

// ---------------------------------------------------------------------------
// The rule objects.
// ---------------------------------------------------------------------------

func TestAClosureRuleFailsWithTheMessagesItAsksFor(t *testing.T) {
	rule := NewClosureValidationRule(func(attribute string, value any, fail func(string), v *Validator) {
		if value != "ok" {
			fail("must be ok")
		}
	})

	if !rule.Passes("field", "ok") {
		t.Fatal("a value the closure accepts must pass")
	}
	if rule.Passes("field", "no") {
		t.Fatal("a value the closure refuses must not pass")
	}
	if got := rule.Message(); len(got) != 1 || got[0] != "must be ok" {
		t.Fatalf("got %v", got)
	}
	if !rule.Failed {
		t.Fatal("Failed must record that fail was called")
	}
}

// even answers to a ValidationRule written the way one is written today.
type even struct{}

func (even) Validate(attribute string, value any, fail func(message string)) {
	if n, ok := numberOf(value); !ok || int(n)%2 != 0 {
		fail("The :attribute field must be even.")
	}
}

func TestAnInvokableRuleIsWrappedIntoTheRuleShape(t *testing.T) {
	rule := NewInvokableValidationRule(even{})

	if !rule.Passes("amount", "4") {
		t.Fatal("four is even")
	}
	if rule.Passes("amount", "5") {
		t.Fatal("five is not")
	}
	if got := rule.Message(); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if _, isRule := any(rule).(Rule); !isRule {
		t.Fatal("the wrapper must read as a Rule")
	}
	if rule.Invokable() == nil {
		t.Fatal("Invokable must answer with what it wraps")
	}
}

func TestARuleObjectRunThroughAfterPutsItsMessageOnTheField(t *testing.T) {
	v := Make(Data{"amount": "5"}, MustCompile(Rules{"amount": "required"}),
		WithCustomAttributes(map[string]string{"amount": "total"}))

	v.After(func(v *Validator) {
		v.ValidateUsingCustomRule("amount", v.GetValue("amount"), NewInvokableValidationRule(even{}))
	})

	if v.Passes() {
		t.Fatal("five must fail")
	}
	// The :attribute placeholder is filled by the same pipeline every other
	// message goes through, custom attribute name included.
	if got := v.Errors().First("amount"); got != "The total field must be even." {
		t.Fatalf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Factory.
// ---------------------------------------------------------------------------

func TestTheFactoryHandsEveryValidatorWhatItCarries(t *testing.T) {
	f := NewFactory(english)
	f.SetAttributeNames(map[string]string{"email": "e-mail"})

	v := f.Make(Data{"email": ""}, MustCompile(Rules{"email": "required"}))

	if got := v.Errors().First("email"); got != "The e-mail field is required." {
		t.Fatalf("got %q", got)
	}
}

func TestTheFactoryValidatesInOneCall(t *testing.T) {
	f := NewFactory(english)

	if _, err := f.Validate(Data{"name": ""}, MustCompile(Rules{"name": "required"})); err == nil {
		t.Fatal("want the exception")
	}
	if _, err := f.Validate(Data{"name": "Ana"}, MustCompile(Rules{"name": "required"})); err != nil {
		t.Fatalf("unexpected failure: %v", err)
	}
}

func TestExtendMakesANameRealForEverySetCompiledAfterIt(t *testing.T) {
	f := NewFactory(nil)
	f.Extend("even_number", func(v *Validator, attribute string, value any, parameters []string) bool {
		n, ok := numberOf(value)
		return ok && int(n)%2 == 0
	}, "must be an even number")

	t.Cleanup(func() { delete(specs, "even_number") })

	set, err := Compile(Rules{"amount": "required|even_number"})
	if err != nil {
		t.Fatalf("the extended rule must compile: %v", err)
	}

	if f.Make(Data{"amount": "5"}, set).Passes() {
		t.Fatal("five must fail")
	}
	if got := f.Make(Data{"amount": "5"}, set).Errors().First("amount"); got != "must be an even number" {
		t.Fatalf("got %q", got)
	}
	if !f.Make(Data{"amount": "4"}, set).Passes() {
		t.Fatal("four must pass")
	}
}

func TestExtendingATwiceTakenNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("two answers to one rule name must not boot")
		}
	}()

	Extend("required", func(v *Validator, attribute string, value any, parameters []string) bool {
		return true
	}, "")
}

func TestAFactoryReplacerReachesTheMessage(t *testing.T) {
	f := NewFactory(nil)
	f.Replacer("min", func(message, attribute, rule string, parameters []string, v *Validator) string {
		return "at least " + param(parameters, 0) + " of them"
	})

	v := f.Make(Data{"name": "a"}, MustCompile(Rules{"name": "min:5"}))

	if got := v.Errors().First("name"); got != "at least 5 of them" {
		t.Fatalf("got %q", got)
	}
}

// TestEveryDependentRuleIsARuleThisPackageHas: $dependentRules is written out
// by hand, and a name misspelled in it fails OPEN -- the rule keeps the literal
// "foo.*.baz", finds no field, and passes. The catalogue is the only thing that
// can prove the spelling.
func TestEveryDependentRuleIsARuleThisPackageHas(t *testing.T) {
	for _, name := range dependentRules {
		if _, known := specs[name]; !known {
			t.Errorf("dependent rule %q is not a rule this package has", name)
		}
	}
}

// TestEveryUploadedFileRuleIsARuleThisPackageHas, for the same reason: the list
// decides whether a truncated upload is reported as `uploaded` at all.
func TestEveryUploadedFileRuleIsARuleThisPackageHas(t *testing.T) {
	for _, name := range uploadedFileRules {
		if _, known := specs[name]; !known {
			t.Errorf("file rule %q is not a rule this package has", name)
		}
	}
}
