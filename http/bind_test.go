package http_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
)

// post builds a Context for a url-encoded POST of form to target.
func post(target string, form url.Values) *hhttp.Context {
	r := httptest.NewRequest(stdhttp.MethodPost, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return hhttp.NewContext(httptest.NewRecorder(), r, nil, nil)
}

// get builds a Context for a GET of target.
func get(target string) *hhttp.Context {
	return hhttp.NewContext(httptest.NewRecorder(), httptest.NewRequest(stdhttp.MethodGet, target, nil), nil, nil)
}

// fieldErrors asserts err is a validation.Errors and returns it.
func fieldErrors(t *testing.T, err error) validation.Errors {
	t.Helper()
	var errs validation.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("error = %#v, want validation.Errors so the handler can hand it to Reject", err)
	}
	return errs
}

type signUpRequest struct {
	Name    string `form:"name"`
	Email   string `form:"email"`
	IsAdmin bool
	Role    string `form:"role"`
	Note    string `form:"-"`
}

// TestBindIsTheAllowlist is the guarantee the request struct exists for: a key
// the struct does not declare under that name reaches no field. A form that
// sends is_admin=1 against a struct with an untagged IsAdmin must leave it
// false, and a tagged field takes only its own key.
func TestBindIsTheAllowlist(t *testing.T) {
	ctx := post("/sign-up", url.Values{
		"name":     {"Ana"},
		"email":    {"ana@example.com"},
		"is_admin": {"1"},
		"IsAdmin":  {"1"},
		"Role":     {"owner"},
		"admin":    {"true"},
		"-":        {"smuggled"},
	})

	var req signUpRequest
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	want := signUpRequest{Name: "Ana", Email: "ana@example.com"}
	if req != want {
		t.Fatalf("Bind wrote %+v, want %+v: only a tagged field may be written, and only from its own key", req, want)
	}
}

// TestBindTrimsEveryValue: a value pasted with a trailing space is the most
// common way a lookup by email misses, and every controller used to trim by
// hand.
func TestBindTrimsEveryValue(t *testing.T) {
	type request struct {
		Email  string   `form:"email"`
		Amount int      `form:"amount"`
		Paid   bool     `form:"paid"`
		Note   *string  `form:"note"`
		Tags   []string `form:"tags"`
	}
	ctx := post("/", url.Values{
		"email":  {"  ana@example.com \t"},
		"amount": {" 42 "},
		"paid":   {" on "},
		"note":   {"   "},
		"tags":   {" a ", "b  "},
	})

	var req request
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Email != "ana@example.com" || req.Amount != 42 || !req.Paid {
		t.Fatalf("Bind wrote %+v, want every value trimmed before it is converted", req)
	}
	if req.Note != nil {
		t.Fatalf("note = %q, want nil: a value that is only whitespace is empty", *req.Note)
	}
	if !reflect.DeepEqual(req.Tags, []string{"a", "b"}) {
		t.Fatalf("tags = %q, want each value trimmed", req.Tags)
	}
}

// passwordForm is a change-password screen: the keys Bind leaves as typed
// beside keys that only look like them, which it trims.
type passwordForm struct {
	Email                   string   `form:"email"`
	Name                    string   `form:"name"`
	Password                string   `form:"password"`
	PasswordConfirmation    string   `form:"password_confirmation"`
	CurrentPassword         *string  `form:"current_password"`
	NewPassword             string   `form:"new_password"`
	NewPasswordConfirmation string   `form:"new_password_confirmation"`
	RecoveryPassword        []string `form:"recovery_password"`
	PasswordHint            string   `form:"password_hint"`
	Passwordless            string   `form:"passwordless"`
}

