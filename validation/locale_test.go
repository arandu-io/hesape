package validation_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/validation"
)

func portuguese() *validation.Factory {
	return validation.NewFactory(validation.FromTranslation(translation.New(nil, "pt-BR", "en")))
}

// TestPortugueseSentencesFillEveryPlaceholder is the sentence a person reads, for
// the rules whose sentence carries something besides the field.
func TestPortugueseSentencesFillEveryPlaceholder(t *testing.T) {
	cases := []struct {
		rule string
		data validation.Data
		want string
	}{
		{"required", validation.Data{"f": ""}, "O campo f é obrigatório."},
		{"min:3", validation.Data{"f": "ab"}, "O campo f deve ter pelo menos 3 caracteres."},
		{"max:3", validation.Data{"f": "abcd"}, "O campo f não pode ter mais de 3 caracteres."},
		{"numeric|min:3", validation.Data{"f": "2"}, "O campo f deve ser no mínimo 3."},
		{"numeric|max:3", validation.Data{"f": "4"}, "O campo f não pode ser maior que 3."},
		{"array|min:2", validation.Data{"f": list(1)}, "O campo f deve ter pelo menos 2 itens."},
		{"file|max:1", validation.Data{"f": contractFile{size: 5 * 1024}}, "O campo f não pode ser maior que 1 quilobytes."},
		{"between:3,5", validation.Data{"f": "a"}, "O campo f deve ter entre 3 e 5 caracteres."},
		{"size:3", validation.Data{"f": "a"}, "O campo f deve ter 3 caracteres."},
		{"required_if:o,x", validation.Data{"f": "", "o": "x"}, "O campo f é obrigatório quando o for x."},
		{"required_unless:o,x,y", validation.Data{"f": "", "o": "z"}, "O campo f é obrigatório a menos que o esteja em x, y."},
		{"required_with:o,p", validation.Data{"f": "", "o": "x"}, "O campo f é obrigatório quando o / p estiver presente."},
		{"same:o", validation.Data{"f": "a", "o": "b"}, "O campo f deve ser igual a o."},
		{"different:o", validation.Data{"f": "a", "o": "a"}, "Os campos f e o devem ser diferentes."},
		{"in:a,b", validation.Data{"f": "z"}, "O valor selecionado para o campo f é inválido."},
		{"after:2026-01-01", validation.Data{"f": "2025-01-01"}, "O campo f deve ser uma data posterior a 2026-01-01."},
		{"date_format:2006-01-02", validation.Data{"f": "x"}, "O campo f deve seguir o formato 2006-01-02."},
		{"gt:o", validation.Data{"f": "5", "o": "9"}, "O campo f deve ser maior que 9."},
		{"gte:10", validation.Data{"f": "5"}, "O campo f deve ser maior ou igual a 10."},
		{"digits:3", validation.Data{"f": "5"}, "O campo f deve ter 3 dígitos."},
		{"digits_between:2,3", validation.Data{"f": "5"}, "O campo f deve ter entre 2 e 3 dígitos."},
		{"starts_with:a,b", validation.Data{"f": "z"}, "O campo f deve começar com um destes: a, b."},
		{"mimes:jpg,png", validation.Data{"f": contractFile{size: 1}}, "O campo f deve ser um arquivo do tipo: jpg, png."},
		{"prohibits:o", validation.Data{"f": "a", "o": "b"}, "O campo f impede que o esteja presente."},
		{"array_keys:a,b", validation.Data{"f": keyed("c", 1)}, "O campo f só pode conter as seguintes chaves: a, b."},
		{"encoding:ascii", validation.Data{"f": "ç"}, "O campo f deve estar codificado em ascii."},
		{"multiple_of:5", validation.Data{"f": "7"}, "O campo f deve ser múltiplo de 5."},
		{"confirmed", validation.Data{"f": "a"}, "A confirmação do campo f não confere."},
		{"accepted", validation.Data{"f": "no"}, "O campo f deve ser aceito."},
		{"email", validation.Data{"f": "x"}, "O campo f deve ser um endereço de e-mail válido."},
		{"unique:users", validation.Data{"f": "x"}, "O valor informado para o campo f já está em uso."},
	}
	factory := portuguese()
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			set, err := factory.Compile(validation.Rules{"f": c.rule, "o": "sometimes", "p": "sometimes", "f_confirmation": "sometimes"})
			if err != nil {
				t.Fatal(err)
			}
			got := factory.Make(c.data, set).Errors().Get("f")
			if len(got) == 0 || got[0] != c.want {
				t.Errorf("f = %q, want %q", got, c.want)
			}
		})
	}
}

