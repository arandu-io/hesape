package html

import (
	"errors"
	"html/template"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrNoToken is what [FormBuilder.Token] answers when neither the constructor
// nor the session has a CSRF token.
//
// It is an error, because the alternative -- writing the form without the
// field -- ships a form that fails validation on submit, and the bug report
// that comes back is about the wrong thing.
var ErrNoToken = errors.New("html: no CSRF token: pass one to NewFormBuilder or SetSessionStore")

// FormBuilder builds escaped HTML form elements as values.
//
// Nothing here competes with the kyse components: these return escaped markup
// as a value, for the handful of controls kyse has no component for. The
// package comment says which is which.
type FormBuilder struct {
	html      *HtmlBuilder
	url       UrlGenerator
	csrfToken string
	session   Store
	model     Model

	// labels is the names a label has been drawn for, so
	// [FormBuilder.GetIdAttribute] can give the input the id the label
	// points at.
	labels []string
}

// spoofedMethods are the HTTP methods a form cannot send directly, spoofed
// with a hidden _method field instead.
var spoofedMethods = []string{"DELETE", "PATCH", "PUT"}

// skipValueTypes are the input types that are never filled from old input
// or from the model.
var skipValueTypes = []string{"file", "password", "checkbox", "radio"}

// NewFormBuilder returns a builder over an [HtmlBuilder] and a [UrlGenerator].
//
// csrfToken may be empty, and then [FormBuilder.Token] asks the session for
// one instead.
func NewFormBuilder(html *HtmlBuilder, url UrlGenerator, csrfToken string) *FormBuilder {
	return &FormBuilder{html: html, url: url, csrfToken: csrfToken}
}

// OpenOptions is what [FormBuilder.Open] takes: five reserved fields, and a
// separate map for arbitrary HTML attributes, so the reserved set is the
// type itself rather than a list somebody has to keep in step with it.
type OpenOptions struct {
	// Method is the HTTP method: "post" when empty. DELETE, PATCH and PUT
	// are spoofed -- the form posts and carries a hidden _method.
	Method string

	// URL is a path, then any extra segments, covered together by one
	// slice.
	URL []string

	// Route is a route name, then its parameters.
	Route []string

	// Action is a controller action, then its parameters.
	Action []string

	// Files, when true, sets enctype to multipart/form-data.
	Files bool

	// Attributes is any other HTML attribute to write on the form tag.
	Attributes Attrs
}

// Open builds the opening tag and the appendage: the hidden _method for a
// spoofed method, and the CSRF token for anything that is not GET. It
// returns (template.HTML, error), because Route and Action can fail.
func (f *FormBuilder) Open(options OpenOptions) (template.HTML, error) {
	method := options.Method
	if method == "" {
		method = "post"
	}

	action, err := f.getAction(options)
	if err != nil {
		return "", err
	}

	attributes := Attrs{
		"method":         f.getMethod(method),
		"action":         action,
		"accept-charset": "UTF-8",
	}
	if options.Files {
		attributes["enctype"] = "multipart/form-data"
	}

	// An attribute the caller passed overrides the computed one.
	for key, value := range options.Attributes {
		attributes[key] = value
	}

	appendage, err := f.getAppendage(method)
	if err != nil {
		return "", err
	}

	return template.HTML("<form" + string(f.html.Attributes(attributes)) + ">" + string(appendage)), nil
}

// Model is [FormBuilder.Open] with a model behind it, so every input finds
// its own value.
func (f *FormBuilder) Model(model Model, options OpenOptions) (template.HTML, error) {
	f.model = model
	return f.Open(options)
}

// SetModel replaces the model.
func (f *FormBuilder) SetModel(model Model) { f.model = model }

// Close forgets the model and the labels, so the next form on the page
// starts clean.
func (f *FormBuilder) Close() template.HTML {
	f.labels = nil
	f.model = nil

	return "</form>"
}

// Token builds the hidden _token field.
//
// It returns an error when there is no token to write: a form that
// silently ships without one is a form that fails CSRF validation on
// submit, which is a bug report about the wrong thing.
func (f *FormBuilder) Token() (template.HTML, error) {
	token := f.csrfToken
	if token == "" {
		if f.session == nil {
			return "", ErrNoToken
		}
		token = f.session.GetToken()
	}
	if token == "" {
		return "", ErrNoToken
	}

	return f.Hidden("_token", token, nil), nil
}

// Label builds a label tag.
//
// An empty value falls back to the field name, with the underscores turned
// into spaces and each word capitalised.
//
// The name goes through the attribute writer like everything else, so a
// name carrying a double quote cannot close the attribute, and a name that
// is not a legal attribute value cannot escape the tag.
func (f *FormBuilder) Label(name, value string, options Attrs) template.HTML {
	f.labels = append(f.labels, name)

	attributes := cloneAttrs(options)
	attributes["for"] = name

	return template.HTML("<label" + string(f.html.Attributes(attributes)) + ">" +
		escape(f.formatLabel(name, value)) + "</label>")
}

// formatLabel turns a field name into a label: underscores become spaces,
// and each word is capitalised.
func (f *FormBuilder) formatLabel(name, value string) string {
	if value != "" {
		return value
	}

	words := strings.Split(strings.ReplaceAll(name, "_", " "), " ")
	for i, word := range words {
		if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// Input builds an input tag of the given type.
//
// An empty value leaves the value attribute off entirely, rather than
// writing it empty. Only one caller writes it empty on purpose --
// [FormBuilder.Password], for the reason its own comment gives.
func (f *FormBuilder) Input(inputType, name, value string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if _, ok := attributes["name"]; !ok && name != "" {
		attributes["name"] = name
	}

	// The id is looked up before the value: a label drawn earlier in the
	// form is what gives this input its id.
	if id := f.GetIdAttribute(name, options); id != "" {
		attributes["id"] = id
	}

	if !slices.Contains(skipValueTypes, inputType) {
		value = f.GetValueAttribute(name, value)
	}

	attributes["type"] = inputType
	if value != "" {
		attributes["value"] = value
	}

	return template.HTML("<input" + string(f.html.Attributes(attributes)) + ">")
}

// Text is [FormBuilder.Input] of type text.
func (f *FormBuilder) Text(name, value string, options Attrs) template.HTML {
	return f.Input("text", name, value, options)
}

// Password is [FormBuilder.Input] of type password.
//
// It writes value="" where the other inputs would write nothing: an
// explicit empty value is what stops a browser restoring a password into
// the box on a back navigation.
func (f *FormBuilder) Password(name string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	attributes["value"] = ""

	return f.Input("password", name, "", attributes)
}

// Hidden is [FormBuilder.Input] of type hidden.
func (f *FormBuilder) Hidden(name, value string, options Attrs) template.HTML {
	return f.Input("hidden", name, value, options)
}

// Email is [FormBuilder.Input] of type email.
//
// It is not [HtmlBuilder.Email], which obfuscates an address for display.
func (f *FormBuilder) Email(name, value string, options Attrs) template.HTML {
	return f.Input("email", name, value, options)
}

// URL is [FormBuilder.Input] of type url. The name is uppercase because Go
// writes an initialism that way.
func (f *FormBuilder) URL(name, value string, options Attrs) template.HTML {
	return f.Input("url", name, value, options)
}

// File is [FormBuilder.Input] of type file.
func (f *FormBuilder) File(name string, options Attrs) template.HTML {
	return f.Input("file", name, "", options)
}

// Textarea builds a textarea element.
//
// cols and rows default to 50 and 10, and a "size" attribute of "30x5"
// sets both and is then dropped.
func (f *FormBuilder) Textarea(name, value string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if _, ok := attributes["name"]; !ok && name != "" {
		attributes["name"] = name
	}

	f.setTextAreaSize(attributes)

	if id := f.GetIdAttribute(name, options); id != "" {
		attributes["id"] = id
	}

	value = f.GetValueAttribute(name, value)
	delete(attributes, "size")

	return template.HTML("<textarea" + string(f.html.Attributes(attributes)) + ">" +
		escape(value) + "</textarea>")
}

// setTextAreaSize resolves cols and rows, including the "size" shortcut,
// writing the map in place.
func (f *FormBuilder) setTextAreaSize(attributes Attrs) {
	if size, ok := attributes["size"]; ok {
		if cols, rows, found := strings.Cut(size, "x"); found {
			attributes["cols"] = cols
			attributes["rows"] = rows
			return
		}
	}

	if _, ok := attributes["cols"]; !ok {
		attributes["cols"] = "50"
	}
	if _, ok := attributes["rows"]; !ok {
		attributes["rows"] = "10"
	}
}

// Option is one entry of the list [FormBuilder.Select] takes.
//
// There are two shapes:
//
//	Option{Value: "br", Display: "Brazil"}                <option value="br">Brazil</option>
//	Option{Value: "South America", Options: [...]}        <optgroup label="South America">
//
// The label of a group comes from Value and not from Display -- see
// [FormBuilder.GetSelectOption], which passes it through as the label.
type Option struct {
	// Value is the value attribute of an option, or the label of a group.
	Value string

	// Display is the text of an option. It is not read for a group.
	Display string

	// Options being non-nil makes this an optgroup rather than an option.
	Options []Option
}

// Select builds a select element.
//
// selected is a slice, the same shape [Store.GetOldInput] returns: a
// multiple select has more than one selected value. Nil means nothing is
// selected unless old input or the model says otherwise.
func (f *FormBuilder) Select(name string, list []Option, selected []string, options Attrs) template.HTML {
	// The value attribute of a select is really the selected option, so it
	// is resolved the same way as for any other input, and in the same
	// order -- old input, then the argument, then the model.
	if old := f.Old(name); len(old) > 0 {
		selected = old
	} else if len(selected) == 0 {
		if modelValue, ok := f.getModelValueAttribute(name); ok && modelValue != "" {
			selected = []string{modelValue}
		}
	}

	attributes := cloneAttrs(options)
	if id := f.GetIdAttribute(name, options); id != "" {
		attributes["id"] = id
	}
	if _, ok := attributes["name"]; !ok && name != "" {
		attributes["name"] = name
	}

	var items strings.Builder
	for _, option := range list {
		items.WriteString(string(f.GetSelectOption(option, selected)))
	}

	return template.HTML("<select" + string(f.html.Attributes(attributes)) + ">" +
		items.String() + "</select>")
}

// SelectRange builds a select over the integers from begin to end, where
// each option is its own label.
func (f *FormBuilder) SelectRange(name string, begin, end int, selected []string, options Attrs) template.HTML {
	step := 1
	if end < begin {
		step = -1
	}

	var list []Option
	for i := begin; ; i += step {
		value := strconv.Itoa(i)
		list = append(list, Option{Value: value, Display: value})
		if i == end {
			break
		}
	}

	return f.Select(name, list, selected, options)
}

// SelectYear is [FormBuilder.SelectRange] under a second name, for a select
// over years.
func (f *FormBuilder) SelectYear(name string, begin, end int, selected []string, options Attrs) template.HTML {
	return f.SelectRange(name, begin, end, selected, options)
}

// DefaultMonthFormat is the layout [FormBuilder.SelectMonth] uses when none
// is given: the full month name.
const DefaultMonthFormat = "January"

// SelectMonth builds a select over the twelve months, valued one to twelve.
//
// format is a Go time layout, and an empty one is [DefaultMonthFormat]. The
// months come out in English: Go's time package has no locale support, and
// this framework does not pull in golang.org/x/text for one. A localised
// month belongs in the translation layer, where the rest of the language
// lives.
func (f *FormBuilder) SelectMonth(name string, selected []string, options Attrs, format string) template.HTML {
	if format == "" {
		format = DefaultMonthFormat
	}

	list := make([]Option, 0, 12)
	for month := 1; month <= 12; month++ {
		first := time.Date(2000, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		list = append(list, Option{Value: strconv.Itoa(month), Display: first.Format(format)})
	}

	return f.Select(name, list, selected, options)
}

// GetSelectOption builds one <option> or <optgroup>, branching on whether
// option.Options is non-nil.
func (f *FormBuilder) GetSelectOption(option Option, selected []string) template.HTML {
	if option.Options != nil {
		return f.optionGroup(option.Options, option.Value, selected)
	}
	return f.option(option.Display, option.Value, selected)
}

// optionGroup builds an optgroup and its options.
func (f *FormBuilder) optionGroup(list []Option, label string, selected []string) template.HTML {
	var items strings.Builder
	for _, option := range list {
		items.WriteString(string(f.option(option.Display, option.Value, selected)))
	}

	return template.HTML(`<optgroup label="` + escape(label) + `">` + items.String() + "</optgroup>")
}

// option builds one <option> element.
//
// The value is escaped once, in the attribute writer.
func (f *FormBuilder) option(display, value string, selected []string) template.HTML {
	attributes := Attrs{"value": value}
	if f.getSelectedValue(value, selected) != "" {
		attributes["selected"] = "selected"
	}

	return template.HTML("<option" + string(f.html.Attributes(attributes)) + ">" +
		escape(display) + "</option>")
}

// getSelectedValue returns "selected" when value is among the selected
// ones.
func (f *FormBuilder) getSelectedValue(value string, selected []string) string {
	if slices.Contains(selected, value) {
		return "selected"
	}
	return ""
}

// Checkbox builds a checkbox input.
//
// An empty value defaults to 1. checked is a pointer so that nil and false
// are distinct: nil means "decide from the old input and the model", and
// false means "not checked, whatever they say".
func (f *FormBuilder) Checkbox(name, value string, checked *bool, options Attrs) template.HTML {
	if value == "" {
		value = "1"
	}
	return f.checkable("checkbox", name, value, checked, options)
}

// Radio builds a radio input. An empty value falls back to the field name.
func (f *FormBuilder) Radio(name, value string, checked *bool, options Attrs) template.HTML {
	if value == "" {
		value = name
	}
	return f.checkable("radio", name, value, checked, options)
}

// checkable builds a checkbox or radio input, deciding whether it starts
// checked.
func (f *FormBuilder) checkable(inputType, name, value string, checked *bool, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if f.getCheckedState(inputType, name, value, checked) {
		attributes["checked"] = "checked"
	}

	return f.Input(inputType, name, value, attributes)
}

// getCheckedState decides whether an input starts checked, branching on
// its type.
func (f *FormBuilder) getCheckedState(inputType, name, value string, checked *bool) bool {
	switch inputType {
	case "checkbox":
		return f.getCheckboxCheckedState(name, value, checked)
	case "radio":
		return f.getRadioCheckedState(name, value, checked)
	default:
		return f.GetValueAttribute(name) == value
	}
}

// getCheckboxCheckedState decides whether a checkbox starts checked.
//
// The first branch is the one that matters and the one nobody expects: when a
// submission came back rejected and this box is not in the old input, the box
// was unchecked, and an unchecked box posts nothing. Without it every checkbox
// comes back checked from its default after every failed validation.
func (f *FormBuilder) getCheckboxCheckedState(name, value string, checked *bool) bool {
	if f.session != nil && !f.OldInputIsEmpty() && f.Old(name) == nil {
		return false
	}

	if f.missingOldAndModel(name) {
		return checked != nil && *checked
	}

	if posted := f.Old(name); len(posted) > 0 {
		return slices.Contains(posted, value)
	}

	// The empty string and "0" are false; everything else is true.
	modelValue, _ := f.getModelValueAttribute(name)
	return modelValue != "" && modelValue != "0"
}

// getRadioCheckedState decides whether a radio input starts checked.
func (f *FormBuilder) getRadioCheckedState(name, value string, checked *bool) bool {
	if f.missingOldAndModel(name) {
		return checked != nil && *checked
	}
	return f.GetValueAttribute(name) == value
}

// missingOldAndModel reports whether there is neither old input nor a
// model value for name.
func (f *FormBuilder) missingOldAndModel(name string) bool {
	if f.Old(name) != nil {
		return false
	}
	_, ok := f.getModelValueAttribute(name)
	return !ok
}

// Reset is [FormBuilder.Input] of type reset.
func (f *FormBuilder) Reset(value string, attributes Attrs) template.HTML {
	return f.Input("reset", "", value, attributes)
}

// Image is [FormBuilder.Input] of type image.
//
// It is not [HtmlBuilder.Image], which writes an img element. The src goes
// through the attribute writer here, so it is escaped.
func (f *FormBuilder) Image(url, name string, attributes Attrs) template.HTML {
	merged := cloneAttrs(attributes)
	merged["src"] = f.url.Asset(url)

	return f.Input("image", name, "", merged)
}

// Submit is [FormBuilder.Input] of type submit.
func (f *FormBuilder) Submit(value string, options Attrs) template.HTML {
	return f.Input("submit", "", value, options)
}

// Button builds a button element.
//
// The label is escaped, so a button labelled from anything a person typed
// cannot carry markup into the page. A button that has to contain markup --
// an icon beside the text -- is a kyse component, and the package comment
// names it.
func (f *FormBuilder) Button(value string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if _, ok := attributes["type"]; !ok {
		attributes["type"] = "button"
	}

	return template.HTML("<button" + string(f.html.Attributes(attributes)) + ">" +
		escape(value) + "</button>")
}

// getMethod returns the HTTP method to send the form as: anything that is
// not GET posts.
func (f *FormBuilder) getMethod(method string) string {
	method = strings.ToUpper(method)
	if method != "GET" {
		return "POST"
	}
	return method
}

// getAction resolves the form's action attribute, branching on which of
// URL, Route or Action was given.
func (f *FormBuilder) getAction(options OpenOptions) (string, error) {
	switch {
	case len(options.URL) > 0:
		return f.url.To(options.URL[0], options.URL[1:]...), nil
	case len(options.Route) > 0:
		return f.url.Route(options.Route[0], options.Route[1:]...)
	case len(options.Action) > 0:
		return f.url.Action(options.Action[0], options.Action[1:]...)
	default:
		return f.url.Current(), nil
	}
}

// getAppendage builds the hidden fields that follow the opening tag.
func (f *FormBuilder) getAppendage(method string) (template.HTML, error) {
	method = strings.ToUpper(method)

	var appendage strings.Builder
	if slices.Contains(spoofedMethods, method) {
		appendage.WriteString(string(f.Hidden("_method", method, nil)))
	}

	if method != "GET" {
		token, err := f.Token()
		if err != nil {
			return "", err
		}
		appendage.WriteString(string(token))
	}

	return template.HTML(appendage.String()), nil
}

// GetIdAttribute returns the id to give an input, or "" when there is
// none: an id="" is not an attribute anybody wants written.
func (f *FormBuilder) GetIdAttribute(name string, attributes Attrs) string {
	if id, ok := attributes["id"]; ok {
		return id
	}
	if slices.Contains(f.labels, name) {
		return name
	}
	return ""
}

// GetValueAttribute returns what the input should show, from the old
// input first, then the argument, then the model.
//
// value is variadic, so it can be left out entirely. That the old input
// wins over the argument is what makes a rejected form come back filled in
// with what was typed rather than with what the handler passed.
func (f *FormBuilder) GetValueAttribute(name string, value ...string) string {
	given := ""
	if len(value) > 0 {
		given = value[0]
	}

	if name == "" {
		return given
	}

	if old := f.Old(name); len(old) > 0 {
		return old[0]
	}

	if given != "" {
		return given
	}

	modelValue, _ := f.getModelValueAttribute(name)
	return modelValue
}

// getModelValueAttribute reads a field off the model, transforming the
// name first.
func (f *FormBuilder) getModelValueAttribute(name string) (string, bool) {
	if f.model == nil {
		return "", false
	}
	return f.model.GetAttribute(f.transformKey(name))
}

// Old returns what was typed into name on the rejected submission.
//
// Nil means the field was not in the old input at all, which is a
// different answer from an empty slice, a field that was submitted empty.
// [FormBuilder.Checkbox] reads the difference.
func (f *FormBuilder) Old(name string) []string {
	if f.session == nil {
		return nil
	}
	return f.session.GetOldInput(f.transformKey(name))
}

// OldInputIsEmpty reports whether there is old input at all.
//
// A builder with no session answers false, which reads backwards: kept
// because the one caller, [FormBuilder.Checkbox], tests for the session
// separately in the same condition, so the surprising half is never
// reached alone.
func (f *FormBuilder) OldInputIsEmpty() bool {
	return f.session != nil && f.session.OldInputIsEmpty()
}

// transformKey turns the array syntax of a field name into the dot syntax
// the session is keyed by.
func (f *FormBuilder) transformKey(key string) string {
	return strings.NewReplacer(".", "_", "[]", "", "[", ".", "]", "").Replace(key)
}

// GetSessionStore returns the session store.
func (f *FormBuilder) GetSessionStore() Store { return f.session }

// SetSessionStore replaces the session store, and returns the builder, for
// chaining.
func (f *FormBuilder) SetSessionStore(session Store) *FormBuilder {
	f.session = session
	return f
}
