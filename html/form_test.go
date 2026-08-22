package html_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/html"
)

func newForm() (*html.FormBuilder, *fakeUrls) {
	urls := newFakeUrls()
	return html.NewFormBuilder(html.NewHtmlBuilder(urls), urls, "tok3n"), urls
}

func TestOpenWritesTheMethodActionAndToken(t *testing.T) {
	form, _ := newForm()

	got, err := form.Open(html.OpenOptions{URL: []string{"/invoices"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := `<form accept-charset="UTF-8" action="http://example.test/invoices" method="POST">` +
		`<input name="_token" type="hidden" value="tok3n">`
	if string(got) != want {
		t.Fatalf("Open = %q, want %q", got, want)
	}
}

func TestOpenFallsBackToTheCurrentUrl(t *testing.T) {
	form, urls := newForm()
	urls.current = "/here"

	got, err := form.Open(html.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !strings.Contains(string(got), `action="/here"`) {
		t.Fatalf("Open = %q, want the current URL", got)
	}
}

func TestOpenSpoofsTheMethod(t *testing.T) {
	form, _ := newForm()

	got, err := form.Open(html.OpenOptions{Method: "put", URL: []string{"/invoices/7"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !strings.Contains(string(got), `method="POST"`) {
		t.Errorf("Open = %q, want method POST", got)
	}
	if !strings.Contains(string(got), `name="_method" type="hidden" value="PUT"`) {
		t.Errorf("Open = %q, want the _method field", got)
	}
}

// A GET form carries neither the spoofer nor the token.
func TestOpenGetHasNoAppendage(t *testing.T) {
	form, _ := newForm()

	got, err := form.Open(html.OpenOptions{Method: "get", URL: []string{"/search"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if strings.Contains(string(got), "_token") || strings.Contains(string(got), "_method") {
		t.Fatalf("Open = %q, want no hidden fields", got)
	}
	if !strings.Contains(string(got), `method="GET"`) {
		t.Fatalf("Open = %q, want method GET", got)
	}
}

func TestOpenWithFilesSetsTheEnctype(t *testing.T) {
	form, _ := newForm()

	got, _ := form.Open(html.OpenOptions{Files: true})
	if !strings.Contains(string(got), `enctype="multipart/form-data"`) {
		t.Fatalf("Open = %q, want the enctype", got)
	}
}

// PHP's array_merge lets the caller's attributes win over the computed ones.
func TestOpenLetsTheCallerOverrideAComputedAttribute(t *testing.T) {
	form, _ := newForm()

	got, _ := form.Open(html.OpenOptions{Attributes: html.Attrs{"action": "/elsewhere", "class": "stack"}})
	if !strings.Contains(string(got), `action="/elsewhere"`) {
		t.Fatalf("Open = %q, want the caller's action", got)
	}
}

func TestOpenReportsAFailedRouteLookup(t *testing.T) {
	urls := newFakeUrls()
	urls.err = url.InvalidHostError("no such route")
	form := html.NewFormBuilder(html.NewHtmlBuilder(urls), urls, "tok3n")

	if _, err := form.Open(html.OpenOptions{Route: []string{"absent"}}); err == nil {
		t.Fatal("Open with an unknown route returned no error")
	}
}

func TestTokenWithoutOneIsAnError(t *testing.T) {
	urls := newFakeUrls()
	form := html.NewFormBuilder(html.NewHtmlBuilder(urls), urls, "")

	if _, err := form.Token(); !errors.Is(err, html.ErrNoToken) {
		t.Fatalf("Token = %v, want ErrNoToken", err)
	}
}

func TestTokenComesFromTheSessionWhenTheBuilderHasNone(t *testing.T) {
	urls := newFakeUrls()
	form := html.NewFormBuilder(html.NewHtmlBuilder(urls), urls, "")
	form.SetSessionStore(html.OldInput{Token: "from-session"})

	got, err := form.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !strings.Contains(string(got), `value="from-session"`) {
		t.Fatalf("Token = %q", got)
	}
}

func TestCloseForgetsTheModelAndTheLabels(t *testing.T) {
	form, _ := newForm()
	form.Label("email", "", nil)
	form.SetModel(html.ModelAttributes{"email": "ada@example.com"})

	if got := string(form.Close()); got != "</form>" {
		t.Fatalf("Close = %q", got)
	}
	if got := string(form.Text("email", "", nil)); strings.Contains(got, "ada@example.com") {
		t.Fatalf("Text after Close = %q, want no model value", got)
	}
	if got := string(form.Text("email", "", nil)); strings.Contains(got, `id="email"`) {
		t.Fatalf("Text after Close = %q, want no id from a forgotten label", got)
	}
}

func TestLabelTitleCasesTheName(t *testing.T) {
	form, _ := newForm()

	got := string(form.Label("first_name", "", nil))
	want := `<label for="first_name">First Name</label>`

	if got != want {
		t.Fatalf("Label = %q, want %q", got, want)
	}
}

// FormBuilder.php:202 writes the name into for="" unescaped.
func TestLabelEscapesTheName(t *testing.T) {
	form, _ := newForm()

	got := string(form.Label(`a" onmouseover="alert(1)`, "Name", nil))
	if strings.Contains(got, `onmouseover="alert(1)"`) {
		t.Fatalf("Label let the name close the attribute: %s", got)
	}
}

// A label drawn first gives the input its id, which is what ties the two.
func TestLabelGivesTheInputItsId(t *testing.T) {
	form, _ := newForm()
	form.Label("email", "", nil)

	if got := string(form.Text("email", "", nil)); !strings.Contains(got, `id="email"`) {
		t.Fatalf("Text = %q, want an id", got)
	}
}

func TestInputOmitsAnEmptyValue(t *testing.T) {
	form, _ := newForm()

	got := string(form.Text("email", "", nil))
	want := `<input name="email" type="text">`

	if got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
}

// PHP hands the empty string rather than null for a password, and that explicit
// empty value is what stops a browser restoring one.
func TestPasswordWritesAnEmptyValue(t *testing.T) {
	form, _ := newForm()

	got := string(form.Password("secret", nil))
	want := `<input name="secret" type="password" value="">`

	if got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
}

func TestPasswordIsNeverFilledFromOldInput(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"secret": {"hunter2"}}})

	if got := string(form.Password("secret", nil)); strings.Contains(got, "hunter2") {
		t.Fatalf("Password = %q, want no old value", got)
	}
}

func TestOldInputWinsOverTheGivenValue(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"email": {"typed@example.com"}}})

	if got := string(form.Text("email", "stored@example.com", nil)); !strings.Contains(got, `value="typed@example.com"`) {
		t.Fatalf("Text = %q, want the old input", got)
	}
}

func TestTheModelFillsTheInput(t *testing.T) {
	form, _ := newForm()
	form.SetModel(html.ModelAttributes{"email": "ada@example.com"})

	if got := string(form.Text("email", "", nil)); !strings.Contains(got, `value="ada@example.com"`) {
		t.Fatalf("Text = %q, want the model value", got)
	}
}

// FormBuilder::transformKey turns the array syntax into the session's key.
func TestTransformKeyReachesANestedName(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"address.city": {"Recife"}}})

	if got := string(form.Text("address[city]", "", nil)); !strings.Contains(got, `value="Recife"`) {
		t.Fatalf("Text = %q, want the old input under the transformed key", got)
	}
}

func TestTheTypedInputs(t *testing.T) {
	form, _ := newForm()

	for _, c := range []struct {
		got  string
		want string
	}{
		{string(form.Email("email", "", nil)), `type="email"`},
		{string(form.URL("site", "", nil)), `type="url"`},
		{string(form.Hidden("id", "7", nil)), `type="hidden"`},
		{string(form.File("avatar", nil)), `type="file"`},
		{string(form.Submit("Save", nil)), `type="submit"`},
		{string(form.Reset("Undo", nil)), `type="reset"`},
	} {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%q does not contain %q", c.got, c.want)
		}
	}
}

