package validation

import (
	"reflect"
	"strings"
	"time"
)

// This file is how the rules a request declares are run over the request itself.
//
// Bind runs Rules over the raw input of the page or of the API call, before it
// converts anything. A request is also built without a page: a job, a command or
// a service called with a struct it filled in by hand. Those never pass Bind, and
// a service that calls the request's Validate -- the second step, the one that
// spans fields or asks the data -- would otherwise run only that second step and
// accept what the first refuses.
//
// CheckRequest is the way for Validate to run the first step as well, over the
// values the struct holds, so one declaration is the rule of the form, of the API
// and of the direct call:
//
//	func (r NoteRequest) Validate() validation.Errors {
//		errs := validators.CheckRequest(r)
//		// ... what spans fields or asks the data
//		return errs
//	}

// RequestData reads the values a request struct holds into the Data its Rules
// are run over: one entry per field carrying a `form:"name"` tag, named as Bind
// names it. A field without the tag, or tagged "-", is not input and is not read,
// and an exported embedded struct with no tag of its own is read as if its fields
// were declared in the request.
//
// A value is read as the input it stands for:
//
//   - a string, or a type whose underlying type is a string, is the text;
//   - a bool, an integer or a float is the number or the bool;
//   - a slice is a list of its members, read the same way, and a nil one is not
//     there;
//   - a pointer is the value it points to, and a nil one is not there;
//   - a time.Time is the moment, and the zero one is not there;
//   - anything else is the value as it is.
//
// A field that is not there is a key the input did not send, which is what
// Bind's conversion writes as the zero value: `required` refuses it and a rule
// that only checks what was sent leaves it alone. A number is always there, 0
// among them, so a request that wants a positive number says min:1 and not
// required: `required` accepts a 0 as it accepts an unchecked box.
//
// request may be a struct or a pointer to one, and RequestData panics for
// anything else.
func RequestData(request any) Data {
	v := reflect.ValueOf(request)
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		panic("validation: RequestData needs a struct or a pointer to one, and was given " + reflect.TypeOf(request).String())
	}
	data := Data{}
	readRequest(v, data)
	return data
}

var timeType = reflect.TypeFor[time.Time]()

// readRequest writes the tagged fields of a struct into data.
func readRequest(v reflect.Value, data Data) {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		tag, tagged := field.Tag.Lookup("form")
		name, _, _ := strings.Cut(tag, ",")

		if field.Anonymous && !tagged && field.IsExported() && field.Type.Kind() == reflect.Struct && field.Type != timeType {
			readRequest(v.Field(i), data)
			continue
		}
		if !tagged || name == "" || name == "-" || !field.IsExported() {
			continue
		}
		if value, there := requestValue(v.Field(i)); there {
			data[name] = value
		}
	}
}

// requestValue reads one field as the input it stands for, and says whether the
// input has it at all.
func requestValue(v reflect.Value) (any, bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, false
		}
		return requestValue(v.Elem())
	case reflect.String:
		return v.String(), true
	case reflect.Bool:
		return v.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, false
		}
		list := make([]any, 0, v.Len())
		for i := range v.Len() {
			member, there := requestValue(v.Index(i))
			if !there {
				member = nil
			}
			list = append(list, member)
		}
		return list, true
	case reflect.Struct:
		if v.Type() == timeType {
			moment := v.Interface().(time.Time)
			return moment, !moment.IsZero()
		}
	}
	return v.Interface(), true
}

// CheckRequest runs the rules the request declares over the values it holds, and
// answers the failures: the same rules, sentences, overrides and attribute names
// Bind runs over the raw input, so the Validate of a request that calls it says
// what the form says.
//
//	var validators = validation.NewFactory(validation.FromTranslation(tr)).Use(br.Rules())
//
//	func (r PersonRequest) Validate() validation.Errors {
//		errs := validators.CheckRequest(r, validation.WithLocale("pt-BR"))
//		return errs
//	}
//
// The locale is an option of the run, as it is for Make, and the Factory is the
// one the application built, so the modules installed on it are the rules the
// request may name.
//
// The rules of a request that declares none pass, and the ones that do not
// compile are a defect of the program, as they are in Bind: CheckRequest panics
// with the error that names the type, the field and the rule.
func (f *Factory) CheckRequest(request RequestRules, opts ...ValidatorOption) Errors {
	compiled, err := f.ForRequest(request)
	if err != nil {
		panic(err)
	}
	if len(compiled.Attributes) > 0 {
		opts = append([]ValidatorOption{WithCustomAttributes(compiled.Attributes)}, opts...)
	}
	return f.Make(RequestData(request), compiled.Set, opts...).Errors()
}
