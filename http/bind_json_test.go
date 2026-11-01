package http_test

import (
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
)

// eventsDocument is a nested document: an object holding a list of objects,
// each with an optional object of its own.
type eventsDocument struct {
	Source string `json:"source"`
	Events []struct {
		ID   string `json:"id"`
		Qty  int8   `json:"qty"`
		Cost *struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"cost"`
	} `json:"events"`
}

func jsonCtx(body string) *hhttp.Context {
	r := httptest.NewRequest(stdhttp.MethodPost, "/events", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return hhttp.NewContext(httptest.NewRecorder(), r, nil, nil)
}

// TestBindJSONFillsNestedObjectsAndArrays: Bind reads top-level scalars; the
// body of /api/v1 is a document, and every level of it lands in the struct.
func TestBindJSONFillsNestedObjectsAndArrays(t *testing.T) {
	ctx := jsonCtx(`{"source":"sdk","events":[{"id":"a","qty":2,"cost":{"amount":"1.50","currency":"BRL"}},{"id":"b"}]}`)

	var in eventsDocument
	if err := ctx.BindJSON(&in); err != nil {
		t.Fatalf("BindJSON: %v", err)
	}
	if in.Source != "sdk" || len(in.Events) != 2 || in.Events[0].Qty != 2 || in.Events[1].ID != "b" {
		t.Fatalf("document = %+v", in)
	}
	if in.Events[0].Cost == nil || in.Events[0].Cost.Currency != "BRL" || in.Events[1].Cost != nil {
		t.Fatalf("nested object = %+v, %+v", in.Events[0].Cost, in.Events[1].Cost)
	}
}

// TestBindJSONRefusesAFieldTheStructDoesNotDeclare: the struct is the list of
// what may arrive. A dropped field would let a misspelled option pass for
// accepted.
func TestBindJSONRefusesAFieldTheStructDoesNotDeclare(t *testing.T) {
	for name, body := range map[string]string{
		"top level": `{"source":"sdk","is_admin":true}`,
		"nested":    `{"events":[{"id":"a","extra":1}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var in eventsDocument
			errs := fieldErrors(t, jsonCtx(body).BindJSON(&in))
			if got := errs.Get(hhttp.JSONBodyKey); len(got) != 1 || !strings.Contains(got[0], "unknown field") {
				t.Fatalf("errors = %v, want an unknown field on %q", errs, hhttp.JSONBodyKey)
			}
		})
	}
}

// TestBindJSONRefusesABodyThatIsNotOneJSONDocument.
func TestBindJSONRefusesABodyThatIsNotOneJSONDocument(t *testing.T) {
	for name, body := range map[string]string{
		"empty":     ``,
		"spaces":    "  \n",
		"not json":  `source=sdk`,
		"truncated": `{"source":"sdk"`,
		"trailing":  `{"source":"sdk"} {"source":"other"}`,
		"garbage":   `{"source":"sdk"} x`,
	} {
		t.Run(name, func(t *testing.T) {
			var in eventsDocument
			errs := fieldErrors(t, jsonCtx(body).BindJSON(&in))
			if len(errs.Get(hhttp.JSONBodyKey)) == 0 {
				t.Fatalf("errors = %v, want a message on %q", errs, hhttp.JSONBodyKey)
			}
		})
	}
}

// TestBindJSONNamesTheFieldOfAWrongType: a value of the wrong type is a message
// on the field, by the path of the json names, with Bind's sentence for it.
func TestBindJSONNamesTheFieldOfAWrongType(t *testing.T) {
	cases := map[string]struct{ body, field, message string }{
		"text for a number":  {`{"events":[{"qty":"two"}]}`, "events.qty", "must be a whole number"},
		"fraction":           {`{"events":[{"qty":1.5}]}`, "events.qty", "must be a whole number"},
		"over the size":      {`{"events":[{"qty":300}]}`, "events.qty", "is out of range"},
		"number for text":    {`{"source":7}`, "source", "must be text"},
		"text for a list":    {`{"events":"x"}`, "events", "must be a list"},
		"text for an object": {`{"events":[{"cost":"x"}]}`, "events.cost", "must be an object"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var in eventsDocument
			errs := fieldErrors(t, jsonCtx(c.body).BindJSON(&in))
			if got := errs.Get(c.field); len(got) != 1 || got[0] != c.message {
				t.Fatalf("errors = %v, want %q on %q", errs, c.message, c.field)
			}
		})
	}
}