// Submit and Reset take no name, and PHP writes none.
func TestSubmitAndResetHaveNoName(t *testing.T) {
	form, _ := newForm()

	for _, got := range []string{string(form.Submit("Save", nil)), string(form.Reset("Undo", nil))} {
		if strings.Contains(got, "name=") {
			t.Errorf("%q has a name attribute", got)
		}
		if !strings.Contains(got, `value=`) {
			t.Errorf("%q has no value attribute", got)
		}
	}
}

func TestTextareaDefaultsItsSize(t *testing.T) {
	form, _ := newForm()

	got := string(form.Textarea("body", "hello", nil))
	want := `<textarea cols="50" name="body" rows="10">hello</textarea>`

	if got != want {
		t.Fatalf("Textarea = %q, want %q", got, want)
	}
}

func TestTextareaReadsTheSizeShortcut(t *testing.T) {
	form, _ := newForm()

	got := string(form.Textarea("body", "", html.Attrs{"size": "30x5"}))
	if !strings.Contains(got, `cols="30"`) || !strings.Contains(got, `rows="5"`) {
		t.Fatalf("Textarea = %q, want cols 30 and rows 5", got)
	}
	if strings.Contains(got, "size=") {
		t.Fatalf("Textarea = %q, want the size shortcut dropped", got)
	}
}

