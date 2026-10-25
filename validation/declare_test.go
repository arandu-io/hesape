package validation_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/internal/enums"
	"github.com/arandu-io/hesape/validation"
)

// TestTheListFormMeansWhatThePipeFormMeans. The two ways of writing a field's
// rules are one set of rules: the same refusals, in the same order, with the same
// sentences.
func TestTheListFormMeansWhatThePipeFormMeans(t *testing.T) {
	forms := []struct {
		name string
		rule any
	}{
		{"pipe", "required|string|min:3|max:5"},
		{"strings", []string{"required", "string", "min:3", "max:5"}},
		{"any", []any{"required", "string", "min:3", "max:5"}},
	}

	for _, value := range []any{"", "ab", "abcd", "abcdef", 12} {
		var want validation.Errors
		for i, form := range forms {
			set := mustCompile(t, validation.Rules{"f": form.rule})
			errs := runSet(set, validation.Data{"f": value})
			if i == 0 {
				want = errs
				continue
			}
			if !reflect.DeepEqual(want, errs) {
				t.Errorf("value %#v: %s form gave %v, pipe form gave %v", value, form.name, errs, want)
			}
		}
	}
}

// TestAListElementIsNeverSplitAtAPipe is what lets a pattern hold one.
func TestAListElementIsNeverSplitAtAPipe(t *testing.T) {
	set := mustCompile(t, validation.Rules{"f": []any{"required", "regex:^(a|b)$", "max:1"}})
	if errs := runSet(set, validation.Data{"f": "b"}); errs.Any() {
		t.Errorf("b was refused: %v", errs)
	}
	if errs := runSet(set, validation.Data{"f": "c"}); !errs.Any() {
		t.Error("c was accepted")
	}

	_, err := validation.Compile(validation.Rules{"f": []any{"required|email"}})
	if err == nil || !strings.Contains(err.Error(), "one rule per element") {
		t.Errorf("a pipe inside a list element = %v, want a failure that says how to write it", err)
	}
}

// TestARuleThatIsNotARuleDoesNotCompile.
func TestARuleThatIsNotARuleDoesNotCompile(t *testing.T) {
	for _, bad := range []any{[]any{42}, []any{"required", struct{}{}}, 3.5} {
		if _, err := validation.Compile(validation.Rules{"f": bad}); err == nil {
			t.Errorf("%#v compiled", bad)
		}
	}
}

type onlyAda struct{ last string }

func (r *onlyAda) Passes(attribute string, value any) bool {
	r.last = attribute
	return value == "Ada"
}
func (r *onlyAda) Message() []string { return []string{"The :attribute field is not Ada."} }

type lengthRule struct{}

func (lengthRule) Validate(attribute string, value any, fail func(string)) {
	if s, _ := value.(string); len(s) != 3 {
		fail("The :attribute field must be three letters.")
		fail("Second complaint about :attribute.")
	}
}

// TestRuleObjectsClosuresAndBuildersStandInInTheList.
func TestRuleObjectsClosuresAndBuildersStandInInTheList(t *testing.T) {
	closure := func(attribute string, value any, fail func(message string)) {
		if value == "bad" {
			fail("The :attribute field is bad.")
		}
	}
	withValidator := func(attribute string, value any, fail func(string), v *validation.Validator) {
		if v.GetValue("other") == "x" {
			fail("Sees the request.")
		}
	}

	cases := []struct {
		name  string
		rule  any
		data  validation.Data
		wants []string
	}{
		{"a Rule passes", []any{"required", &onlyAda{}}, validation.Data{"f": "Ada"}, nil},
		{"a Rule fails", []any{"required", &onlyAda{}}, validation.Data{"f": "Bob"}, []string{"The f field is not Ada."}},
		{"a ValidationRule says it twice", []any{lengthRule{}}, validation.Data{"f": "ab"},
			[]string{"The f field must be three letters.", "Second complaint about f."}},
		{"a closure", []any{closure}, validation.Data{"f": "bad"}, []string{"The f field is bad."}},
		{"a closure that passes", []any{closure}, validation.Data{"f": "good"}, nil},
		{"a closure that sees the validator", []any{withValidator}, validation.Data{"f": "a", "other": "x"}, []string{"Sees the request."}},
		{"one rule written bare", closure, validation.Data{"f": "bad"}, []string{"The f field is bad."}},
		{"a builder is the rules it spells", []any{validation.NewIn("a", "b"), "required"}, validation.Data{"f": "z"}, []string{"is not one of the allowed values"}},
		{"a builder that spells a chain", []any{validation.NewNumeric().Min(5)}, validation.Data{"f": "3"}, []string{"must be at least 5"}},
		{"an enum read off the type", []any{validation.EnumFrom(enums.InvoiceStatusValues)}, validation.Data{"f": "nope"}, []string{"The selected f is invalid."}},
		{"an enum case", []any{validation.EnumFrom(enums.InvoiceStatusValues)}, validation.Data{"f": "paid"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := runSet(mustCompile(t, validation.Rules{"f": c.rule}), c.data)
			if !reflect.DeepEqual([]string(errs.Get("f")), c.wants) && !(len(errs.Get("f")) == 0 && len(c.wants) == 0) {
				t.Errorf("f = %v, want %v", errs.Get("f"), c.wants)
			}
		})
	}
}