// checkPasswordForm asserts that every password key arrived as typed and every
// other key was trimmed.
func checkPasswordForm(t *testing.T, req passwordForm) {
	t.Helper()
	if req.Password != " secret " || req.PasswordConfirmation != " secret " {
		t.Errorf("password = %q, password_confirmation = %q, want both as typed: a password set with a space at its ends could never be typed again to sign in",
			req.Password, req.PasswordConfirmation)
	}
	if req.CurrentPassword == nil {
		t.Errorf("current_password = nil, want a pointer to the text as typed")
	} else if *req.CurrentPassword != "\told one " {
		t.Errorf("current_password = %q, want the text as typed", *req.CurrentPassword)
	}
	if req.NewPassword != " new " || req.NewPasswordConfirmation != " new " {
		t.Errorf("new_password = %q, new_password_confirmation = %q, want both as typed: confirmed compares the two, and trimming one of them refuses a password that matches",
			req.NewPassword, req.NewPasswordConfirmation)
	}
	if !reflect.DeepEqual(req.RecoveryPassword, []string{" a ", "b "}) {
		t.Errorf("recovery_password = %q, want every value as typed", req.RecoveryPassword)
	}
	if req.Email != "ana@example.com" || req.Name != "Ana" {
		t.Errorf("email = %q, name = %q, want both trimmed: only a password key is left as typed", req.Email, req.Name)
	}
	if req.PasswordHint != "pet" || req.Passwordless != "yes" {
		t.Errorf("password_hint = %q, passwordless = %q, want both trimmed: the rule is a key ending in _password, not a key containing the word",
			req.PasswordHint, req.Passwordless)
	}
}

// TestBindLeavesAPasswordAsTyped: Bind trimmed every value, password included,
// so a password chosen with a space at its ends was stored with the space by
// a form that did not trim and then refused at every sign in through one that
// did.
func TestBindLeavesAPasswordAsTyped(t *testing.T) {
	ctx := post("/", url.Values{
		"email":                     {" ana@example.com "},
		"name":                      {" Ana\t"},
		"password":                  {" secret "},
		"password_confirmation":     {" secret "},
		"current_password":          {"\told one "},
		"new_password":              {" new "},
		"new_password_confirmation": {" new "},
		"recovery_password":         {" a ", "b "},
		"password_hint":             {" pet "},
		"passwordless":              {" yes "},
	})

	var req passwordForm
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	checkPasswordForm(t, req)
}

// TestBindLeavesAJSONPasswordAsTyped: a JSON body reaches the same conversion
// as a form, so the password keys keep their spaces there too.
func TestBindLeavesAJSONPasswordAsTyped(t *testing.T) {
	body := `{"email":" ana@example.com ","name":" Ana\t","password":" secret ","password_confirmation":" secret ",` +
		`"current_password":"\told one ","new_password":" new ","new_password_confirmation":" new ",` +
		`"recovery_password":[" a ","b "],"password_hint":" pet ","passwordless":" yes "}`
	r := httptest.NewRequest(stdhttp.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req passwordForm
	if err := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	checkPasswordForm(t, req)
}

// numbers holds one field of every numeric kind Bind converts into.
type numbers struct {
	Int     int     `form:"int"`
	Int8    int8    `form:"int8"`
	Int16   int16   `form:"int16"`
	Int32   int32   `form:"int32"`
	Int64   int64   `form:"int64"`
	Uint    uint    `form:"uint"`
	Uint8   uint8   `form:"uint8"`
	Uint16  uint16  `form:"uint16"`
	Uint32  uint32  `form:"uint32"`
	Uint64  uint64  `form:"uint64"`
	Float32 float32 `form:"float32"`
	Float64 float64 `form:"float64"`
}

func TestBindConvertsEveryNumericKind(t *testing.T) {
	ctx := post("/", url.Values{
		"int": {"-7"}, "int8": {"-128"}, "int16": {"32767"}, "int32": {"-2147483648"}, "int64": {"9223372036854775807"},
		"uint": {"7"}, "uint8": {"255"}, "uint16": {"65535"}, "uint32": {"4294967295"}, "uint64": {"18446744073709551615"},
		"float32": {"1.5"}, "float64": {"-0.25"},
	})

	var got numbers
	if err := ctx.Bind(&got); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	want := numbers{
		Int: -7, Int8: -128, Int16: 32767, Int32: -2147483648, Int64: 9223372036854775807,
		Uint: 7, Uint8: 255, Uint16: 65535, Uint32: 4294967295, Uint64: 18446744073709551615,
		Float32: 1.5, Float64: -0.25,
	}
	if got != want {
		t.Fatalf("Bind wrote %+v, want %+v", got, want)
	}
}

// TestAValueThatDoesNotConvertIsAFieldError: the form is a person's typing, so
// a wrong value is their mistake and goes back to them on the field, never a
// panic and never a 500.
func TestAValueThatDoesNotConvertIsAFieldError(t *testing.T) {
	for _, tc := range []struct {
		key, value, message string
	}{
		{"int", "abc", "must be a whole number"},
		{"int", "1.5", "must be a whole number"},
		{"int8", "128", "is out of range"},
		{"int64", "99999999999999999999", "is out of range"},
		{"uint", "-1", "must be a whole number of zero or more"},
		{"uint8", "256", "is out of range"},
		{"float64", "twelve", "must be a number"},
		{"float64", "NaN", "must be a number"},
		{"float64", "Inf", "must be a number"},
		{"float32", "1e39", "is out of range"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			var got numbers
			errs := fieldErrors(t, post("/", url.Values{tc.key: {tc.value}}).Bind(&got))

			if messages := errs.Get(tc.key); len(messages) != 1 || messages[0] != tc.message {
				t.Fatalf("errors[%q] = %q, want [%q]", tc.key, messages, tc.message)
			}
			if got != (numbers{}) {
				t.Fatalf("Bind wrote %+v, want the failed field left at zero", got)
			}
		})
	}
}