// TestBindJSONRefusesABodyOverTheLimitWith413: a document cut short would bind
// as a smaller one, so nothing is bound.
func TestBindJSONRefusesABodyOverTheLimitWith413(t *testing.T) {
	body := `{"source":"` + strings.Repeat("a", 200) + `"}`

	var in eventsDocument
	assert413(t, jsonCtx(body).BindJSON(&in, hhttp.JSONLimit(64)))
	if in.Source != "" {
		t.Fatalf("a body over the limit bound %d bytes of source", len(in.Source))
	}

	if err := jsonCtx(body).BindJSON(&in, hhttp.JSONLimit(int64(len(body)))); err != nil {
		t.Fatalf("a body exactly at the limit: %v", err)
	}
}

// TestBindJSONDefaultsToAMegabyte.
func TestBindJSONDefaultsToAMegabyte(t *testing.T) {
	body := `{"source":"` + strings.Repeat("a", int(hhttp.DefaultJSONLimit)) + `"}`

	var in eventsDocument
	assert413(t, jsonCtx(body).BindJSON(&in))
}

// TestBindJSONAnswersABodyTheServerCutOffWith413: the body-size middleware's
// reader fails in the middle, and the document that arrived is not the one sent.
func TestBindJSONAnswersABodyTheServerCutOffWith413(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/json", `{"source":"`+strings.Repeat("a", 4096)+`"}`, 64)
	ctx := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil)

	var in eventsDocument
	assert413(t, ctx.BindJSON(&in))
}

// TestBindJSONPutsTheBodyBack: an endpoint that hashes the body for an
// idempotency key reads it first, and one that logs it reads it after.
func TestBindJSONPutsTheBodyBack(t *testing.T) {
	const body = `{"source":"sdk"}`
	ctx := jsonCtx(body)

	var in eventsDocument
	if err := ctx.BindJSON(&in); err != nil {
		t.Fatalf("BindJSON: %v", err)
	}
	if err := ctx.BindJSON(&in); err != nil {
		t.Fatalf("a second BindJSON: %v", err)
	}
	again, err := io.ReadAll(ctx.Request.Body)
	if err != nil || string(again) != body {
		t.Fatalf("body after BindJSON = %q, %v; want it whole", again, err)
	}
}

// TestBindJSONNeedsAPointer: a dst that cannot be written is a mistake in the
// program, not a 422 for the person who sent the body.
func TestBindJSONNeedsAPointer(t *testing.T) {
	var in eventsDocument
	for name, dst := range map[string]any{"value": in, "nil pointer": (*eventsDocument)(nil), "nil": nil} {
		err := jsonCtx(`{}`).BindJSON(dst)
		var errs validation.Errors
		if err == nil || errors.As(err, &errs) {
			t.Errorf("%s: error = %v, want an ordinary error", name, err)
		}
	}
}

// batchRequest declares rules over a nested document.
type batchRequest struct {
	Items []struct {
		Name string `json:"name"`
		Qty  int    `json:"qty"`
	} `json:"items"`
}

func (batchRequest) Rules() validation.Rules {
	return validation.Rules{
		"items":        "required|array|min:1",
		"items.*.name": "required|string",
		"items.*.qty":  []any{"required", "integer", "min:1"},
	}
}

