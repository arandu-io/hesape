package validation

import (
	"slices"
	"strings"

	"github.com/arandu-io/hesape/str"
)

// This file fills the :min, :max, :other, :values and :date of each message with
// what the rule was actually written with.
//
// It is a table keyed by rule name, snake_case as a rule is spelled everywhere
// in this package. A rule with no entry keeps its message unchanged.

// replacerFunc is one entry of that table.
type replacerFunc func(v *Validator, message, attribute, rule string, parameters []string) string

// ReplacerFunc is a replacer an application registers with AddReplacer: message,
// attribute, rule, parameters, validator.
type ReplacerFunc func(message, attribute, rule string, parameters []string, validator *Validator) string

var replacers = map[string]replacerFunc{
	"accepted_if":    replaceOtherAndValue,
	"declined_if":    replaceOtherAndValue,
	"missing_if":     replaceOtherAndValue,
	"present_if":     replaceOtherAndValue,
	"prohibited_if":  replaceOtherAndValue,
	"required_if":    replaceOtherAndValue,
	"between":        replaceBetween,
	"digits_between": replaceBetween,
	"date_format":    replaceDateFormat,
	"decimal":        replaceDecimal,
	"different":      replaceSame,
	"same":           replaceSame,
	"in_array":       replaceInArray,
	"digits":         replaceDigits,
	"size":           replaceSize,
	"min":            replaceMin,
	"min_digits":     replaceMin,
	"max":            replaceMax,
	"max_digits":     replaceMax,
	"multiple_of":    replaceMultipleOf,

	"extensions":           replaceValueList,
	"mimes":                replaceValueList,
	"mimetypes":            replaceValueList,
	"in":                   replaceDisplayableValueList,
	"not_in":               replaceDisplayableValueList,
	"required_array_keys":  replaceDisplayableValueList,
	"in_array_keys":        replaceDisplayableValueList,
	"doesnt_contain":       replaceDisplayableValueList,
	"array_keys":           replaceArrayKeys,
	"encoding":             replaceEncoding,
	"ends_with":            replaceDisplayableValueList,
	"doesnt_end_with":      replaceDisplayableValueList,
	"starts_with":          replaceDisplayableValueList,
	"doesnt_start_with":    replaceDisplayableValueList,
	"missing_with":         replaceAttributeList,
	"missing_with_all":     replaceAttributeList,
	"present_with":         replaceAttributeList,
	"present_with_all":     replaceAttributeList,
	"required_with":        replaceAttributeList,
	"required_with_all":    replaceAttributeList,
	"required_without":     replaceAttributeList,
	"required_without_all": replaceAttributeList,
	"prohibits":            replaceProhibits,

	"missing_unless":         replaceOtherAndFirstValue,
	"present_unless":         replaceOtherAndFirstValue,
	"required_unless":        replaceOtherAndValues,
	"prohibited_unless":      replaceOtherAndValues,
	"required_if_accepted":   replaceOther,
	"required_if_declined":   replaceOther,
	"prohibited_if_accepted": replaceOther,
	"prohibited_if_declined": replaceOther,

	"gt":  replaceComparison,
	"gte": replaceComparison,
	"lt":  replaceComparison,
	"lte": replaceComparison,

	"before":          replaceDate,
	"before_or_equal": replaceDate,
	"after":           replaceDate,
	"after_or_equal":  replaceDate,
	"date_equals":     replaceDate,

	"dimensions": replaceDimensions,
}

// replaceOtherAndValue is the body every _if rule shares: :other is the field
// that decided, and :value is what it actually holds.
func replaceOtherAndValue(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)
	value := v.GetDisplayableValue(other, v.data.Get(other))

	message = keepCase(message, "other", v.GetDisplayableAttribute(other))
	return keepCase(message, "value", value)
}

// replaceOtherAndFirstValue is the body missing_unless and present_unless share:
// :value is the value written into the rule, not the one the other field
// holds.
func replaceOtherAndFirstValue(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	message = keepCase(message, "other", v.GetDisplayableAttribute(other))
	return keepCase(message, "value", v.GetDisplayableValue(other, param(parameters, 1)))
}

// replaceOtherAndValues is the body required_unless and prohibited_unless share:
// :values is every value written into the rule.
func replaceOtherAndValues(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	rest := sliceFrom(parameters, 1)
	values := make([]string, len(rest))
	for i, value := range rest {
		values[i] = v.GetDisplayableValue(other, value)
	}

	message = keepCase(message, "other", v.GetDisplayableAttribute(other))
	return keepCase(message, "values", strings.Join(values, ", "))
}

// replaceOther fills :other for the four rules whose only parameter is another
// field: required_if_accepted, required_if_declined, prohibited_if_accepted and
// prohibited_if_declined.
func replaceOther(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceSame fills :other for `same` and `different`.
func replaceSame(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceInArray fills :other with the field `in_array` reads its values from.
func replaceInArray(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceBetween fills :min and :max for `between` and `digits_between`.
func replaceBetween(v *Validator, message, attribute, rule string, parameters []string) string {
	message = keepCase(message, "min", param(parameters, 0))
	return keepCase(message, "max", param(parameters, 1))
}

// replaceDateFormat fills :format with the layout the rule was written with.
func replaceDateFormat(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "format", param(parameters, 0))
}

// replaceDecimal fills :decimal with one bound, or both joined by a dash.
func replaceDecimal(v *Validator, message, attribute, rule string, parameters []string) string {
	places := param(parameters, 0)
	if len(parameters) > 1 {
		places = parameters[0] + "-" + parameters[1]
	}
	return keepCase(message, "decimal", places)
}

// replaceDigits fills :digits with how many the rule asked for.
func replaceDigits(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "digits", param(parameters, 0))
}

// replaceSize fills :size with the size the rule asked for.
func replaceSize(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "size", param(parameters, 0))
}