func TestTextareaEscapesItsBody(t *testing.T) {
	form, _ := newForm()

	if got := string(form.Textarea("body", "</textarea><script>", nil)); strings.Contains(got, "<script>") {
		t.Fatalf("Textarea did not escape its body: %s", got)
	}
}

func TestSelect(t *testing.T) {
	form, _ := newForm()

	got := string(form.Select("country", []html.Option{
		{Value: "br", Display: "Brazil"},
		{Value: "pt", Display: "Portugal"},
	}, []string{"pt"}, nil))

	want := `<select name="country">` +
		`<option value="br">Brazil</option>` +
		`<option selected="selected" value="pt">Portugal</option>` +
		`</select>`

	if got != want {
		t.Fatalf("Select = %q, want %q", got, want)
	}
}

func TestSelectEscapesTheDisplayAndTheValue(t *testing.T) {
	form, _ := newForm()

	got := string(form.Select("x", []html.Option{{Value: `a"b`, Display: "<b>c</b>"}}, nil, nil))
	if strings.Contains(got, "<b>") {
		t.Fatalf("Select did not escape the display: %s", got)
	}
	if !strings.Contains(got, `value="a&quot;b"`) {
		t.Fatalf("Select = %q, want the value escaped", got)
	}
}

// PHP takes the optgroup label from the array key, which is Value here.
func TestSelectWritesAnOptionGroup(t *testing.T) {
	form, _ := newForm()

	got := string(form.Select("city", []html.Option{
		{Value: "Brazil", Options: []html.Option{{Value: "rec", Display: "Recife"}}},
	}, nil, nil))

	want := `<select name="city"><optgroup label="Brazil"><option value="rec">Recife</option></optgroup></select>`
	if got != want {
		t.Fatalf("Select = %q, want %q", got, want)
	}
}

func TestSelectTakesTheSelectionFromOldInput(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"country": {"pt"}}})

	got := string(form.Select("country", []html.Option{
		{Value: "br", Display: "Brazil"},
		{Value: "pt", Display: "Portugal"},
	}, []string{"br"}, nil))

	if !strings.Contains(got, `<option selected="selected" value="pt">`) {
		t.Fatalf("Select = %q, want the old input selected", got)
	}
}

// A multiple select has more than one selected value, which is why the
// parameter is a slice.
func TestSelectSelectsMoreThanOne(t *testing.T) {
	form, _ := newForm()

	got := string(form.Select("tags", []html.Option{
		{Value: "a", Display: "A"},
		{Value: "b", Display: "B"},
		{Value: "c", Display: "C"},
	}, []string{"a", "c"}, html.Attrs{"multiple": "multiple"}))

	if strings.Count(got, `selected="selected"`) != 2 {
		t.Fatalf("Select = %q, want two selected options", got)
	}
}