// TestTheAttributeNameIsInTheSentenceInThreeSpellings.
func TestTheAttributeNameIsInTheSentenceInThreeSpellings(t *testing.T) {
	tr := translation.New(nil, "pt-BR", "en")
	tr.AddLines(map[string]string{"validation.required": ":Attribute é obrigatório (:ATTRIBUTE)."}, "pt-BR", "")
	// AddLines replaces the whole group, so the rest of it is gone: the sentence
	// the line was written for is the only thing under test.
	factory := validation.NewFactory(validation.FromTranslation(tr))
	set, _ := factory.Compile(validation.Rules{"birth_date": "required"})
	got := factory.Make(validation.Data{"birth_date": ""}, set).Errors().Get("birth_date")
	if len(got) != 1 || got[0] != "Birth date é obrigatório (BIRTH DATE)." {
		t.Errorf("got %q", got)
	}
}

// TestALanguageFileOverridesTheBundledSentenceTheAttributeAndTheValue.
func TestALanguageFileOverridesTheBundledSentenceTheAttributeAndTheValue(t *testing.T) {
	loader := translation.NewArrayLoader()
	loader.AddMessages("pt-BR", "validation", translation.Lines{
		"email":                       "Informe um e-mail em :attribute.",
		"attributes.cpf":              "CPF",
		"attributes.items.*.name":     "nome do item",
		"custom.password.min.string":  "A senha precisa de :min caracteres, no mínimo.",
		"custom.items.*.qty.required": "Informe a quantidade do item :position.",
		"values.status.draft":         "rascunho",
		"required_if":                 "O campo :attribute é obrigatório com :other = :value.",
	}, "")
	tr := translation.New(loader, "pt-BR", "en")
	factory := validation.NewFactory(validation.FromTranslation(tr))

	set := factory.MustCompile(validation.Rules{
		"email":        "email",
		"cpf":          "required",
		"password":     "min:8",
		"items.*.name": "required",
		"items.*.qty":  "required",
		"status":       "sometimes",
		"note":         "required_if:status,draft",
	})
	errs := factory.Make(validation.Data{
		"email": "x", "cpf": "", "password": "abc", "status": "draft", "note": "",
		"items": list(keyed("name", "", "qty", "")),
	}, set).Errors()

	want := map[string]string{
		"email":        "Informe um e-mail em email.",
		"cpf":          "O campo CPF é obrigatório.",
		"password":     "A senha precisa de 8 caracteres, no mínimo.",
		"items.0.name": "O campo nome do item é obrigatório.",
		"items.0.qty":  "Informe a quantidade do item 1.",
		"note":         "O campo note é obrigatório com status = rascunho.",
	}
	for field, sentence := range want {
		if got := errs.First(field); got != sentence {
			t.Errorf("%s = %q, want %q", field, got, sentence)
		}
	}
}

// TestAnInlineMessageAndAnAttributeNameBeatTheLanguageFile.
func TestAnInlineMessageAndAnAttributeNameBeatTheLanguageFile(t *testing.T) {
	factory := portuguese()
	set := factory.MustCompile(validation.Rules{"cpf": "required", "nome": "required"})
	errs := factory.Make(validation.Data{"cpf": "", "nome": ""}, set,
		validation.WithCustomMessages(map[string]any{"nome.required": "Diga o nome."}),
		validation.WithCustomAttributes(map[string]string{"cpf": "CPF do titular"}),
	).Errors()
	if errs.First("cpf") != "O campo CPF do titular é obrigatório." || errs.First("nome") != "Diga o nome." {
		t.Errorf("errors = %v", errs)
	}
}