// TestEveryFailingFieldIsReported: the person fixes the form once, not once per
// field.
func TestEveryFailingFieldIsReported(t *testing.T) {
	var got numbers
	errs := fieldErrors(t, post("/", url.Values{"int": {"x"}, "uint8": {"999"}, "float64": {"1.25"}}).Bind(&got))

	if !errs.Has("int") || !errs.Has("uint8") || errs.Count() != 2 {
		t.Fatalf("errors = %v, want one message for int and one for uint8", errs)
	}
	if got.Float64 != 1.25 {
		t.Fatalf("float64 = %v, want the field that converted written even though others failed", got.Float64)
	}
}

type checkboxRequest struct {
	Subscribed bool `form:"subscribed"`
}

// TestABoolIsACheckbox: a checked box sends "on", an unchecked one sends
// nothing at all, and absent has to read as false -- not as "leave whatever
// was there".
func TestABoolIsACheckbox(t *testing.T) {
	for _, tc := range []struct {
		form url.Values
		want bool
	}{
		{url.Values{"subscribed": {"on"}}, true},
		{url.Values{"subscribed": {"ON"}}, true},
		{url.Values{"subscribed": {"true"}}, true},
		{url.Values{"subscribed": {"1"}}, true},
		{url.Values{"subscribed": {"yes"}}, true},
		{url.Values{"subscribed": {"off"}}, false},
		{url.Values{"subscribed": {"false"}}, false},
		{url.Values{"subscribed": {"0"}}, false},
		{url.Values{"subscribed": {"no"}}, false},
		{url.Values{"subscribed": {""}}, false},
		{url.Values{}, false},
	} {
		t.Run(tc.form.Encode(), func(t *testing.T) {
			req := checkboxRequest{Subscribed: true}
			if err := post("/", tc.form).Bind(&req); err != nil {
				t.Fatalf("Bind: %v", err)
			}
			if req.Subscribed != tc.want {
				t.Fatalf("subscribed = %v, want %v", req.Subscribed, tc.want)
			}
		})
	}
}

func TestABoolThatIsNeitherIsAFieldError(t *testing.T) {
	var req checkboxRequest
	errs := fieldErrors(t, post("/", url.Values{"subscribed": {"maybe"}}).Bind(&req))
	if got := errs.First("subscribed"); got != "must be true or false" {
		t.Fatalf("errors[subscribed] = %q, want %q", got, "must be true or false")
	}
}

type scheduleRequest struct {
	StartsAt time.Time `form:"starts_at"`
}

// TestATimeReadsWhatTheBrowserSends: the two date inputs a browser has send
// two layouts, neither of them RFC 3339, and an API client sends RFC 3339.
func TestATimeReadsWhatTheBrowserSends(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Time
	}{
		{"2026-10-01", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-10-01T14:30", time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)},
		{"2026-10-01T14:30:15", time.Date(2026, 10, 1, 14, 30, 15, 0, time.UTC)},
		{"2026-10-01T14:30:15-03:00", time.Date(2026, 10, 1, 17, 30, 15, 0, time.UTC)},
	} {
		t.Run(tc.value, func(t *testing.T) {
			var req scheduleRequest
			if err := post("/", url.Values{"starts_at": {tc.value}}).Bind(&req); err != nil {
				t.Fatalf("Bind: %v", err)
			}
			if !req.StartsAt.Equal(tc.want) {
				t.Fatalf("starts_at = %v, want %v", req.StartsAt, tc.want)
			}
		})
	}
}