// TestARuleObjectIsSharedByEveryRequestAndStaysCorrect runs one compiled set from
// many goroutines. The race detector is the assertion.
func TestARuleObjectIsSharedByEveryRequestAndStaysCorrect(t *testing.T) {
	set := mustCompile(t, validation.Rules{"f": []any{&onlyAda{}, validation.EnumFrom(enums.InvoiceStatusValues), lengthRule{}}})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs := runSet(set, validation.Data{"f": "Ada"})
			if !errs.Has("f") {
				t.Error("a value no rule accepts passed")
			}
		}()
	}
	wg.Wait()
}

type mustBeThere struct{}

func (mustBeThere) Implicit() bool                  { return true }
func (mustBeThere) Passes(_ string, value any) bool { return value != nil && value != "" }
func (mustBeThere) Message() []string               { return []string{"The :attribute field is needed."} }

// TestAnImplicitRuleObjectRunsOnABlankValueAndStopsTheField.
func TestAnImplicitRuleObjectRunsOnABlankValueAndStopsTheField(t *testing.T) {
	set := mustCompile(t, validation.Rules{"f": []any{mustBeThere{}, "min:3"}})
	errs := runSet(set, validation.Data{"f": ""})
	if got := errs.Get("f"); len(got) != 1 || got[0] != "The f field is needed." {
		t.Errorf("f = %v, want the one sentence of the implicit rule", got)
	}
}

// TestAMessageOverrideNamesARuleObjectByItsType.
func TestAMessageOverrideNamesARuleObjectByItsType(t *testing.T) {
	set, err := validation.Compile(validation.Rules{"f": []any{&onlyAda{}}},
		validation.WithMessageOverrides(validation.Messages{"f.only_ada": "só a Ada."}))
	if err != nil {
		t.Fatal(err)
	}
	if got := runSet(set, validation.Data{"f": "Bob"}).Get("f"); len(got) != 1 || got[0] != "só a Ada." {
		t.Errorf("f = %v", got)
	}
}

// TestWhenAndUnlessChooseTheRulesOfTheRequest.
func TestWhenAndUnlessChooseTheRulesOfTheRequest(t *testing.T) {
	company := func(d validation.Data) bool { return d.Get("kind") == "company" }
	set := mustCompile(t, validation.Rules{
		"kind":  "required",
		"taxid": validation.When(company, []any{"required", "digits:14"}, "nullable"),
		"name":  validation.Unless(company, "required|max:5", []any{"required", "max:20"}),
	})

	errs := runSet(set, validation.Data{"kind": "company", "taxid": "123", "name": "Acme Company Ltd"})
	if !errs.Has("taxid") || errs.Has("name") {
		t.Errorf("a company: %v", errs)
	}
	errs = runSet(set, validation.Data{"kind": "person", "name": "Maximilian"})
	if errs.Has("taxid") || !errs.Has("name") {
		t.Errorf("a person: %v", errs)
	}
}

// TestBothBranchesOfAConditionAreCheckedAtBoot.
func TestBothBranchesOfAConditionAreCheckedAtBoot(t *testing.T) {
	_, err := validation.Compile(validation.Rules{"f": validation.When(func(validation.Data) bool { return false }, "required", "requried")})
	if err == nil || !strings.Contains(err.Error(), "requried") {
		t.Errorf("the branch nobody takes was not checked: %v", err)
	}
}

// TestForEachDecidesTheRulesOfEachMember.
func TestForEachDecidesTheRulesOfEachMember(t *testing.T) {
	set := mustCompile(t, validation.Rules{
		"items.*": validation.ForEach(func(value any, attribute string, data validation.Data) any {
			if m, ok := value.(validation.Data); ok && m["kind"] == "num" {
				return validation.Rules{"v": "required|integer"}
			}
			return validation.Rules{"v": "required|alpha"}
		}),
	})
	data := validation.Data{"items": list(keyed("kind", "num", "v", "x"), keyed("kind", "txt", "v", "1"), keyed("kind", "txt", "v", "ok"))}
	errs := runSet(set, data)
	if !errs.Has("items.0.v") || !errs.Has("items.1.v") || errs.Has("items.2.v") {
		t.Errorf("errors = %v", errs)
	}
}
