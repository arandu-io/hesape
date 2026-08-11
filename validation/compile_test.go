package validation_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/validation"
)

// refuse compiles a rule set that is expected not to, and returns the failures.
func refuse(t *testing.T, rules validation.Rules, opts ...validation.Option) validation.CompileErrors {
	t.Helper()
	set, err := validation.Compile(rules, opts...)
	if err == nil {
		t.Fatalf("Compile(%v) succeeded, want a boot failure (fields %v)", rules, set.Fields())
	}
	var failures validation.CompileErrors
	if !errors.As(err, &failures) {
		t.Fatalf("Compile returned %T, want CompileErrors", err)
	}
	return failures
}

// TestAnUnknownRuleFailsAtCompileNamingFieldAndFile is what earns the string
// form: the typo is found before main runs, and the message says where.
func TestAnUnknownRuleFailsAtCompileNamingFieldAndFile(t *testing.T) {
	failures := refuse(t, validation.Rules{"title": "required|maximum:255"})

	if len(failures) != 1 {
		t.Fatalf("failures = %v, want one", failures)
	}
	if failures[0].Field != "title" || failures[0].Rule != "maximum" {
		t.Errorf("failure = %+v, want the field and the rule named", failures[0])
	}
	if !strings.HasSuffix(failures[0].File, "compile_test.go") || failures[0].Line == 0 {
		t.Errorf("File:Line = %s:%d, want this test file", failures[0].File, failures[0].Line)
	}
	if !strings.Contains(failures.Error(), "compile_test.go") {
		t.Errorf("the rendered failure does not name the source:\n%s", failures.Error())
	}
}

// TestCompileReportsEveryFailureNotOnlyTheFirst: a set with three mistakes
// reports three. One at a time turns a boot check into three restarts.
func TestCompileReportsEveryFailureNotOnlyTheFirst(t *testing.T) {
	failures := refuse(t, validation.Rules{
		"titel": "requried|max:255",
		"age":   "integer|min:twelve",
		"role":  "in:",
	})

	if len(failures) != 3 {
		t.Fatalf("failures = %d, want 3:\n%s", len(failures), failures.Error())
	}
	// Sorted by field, so the message is the same on every run.
	want := []string{"age", "role", "titel"}
	for i, field := range want {
		if failures[i].Field != field {
			t.Errorf("failure %d is about %q, want %q", i, failures[i].Field, field)
		}
	}
}

// TestAMisspelledRuleSuggestsTheNearMiss. The alphabet is sixty-one names and a
// transposition is the common mistake.
func TestAMisspelledRuleSuggestsTheNearMiss(t *testing.T) {
	for _, c := range []struct{ typo, want string }{
		{"requried", `"required"`},
		{"maks", `"max"`},
		{"intger", `"integer"`},
	} {
		failures := refuse(t, validation.Rules{"f": c.typo})
		if !strings.Contains(failures[0].Msg, c.want) {
			t.Errorf("%q suggested %q, want %s", c.typo, failures[0].Msg, c.want)
		}
	}
}

// TestARuleReferencingAnUndeclaredFieldFailsAtCompile is the check that pays
// for itself: `confirmed:passwrod` compares against the empty string for ever,
// and Laravel says nothing about it.
func TestARuleReferencingAnUndeclaredFieldFailsAtCompile(t *testing.T) {
	for _, chain := range []string{
		"confirmed:passwrod",
		"same:passwrod",
		"different:passwrod",
		"gt:passwrod",
		"lte:passwrod",

		"required_if:passwrod,1",
		"required_unless:passwrod,1",
	} {
		failures := refuse(t, validation.Rules{"password": chain})
		if !strings.Contains(failures[0].Msg, `"passwrod"`) {
			t.Errorf("%q was accepted with %q", chain, failures[0].Msg)
		}
	}

	// Plain `confirmed` is exempt: the confirmation box carries no rules of its
	// own in any form anybody writes, so demanding one would be a boot failure
	// on correct input.
	if _, err := validation.Compile(validation.Rules{"password": "confirmed"}); err != nil {
		t.Errorf("plain confirmed was refused: %v", err)
	}
}