func TestATimeThatDoesNotParseIsAFieldError(t *testing.T) {
	for _, value := range []string{"2026-13-01", "01/10/2026", "tomorrow"} {
		t.Run(value, func(t *testing.T) {
			var req scheduleRequest
			errs := fieldErrors(t, post("/", url.Values{"starts_at": {value}}).Bind(&req))
			if got := errs.First("starts_at"); got != "is not a valid date" {
				t.Fatalf("errors[starts_at] = %q, want %q", got, "is not a valid date")
			}
			if !req.StartsAt.IsZero() {
				t.Fatalf("starts_at = %v, want zero after a failed conversion", req.StartsAt)
			}
		})
	}
}

type optionalRequest struct {
	Note     *string    `form:"note"`
	Quantity *int       `form:"quantity"`
	Featured *bool      `form:"featured"`
	DueOn    *time.Time `form:"due_on"`
}

// TestAPointerSaysWhetherAnythingWasSent: nil for absent and for empty, so a
// handler can tell "no quantity" from "a quantity of zero".
func TestAPointerSaysWhetherAnythingWasSent(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		note, quantity := "stale", 9
		req := optionalRequest{Note: &note, Quantity: &quantity}
		if err := post("/", url.Values{}).Bind(&req); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		if req != (optionalRequest{}) {
			t.Fatalf("Bind wrote %+v, want every pointer nil when nothing was sent", req)
		}
	})

	t.Run("empty", func(t *testing.T) {
		var req optionalRequest
		form := url.Values{"note": {""}, "quantity": {""}, "featured": {""}, "due_on": {""}}
		if err := post("/", form).Bind(&req); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		if req != (optionalRequest{}) {
			t.Fatalf("Bind wrote %+v, want every pointer nil for an empty value", req)
		}
	})

	t.Run("zero", func(t *testing.T) {
		var req optionalRequest
		form := url.Values{"note": {"x"}, "quantity": {"0"}, "featured": {"off"}, "due_on": {"2026-10-01"}}
		if err := post("/", form).Bind(&req); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		if req.Note == nil || *req.Note != "x" || req.Quantity == nil || *req.Quantity != 0 ||
			req.Featured == nil || *req.Featured || req.DueOn == nil || req.DueOn.Day() != 1 {
			t.Fatalf("Bind wrote %+v, want a pointer to each value, zero included", req)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		var req optionalRequest
		errs := fieldErrors(t, post("/", url.Values{"quantity": {"many"}}).Bind(&req))
		if req.Quantity != nil {
			t.Fatalf("quantity = %d, want nil after a failed conversion", *req.Quantity)
		}
		if got := errs.First("quantity"); got != "must be a whole number" {
			t.Fatalf("errors[quantity] = %q", got)
		}
	})
}

type filterRequest struct {
	Status string   `form:"status"`
	Tags   []string `form:"tags"`
}

// TestARepeatedKeyFillsASlice: a <select multiple> sends its key once per
// option chosen, and a scalar field takes the first.
func TestARepeatedKeyFillsASlice(t *testing.T) {
	var req filterRequest
	if err := get("/?tags=red&tags=blue&tags=red&status=open&status=closed").Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !reflect.DeepEqual(req.Tags, []string{"red", "blue", "red"}) {
		t.Fatalf("tags = %q, want every value in order", req.Tags)
	}
	if req.Status != "open" {
		t.Fatalf("status = %q, want the first value", req.Status)
	}

	stale := filterRequest{Tags: []string{"stale"}}
	if err := get("/").Bind(&stale); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if stale.Tags != nil {
		t.Fatalf("tags = %q, want nil when the key never arrived", stale.Tags)
	}
}

// TestAGetReadsTheQuery and the POST below pin where the values come from: the
// query on a GET, and the body alone on a POST.
func TestAGetReadsTheQuery(t *testing.T) {
	var req filterRequest
	if err := get("/invoices?status=paid").Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Status != "paid" {
		t.Fatalf("status = %q, want the query value on a GET", req.Status)
	}
}

