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
// PHP has no such error: FormBuilder::token calls getToken on a null session
// and the request dies on a fatal. It is an error here because the alternative
// -- writing the form without the field -- ships a form that fails validation
// on submit, and the bug report that comes back is about the wrong thing.
var ErrNoToken = errors.New("html: no CSRF token: pass one to NewFormBuilder or SetSessionStore")

// FormBuilder answers to Illuminate\Html\FormBuilder.
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

	// labels answers to FormBuilder::$labels: the names a label has been drawn
	// for, so [FormBuilder.GetIdAttribute] can give the input the id the label
	// points at.
	labels []string
}

// spoofedMethods answers to FormBuilder::$spoofedMethods.
var spoofedMethods = []string{"DELETE", "PATCH", "PUT"}

// skipValueTypes answers to FormBuilder::$skipValueTypes: the input types that
// are never filled from old input or from the model.
var skipValueTypes = []string{"file", "password", "checkbox", "radio"}

// NewFormBuilder answers to FormBuilder::__construct.
//
// csrfToken may be empty, and then [FormBuilder.Token] asks the session for one
// -- which is PHP's `! empty($this->csrfToken) ? ... : $this->session->getToken()`.
func NewFormBuilder(html *HtmlBuilder, url UrlGenerator, csrfToken string) *FormBuilder {
	return &FormBuilder{html: html, url: url, csrfToken: csrfToken}
}

// OpenOptions answers to the $options array FormBuilder::open takes.
//
// PHP mixes five reserved keys and any number of HTML attributes in one array
// and pulls the five back out with array_except($options, $this->reserved).
// Here the five are fields and the attributes are a map, so the reserved list
// is the type rather than a slice somebody has to keep in step with it.
type OpenOptions struct {
	// Method is the HTTP method: "post" when empty, as in PHP. DELETE, PATCH
	// and PUT are spoofed -- the form posts and carries a hidden _method.
	Method string

	// URL is the "url" key: a path, then any extra segments. PHP accepts a
	// string or an array and a slice covers both.
	URL []string

	// Route is the "route" key: a route name, then its parameters.
	Route []string

	// Action is the "action" key: a controller action, then its parameters.
	Action []string

	// Files is the "files" key: true sets enctype to multipart/form-data.
	Files bool

	// Attributes is everything else in PHP's array -- what array_except leaves.
	Attributes Attrs
}

// Open answers to FormBuilder::open.
//
// It returns the opening tag and the appendage: the hidden _method for a
// spoofed method, and the CSRF token for anything that is not GET. PHP returns
// a string and lets the route lookup throw; this returns
// (template.HTML, error), because Route and Action can fail.
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

	// PHP does array_merge($attributes, array_except($options, $reserved)), and
	// array_merge lets the second array win. So an attribute the caller passed
	// overrides the computed one, and so it does here.
	for key, value := range options.Attributes {
		attributes[key] = value
	}

	appendage, err := f.getAppendage(method)
	if err != nil {
		return "", err
	}

	return template.HTML("<form" + string(f.html.Attributes(attributes)) + ">" + string(appendage)), nil
}

// Model answers to FormBuilder::model: [FormBuilder.Open] with a model behind
// it, so every input finds its own value.
func (f *FormBuilder) Model(model Model, options OpenOptions) (template.HTML, error) {
	f.model = model
	return f.Open(options)
}

// SetModel answers to FormBuilder::setModel.
func (f *FormBuilder) SetModel(model Model) { f.model = model }

// Close answers to FormBuilder::close. It forgets the model and the labels, so
// the next form on the page starts clean.
func (f *FormBuilder) Close() template.HTML {
	f.labels = nil
	f.model = nil

	return "</form>"
}

// Token answers to FormBuilder::token: the hidden _token field.
//
// PHP reads $this->session->getToken() when no token was given to the
// constructor and fatals when there is no session either. This returns an error
// instead: a form that silently ships without a token is a form that fails CSRF
// validation on submit, which is a bug report about the wrong thing.
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

// Label answers to FormBuilder::label.
//
// An empty value is PHP's null, and then the label reads the field name with
// the underscores turned into spaces and each word capitalised.
//
// PHP writes `'<label for="'.$name.'"'` -- the name is concatenated into a
// quoted attribute unescaped, so a field name carrying a double quote closes it.
// Here the name goes through the attribute writer like everything else, which
// also means a name that is not a legal attribute value cannot escape the tag.
func (f *FormBuilder) Label(name, value string, options Attrs) template.HTML {
	f.labels = append(f.labels, name)

	attributes := cloneAttrs(options)
	attributes["for"] = name

	return template.HTML("<label" + string(f.html.Attributes(attributes)) + ">" +
		escape(f.formatLabel(name, value)) + "</label>")
}