// TestAComparisonTakesEitherALiteralBoundOrAFieldName, which is the fork
// validateGt itself makes: the parameter is a number and no field of that name
// is declared, so it is a bound.
func TestAComparisonTakesEitherALiteralBoundOrAFieldName(t *testing.T) {
	set, err := validation.Compile(validation.Rules{"quantity": "integer|gt:10"})
	if err != nil {
		t.Fatalf("a literal bound was refused: %v", err)
	}
	if _, errs := set.Validate(url.Values{"quantity": {"11"}}); errs.Any() {
		t.Errorf("11 failed gt:10: %v", errs)
	}
	if _, errs := set.Validate(url.Values{"quantity": {"10"}}); !errs.Any() {
		t.Error("10 passed gt:10")
	}

	// A name that is not a number still has to be declared, because a
	// comparison against a field nobody wrote is a rule that never fires.
	refuse(t, validation.Rules{"high": "integer|gt:nowhere"})
}

// TestRegexTakesTheRestOfTheChainIncludingPipes. Laravel's answer to this is
// that the string form cannot be used at all and the rules must be an array; an
// array form here would be a second spelling of a rule set, so the answer is
// position.
func TestRegexTakesTheRestOfTheChainIncludingPipes(t *testing.T) {
	set, err := validation.Compile(validation.Rules{"slug": `required|max:255|regex:^[a-z0-9|_-]+$`})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := set.RulesFor("slug"); len(got) != 3 || got[2] != "regex" {
		t.Fatalf("RulesFor = %v, want the pattern to be one rule at the end", got)
	}
	if _, errs := set.Validate(url.Values{"slug": {"a|b_c"}}); errs.Any() {
		t.Errorf("the pipe was read as a separator, not as part of the pattern: %v", errs)
	}
	if _, errs := set.Validate(url.Values{"slug": {"A B"}}); !errs.Any() {
		t.Error("the pattern did not run")
	}
}

// TestRegexAnywhereButLastFailsAtCompile: the pattern runs to the end of the
// chain, so a rule written after it is swallowed and never runs.
func TestRegexAnywhereButLastFailsAtCompile(t *testing.T) {
	failures := refuse(t, validation.Rules{"slug": `regex:^a$|max:10`})
	if !strings.Contains(failures[0].Msg, "must be last") {
		t.Errorf("Msg = %q, want it to say the pattern must be last", failures[0].Msg)
	}

	failures = refuse(t, validation.Rules{"slug": `regex:^a$|not_regex:^b$`})
	if len(failures) == 0 {
		t.Fatal("regex and not_regex together were accepted")
	}

	// A pattern that genuinely alternates is not refused.
	if _, err := validation.Compile(validation.Rules{"slug": `regex:^(cat|dog)$`}); err != nil {
		t.Errorf("an honest alternation was refused: %v", err)
	}
}

// TestAnUncompilableRegexFailsAtCompileNotOnFirstRequest. Laravel never
// compiles a pattern until a request touches it, so an unclosed group is a 500
// on the first form somebody submits.
func TestAnUncompilableRegexFailsAtCompileNotOnFirstRequest(t *testing.T) {
	failures := refuse(t, validation.Rules{"slug": `regex:^(a$`})
	if failures[0].Field != "slug" || !strings.Contains(failures[0].Msg, "does not compile") {
		t.Errorf("failure = %+v", failures[0])
	}
}

// TestInSplitsOnCommaAndHonoursQuoting, which is Laravel's str_getcsv: a value
// containing a comma is quoted, and it stays one value.
func TestInSplitsOnCommaAndHonoursQuoting(t *testing.T) {
	set, err := validation.Compile(validation.Rules{"f": `in:"a,b",c`})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, errs := set.Validate(url.Values{"f": {"a,b"}}); errs.Any() {
		t.Errorf("the quoted value was split: %v", errs)
	}
	if _, errs := set.Validate(url.Values{"f": {"c"}}); errs.Any() {
		t.Errorf("the second value was lost: %v", errs)
	}
	if _, errs := set.Validate(url.Values{"f": {"a"}}); !errs.Any() {
		t.Error("half of the quoted value was accepted on its own")
	}

	failures := refuse(t, validation.Rules{"f": `in:a"b,c`})
	if !strings.Contains(failures[0].Msg, "malformed") {
		t.Errorf("Msg = %q, want a malformed list", failures[0].Msg)
	}
}

