package validation_test

import (
	"testing"

	"github.com/arandu-io/hesape/internal/enums"
	"github.com/arandu-io/hesape/validation"
)

// TestTheEnumRuleAsksTheType. The set lives in one place, and `enum` with no
// list is the rule written against it.
func TestTheEnumRuleAsksTheType(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
		fails bool
	}{
		{"a case", enums.InvoiceStatusPaid, false},
		{"a stranger of the same type", enums.InvoiceStatus("deleted"), true},
		{"the zero value is not a case", enums.InvoiceStatus(""), true},
		{"an integer-backed case", enums.PriorityHigh, false},
		{"an integer outside the set", enums.Priority(9), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if errs := data(t, validation.Rules{"f": "enum"}, validation.Data{"f": c.value}); errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// TestTheEnumRuleAcceptsACaseTheListDoesNotName. The rule used to be the list,
// so a case added to the type and not to the rule was refused -- the value the
// column accepts, rejected by the form.
func TestTheEnumRuleAcceptsACaseTheListDoesNotName(t *testing.T) {
	errs := data(t,
		validation.Rules{"f": "enum:draft,sent"},
		validation.Data{"f": enums.InvoiceStatusPaid},
	)
	if errs.Any() {
		t.Errorf("paid is a case of the type and the rule refused it: %v", errs)
	}
}

// TestTheEnumRuleRefusesAListThatTheTypeDoesNotDeclare. The other direction of
// the same drift: a case in the rule that nothing can produce. Failing the field
// is what makes it visible, because a rule written against a different type is
// not a rule anybody can rely on.
func TestTheEnumRuleRefusesAListThatTheTypeDoesNotDeclare(t *testing.T) {
	errs := data(t,
		validation.Rules{"f": "enum:draft,sent,paid,void,cancelled"},
		validation.Data{"f": enums.InvoiceStatusDraft},
	)
	if !errs.Any() {
		t.Error("the rule names a case the type does not declare and nothing reported it")
	}
}

// TestTheEnumRuleReadsTheListForAnUntypedValue. A field that has not been typed
// yet carries a string, and then the parameters are the only set there is.
func TestTheEnumRuleReadsTheListForAnUntypedValue(t *testing.T) {
	for _, c := range []struct {
		name  string
		rules validation.Rules
		value any
		fails bool
	}{
		{"a case", validation.Rules{"f": "enum:draft,published"}, "draft", false},
		{"a stranger", validation.Rules{"f": "enum:draft,published"}, "deleted", true},
		{"no type and no list decides nothing", validation.Rules{"f": "enum"}, "draft", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if errs := data(t, c.rules, validation.Data{"f": c.value}); errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// TestEnumOfDerivesTheCases. The rule object built from the type cannot carry a
// list that disagrees with it, because it never wrote one.
func TestEnumOfDerivesTheCases(t *testing.T) {
	rule := validation.EnumOf(enums.InvoiceStatusValues()...)

	for _, c := range []struct {
		name  string
		value any
		want  bool
	}{
		{"a typed case", enums.InvoiceStatusVoid, true},
		{"a typed stranger", enums.InvoiceStatus("deleted"), false},
		{"the same case as text", "void", true},
		{"a stranger as text", "deleted", false},
		{"nothing", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := rule.Passes("f", c.value); got != c.want {
				t.Errorf("Passes(%v) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

// TestEnumOfNarrowsWithOnlyAndExcept. Narrowing is written in shown spellings,
// which is what the caller has to hand, and it applies to a typed value too.
func TestEnumOfNarrowsWithOnlyAndExcept(t *testing.T) {
	only := validation.EnumOf(enums.InvoiceStatusValues()...).Only("draft", "sent")
	if !only.Passes("f", enums.InvoiceStatusDraft) {
		t.Error("draft is in Only and was refused")
	}
	if only.Passes("f", enums.InvoiceStatusPaid) {
		t.Error("paid is outside Only and was accepted")
	}

	except := validation.EnumOf(enums.InvoiceStatusValues()...).Except("void")
	if except.Passes("f", enums.InvoiceStatusVoid) {
		t.Error("void is excluded and was accepted")
	}
	if !except.Passes("f", enums.InvoiceStatusPaid) {
		t.Error("paid is not excluded and was refused")
	}
}

// TestEnumOfReadsAnIntegerBackedType. The cases are the shown spellings, which
// is what a form control sends back and what the generated parser reads.
func TestEnumOfReadsAnIntegerBackedType(t *testing.T) {
	rule := validation.EnumOf(enums.PriorityValues()...)

	if !rule.Passes("f", enums.PriorityNormal) {
		t.Error("normal is a case and was refused")
	}
	if !rule.Passes("f", "high") {
		t.Error("the shown spelling of a case was refused")
	}
	if rule.Passes("f", enums.Priority(9)) {
		t.Error("9 is not a case and was accepted")
	}
}