func TestSelectRangeAndSelectYear(t *testing.T) {
	form, _ := newForm()

	got := string(form.SelectRange("n", 1, 3, nil, nil))
	want := `<select name="n"><option value="1">1</option><option value="2">2</option><option value="3">3</option></select>`
	if got != want {
		t.Fatalf("SelectRange = %q, want %q", got, want)
	}

	if year := string(form.SelectYear("y", 2024, 2026, nil, nil)); !strings.Contains(year, `<option value="2026">2026</option>`) {
		t.Fatalf("SelectYear = %q", year)
	}
}

// PHP's range() counts down when the end is below the start.
func TestSelectRangeCountsDown(t *testing.T) {
	form, _ := newForm()

	got := string(form.SelectRange("n", 3, 1, nil, nil))
	if !strings.HasPrefix(got, `<select name="n"><option value="3">`) {
		t.Fatalf("SelectRange = %q, want it to start at 3", got)
	}
	if strings.Count(got, "<option") != 3 {
		t.Fatalf("SelectRange = %q, want three options", got)
	}
}

func TestSelectMonth(t *testing.T) {
	form, _ := newForm()

	got := string(form.SelectMonth("month", []string{"3"}, nil, ""))
	if !strings.Contains(got, `<option selected="selected" value="3">March</option>`) {
		t.Fatalf("SelectMonth = %q", got)
	}
	if strings.Count(got, "<option") != 12 {
		t.Fatalf("SelectMonth = %q, want twelve options", got)
	}

	if short := string(form.SelectMonth("month", nil, nil, "Jan")); !strings.Contains(short, ">Jan<") {
		t.Fatalf("SelectMonth with a layout = %q", short)
	}
}

func TestCheckboxDefaultsToOneAndTakesTheGivenState(t *testing.T) {
	form, _ := newForm()
	checked := true

	got := string(form.Checkbox("terms", "", &checked, nil))
	want := `<input checked="checked" name="terms" type="checkbox" value="1">`

	if got != want {
		t.Fatalf("Checkbox = %q, want %q", got, want)
	}
}

// The branch nobody expects: an unchecked box posts nothing, so after a
// rejected submission it has to come back unchecked whatever its default says.
func TestCheckboxIsUncheckedWhenTheSubmissionOmittedIt(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"email": {"ada@example.com"}}})
	checked := true

	if got := string(form.Checkbox("terms", "", &checked, nil)); strings.Contains(got, "checked") {
		t.Fatalf("Checkbox = %q, want it unchecked", got)
	}
}

func TestCheckboxIsCheckedFromOldInput(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"tags": {"a", "c"}}})

	if got := string(form.Checkbox("tags", "c", nil, nil)); !strings.Contains(got, "checked") {
		t.Fatalf("Checkbox = %q, want it checked", got)
	}
	if got := string(form.Checkbox("tags", "b", nil, nil)); strings.Contains(got, "checked") {
		t.Fatalf("Checkbox = %q, want it unchecked", got)
	}
}

func TestCheckboxIsCheckedFromTheModel(t *testing.T) {
	form, _ := newForm()
	form.SetModel(html.ModelAttributes{"active": "1", "archived": "0"})

	if got := string(form.Checkbox("active", "", nil, nil)); !strings.Contains(got, "checked") {
		t.Fatalf("Checkbox = %q, want it checked", got)
	}
	if got := string(form.Checkbox("archived", "", nil, nil)); strings.Contains(got, "checked") {
		t.Fatalf("Checkbox = %q, want it unchecked", got)
	}
}

func TestRadioDefaultsItsValueToTheName(t *testing.T) {
	form, _ := newForm()

	got := string(form.Radio("plan", "", nil, nil))
	if !strings.Contains(got, `value="plan"`) {
		t.Fatalf("Radio = %q, want the name as the value", got)
	}
}

