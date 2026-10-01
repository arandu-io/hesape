package http

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/validation"
)

// bindTimeLayouts are the spellings Bind accepts for a time.Time, in the order
// it tries them: what an <input type="datetime-local"> sends, with and without
// seconds, what an <input type="date"> sends, and RFC 3339. A value with no
// offset is read as UTC.
var bindTimeLayouts = []string{
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	"2006-01-02",
	time.RFC3339,
}

var (
	timeType     = reflect.TypeFor[time.Time]()
	durationType = reflect.TypeFor[time.Duration]()
)

// Bind reads the request's form into the struct dst points to, one field per
// `form:"name"` tag.
//
//	type StoreInvoiceRequest struct {
//	    Customer string     `form:"customer"`
//	    Amount   int64      `form:"amount"`
//	    DueOn    time.Time  `form:"due_on"`
//	    Paid     bool       `form:"paid"`
//	    Notes    *string    `form:"notes"`
//	    Tags     []string   `form:"tags"`
//	}
//
//	var req StoreInvoiceRequest
//	if err := ctx.Bind(&req); err != nil {
//	    return err
//	}
//
// # What it writes
//
// Only fields carrying a form tag, and every one of them. A field without the
// tag is never written, and a form key no field names is ignored: the struct
// is the list of what may arrive, so a request carrying is_admin reaches no
// field unless one was declared with that name. A tag of "-" is the same as no
// tag. An exported embedded struct with no tag of its own is read into as if
// its fields were declared in dst.
//
// Every tagged field is written whether its key arrived or not, so the struct
// after Bind depends on the form alone. An absent key writes the zero value:
// false for a bool, which is what an unchecked checkbox means, and nil for a
// pointer or a slice.
//
// # Where the values come from
//
// From the map Context.Input and Request.Input read, so a struct holds what a
// rule validated and nothing else. For GET and HEAD that is the query string.
// For every other method it is the body alone, and the query string is never
// read: a url-encoded body, for DELETE as well, or the text fields of a
// multipart body, whose files stay where the upload path reads them, or the
// top-level fields of a JSON object, where a number, a boolean or a string is
// read as its text, an array as one value per element, and null and a nested
// object as absent.
//
// # How a value is converted
//
// Every value is trimmed with strings.TrimSpace first. Then, by the kind of the
// field:
//
//   - string, and a type whose underlying type is string: the trimmed text.
//   - bool: "1", "true", "on" and "yes" are true; "0", "false", "off", "no"
//     and the empty string are false, in any case.
//   - int, int8, int16, int32, int64, and the unsigned sizes: base ten, within
//     the size of the field.
//   - float32, float64: a finite number.
//   - time.Time: "2006-01-02T15:04" and "2006-01-02T15:04:05" (an <input
//     type="datetime-local">), "2006-01-02" (an <input type="date">), or RFC
//     3339. A value with no offset is UTC.
//   - a pointer to any of the above: nil when the key is absent or its value
//     is empty, and a pointer to the converted value otherwise. It is how a
//     field says that nothing was sent, as distinct from a zero.
//   - []string, and a slice of a type whose underlying type is string: every
//     value the key arrived with, each trimmed, in order. A <select multiple>
//     and a group of checkboxes sharing a name send exactly that.
//
// Any other field takes the first value its key arrived with. An empty value
// writes the zero value of a non-pointer field rather than failing: whether a
// field may be empty is a rule for Validate, not a conversion.
//
// # What it returns
//
// A value that does not convert -- "abc" for an int, "2026-13-01" for a date,
// 300 for an int8 -- leaves its field at the zero value and becomes a message
// on that field, keyed by its form name, in a validation.Errors. Every field
// is read before Bind returns, so the Errors carry every field that failed and
// not only the first. It is the same type Validate returns and Reject flashes,
// so a handler hands it back as it would a rule failure.
//
// Anything else is a mistake in the program and not in the request, and comes
// back as an ordinary error before or instead of the field errors: dst that is
// not a non-nil pointer to a struct, a tagged field that is unexported or of a
// type Bind does not convert into (time.Duration among them, because "90"
// would be read as ninety nanoseconds), and a body that could not be parsed.
// It returns rather than panics, like every other failure a handler returns.
//
// A body cut off by the limit the server put on it binds nothing: the error is
// a *exceptions.PostTooLargeException, which answers 413, rather than a struct
// filled from whatever part of the body arrived.
func (c *Context) Bind(dst any) error {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("http: Bind needs a non-nil pointer to a struct, and was given %T", dst)
	}

	input := c.inputRequest()
	if readsBody(c.Request.Method) && !input.IsJSON() {
		if err := parseBody(c.Request); err != nil {
			if tooLarge := bodyTooLarge(err); tooLarge != nil {
				return tooLarge
			}
			return fmt.Errorf("http: reading the form: %w", err)
		}
	}

	form := formValues(input.inputMap())
	if input.tooLarge != nil {
		return input.tooLarge
	}
	errs := validation.Errors{}
	if err := bindStruct(target.Elem(), form, errs); err != nil {
		return err
	}
	if errs.Any() {
		return errs
	}
	return nil
}

