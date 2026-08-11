package validation

import (
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/support"
)

func TestValidateReturnsTheValidationExceptionAControllerTurnsInto422(t *testing.T) {
	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}), WithTranslator(english))

	_, err := v.Validate()
	if err == nil {
		t.Fatal("a failed validation must not answer nil")
	}

	var invalid *ValidationException
	if !errors.As(err, &invalid) {
		t.Fatalf("want a *ValidationException, got %T", err)
	}
	if invalid.GetStatus() != 422 {
		t.Fatalf("status: got %d, want 422", invalid.GetStatus())
	}
	if invalid.GetErrorBag() != "default" {
		t.Fatalf("bag: got %q", invalid.GetErrorBag())
	}
	if got := invalid.Errors()["name"]; len(got) != 1 {
		t.Fatalf("errors: got %v", invalid.Errors())
	}
	if !strings.Contains(invalid.Error(), "required") {
		t.Fatalf("the summary must be the first message, got %q", invalid.Error())
	}
}

func TestTheSummaryCountsTheRest(t *testing.T) {
	v := Make(Data{"a": "", "b": "", "c": ""},
		MustCompile(Rules{"a": "required", "b": "required", "c": "required"}))

	_, err := v.Validate()

	if !strings.Contains(err.Error(), "(and 2 more errors)") {
		t.Fatalf("got %q", err.Error())
	}
}

func TestOneExtraFailureIsSingular(t *testing.T) {
	v := Make(Data{"a": "", "b": ""}, MustCompile(Rules{"a": "required", "b": "required"}))

	_, err := v.Validate()

	if !strings.Contains(err.Error(), "(and 1 more error)") {
		t.Fatalf("got %q", err.Error())
	}
}

func TestTheExceptionCarriesTheBagAndTheRedirect(t *testing.T) {
	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}))

	_, err := v.ValidateWithBag("register")

	var invalid *ValidationException
	errors.As(err, &invalid)

	if invalid.GetErrorBag() != "register" {
		t.Fatalf("bag: got %q", invalid.GetErrorBag())
	}

	invalid.Status(400).RedirectTo("/register")

	if invalid.GetStatus() != 400 || invalid.GetRedirectTo() != "/register" {
		t.Fatalf("got %d %q", invalid.GetStatus(), invalid.GetRedirectTo())
	}
}

func TestWithMessagesBuildsOneOutOfAPlainMap(t *testing.T) {
	e := WithMessages(map[string][]string{
		"email": {"that address is already registered"},
	})

	if got := e.Errors()["email"]; len(got) != 1 || got[0] != "that address is already registered" {
		t.Fatalf("got %v", e.Errors())
	}
	if e.GetStatus() != 422 {
		t.Fatalf("status: got %d", e.GetStatus())
	}
}

func TestSetExceptionReplacesWhatAFailureBecomes(t *testing.T) {
	sentinel := errors.New("refused")

	v := Make(Data{"name": ""}, MustCompile(Rules{"name": "required"}))
	v.SetException(func(*Validator) error { return sentinel })

	if _, err := v.Validate(); !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}

func TestValidatePassingAnswersWithTheValidatedInput(t *testing.T) {
	v := Make(Data{"name": "Ana", "extra": "ignored"}, MustCompile(Rules{"name": "required"}))

	validated, err := v.Validate()
	if err != nil {
		t.Fatalf("unexpected failure: %v", err)
	}
	if validated.String("name") != "Ana" {
		t.Fatalf("got %q", validated.String("name"))
	}
	if validated.Has("extra") {
		t.Fatal("a field nobody wrote a rule for must not be in the validated input")
	}
}

func TestSafeReadsTheValidatedInputAndOnlyTheKeysAsked(t *testing.T) {
	v := Make(Data{"name": "Ana", "email": "ana@example.com"},
		MustCompile(Rules{"name": "required", "email": "required|email"}))

	all, err := v.Safe()
	if err != nil {
		t.Fatalf("unexpected failure: %v", err)
	}
	if len(all.All()) != 2 {
		t.Fatalf("got %v", all.All())
	}

	only, err := v.Safe("name")
	if err != nil {
		t.Fatalf("unexpected failure: %v", err)
	}
	if len(only.All()) != 1 || only.Input("name") != "Ana" {
		t.Fatalf("got %v", only.All())
	}
}

func TestAfterRunsOnceEveryRuleHas(t *testing.T) {
	v := Make(Data{"name": "Ana"}, MustCompile(Rules{"name": "required"}))

	ran := 0
	v.After(func(v *Validator) {
		ran++
		v.AddFailure("name", "required", nil)
	})

	if v.Passes() {
		t.Fatal("an after hook that adds a failure must make the validator fail")
	}
	if ran != 1 {
		t.Fatalf("the hook ran %d times", ran)
	}
}

func TestAddRulesMergesAnotherSetIn(t *testing.T) {
	v := Make(Data{"name": "Ana"}, MustCompile(Rules{"name": "required"}))

	if !v.Passes() {
		t.Fatal("the first set passes")
	}

	v.AddRules(MustCompile(Rules{"name": "min:10"}))

	if v.Passes() {
		t.Fatal("the merged rule must be run")
	}
	if !v.HasRule("name", []string{"min"}) {
		t.Fatal("the merged rule must be visible to HasRule")
	}
}

func TestSometimesAddsRulesOnlyWhenTheCallbackSaysSo(t *testing.T) {
	set := MustCompile(Rules{"kind": "required", "vat": "required"})

	company := Make(Data{"kind": "company", "vat": ""}, set)
	company.Sometimes([]string{"vat"}, MustCompile(Rules{"vat": "digits:14"}),
		func(payload *support.Fluent, value any) bool { return payload.Get("kind") == "company" })

	if company.Passes() {
		t.Fatal("a company with no VAT number must fail")
	}

	person := Make(Data{"kind": "person", "vat": "1"}, MustCompile(Rules{"kind": "required"}))
	person.Sometimes([]string{"vat"}, MustCompile(Rules{"vat": "digits:14"}),
		func(payload *support.Fluent, value any) bool { return payload.Get("kind") == "company" })

	if !person.Passes() {
		t.Fatalf("a person must not be asked for one: %v", person.Errors())
	}
}

func TestGetRuleAnswersWithTheParametersItWasWrittenWith(t *testing.T) {
	v := Make(Data{}, MustCompile(Rules{"name": "required|between:2,30"}))

	name, parameters, held := v.GetRule("name", []string{"between"})
	if !held || name != "between" || len(parameters) != 2 || parameters[0] != "2" {
		t.Fatalf("got %q %v %v", name, parameters, held)
	}

	if _, _, held := v.GetRule("name", []string{"email"}); held {
		t.Fatal("a rule the field does not declare must not be found")
	}
}
