package validation_test

import (
	"testing"
	"time"

	"github.com/arandu-io/hesape/validation"
)

type namedKind string

type AuditFields struct {
	Reason string `form:"reason"`
}

// checkedRequest is a request the way an application writes one: tagged fields
// of each kind Bind converts into, an embedded struct, and a field that is not
// input.
type checkedRequest struct {
	Name     string     `form:"name,raw"`
	Kind     namedKind  `form:"kind"`
	Quantity int        `form:"quantity"`
	Price    float64    `form:"price"`
	Active   bool       `form:"active"`
	Note     *string    `form:"note"`
	Tags     []string   `form:"tags"`
	DueOn    time.Time  `form:"due_on"`
	Until    *time.Time `form:"until"`
	Internal string
	Skipped  string `form:"-"`
	AuditFields
}

func (checkedRequest) Rules() validation.Rules {
	return validation.Rules{
		"name":     "required|string|max:5",
		"kind":     "required|in:a,b",
		"quantity": "integer|min:1|max:10",
		"price":    "numeric|min:0",
		"active":   "boolean",
		"note":     "nullable|string|max:3",
		"tags":     "required|array|min:1",
		"due_on":   "required",
		"reason":   "required",
	}
}

func (checkedRequest) Messages() validation.Messages {
	return validation.Messages{"name.required": "Informe o nome."}
}

func (checkedRequest) Attributes() map[string]string {
	return map[string]string{"quantity": "quantidade"}
}

func valid() checkedRequest {
	return checkedRequest{
		Name: "Curso", Kind: "a", Quantity: 3, Price: 1.5, Active: true,
		Tags: []string{"x"}, DueOn: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		AuditFields: AuditFields{Reason: "porque"},
	}
}

func TestCheckRequestPassesWhatTheStructHolds(t *testing.T) {
	if errs := portuguese().CheckRequest(valid(), validation.WithLocale("pt-BR")); errs.Any() {
		t.Fatalf("a valid request failed: %v", errs)
	}
}

func TestCheckRequestRunsTheRulesOverEachKindOfField(t *testing.T) {
	cases := []struct {
		name   string
		change func(*checkedRequest)
		field  string
	}{
		{"a blank string is absent", func(r *checkedRequest) { r.Name = "   " }, "name"},
		{"a string over the limit", func(r *checkedRequest) { r.Name = "abcdef" }, "name"},
		{"a named string type outside the list", func(r *checkedRequest) { r.Kind = "z" }, "kind"},
		{"an integer under its minimum", func(r *checkedRequest) { r.Quantity = 0 }, "quantity"},
		{"an integer over its maximum", func(r *checkedRequest) { r.Quantity = 11 }, "quantity"},
		{"a float under its minimum", func(r *checkedRequest) { r.Price = -1 }, "price"},
		{"a pointer to text over the limit", func(r *checkedRequest) { s := "abcd"; r.Note = &s }, "note"},
		{"a nil slice is absent", func(r *checkedRequest) { r.Tags = nil }, "tags"},
		{"an empty slice is absent", func(r *checkedRequest) { r.Tags = []string{} }, "tags"},
		{"the zero time is absent", func(r *checkedRequest) { r.DueOn = time.Time{} }, "due_on"},
		{"a field of an embedded struct", func(r *checkedRequest) { r.Reason = "" }, "reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := valid()
			c.change(&r)
			errs := portuguese().CheckRequest(r, validation.WithLocale("pt-BR"))
			if !errs.Has(c.field) {
				t.Errorf("no failure on %q: %v", c.field, errs)
			}
			if len(errs) != 1 {
				t.Errorf("failures on other fields: %v", errs)
			}
		})
	}
}

func TestCheckRequestLeavesAnUnsentPointerToTheRulesThatAllowIt(t *testing.T) {
	r := valid()
	r.Note = nil
	if errs := portuguese().CheckRequest(r, validation.WithLocale("pt-BR")); errs.Any() {
		t.Fatalf("a nil pointer under nullable failed: %v", errs)
	}
}

func TestCheckRequestDoesNotReadAFieldThatIsNotInput(t *testing.T) {
	data := validation.RequestData(valid())
	for _, key := range []string{"Internal", "Skipped", "-", ""} {
		if _, there := data[key]; there {
			t.Errorf("RequestData read %q, which is not a form field", key)
		}
	}
	if _, there := data["reason"]; !there {
		t.Error("RequestData did not read the embedded struct's field")
	}
}

func TestCheckRequestSaysWhatBindSays(t *testing.T) {
	r := valid()
	r.Name = ""
	r.Quantity = 0
	errs := portuguese().CheckRequest(r, validation.WithLocale("pt-BR"))
	if got := errs.First("name"); got != "Informe o nome." {
		t.Errorf("the override was not used: %q", got)
	}
	if got := errs.First("quantity"); got != "O campo quantidade deve ser no mínimo 1." {
		t.Errorf("the attribute name or the sentence was not used: %q", got)
	}
}

func TestCheckRequestPanicsForARuleNothingDeclares(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a request whose rules do not compile was checked")
		}
	}()
	portuguese().CheckRequest(unknownRule{})
}

type unknownRule struct {
	Name string `form:"name"`
}

func (unknownRule) Rules() validation.Rules { return validation.Rules{"name": "no_such_rule"} }

func TestRequestDataRefusesWhatIsNotAStruct(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("RequestData read a string")
		}
	}()
	validation.RequestData("not a request")
}

// TestAMessageOverrideComesBeforeTheLanguageFile. The sentence a request words
// for one field and one rule is more specific than the sentence of the rule in
// any locale, and used to be reached only by a Validator with no translator: a
// Factory over pt-BR said the catalogue's line and never the one written beside
// the rules.
func TestAMessageOverrideComesBeforeTheLanguageFile(t *testing.T) {
	set := portuguese().MustCompile(
		validation.Rules{"name": "required|max:3", "city": "required"},
		validation.WithMessageOverrides(validation.Messages{"name.max": "Use até 3 letras."}),
	)
	errs := portuguese().Make(validation.Data{"name": "abcd", "city": ""}, set, validation.WithLocale("pt-BR")).Errors()
	if got := errs.First("name"); got != "Use até 3 letras." {
		t.Errorf("the override lost to the language file: %q", got)
	}
	if got := errs.First("city"); got != "O campo city é obrigatório." {
		t.Errorf("a rule with no override changed its sentence: %q", got)
	}
}