// formValues is the input map as the lists of text Bind converts from.
func formValues(input map[string]any) url.Values {
	values := make(url.Values, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case nil, map[string]any:
			continue
		case []any:
			list := make([]string, 0, len(typed))
			for _, item := range typed {
				list = append(list, formText(item))
			}
			values[key] = list
		default:
			values[key] = []string{formText(typed)}
		}
	}
	return values
}

// formText is one input value as the text a form would have sent for it. A
// boolean is spelled out, because the empty string stringify gives false would
// read as absent for a pointer field.
func formText(value any) string {
	if b, ok := value.(bool); ok {
		return strconv.FormatBool(b)
	}
	return stringify(value)
}

// bindStruct reads the form into every tagged field of v, recursing into
// exported embedded structs that carry no tag of their own.
func bindStruct(v reflect.Value, form url.Values, errs validation.Errors) error {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		name, tagged := field.Tag.Lookup("form")

		if field.Anonymous && !tagged && field.IsExported() && field.Type.Kind() == reflect.Struct {
			if err := bindStruct(v.Field(i), form, errs); err != nil {
				return err
			}
			continue
		}
		if !tagged || name == "" || name == "-" {
			continue
		}
		if !field.IsExported() {
			return fmt.Errorf("http: Bind cannot write %s.%s, which is unexported and tagged form:%q", t, field.Name, name)
		}
		if err := bindField(v.Field(i), name, form[name], errs); err != nil {
			return fmt.Errorf("http: Bind cannot write %s.%s, tagged form:%q: %w", t, field.Name, name, err)
		}
	}
	return nil
}

// errUnsupportedField is what bindField reports for a field type it does not
// convert into.
var errUnsupportedField = errors.New("its type is not one Bind converts into")

// bindField writes one field from the values its key arrived with.
//
// It returns an error only for a field type it cannot convert into; a value
// that does not convert is added to errs.
func bindField(fv reflect.Value, name string, values []string, errs validation.Errors) error {
	ft := fv.Type()

	raw := ""
	if len(values) > 0 {
		raw = strings.TrimSpace(values[0])
	}

	switch {
	case ft.Kind() == reflect.Slice:
		if ft.Elem().Kind() != reflect.String {
			return errUnsupportedField
		}
		if len(values) == 0 {
			fv.SetZero()
			return nil
		}
		out := reflect.MakeSlice(ft, len(values), len(values))
		for i, value := range values {
			out.Index(i).SetString(strings.TrimSpace(value))
		}
		fv.Set(out)
		return nil

	case ft.Kind() == reflect.Pointer:
		if !convertible(ft.Elem()) {
			return errUnsupportedField
		}
		fv.SetZero()
		if raw == "" {
			return nil
		}
		ptr := reflect.New(ft.Elem())
		if message := convert(ptr.Elem(), raw); message != "" {
			errs.Add(name, message)
			return nil
		}
		fv.Set(ptr)
		return nil

	case convertible(ft):
		fv.SetZero()
		if raw == "" {
			return nil
		}
		if message := convert(fv, raw); message != "" {
			fv.SetZero()
			errs.Add(name, message)
		}
		return nil
	}
	return errUnsupportedField
}

// convertible reports whether convert can write a value of type t.
func convertible(t reflect.Type) bool {
	if t == timeType {
		return true
	}
	if t == durationType {
		return false
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// convert writes raw, which is trimmed and not empty, into fv, whose type
// convertible accepted. It returns the message for the field when raw does
// not convert, and the empty string when it did.
//
// The messages read after the field's name, the way the validation rules'
// messages do.
func convert(fv reflect.Value, raw string) string {
	if fv.Type() == timeType {
		for _, layout := range bindTimeLayouts {
			if t, err := time.Parse(layout, raw); err == nil {
				fv.Set(reflect.ValueOf(t))
				return ""
			}
		}
		return "is not a valid date"
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)

	case reflect.Bool:
		switch {
		case truthy(raw):
			fv.SetBool(true)
		case falsy(raw):
			fv.SetBool(false)
		default:
			return "must be true or false"
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return "is out of range"
			}
			return "must be a whole number"
		}
		fv.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return "is out of range"
			}
			return "must be a whole number of zero or more"
		}
		fv.SetUint(n)

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return "is out of range"
			}
			return "must be a number"
		}
		// ParseFloat accepts "NaN" and "Inf", and neither is a number a
		// person meant to type into a form.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "must be a number"
		}
		fv.SetFloat(f)
	}
	return ""
}