// TestBindJSONRunsTheRulesTheRequestDeclares: the same hook as Bind. A failure
// is a 422 problem document in the language of the request with nothing bound,
// and what passed is in Validated.
func TestBindJSONRunsTheRulesTheRequestDeclares(t *testing.T) {
	run := func(body string) (*httptest.ResponseRecorder, batchRequest, validation.Input) {
		var in batchRequest
		var validated validation.Input
		rec := httptest.NewRecorder()
		hhttp.NewAdapter(nil, nil, nil, hhttp.WithValidator(factoryPT()))(func(c *hhttp.Context) error {
			if err := c.BindJSON(&in); err != nil {
				return err
			}
			validated = c.Validated()
			return nil
		}).ServeHTTP(rec, jsonPost(body, nil))
		return rec, in, validated
	}

	rec, in, validated := run(`{"items":[{"name":"a","qty":1},{"name":"b","qty":2}]}`)
	if rec.Code != stdhttp.StatusOK || len(in.Items) != 2 || in.Items[1].Name != "b" {
		t.Fatalf("a valid document: status %d, %+v", rec.Code, in)
	}
	if items, _ := validated.Tree()["items"].([]any); len(items) != 2 {
		t.Fatalf("Validated = %v", validated.Tree())
	}

	rec, in, _ = run(`{"items":[{"name":"","qty":0}]}`)
	var problem struct{ Errors map[string][]string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if rec.Code != stdhttp.StatusUnprocessableEntity ||
		problem.Errors["items.0.name"][0] != "O campo items.0.name é obrigatório." ||
		!strings.Contains(problem.Errors["items.0.qty"][0], "no mínimo 1") {
		t.Fatalf("status %d, errors = %v", rec.Code, problem.Errors)
	}
	if len(in.Items) != 0 {
		t.Fatalf("a document the rules refused was bound: %+v", in)
	}

	rec, _, _ = run(`[1,2]`)
	if rec.Code != stdhttp.StatusUnprocessableEntity {
		t.Fatalf("an array for a request with rules: status %d, want 422", rec.Code)
	}
}

// TestBindJSONSpeaksTheApplicationsLanguageForAWrongType.
func TestBindJSONSpeaksTheApplicationsLanguageForAWrongType(t *testing.T) {
	rec := httptest.NewRecorder()
	hhttp.NewAdapter(nil, nil, nil, hhttp.WithValidator(factoryPT()))(func(c *hhttp.Context) error {
		var in eventsDocument
		return c.BindJSON(&in)
	}).ServeHTTP(rec, jsonPost(`{"events":[{"qty":"two"}]}`, nil))

	var problem struct{ Errors map[string][]string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if rec.Code != stdhttp.StatusUnprocessableEntity || len(problem.Errors["events.qty"]) != 1 ||
		problem.Errors["events.qty"][0] == "must be a whole number" {
		t.Fatalf("status %d, errors = %v, want the portuguese sentence of the integer rule", rec.Code, problem.Errors)
	}
}

// TestDecodeJSONIsBindJSONForADocumentThatIsNotTheBody: a JSON value held in a
// field of another document is read as strictly as a body is.
func TestDecodeJSONIsBindJSONForADocumentThatIsNotTheBody(t *testing.T) {
	var in eventsDocument
	if err := hhttp.DecodeJSON([]byte(`{"source":"sdk","events":[{"id":"a"}]}`), &in); err != nil || in.Source != "sdk" || len(in.Events) != 1 {
		t.Fatalf("DecodeJSON = %v, %+v", err, in)
	}

	for name, body := range map[string]string{
		"empty":    ``,
		"unknown":  `{"nope":1}`,
		"trailing": `{} {}`,
		"not json": `x`,
	} {
		errs := fieldErrors(t, hhttp.DecodeJSON([]byte(body), &in))
		if len(errs.Get(hhttp.JSONBodyKey)) == 0 {
			t.Errorf("%s: errors = %v, want a message on %q", name, errs, hhttp.JSONBodyKey)
		}
	}

	errs := fieldErrors(t, hhttp.DecodeJSON([]byte(`{"events":[{"qty":"x"}]}`), &in))
	if got := errs.Get("events.qty"); len(got) != 1 || got[0] != "must be a whole number" {
		t.Errorf("errors = %v, want a whole number on events.qty", errs)
	}

	var errsValue validation.Errors
	if err := hhttp.DecodeJSON([]byte(`{}`), in); err == nil || errors.As(err, &errsValue) {
		t.Errorf("a dst that is not a pointer: error = %v, want an ordinary error", err)
	}
}