// TestDateFormatRejectsAPHPStyleLayout. It is the one Laravel string that
// cannot be copied, because the string means something else in the host
// language -- and it cannot be caught by a round trip alone: Format("Y-m-d")
// returns "Y-m-d" and Parse reads it straight back.
func TestDateFormatRejectsAPHPStyleLayout(t *testing.T) {
	failures := refuse(t, validation.Rules{"published_at": "date_format:Y-m-d"})
	if !strings.Contains(failures[0].Msg, "2006-01-02") {
		t.Errorf("Msg = %q, want the Go layout offered", failures[0].Msg)
	}
	if _, err := validation.Compile(validation.Rules{"published_at": "date_format:2006-01-02"}); err != nil {
		t.Errorf("a Go layout was refused: %v", err)
	}
}

// TestUniqueAndExistsFailClosedWithoutAGrantAndAVerifier. They reach a
// repository, and a rule set compiled at boot carries no security.Grant: the
// Grant and the verifier arrive with the request, through WithPresence. RULE 17
// has no exception for a read, so without them the rule does not pass quietly
// -- it fails.
func TestUniqueAndExistsFailClosedWithoutAGrantAndAVerifier(t *testing.T) {
	for _, rule := range []string{"unique:users", "exists:users,id"} {
		set, err := validation.Compile(validation.Rules{"email": rule})
		if err != nil {
			t.Fatalf("Compile(%q) = %v", rule, err)
		}
		if _, errs := set.Validate(url.Values{"email": {"a@b.co"}}); !errs.Any() {
			t.Errorf("%q passed with no verifier and no Grant", rule)
		}
	}
}

// TestEveryRuleLaravelHasIsShipped: a name that is not here reads as a gap and
// invites a pull request adding it. One per family, plus every name the old
// refusal list carried.
func TestEveryRuleLaravelHasIsShipped(t *testing.T) {
	for _, chain := range []string{
		"string", "nullable", "array", "list", "distinct", "in_array:other",
		"file", "image", "mimes:jpg", "mimetypes:image/jpeg", "extensions:jpg",
		"dimensions:min_width=100",
		"active_url", "current_password",
		"required_if_accepted:other", "prohibited_unless:other,a", "present_with:other",
		"accepted_if:other,a", "declined_if:other,a", "required_if_declined:other",
		"prohibited_if:other,a", "prohibits:other", "missing_if:other,a",
		"missing_unless:other,a", "missing_with:other", "missing_with_all:other",
		"present_if:other,a", "present_unless:other,a", "present_with_all:other",
		"required_with_all:other", "required_without_all:other",
		"exclude", "exclude_if:other,a", "exclude_unless:other,a",
		"exclude_with:other", "exclude_without:other",
		"multiple_of:3", "max_digits:4", "min_digits:2", "date_equals:2026-01-01",
		"contains:a", "enum:a,b", "required_array_keys:a",
		"unique:users", "exists:users", "email:dns",
	} {
		if _, err := validation.Compile(validation.Rules{"f": chain, "other": "sometimes"}); err != nil {
			t.Errorf("Compile(%q) = %v", chain, err)
		}
	}
}

// TestASecondSpellingOfARuleIsRedirectedRatherThanUnknown. What is left in the
// refusal list is an alias somebody remembers wrong, and "unknown rule" reads
// as an omission.
func TestASecondSpellingOfARuleIsRedirectedRatherThanUnknown(t *testing.T) {
	for _, rule := range []string{"int", "bool", "notregex", "array_keys", "base64", "same_as"} {
		failures := refuse(t, validation.Rules{"f": rule})
		if strings.Contains(failures[0].Msg, "unknown rule") {
			t.Errorf("%q is refused as unknown rather than with a redirection", rule)
		}
	}
}