// formatLabel answers to FormBuilder::formatLabel.
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

// Input answers to FormBuilder::input.
//
// An empty value is PHP's null: the value attribute is left off entirely rather
// than written empty. PHP distinguishes null from the empty string and only one
// caller passes the empty string on purpose -- [FormBuilder.Password], which
// still writes value="" here for the reason its own comment gives.
func (f *FormBuilder) Input(inputType, name, value string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if _, ok := attributes["name"]; !ok && name != "" {
		attributes["name"] = name
	}

	// The id is looked up before the value, exactly as in PHP: a label drawn
	// earlier in the form is what gives this input its id.
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

// Text answers to FormBuilder::text.
func (f *FormBuilder) Text(name, value string, options Attrs) template.HTML {
	return f.Input("text", name, value, options)
}

// Password answers to FormBuilder::password.
//
// It writes value="" where the other inputs would write nothing. That is PHP's
// behaviour -- input() is handed the empty string rather than null -- and it is
// worth keeping: an explicit empty value is what stops a browser restoring a
// password into the box on a back navigation.
func (f *FormBuilder) Password(name string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	attributes["value"] = ""

	return f.Input("password", name, "", attributes)
}

// Hidden answers to FormBuilder::hidden.
func (f *FormBuilder) Hidden(name, value string, options Attrs) template.HTML {
	return f.Input("hidden", name, value, options)
}

// Email answers to FormBuilder::email: an input of type email.
//
// It is not [HtmlBuilder.Email], which obfuscates an address for display.
func (f *FormBuilder) Email(name, value string, options Attrs) template.HTML {
	return f.Input("email", name, value, options)
}

// URL answers to FormBuilder::url: an input of type url. The name is uppercase
// because Go writes an initialism that way; PHP's is url().
func (f *FormBuilder) URL(name, value string, options Attrs) template.HTML {
	return f.Input("url", name, value, options)
}

// File answers to FormBuilder::file.
func (f *FormBuilder) File(name string, options Attrs) template.HTML {
	return f.Input("file", name, "", options)
}

// Textarea answers to FormBuilder::textarea.
//
// cols and rows default to 50 and 10, and a "size" attribute of "30x5" sets
// both and is then dropped, which is PHP's shortcut.
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

// setTextAreaSize answers to FormBuilder::setTextAreaSize and
// setQuickTextAreaSize, which are one step here because the map is written in
// place.
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

// Option is one entry of the $list array [FormBuilder.Select] takes.
//
// PHP's list is an associative array from value to display text, and a display
// that is itself an array becomes an optgroup whose label is the key. So:
//
//	Option{Value: "br", Display: "Brazil"}                <option value="br">Brazil</option>
//	Option{Value: "South America", Options: [...]}        <optgroup label="South America">
//
// The label of a group comes from Value and not from Display, because that is
// the key in PHP -- see FormBuilder::getSelectOption, which passes $value into
// optionGroup as $label.
type Option struct {
	// Value is the value attribute of an option, or the label of a group.
	Value string

	// Display is the text of an option. It is not read for a group.
	Display string

	// Options being non-nil makes this an optgroup rather than an option.
	Options []Option
}

// Select answers to FormBuilder::select.
//
// selected is a slice where PHP takes a scalar or an array, which is the same
// mechanical widening [Store.GetOldInput] does: a multiple select has more than
// one selected value, and FormBuilder::getSelectedValue already branches on it.
// Nil means nothing is selected unless old input or the model says otherwise.
func (f *FormBuilder) Select(name string, list []Option, selected []string, options Attrs) template.HTML {
	// PHP writes `$selected = $this->getValueAttribute($name, $selected)`: the
	// value attribute of a select is really the selected option, so it is
	// resolved the same way as for any other input, and in the same order --
	// old input, then the argument, then the model.
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

// SelectRange answers to FormBuilder::selectRange: a select over the integers
// from begin to end, where each option is its own label.
//
// PHP documents $begin and $end as strings and hands them to range(), which
// also walks letters. These are ints, which is what every caller of it passes.
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

// SelectYear answers to FormBuilder::selectYear.
//
// PHP forwards every argument to selectRange with call_user_func_array, so the
// two are the same function under two names. So are these.
func (f *FormBuilder) SelectYear(name string, begin, end int, selected []string, options Attrs) template.HTML {
	return f.SelectRange(name, begin, end, selected, options)
}

// DefaultMonthFormat is the layout [FormBuilder.SelectMonth] uses when none is
// given. It is PHP's '%B', the full month name.
const DefaultMonthFormat = "January"

// SelectMonth answers to FormBuilder::selectMonth: a select over the twelve
// months, valued one to twelve.
//
// format is a Go time layout where PHP takes a strftime format, and an empty
// one is [DefaultMonthFormat]. The months come out in English: strftime reads
// the process locale, Go's time package has no locale, and golang.org/x/text --
// which does -- is refused by ADR 0004. A localised month belongs in the
// translation layer, where the rest of the language lives.
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

// GetSelectOption answers to FormBuilder::getSelectOption.
//
// PHP takes the display and the value as two arguments because they are the
// value and the key of one array entry. Here they arrive as one [Option], and
// the branch on is_array($display) is the branch on Options being non-nil.
func (f *FormBuilder) GetSelectOption(option Option, selected []string) template.HTML {
	if option.Options != nil {
		return f.optionGroup(option.Options, option.Value, selected)
	}
	return f.option(option.Display, option.Value, selected)
}

// optionGroup answers to FormBuilder::optionGroup.
func (f *FormBuilder) optionGroup(list []Option, label string, selected []string) template.HTML {
	var items strings.Builder
	for _, option := range list {
		items.WriteString(string(f.option(option.Display, option.Value, selected)))
	}

	return template.HTML(`<optgroup label="` + escape(label) + `">` + items.String() + "</optgroup>")
}

// option answers to FormBuilder::option.
//
// PHP escapes the value with e() and then hands it to attributes(), which
// escapes it again -- harmlessly, because e() does not double-encode. This
// escapes it once, in the attribute writer, and the output is the same.
func (f *FormBuilder) option(display, value string, selected []string) template.HTML {
	attributes := Attrs{"value": value}
	if f.getSelectedValue(value, selected) != "" {
		attributes["selected"] = "selected"
	}

	return template.HTML("<option" + string(f.html.Attributes(attributes)) + ">" +
		escape(display) + "</option>")
}

// getSelectedValue answers to FormBuilder::getSelectedValue.
func (f *FormBuilder) getSelectedValue(value string, selected []string) string {
	if slices.Contains(selected, value) {
		return "selected"
	}
	return ""
}

// Checkbox answers to FormBuilder::checkbox.
//
// An empty value is PHP's default of 1. checked is a pointer because PHP's
// default is null and null is not false: null means "decide from the old input
// and the model", and false means "not checked, whatever they say".
func (f *FormBuilder) Checkbox(name, value string, checked *bool, options Attrs) template.HTML {
	if value == "" {
		value = "1"
	}
	return f.checkable("checkbox", name, value, checked, options)
}

// Radio answers to FormBuilder::radio. An empty value is PHP's null, and then
// the value is the field name.
func (f *FormBuilder) Radio(name, value string, checked *bool, options Attrs) template.HTML {
	if value == "" {
		value = name
	}
	return f.checkable("radio", name, value, checked, options)
}

// checkable answers to FormBuilder::checkable.
func (f *FormBuilder) checkable(inputType, name, value string, checked *bool, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if f.getCheckedState(inputType, name, value, checked) {
		attributes["checked"] = "checked"
	}

	return f.Input(inputType, name, value, attributes)
}

// getCheckedState answers to FormBuilder::getCheckedState.
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

// getCheckboxCheckedState answers to FormBuilder::getCheckboxCheckedState.
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

	// PHP's (bool) cast on the model value: the empty string and "0" are false
	// and everything else is true.
	modelValue, _ := f.getModelValueAttribute(name)
	return modelValue != "" && modelValue != "0"
}

// getRadioCheckedState answers to FormBuilder::getRadioCheckedState.
func (f *FormBuilder) getRadioCheckedState(name, value string, checked *bool) bool {
	if f.missingOldAndModel(name) {
		return checked != nil && *checked
	}
	return f.GetValueAttribute(name) == value
}

// missingOldAndModel answers to FormBuilder::missingOldAndModel.
func (f *FormBuilder) missingOldAndModel(name string) bool {
	if f.Old(name) != nil {
		return false
	}
	_, ok := f.getModelValueAttribute(name)
	return !ok
}

// Reset answers to FormBuilder::reset.
func (f *FormBuilder) Reset(value string, attributes Attrs) template.HTML {
	return f.Input("reset", "", value, attributes)
}

// Image answers to FormBuilder::image: an input of type image.
//
// It is not [HtmlBuilder.Image], which writes an img element. The src goes
// through the attribute writer here, so it is escaped; PHP's HtmlBuilder::image
// is where the unescaped concatenation is.
func (f *FormBuilder) Image(url, name string, attributes Attrs) template.HTML {
	merged := cloneAttrs(attributes)
	merged["src"] = f.url.Asset(url)

	return f.Input("image", name, "", merged)
}

// Submit answers to FormBuilder::submit.
func (f *FormBuilder) Submit(value string, options Attrs) template.HTML {
	return f.Input("submit", "", value, options)
}

// Button answers to FormBuilder::button.
//
// PHP writes `'<button'.attrs.'>'.$value.'</button>'` -- the label is
// concatenated raw, so a button labelled from anything a person typed carries
// their markup into the page. Here the label is escaped. A button that has to
// contain markup -- an icon beside the text -- is a kyse component, and the
// package comment names it.
func (f *FormBuilder) Button(value string, options Attrs) template.HTML {
	attributes := cloneAttrs(options)
	if _, ok := attributes["type"]; !ok {
		attributes["type"] = "button"
	}

	return template.HTML("<button" + string(f.html.Attributes(attributes)) + ">" +
		escape(value) + "</button>")
}

// getMethod answers to FormBuilder::getMethod: anything that is not GET posts.
func (f *FormBuilder) getMethod(method string) string {
	method = strings.ToUpper(method)
	if method != "GET" {
		return "POST"
	}
	return method
}

// getAction answers to FormBuilder::getAction, and to the three small methods
// under it -- getUrlAction, getRouteAction and getControllerAction -- which are
// one switch here because the array-or-string branch each of them carries is
// gone: a slice is both.
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

// getAppendage answers to FormBuilder::getAppendage: the hidden fields that
// follow the opening tag.
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

// GetIdAttribute answers to FormBuilder::getIdAttribute.
//
// PHP returns null when there is no id to give, and the caller drops the
// attribute; the empty string is that null here, because an id="" is not an
// attribute anybody wants written.
func (f *FormBuilder) GetIdAttribute(name string, attributes Attrs) string {
	if id, ok := attributes["id"]; ok {
		return id
	}
	if slices.Contains(f.labels, name) {
		return name
	}
	return ""
}

// GetValueAttribute answers to FormBuilder::getValueAttribute: what the input
// should show, from the old input first, then the argument, then the model.
//
// value is variadic because PHP's is optional, and the empty string is PHP's
// null throughout. That the old input wins over the argument is what makes a
// rejected form come back filled in with what was typed rather than with what
// the handler passed.
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

// getModelValueAttribute answers to FormBuilder::getModelValueAttribute.
func (f *FormBuilder) getModelValueAttribute(name string) (string, bool) {
	if f.model == nil {
		return "", false
	}
	return f.model.GetAttribute(f.transformKey(name))
}

// Old answers to FormBuilder::old.
//
// Nil is PHP's null -- the field was not in the old input at all -- and that is
// a different answer from an empty slice, which is a field that was submitted
// empty. [FormBuilder.Checkbox] reads the difference.
func (f *FormBuilder) Old(name string) []string {
	if f.session == nil {
		return nil
	}
	return f.session.GetOldInput(f.transformKey(name))
}

// OldInputIsEmpty answers to FormBuilder::oldInputIsEmpty.
//
// A builder with no session answers false, which reads backwards and is what
// PHP answers: `isset($this->session) && count(...) == 0` is false when the
// first half is. It is kept because the one caller,
// [FormBuilder.Checkbox], tests for the session separately in the same
// condition, so the surprising half is never reached alone.
func (f *FormBuilder) OldInputIsEmpty() bool {
	return f.session != nil && f.session.OldInputIsEmpty()
}

// transformKey answers to FormBuilder::transformKey: the array syntax of a
// field name turned into the dot syntax the session is keyed by.
func (f *FormBuilder) transformKey(key string) string {
	return strings.NewReplacer(".", "_", "[]", "", "[", ".", "]", "").Replace(key)
}

// GetSessionStore answers to FormBuilder::getSessionStore.
func (f *FormBuilder) GetSessionStore() Store { return f.session }

// SetSessionStore answers to FormBuilder::setSessionStore. It returns the
// builder, as PHP returns $this.
func (f *FormBuilder) SetSessionStore(session Store) *FormBuilder {
	f.session = session
	return f
}
