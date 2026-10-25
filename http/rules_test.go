package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/validation"
)

// brDocuments is a module with the rules a Brazilian form needs, declared the
// way a module declares them. Its checks are stand-ins: what the test is about is
// how the rules reach the person, not how a CPF is computed.
type brDocuments struct{}

func (brDocuments) RegisterValidation(r *validation.Registry) {
	digits := func(n int) validation.ExtensionFunc {
		return func(_ *validation.Validator, _ string, value any, _ []string) bool {
			s, _ := value.(string)
			return len(s) == n && strings.Trim(s, "0123456789") == ""
		}
	}
	r.Add(validation.RuleDef{
		Name: "cpf", Check: digits(11),
		Messages: map[string]string{"pt-BR": "O campo :attribute não é um CPF válido.", "en": "The :attribute field is not a valid CPF."},
	})
	r.Add(validation.RuleDef{
		Name: "cnpj", Check: digits(14),
		Messages: map[string]string{"pt-BR": "O campo :attribute não é um CNPJ válido.", "en": "The :attribute field is not a valid CNPJ."},
	})
	r.Add(validation.RuleDef{
		Name: "formato_cep", Check: digits(8),
		Messages: map[string]string{"pt-BR": "O campo :attribute não tem o formato de um CEP.", "en": "The :attribute field is not a CEP."},
	})
	r.Add(validation.RuleDef{
		Name: "celular_com_ddd", Check: digits(11),
		Messages: map[string]string{"pt-BR": "O campo :attribute deve ser um celular com DDD.", "en": "The :attribute field must be a mobile phone with area code."},
	})
}

var order []string

// register is the request of the acceptance example, written the way the
// framework asks: the rules it declares, and the struct they fill.
type register struct {
	CPF     string `form:"cpf"`
	CNPJ    string `form:"cnpj"`
	CEP     string `form:"cep"`
	Celular string `form:"celular"`
	Age     int    `form:"age"`
}

var registerRulesCalled atomic.Int32

func (register) Rules() validation.Rules {
	registerRulesCalled.Add(1)
	order = append(order, "rules")
	return validation.Rules{
		"cpf":     []any{"required", "cpf"},
		"cnpj":    "required|cnpj",
		"cep":     []any{"required", "formato_cep"},
		"celular": "required|celular_com_ddd",
		"age":     "nullable|integer|min:18",
	}
}

func (register) Attributes() map[string]string {
	return map[string]string{"cpf": "CPF", "cnpj": "CNPJ"}
}

// Validate is the second step, the one the service calls.
func (r register) Validate() validation.Errors {
	order = append(order, "validate")
	return nil
}

func factoryPT() *validation.Factory {
	tr := translation.New(nil, "pt-BR", "en")
	return validation.NewFactory(validation.FromTranslation(tr)).Use(brDocuments{})
}

// controller is the action of the acceptance example: it binds, and answers with
// what the rules let through.
func controller(c *hhttp.Context) error {
	var in register
	if err := c.Bind(&in); err != nil {
		return err
	}
	if errs := in.Validate(); errs.Any() {
		return errs
	}
	c.Response.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(c.Response).Encode(map[string]any{"validated": c.Validated().Tree(), "bound": in})
}

func serve(t *testing.T, f *session.Flash, factory *validation.Factory, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	hhttp.NewAdapter(nil, nil, f, hhttp.WithValidator(factory))(controller).ServeHTTP(rec, req)
	return rec
}

func formPost(body url.Values, header map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://example.test/people", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req
}

func jsonPost(body string, header map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://example.test/people", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req
}

