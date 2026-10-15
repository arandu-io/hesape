package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// messageForm is a free-text form: a Markdown body, a summary and a list kept
// as typed beside a subject that is trimmed as every other key is.
type messageForm struct {
	Subject string    `form:"subject"`
	Body    string    `form:"body,raw"`
	Summary *string   `form:"summary,raw"`
	Lines   []string  `form:"lines,raw"`
	Label   labelText `form:"label,raw"`
}

type labelText string

// TestARawFieldIsBoundAsTyped: a Markdown body loses the indentation that
// opens its code block and the newline that ends it when it is trimmed, so an
// application that stores text as typed read it by hand instead of binding it.
func TestARawFieldIsBoundAsTyped(t *testing.T) {
	ctx := post("/", url.Values{
		"subject": {"  Hello \n"},
		"body":    {"    indented code\n\nlast line\n"},
		"summary": {"   "},
		"lines":   {" a ", "\tb\n"},
		"label":   {" x "},
	})
	var req messageForm
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Subject != "Hello" {
		t.Errorf("subject = %q, want it trimmed: a field without raw is unchanged", req.Subject)
	}
	if req.Body != "    indented code\n\nlast line\n" {
		t.Errorf("body = %q, want the text as typed", req.Body)
	}
	if req.Summary == nil || *req.Summary != "   " {
		t.Errorf("summary = %v, want a pointer to the spaces as typed: whether they are acceptable is a rule", req.Summary)
	}
	if !reflect.DeepEqual(req.Lines, []string{" a ", "\tb\n"}) {
		t.Errorf("lines = %q, want every value as typed", req.Lines)
	}
	if req.Label != " x " {
		t.Errorf("label = %q, want a named string type as typed", req.Label)
	}
}

// TestARawFieldIsBoundAsTypedFromJSON: a JSON body reaches the same
// conversion as a form.
func TestARawFieldIsBoundAsTypedFromJSON(t *testing.T) {
	r := httptest.NewRequest(stdhttp.MethodPost, "/", strings.NewReader(`{"subject":" Hi ","body":"  - item\n"}`))
	r.Header.Set("Content-Type", "application/json")
	var req messageForm
	if err := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Subject != "Hi" || req.Body != "  - item\n" {
		t.Fatalf("subject = %q, body = %q, want the subject trimmed and the body as typed", req.Subject, req.Body)
	}
}

// TestARawOptionBindCannotHonourIsAMistakeInTheProgram: raw on a field that
// does not hold text, and an option Bind does not know, are refused rather than
// ignored -- a misspelled raw that bound as if absent would trim the text the
// tag was written to keep.
func TestARawOptionBindCannotHonourIsAMistakeInTheProgram(t *testing.T) {
	cases := map[string]any{
		"raw on a number": &struct {
			Count int `form:"count,raw"`
		}{},
		"raw on a bool": &struct {
			Paid bool `form:"paid,raw"`
		}{},
		"a misspelled option": &struct {
			Body string `form:"body,raws"`
		}{},
	}
	for name, dst := range cases {
		err := post("/", url.Values{"count": {"1"}, "paid": {"on"}, "body": {" x "}}).Bind(dst)
		if err == nil {
			t.Errorf("%s bound without an error", name)
			continue
		}
		if _, isFieldError := err.(interface{ Any() bool }); isFieldError {
			t.Errorf("%s answered a field error %v, want a mistake in the program", name, err)
		}
	}
}