// TestTheLocaleOfTheRunChoosesTheLanguage. One translator answers every request.
func TestTheLocaleOfTheRunChoosesTheLanguage(t *testing.T) {
	factory := portuguese()
	set := factory.MustCompile(validation.Rules{"name": "required|min:3"})
	data := validation.Data{"name": ""}

	if got := factory.Make(data, set).Errors().First("name"); got != "O campo name é obrigatório." {
		t.Errorf("default = %q", got)
	}
	if got := factory.Make(data, set, validation.WithLocale("en")).Errors().First("name"); got != "The name field is required." {
		t.Errorf("en = %q", got)
	}
	if got := factory.Make(data, set, validation.WithLocale("pt-BR")).Errors().First("name"); got != "O campo name é obrigatório." {
		t.Errorf("pt-BR = %q", got)
	}
}

// TestTheAdapterBridgesTheTwoSignatures.
func TestTheAdapterBridgesTheTwoSignatures(t *testing.T) {
	var missing atomic.Int32
	tr := translation.New(nil, "pt-BR", "en")
	tr.HandleMissingKeysUsing(func(key string, _ translation.Replace, _ string, _ bool) string {
		missing.Add(1)
		return key
	})
	adapter := validation.FromTranslation(tr)

	if got := adapter.Get("validation.required", nil, "pt-BR"); got != "O campo :attribute é obrigatório." {
		t.Errorf("a line = %v", got)
	}
	if got := adapter.Get("validation.required", nil, "en"); got != "The :attribute field is required." {
		t.Errorf("a line in another locale = %v", got)
	}
	if got := adapter.Get("validation.required", map[string]any{"attribute": "CPF"}, ""); got != "O campo CPF é obrigatório." {
		t.Errorf("a line with replacements = %v", got)
	}
	group, ok := adapter.Get("validation.min", nil, "pt-BR").(map[string]any)
	if !ok || group["string"] != "O campo :attribute deve ter pelo menos :min caracteres." {
		t.Errorf("a group = %v", group)
	}
	if got := adapter.Get("validation.custom.cpf.required", nil, "pt-BR"); got != "validation.custom.cpf.required" {
		t.Errorf("a key with no line = %v", got)
	}
	if got := adapter.Get("validation.custom", nil, "pt-BR"); got != "validation.custom" {
		t.Errorf("an empty group = %v", got)
	}
	if got := adapter.Choice("validation.required", 2, nil, "en"); got != "The :attribute field is required." {
		t.Errorf("Choice = %q", got)
	}
	if missing.Load() != 0 {
		t.Errorf("the translator was told of %d missing keys that were only probed", missing.Load())
	}

	nothing := validation.FromTranslation(nil)
	if nothing.Get("validation.required", nil, "") != "validation.required" || nothing.Choice("k", 1, nil, "") != "k" {
		t.Error("a nil translator must answer with the keys")
	}
}