// replaceMin fills :min for `min` and `min_digits`.
func replaceMin(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "min", param(parameters, 0))
}

// replaceMax fills :max for `max` and `max_digits`.
func replaceMax(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "max", param(parameters, 0))
}

// replaceMultipleOf fills :value with the divisor.
func replaceMultipleOf(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "value", param(parameters, 0))
}

// replaceValueList fills :values with the parameters as written, comma
// separated. It is what `extensions`, `mimes` and `mimetypes` use.
func replaceValueList(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "values", strings.Join(parameters, ", "))
}

// replaceDisplayableValueList fills :values for the rules whose parameters are
// VALUES, so each goes through the values lines first.
func replaceDisplayableValueList(v *Validator, message, attribute, rule string, parameters []string) string {
	values := make([]string, len(parameters))
	for i, parameter := range parameters {
		values[i] = v.GetDisplayableValue(attribute, parameter)
	}
	return keepCase(message, "values", strings.Join(values, ", "))
}

// replaceAttributeList fills :values for the rules whose parameters are FIELD
// NAMES, so each goes through the attribute names, and the separator is " / "
// rather than ", ".
func replaceAttributeList(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "values", strings.Join(v.getAttributeList(parameters), " / "))
}

// replaceProhibits spells the same list of field names into :other rather than
// :values.
func replaceProhibits(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "other", strings.Join(v.getAttributeList(parameters), " / "))
}

// replaceComparison fills :value for the four comparisons: the SIZE of the other
// field when it has one, and its NAME when the field was not sent at all.
func replaceComparison(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	value := v.GetValue(other)
	if value == nil {
		return keepCase(message, "value", v.GetDisplayableAttribute(other))
	}
	size, _ := v.GetSize(attribute, value)
	return keepCase(message, "value", sizeText(size))
}

// replaceDate fills :date for the five date comparisons: the moment when the
// argument reads as one, and the name of the other field when it does not.
//
// It reads the argument with parseDate, which is the same set of spellings the
// date rules themselves read.
func replaceDate(v *Validator, message, attribute, rule string, parameters []string) string {
	moment := param(parameters, 0)

	if _, isDate := parseDate("", moment); !isDate {
		return keepCase(message, "date", v.GetDisplayableAttribute(moment))
	}
	return keepCase(message, "date", v.GetDisplayableValue(attribute, moment))
}

// replaceDimensions fills one placeholder per named parameter, each spelled by
// its own name: "min_width=100" fills :min_width.
func replaceDimensions(v *Validator, message, attribute, rule string, parameters []string) string {
	for name, value := range v.ParseNamedParameters(parameters) {
		message = keepCase(message, name, value)
	}
	return message
}

// ReplaceRequiredIfDeclined fills :other for `required_if_declined`. It is
// exported because it is called from outside this package.
func (v *Validator) ReplaceRequiredIfDeclined(message, attribute, rule string, parameters []string) string {
	return replaceOther(v, message, attribute, rule, parameters)
}

// ReplaceProhibitedIfDeclined fills :other for `prohibited_if_declined`,
// exported for the reason ReplaceRequiredIfDeclined is.
func (v *Validator) ReplaceProhibitedIfDeclined(message, attribute, rule string, parameters []string) string {
	return replaceOther(v, message, attribute, rule, parameters)
}

// keepCase fills the three spellings of one placeholder: ":name" with the value,
// ":Name" with its first letter upper case, and ":NAME" with all of it upper
// case. A line chooses its own capitalisation by the spelling it writes, which
// is how one argument serves a sentence that opens with it and one that does
// not.
func keepCase(message, name, value string) string {
	message = strings.ReplaceAll(message, ":"+name, value)
	message = strings.ReplaceAll(message, ":"+strings.ToUpper(name), strings.ToUpper(value))
	return strings.ReplaceAll(message, ":"+str.Ucfirst(name), str.Ucfirst(value))
}

// replaceEncoding fills :encoding with the name the rule was written with.
func replaceEncoding(v *Validator, message, attribute, rule string, parameters []string) string {
	return keepCase(message, "encoding", param(parameters, 0))
}

// replaceArrayKeys fills :values with the keys the rule allows and :unexpected
// with the keys the value held that it does not.
func replaceArrayKeys(v *Validator, message, attribute, rule string, parameters []string) string {
	message = replaceDisplayableValueList(v, message, attribute, rule, parameters)

	var unexpected []string
	if keys, ok := keysOf(v.GetValue(attribute)); ok {
		slices.Sort(keys)
		for _, key := range keys {
			if !slices.Contains(parameters, key) {
				unexpected = append(unexpected, v.GetDisplayableValue(attribute, key))
			}
		}
	}
	return keepCase(message, "unexpected", strings.Join(unexpected, ", "))
}