func TestAPostReadsTheBodyAndNeverTheQuery(t *testing.T) {
	var req filterRequest
	if err := post("/invoices?status=from-query&tags=q", url.Values{"status": {"from-body"}, "tags": {"b"}}).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Status != "from-body" {
		t.Fatalf("status = %q, want the body's value", req.Status)
	}
	if !reflect.DeepEqual(req.Tags, []string{"b"}) {
		t.Fatalf("tags = %q, want the body's values and none of the query's", req.Tags)
	}
}

// TestAMultipartFormBindsItsTextFields: a form that uploads a file is still a
// form. Its text fields are read, and the query string is not, as for a
// url-encoded body: the value a field takes must not depend on the form's
// enctype.
func TestAMultipartFormBindsItsTextFields(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("status", " draft ")
	part, _ := writer.CreateFormFile("attachment", "invoice.pdf")
	_, _ = part.Write([]byte("%PDF-1.7"))
	_ = writer.Close()

	r := httptest.NewRequest(stdhttp.MethodPost, "/?status=from-query", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	var req filterRequest
	if err := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Status != "draft" {
		t.Fatalf("status = %q, want the multipart text field, trimmed, and not the query's", req.Status)
	}
}

type Audit struct {
	Reason string `form:"reason"`
}

type refundRequest struct {
	Audit
	Amount int `form:"amount"`
}

func TestAnEmbeddedStructIsReadInto(t *testing.T) {
	var req refundRequest
	if err := post("/", url.Values{"reason": {"damaged"}, "amount": {"10"}}).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Reason != "damaged" || req.Amount != 10 {
		t.Fatalf("Bind wrote %+v, want the embedded struct's tagged field written too", req)
	}
}

type status string

func TestANamedStringTypeIsText(t *testing.T) {
	var req struct {
		Status status   `form:"status"`
		Labels []status `form:"labels"`
	}
	if err := post("/", url.Values{"status": {"open"}, "labels": {"a", "b"}}).Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Status != "open" || !reflect.DeepEqual(req.Labels, []status{"a", "b"}) {
		t.Fatalf("Bind wrote %+v", req)
	}
}

// TestAMistakeInTheProgramIsNotAFieldError: these are the handler's bugs, not
// the person's, so they must not come back as a message on the form -- and
// must not panic either.
func TestAMistakeInTheProgramIsNotAFieldError(t *testing.T) {
	var notAStruct int
	var nilPointer *signUpRequest
	var unexported struct {
		name string `form:"name"`
	}
	var duration struct {
		Timeout time.Duration `form:"timeout"`
	}
	var nested struct {
		Address struct{ City string } `form:"address"`
	}
	var intSlice struct {
		IDs []int `form:"ids"`
	}

	for _, tc := range []struct {
		name string
		dst  any
		want string
	}{
		{"a struct value", signUpRequest{}, "non-nil pointer to a struct"},
		{"a pointer to an int", &notAStruct, "non-nil pointer to a struct"},
		{"a nil pointer", nilPointer, "non-nil pointer to a struct"},
		{"nil", nil, "non-nil pointer to a struct"},
		{"an unexported field", &unexported, "unexported"},
		{"a duration", &duration, "Timeout"},
		{"a nested struct", &nested, "Address"},
		{"a slice of ints", &intSlice, "IDs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := post("/", url.Values{"name": {"x"}, "timeout": {"90"}, "ids": {"1"}}).Bind(tc.dst)
			if err == nil {
				t.Fatal("Bind accepted a destination it cannot write")
			}
			var errs validation.Errors
			if errors.As(err, &errs) {
				t.Fatalf("error = %v, a programming mistake reported as a field error the person would be asked to fix", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestAnUnreadableBodyIsAnError(t *testing.T) {
	r := httptest.NewRequest(stdhttp.MethodPost, "/", strings.NewReader("name=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var req signUpRequest
	err := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&req)
	if err == nil {
		t.Fatal("a body that does not decode was bound")
	}
	var errs validation.Errors
	if errors.As(err, &errs) {
		t.Fatalf("error = %v, want a request error rather than a message on a field", err)
	}
}

// TestBindSucceedsWithNoErrorValue: a nil validation.Errors in an error
// interface is not nil, and a handler checking err != nil would reject every
// valid form.
func TestBindSucceedsWithNoErrorValue(t *testing.T) {
	var req signUpRequest
	if err := post("/", url.Values{"name": {"Ana"}}).Bind(&req); err != nil {
		t.Fatalf("err = %#v, want a nil error when everything converted", err)
	}
}