// TestWithNoTranslatorTheSentencesAreTheShortOnes keeps the behaviour of a
// validator that was never given a catalogue.
func TestWithNoTranslatorTheSentencesAreTheShortOnes(t *testing.T) {
	set := mustCompile(t, validation.Rules{"name": "required"})
	if got := validation.Make(validation.Data{"name": ""}, set).Errors().First("name"); got != "is required" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// The extension point.
// ---------------------------------------------------------------------------

type documents struct{}

func (documents) RegisterValidation(r *validation.Registry) {
	r.Add(validation.RuleDef{
		Name:    "cpf",
		Aliases: []string{"formato_cpf"},
		Check: func(v *validation.Validator, attribute string, value any, parameters []string) bool {
			s, _ := value.(string)
			return strings.HasPrefix(s, "111")
		},
		Messages: map[string]string{
			"pt-BR": "O campo :attribute não é um CPF válido.",
			"en":    "The :attribute field is not a valid CPF.",
		},
	})
	r.Add(validation.RuleDef{
		Name:   "celular",
		Params: validation.Params{Min: 0, Max: 1},
		Args: func(parameters []string) error {
			if len(parameters) == 1 && parameters[0] != "ddd" {
				return errString("celular takes only ddd")
			}
			return nil
		},
		Check: func(v *validation.Validator, attribute string, value any, parameters []string) bool {
			s, _ := value.(string)
			return len(s) >= 9
		},
		Messages: map[string]string{"pt-BR": "O campo :attribute deve ser um celular :kind válido."},
		Replace: func(message, attribute, rule string, parameters []string, v *validation.Validator) string {
			kind := "sem DDD"
			if len(parameters) == 1 {
				kind = "com DDD"
			}
			return strings.ReplaceAll(message, ":kind", kind)
		},
	})
	r.Add(validation.RuleDef{
		Name:     "cpf_required",
		Implicit: true,
		Check: func(v *validation.Validator, attribute string, value any, parameters []string) bool {
			return value != nil && value != ""
		},
		Messages: map[string]string{"pt-BR": "Informe o CPF."},
	})
}

type errString string

func (e errString) Error() string { return string(e) }

// TestAModuleDeclaresItsRulesItsSentencesAndItsReplacers.
func TestAModuleDeclaresItsRulesItsSentencesAndItsReplacers(t *testing.T) {
	factory := portuguese().Use(documents{})
	set := factory.MustCompile(validation.Rules{
		"cpf":   "required|cpf",
		"alias": []any{"required", "formato_cpf"},
		"cel":   "celular:ddd",
		"cel2":  []any{"celular"},
		"blank": "cpf_required",
	})
	errs := factory.Make(validation.Data{"cpf": "222", "alias": "333", "cel": "1", "cel2": "1", "blank": ""}, set).Errors()

	want := map[string]string{
		"cpf":   "O campo cpf não é um CPF válido.",
		"alias": "O campo alias não é um CPF válido.",
		"cel":   "O campo cel deve ser um celular com DDD válido.",
		"cel2":  "O campo cel2 deve ser um celular sem DDD válido.",
		"blank": "Informe o CPF.",
	}
	for field, sentence := range want {
		if got := errs.First(field); got != sentence {
			t.Errorf("%s = %q, want %q", field, got, sentence)
		}
	}

	english := factory.Make(validation.Data{"cpf": "222"}, factory.MustCompile(validation.Rules{"cpf": "cpf"}),
		validation.WithLocale("en")).Errors().First("cpf")
	if english != "The cpf field is not a valid CPF." {
		t.Errorf("en = %q", english)
	}
	other := factory.Make(validation.Data{"cpf": "222"}, factory.MustCompile(validation.Rules{"cpf": "cpf"}),
		validation.WithLocale("es")).Errors().First("cpf")
	if other != "The cpf field is not a valid CPF." {
		t.Errorf("a locale with no line must fall back to English, got %q", other)
	}
}

// TestTheLanguageFileOverridesAModulesSentence.
func TestTheLanguageFileOverridesAModulesSentence(t *testing.T) {
	loader := translation.NewArrayLoader()
	loader.AddMessages("pt-BR", "validation", translation.Lines{"cpf": "CPF inválido em :attribute."}, "")
	factory := validation.NewFactory(validation.FromTranslation(translation.New(loader, "pt-BR", "en"))).Use(documents{})
	set := factory.MustCompile(validation.Rules{"cpf": "cpf"})
	if got := factory.Make(validation.Data{"cpf": "222"}, set).Errors().First("cpf"); got != "CPF inválido em cpf." {
		t.Errorf("got %q", got)
	}
}

// TestAModuleRuleIsBootCheckedLikeACatalogueRule.
func TestAModuleRuleIsBootCheckedLikeACatalogueRule(t *testing.T) {
	factory := portuguese().Use(documents{})
	for _, bad := range []string{"celular:ddd,x", "celular:fixo", "cpf:1", "cpff"} {
		if _, err := factory.Compile(validation.Rules{"f": bad}); err == nil {
			t.Errorf("%q compiled", bad)
		}
	}
	if _, err := validation.Compile(validation.Rules{"f": "cpf"}); err == nil {
		t.Error("the package-level Compile knew a rule only a Factory was given")
	}
}

// TestADeclarationThatCannotBeUsedPanicsAtBoot.
func TestADeclarationThatCannotBeUsedPanicsAtBoot(t *testing.T) {
	check := func(*validation.Validator, string, any, []string) bool { return true }
	for name, def := range map[string]validation.RuleDef{
		"no check":         {Name: "x"},
		"a catalogue name": {Name: "required", Check: check},
		"an upper case":    {Name: "Cpf", Check: check},
		"a pipe":           {Name: "a|b", Check: check},
		"a refused name":   {Name: "same_as", Check: check},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			validation.NewRegistry().Add(def)
		}()
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a name declared twice: no panic")
			}
		}()
		registry := validation.NewRegistry()
		registry.Use(documents{}, documents{})
	}()
}