// TestAJSONRequestThatFailsTheRulesIsA422WithTheSentencesInPortuguese is the
// acceptance example: the controller binds, and the person gets, per field, what
// is wrong in their language with the field's name in it.
func TestAJSONRequestThatFailsTheRulesIsA422WithTheSentencesInPortuguese(t *testing.T) {
	rec := serve(t, nil, factoryPT(), jsonPost(`{"cpf":"","cnpj":"123","cep":"abc","celular":"11999999999"}`, nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var problem struct {
		Errors map[string][]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"cpf":  "O campo CPF é obrigatório.",
		"cnpj": "O campo CNPJ não é um CNPJ válido.",
		"cep":  "O campo cep não tem o formato de um CEP.",
	}
	for field, sentence := range want {
		if got := problem.Errors[field]; len(got) != 1 || got[0] != sentence {
			t.Errorf("errors[%s] = %v, want %q", field, got, sentence)
		}
	}
	if _, bad := problem.Errors["celular"]; bad {
		t.Errorf("celular passed and was reported: %v", problem.Errors["celular"])
	}
}

// TestAFormThatFailsTheRulesGoesBackWithTheMessagesAndWhatWasTyped.
func TestAFormThatFailsTheRulesGoesBackWithTheMessagesAndWhatWasTyped(t *testing.T) {
	f := session.NewFlash([]byte(strings.Repeat("k", 32)), false)
	req := formPost(url.Values{"cpf": {"123"}, "cnpj": {"12345678000199"}, "cep": {"50000000"}, "celular": {"81999999999"}, "note": {"typed"}},
		map[string]string{"Referer": "http://example.test/people/new", "Accept": "text/html"})
	rec := serve(t, f, factoryPT(), req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/people/new" {
		t.Fatalf("answered %d to %q, want a 303 back to the form", rec.Code, rec.Header().Get("Location"))
	}

	next := httptest.NewRequest(http.MethodGet, "/people/new", nil)
	next.Header.Set("Accept", "text/html")
	for _, c := range rec.Result().Cookies() {
		next.AddCookie(c)
	}
	errs, old, ok := f.Take(httptest.NewRecorder(), next)
	if !ok {
		t.Fatal("the form came back with nothing on it")
	}
	if got := errs["cpf"]; len(got) != 1 || got[0] != "O campo CPF não é um CPF válido." {
		t.Errorf("errors[cpf] = %v", got)
	}
	if len(old["note"]) != 1 || old["note"][0] != "typed" || old["cnpj"][0] != "12345678000199" {
		t.Errorf("old input = %v", old)
	}
}

// TestSuccessReturnsOnlyTheValidatedFields. The field nobody declared is not in
// what comes back, and the struct is filled as it always was.
func TestSuccessReturnsOnlyTheValidatedFields(t *testing.T) {
	rec := serve(t, nil, factoryPT(), jsonPost(
		`{"cpf":"12345678901","cnpj":"12345678000199","cep":"50000000","celular":"81999999999","is_admin":true,"age":30}`, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Validated map[string]any
		Bound     map[string]any
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, leaked := out.Validated["is_admin"]; leaked || len(out.Validated) != 5 {
		t.Errorf("validated = %v", out.Validated)
	}
	if out.Bound["CPF"] != "12345678901" || out.Bound["Age"] != 30.0 {
		t.Errorf("bound = %v", out.Bound)
	}
}

// TestTheLocaleOfTheRequestChoosesTheLanguage. One Factory answers both.
func TestTheLocaleOfTheRequestChoosesTheLanguage(t *testing.T) {
	handler := translation.Middleware([]string{"pt-BR", "en"}, "pt-BR")(
		hhttp.NewAdapter(nil, nil, nil, hhttp.WithValidator(factoryPT()))(controller))

	for _, c := range []struct{ accept, want string }{
		{"en", "The CPF field is required."},
		{"pt-BR", "O campo CPF é obrigatório."},
		{"", "O campo CPF é obrigatório."},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, jsonPost(`{"cpf":""}`, map[string]string{"Accept-Language": c.accept}))
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("Accept-Language %q: %s, want %q", c.accept, rec.Body, c.want)
		}
	}
}

// TestRulesRunBeforeTheConversionAndBeforeValidate pins the order of the steps: a
// value the rules refuse is never converted, and the second step is only reached
// when the first passed.
func TestRulesRunBeforeTheConversionAndBeforeValidate(t *testing.T) {
	order = nil
	rec := serve(t, nil, factoryPT(), jsonPost(`{"cpf":"","age":"not a number"}`, nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatal(rec.Code)
	}
	// Both the rule on age and the failed conversion would say something about
	// age; the rules spoke, and the conversion was never attempted.
	var problem struct{ Errors map[string][]string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if got := problem.Errors["age"]; len(got) == 0 || !strings.Contains(got[0], "inteiro") {
		t.Errorf("errors[age] = %v, want the sentence of the integer rule first", got)
	}
	for _, step := range order {
		if step == "validate" {
			t.Errorf("Validate ran although the rules failed: %v", order)
		}
	}

	order = nil
	rec = serve(t, nil, factoryPT(), jsonPost(`{"cpf":"12345678901","cnpj":"12345678000199","cep":"50000000","celular":"81999999999"}`, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(order) != 2 || order[0] != "rules" || order[1] != "validate" {
		t.Errorf("order = %v, want rules then validate", order)
	}
}

type nestedRequest struct{}

func (nestedRequest) Rules() validation.Rules {
	return validation.Rules{
		"items":        "required|array|min:1",
		"items.*.name": "required|string",
		"items.*.qty":  []any{"required", "integer", "min:1"},
	}
}

// TestNestedInputIsValidatedAndReturnedNested.
func TestNestedInputIsValidatedAndReturnedNested(t *testing.T) {
	factory := factoryPT()
	run := func(body string) (*httptest.ResponseRecorder, validation.Input) {
		var validated validation.Input
		rec := httptest.NewRecorder()
		hhttp.NewAdapter(nil, nil, nil, hhttp.WithValidator(factory))(func(c *hhttp.Context) error {
			if err := c.Bind(&nestedRequest{}); err != nil {
				return err
			}
			validated = c.Validated()
			return nil
		}).ServeHTTP(rec, jsonPost(body, nil))
		return rec, validated
	}

	rec, in := run(`{"items":[{"name":"a","qty":1,"price":9},{"name":"b","qty":2}],"x":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	tree := in.Tree()
	items := tree["items"].([]any)
	if len(tree) != 1 || len(items) != 2 || items[0].(validation.Data)["price"] != nil || items[1].(validation.Data)["name"] != "b" {
		t.Errorf("tree = %v", tree)
	}

	rec, _ = run(`{"items":[{"name":"","qty":0}]}`)
	var problem struct{ Errors map[string][]string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if problem.Errors["items.0.name"][0] != "O campo items.0.name é obrigatório." ||
		!strings.Contains(problem.Errors["items.0.qty"][0], "no mínimo 1") {
		t.Errorf("errors = %v", problem.Errors)
	}
}

// TestARequestWithoutRulesBindsAsItAlwaysDid.
func TestARequestWithoutRulesBindsAsItAlwaysDid(t *testing.T) {
	type plain struct {
		Name string `form:"name"`
	}
	ctx := post("/x", url.Values{"name": {"Ada"}})
	var in plain
	if err := ctx.Bind(&in); err != nil || in.Name != "Ada" {
		t.Errorf("Bind = %v, %+v", err, in)
	}
	if len(ctx.Validated().Tree()) != 0 {
		t.Error("Validated holds something although no rules ran")
	}
}

// TestTheConversionSpeaksTheApplicationsLanguageWhenOneIsWired.
func TestTheConversionSpeaksTheApplicationsLanguageWhenOneIsWired(t *testing.T) {
	type typed struct {
		Age int `form:"age"`
	}
	req := formPost(url.Values{"age": {"abc"}}, nil)
	req = req.WithContext(hhttp.WithValidation(req.Context(), factoryPT()))
	ctx := hhttp.NewContext(httptest.NewRecorder(), req, nil, nil)
	err := ctx.Bind(&typed{})
	errs, ok := err.(validation.Errors)
	if !ok || errs.First("age") != "O campo age deve ser um número inteiro." {
		t.Errorf("Bind = %#v", err)
	}

	// With nothing wired the short sentence stays.
	bare := post("/x", url.Values{"age": {"abc"}})
	if errs, _ := bare.Bind(&typed{}).(validation.Errors); errs.First("age") != "must be a whole number" {
		t.Errorf("unconfigured: %v", errs)
	}
}

// TestRequestValidateUsesTheApplicationsFactory.
func TestRequestValidateUsesTheApplicationsFactory(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"name": "required"})
	req := formPost(url.Values{"name": {""}}, nil)
	req = req.WithContext(hhttp.WithValidation(req.Context(), factoryPT()))

	_, err := hhttp.NewRequest(req).Validate(set)
	var invalid *validation.ValidationException
	invalid, _ = err.(*validation.ValidationException)
	if invalid == nil || invalid.Errors()["name"][0] != "O campo name é obrigatório." {
		t.Errorf("Validate = %v", err)
	}
}

// TestTheBootCheckOfTheRequests.
func TestTheBootCheckOfTheRequests(t *testing.T) {
	if err := factoryPT().CheckRequests(register{}, nestedRequest{}); err != nil {
		t.Errorf("CheckRequests = %v", err)
	}
	bare := validation.NewFactory(nil)
	if err := bare.CheckRequests(register{}); err == nil || !strings.Contains(err.Error(), `"cpf"`) {
		t.Errorf("without the module installed: %v", err)
	}
}