// TestAMalformedArgumentFailsAtCompile covers the arguments a request would
// otherwise parse, wrongly, once per request.
func TestAMalformedArgumentFailsAtCompile(t *testing.T) {
	for _, c := range []struct{ chain, want string }{
		{"required:1", "takes no argument"},
		{"min", "needs 1 argument"},
		{"in", "needs 1 argument"},
		{"min:twelve", "needs a whole number"},
		{"integer|min:twelve", "needs a number"},
		{"min:-3", "not negative"},
		{"min:2.5", "whole number"},
		{"between:10,2", "above its high bound"},
		{"digits_between:4,2", "above its high bound"},
		{"between:1", "needs 2 arguments"},
		{"alpha:unicode", `only "ascii"`},
		{"required|required", "written twice"},
		{"Max:3", "lowercase"},
		{"after:soonish", "needs a moment"},
	} {
		failures := refuse(t, validation.Rules{"f": c.chain})
		if !strings.Contains(failures[0].Msg, c.want) {
			t.Errorf("%q was refused with %q, want it to mention %q", c.chain, failures[0].Msg, c.want)
		}
	}
}

// TestConflictingRulesOnOneFieldFailAtCompile: a chain that nothing can pass is
// a merge accident, and it only shows up as a form nobody can submit.
func TestConflictingRulesOnOneFieldFailAtCompile(t *testing.T) {
	for _, chain := range []string{
		"required|prohibited",
		"required|missing",
		"integer|alpha",
		"numeric|boolean",
		"lowercase|uppercase",
		"min:10|max:2",
		"in:a,b|not_in:b,c",
	} {
		failures := refuse(t, validation.Rules{"f": chain})
		if len(failures) == 0 {
			t.Errorf("%q was accepted", chain)
		}
	}
}

// TestAMessageKeyedOnSomethingTheSetDoesNotDeclareFailsAtCompile: the default
// sentence is still there and still reads correctly, so a typo in an override
// is invisible on the screen.
func TestAMessageKeyedOnSomethingTheSetDoesNotDeclareFailsAtCompile(t *testing.T) {
	rules := validation.Rules{"email": "required|email"}

	refuse(t, rules, validation.WithMessageOverrides(validation.Messages{"emial.required": "..."}))
	refuse(t, rules, validation.WithMessageOverrides(validation.Messages{"email.max": "..."}))
	refuse(t, rules, validation.WithMessageOverrides(validation.Messages{"email": "..."}))

	set, err := validation.Compile(rules, validation.WithMessageOverrides(validation.Messages{
		"email.required": "we need an address to send the receipt to",
	}))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, errs := set.Validate(url.Values{"email": {""}})
	if got := errs["email"]; len(got) != 1 || got[0] != "we need an address to send the receipt to" {
		t.Errorf("email = %v, want the override", got)
	}
}

// TestAnEmptyChainOrFieldNameFailsAtCompile, because both are entries somebody
// meant to finish.
func TestAnEmptyChainOrFieldNameFailsAtCompile(t *testing.T) {
	refuse(t, validation.Rules{"f": ""})
	refuse(t, validation.Rules{"": "required"})
}

// TestMustCompilePanicsWithEveryFailureAndTheSource, which is the whole point
// of putting a rule set in a package-level variable.
func TestMustCompilePanicsWithEveryFailureAndTheSource(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustCompile did not panic on a rule set that is not valid")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("panicked with %T, want an error", r)
		}
		msg := err.Error()
		for _, want := range []string{"compile_test.go", `field "age"`, `field "titel"`, "requried"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the panic does not mention %q:\n%s", want, msg)
			}
		}
	}()

	validation.MustCompile(validation.Rules{
		"titel": "requried",
		"age":   "integer|min:twelve",
	})
}

// TestSourceNamesWhereTheSetWasCompiled, so aru doctor and a panic agree.
func TestSourceNamesWhereTheSetWasCompiled(t *testing.T) {
	set := mustCompile(t, validation.Rules{"f": "required"})
	file, line := set.Source()
	if !strings.HasSuffix(file, "rules_test.go") || line == 0 {
		t.Errorf("Source() = %s:%d, want the caller of Compile", file, line)
	}
}