// TestASetCompiledBeforeTheModulesWereInstalledIsResolvedByTheFactory is the
// package-level variable: it is compiled when the program starts, and the modules
// are installed where the application is wired, which is later.
func TestASetCompiledBeforeTheModulesWereInstalledIsResolvedByTheFactory(t *testing.T) {
	early, err := validation.Compile(validation.Rules{"cpf": "required|cpf"}, validation.WithModuleRules())
	if err != nil {
		t.Fatalf("the set could not be compiled early: %v", err)
	}

	factory := portuguese()
	if err := factory.Check(early); err == nil || !strings.Contains(err.Error(), `"cpf"`) || !strings.Contains(err.Error(), "locale_test.go") {
		t.Errorf("Check with no module installed = %v, want it to name the rule and the file", err)
	}

	factory.Use(documents{})
	if err := factory.Check(early); err != nil {
		t.Fatalf("Check with the module installed = %v", err)
	}
	if got := factory.Make(validation.Data{"cpf": "222"}, early).Errors().First("cpf"); got != "O campo cpf não é um CPF válido." {
		t.Errorf("got %q", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("a set with a rule nothing declares ran without a panic")
		}
	}()
	validation.Make(validation.Data{"cpf": "222"}, early).Passes()
}

// TestATypoStaysATypoEvenWhenTheModulesAreLate.
func TestATypoStaysATypoEvenWhenTheModulesAreLate(t *testing.T) {
	if _, err := validation.Compile(validation.Rules{"f": "Requried"}, validation.WithModuleRules()); err == nil {
		t.Error("a name that cannot be a rule compiled")
	}
	if _, err := validation.Compile(validation.Rules{"f": "same_as"}, validation.WithModuleRules()); err == nil {
		t.Error("a refused name compiled")
	}
}

// ---------------------------------------------------------------------------
// Requests.
// ---------------------------------------------------------------------------

var personRulesCalls atomic.Int32

type person struct{}

func (p person) Rules() validation.Rules {
	personRulesCalls.Add(1)
	return validation.Rules{
		"cpf":   "required|cpf",
		"email": []any{"required", "email"},
	}
}

func (person) Attributes() map[string]string { return map[string]string{"cpf": "CPF"} }

type brokenRequest struct{}

func (brokenRequest) Rules() validation.Rules { return validation.Rules{"f": "required|nope"} }

// TestARequestsRulesAreCompiledOncePerType.
func TestARequestsRulesAreCompiledOncePerType(t *testing.T) {
	factory := portuguese().Use(documents{})
	personRulesCalls.Store(0)
	done := make(chan struct{})
	for i := 0; i < 32; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			set, err := factory.ForRequest(&person{})
			if err != nil || set == nil {
				t.Errorf("ForRequest = %v", err)
			}
		}()
	}
	for i := 0; i < 32; i++ {
		<-done
	}
	if personRulesCalls.Load() != 1 {
		t.Errorf("Rules() was called %d times, want once", personRulesCalls.Load())
	}
}

// TestARequestIsValidatedWithItsAttributeNames.
func TestARequestIsValidatedWithItsAttributeNames(t *testing.T) {
	factory := portuguese().Use(documents{})
	_, err := factory.ValidateRequest(person{}, validation.Data{"cpf": "", "email": "x"})
	var invalid *validation.ValidationException
	if err == nil {
		t.Fatal("no error")
	}
	invalid, _ = err.(*validation.ValidationException)
	if invalid == nil {
		t.Fatalf("err = %T", err)
	}
	got := invalid.Errors()
	if got["cpf"][0] != "O campo CPF é obrigatório." || got["email"][0] != "O campo email deve ser um endereço de e-mail válido." {
		t.Errorf("errors = %v", got)
	}
}

// TestTheBootCheckNamesTheRequestTheFieldTheRuleAndTheFile.
func TestTheBootCheckNamesTheRequestTheFieldTheRuleAndTheFile(t *testing.T) {
	factory := portuguese()
	err := factory.CheckRequests(&person{}, brokenRequest{})
	if err == nil {
		t.Fatal("CheckRequests passed")
	}
	for _, want := range []string{"brokenRequest.Rules", `field "f"`, "nope", "locale_test.go", `"cpf"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not mention %q", err, want)
		}
	}
	if err := portuguese().Use(documents{}).CheckRequests(&person{}); err != nil {
		t.Errorf("with the module installed: %v", err)
	}
}