func TestRadioIsCheckedFromOldInput(t *testing.T) {
	form, _ := newForm()
	form.SetSessionStore(html.OldInput{Values: url.Values{"plan": {"pro"}}})

	if got := string(form.Radio("plan", "pro", nil, nil)); !strings.Contains(got, "checked") {
		t.Fatalf("Radio = %q, want it checked", got)
	}
	if got := string(form.Radio("plan", "free", nil, nil)); strings.Contains(got, "checked") {
		t.Fatalf("Radio = %q, want it unchecked", got)
	}
}

func TestFormImageWritesAnEscapedSource(t *testing.T) {
	form, _ := newForm()

	got := string(form.Image(`a" onerror="alert(1)`, "go", nil))
	if strings.Contains(got, `onerror="alert(1)"`) {
		t.Fatalf("Image let the src close the attribute: %s", got)
	}
	if !strings.Contains(got, `type="image"`) {
		t.Fatalf("Image = %q", got)
	}
}

// FormBuilder.php:729 concatenates the label raw.
func TestButtonEscapesItsLabel(t *testing.T) {
	form, _ := newForm()

	got := string(form.Button("<script>alert(1)</script>", nil))
	if strings.Contains(got, "<script>") {
		t.Fatalf("Button did not escape its label: %s", got)
	}
	if !strings.Contains(got, `type="button"`) {
		t.Fatalf("Button = %q, want the default type", got)
	}
}

func TestButtonKeepsAGivenType(t *testing.T) {
	form, _ := newForm()

	if got := string(form.Button("Save", html.Attrs{"type": "submit"})); !strings.Contains(got, `type="submit"`) {
		t.Fatalf("Button = %q", got)
	}
}

func TestGetSessionStoreAndSetSessionStore(t *testing.T) {
	form, _ := newForm()

	if form.GetSessionStore() != nil {
		t.Fatal("GetSessionStore on a fresh builder is not nil")
	}
	if form.SetSessionStore(html.OldInput{}) != form {
		t.Fatal("SetSessionStore did not return the builder")
	}
	if form.GetSessionStore() == nil {
		t.Fatal("GetSessionStore after SetSessionStore is nil")
	}
}

func TestOldAndOldInputIsEmpty(t *testing.T) {
	form, _ := newForm()

	// PHP answers false with no session, because the isset() guard is the first
	// half of the condition. Faithful, and the caller guards separately.
	if form.Old("email") != nil {
		t.Fatal("Old with no session is not nil")
	}
	if form.OldInputIsEmpty() {
		t.Fatal("OldInputIsEmpty with no session is true; PHP answers false")
	}

	form.SetSessionStore(html.OldInput{Values: url.Values{"email": {"ada@example.com"}}})
	if got := form.Old("email"); len(got) != 1 || got[0] != "ada@example.com" {
		t.Fatalf("Old = %v", got)
	}
	if form.OldInputIsEmpty() {
		t.Fatal("OldInputIsEmpty with old input is true")
	}
}

func TestGetIdAttributeAndGetValueAttribute(t *testing.T) {
	form, _ := newForm()

	if got := form.GetIdAttribute("email", html.Attrs{"id": "given"}); got != "given" {
		t.Errorf("GetIdAttribute = %q, want the given id", got)
	}
	if got := form.GetIdAttribute("email", nil); got != "" {
		t.Errorf("GetIdAttribute with no label = %q, want empty", got)
	}
	if got := form.GetValueAttribute("", "given"); got != "given" {
		t.Errorf("GetValueAttribute with no name = %q, want the given value", got)
	}
}

// Model opens the form and fills the inputs from the record.
func TestModelOpensAndFills(t *testing.T) {
	form, _ := newForm()

	open, err := form.Model(html.ModelAttributes{"email": "ada@example.com"}, html.OpenOptions{URL: []string{"/profile"}})
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	if !strings.Contains(string(open), `action="http://example.test/profile"`) {
		t.Fatalf("Model = %q", open)
	}
	if got := string(form.Text("email", "", nil)); !strings.Contains(got, `value="ada@example.com"`) {
		t.Fatalf("Text = %q, want the model value", got)
	}
}
