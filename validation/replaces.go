package validation

import (
	"strings"
)

// This file answers to Illuminate\Validation\Concerns\ReplacesAttributes: the
// :min, :max, :other, :values and :date of each message, filled in with what the
// rule was actually written with.
//
// The PHP dispatches on a method name -- replace{$rule} -- which Go has no
// spelling for, so the same set is a table keyed by the same rule names. A rule
// with no entry keeps its message unchanged, which is what a missing method
// means there. The keys are snake_case because that is how a rule is spelled
// everywhere in this package; the PHP studlies first and snakes back.

// replacerFunc is one entry of that table.
type replacerFunc func(v *Validator, message, attribute, rule string, parameters []string) string

// ReplacerFunc is a replacer an application registers with AddReplacer, in the
// argument order of the Closure the PHP calls: message, attribute, rule,
// parameters, validator.
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

// replaceOtherAndValue answers to replaceAcceptedIf, replaceDeclinedIf,
// replaceMissingIf, replacePresentIf, replaceProhibitedIf and replaceRequiredIf,
// which are one body in the PHP too: :other is the field that decided, and
// :value is what it actually holds.
func replaceOtherAndValue(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)
	value := v.GetDisplayableValue(other, v.data.Get(other))

	message = strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(other))
	return strings.ReplaceAll(message, ":value", value)
}

// replaceOtherAndFirstValue answers to replaceMissingUnless and
// replacePresentUnless: :value is the value written into the rule, not the one
// the other field holds.
func replaceOtherAndFirstValue(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	message = strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(other))
	return strings.ReplaceAll(message, ":value", v.GetDisplayableValue(other, param(parameters, 1)))
}

// replaceOtherAndValues answers to replaceRequiredUnless and
// replaceProhibitedUnless.
func replaceOtherAndValues(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	rest := sliceFrom(parameters, 1)
	values := make([]string, len(rest))
	for i, value := range rest {
		values[i] = v.GetDisplayableValue(other, value)
	}

	message = strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(other))
	return strings.ReplaceAll(message, ":values", strings.Join(values, ", "))
}

// replaceOther answers to replaceRequiredIfAccepted, replaceRequiredIfDeclined,
// replaceProhibitedIfAccepted and replaceProhibitedIfDeclined.
func replaceOther(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceSame answers to replaceSame and replaceDifferent, which the PHP defines
// as a call to the first.
func replaceSame(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceInArray answers to replaceInArray.
func replaceInArray(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":other", v.GetDisplayableAttribute(param(parameters, 0)))
}

// replaceBetween answers to replaceBetween and replaceDigitsBetween.
func replaceBetween(v *Validator, message, attribute, rule string, parameters []string) string {
	message = strings.ReplaceAll(message, ":min", param(parameters, 0))
	return strings.ReplaceAll(message, ":max", param(parameters, 1))
}

// replaceDateFormat answers to replaceDateFormat.
func replaceDateFormat(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":format", param(parameters, 0))
}

// replaceDecimal answers to replaceDecimal: one bound, or both joined by a dash.
func replaceDecimal(v *Validator, message, attribute, rule string, parameters []string) string {
	places := param(parameters, 0)
	if len(parameters) > 1 {
		places = parameters[0] + "-" + parameters[1]
	}
	return strings.ReplaceAll(message, ":decimal", places)
}

// replaceDigits answers to replaceDigits.
func replaceDigits(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":digits", param(parameters, 0))
}

// replaceSize answers to replaceSize.
func replaceSize(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":size", param(parameters, 0))
}

// replaceMin answers to replaceMin and replaceMinDigits.
func replaceMin(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":min", param(parameters, 0))
}

// replaceMax answers to replaceMax and replaceMaxDigits.
func replaceMax(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":max", param(parameters, 0))
}

// replaceMultipleOf answers to replaceMultipleOf.
func replaceMultipleOf(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":value", param(parameters, 0))
}

// replaceValueList answers to replaceExtensions, replaceMimes and
// replaceMimetypes: the parameters as written, comma separated.
func replaceValueList(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":values", strings.Join(parameters, ", "))
}

// replaceDisplayableValueList answers to replaceIn, replaceNotIn,
// replaceRequiredArrayKeys, replaceStartsWith, replaceEndsWith,
// replaceDoesntStartWith and replaceDoesntEndWith: each parameter is a VALUE, so
// it goes through the values lines first.
func replaceDisplayableValueList(v *Validator, message, attribute, rule string, parameters []string) string {
	values := make([]string, len(parameters))
	for i, parameter := range parameters {
		values[i] = v.GetDisplayableValue(attribute, parameter)
	}
	return strings.ReplaceAll(message, ":values", strings.Join(values, ", "))
}

// replaceAttributeList answers to replaceRequiredWith and the seven rules the
// PHP routes into it: each parameter is a FIELD NAME, so it goes through the
// attribute names, and the separator is " / " rather than ", ".
func replaceAttributeList(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":values", strings.Join(v.getAttributeList(parameters), " / "))
}

// replaceProhibits answers to replaceProhibits, which spells the same list into
// :other rather than :values.
func replaceProhibits(v *Validator, message, attribute, rule string, parameters []string) string {
	return strings.ReplaceAll(message, ":other", strings.Join(v.getAttributeList(parameters), " / "))
}

// replaceComparison answers to replaceGt, replaceGte, replaceLt and replaceLte:
// :value is the SIZE of the other field when it has one, and its NAME when the
// field was not sent at all.
func replaceComparison(v *Validator, message, attribute, rule string, parameters []string) string {
	other := param(parameters, 0)

	value := v.GetValue(other)
	if value == nil {
		return strings.ReplaceAll(message, ":value", v.GetDisplayableAttribute(other))
	}
	size, _ := v.GetSize(attribute, value)
	return strings.ReplaceAll(message, ":value", sizeText(size))
}

// replaceDate answers to replaceBefore and the four rules the PHP routes into
// it: :date is the moment when the argument reads as one, and the name of the
// other field when it does not.
//
// The PHP asks strtotime, which accepts "next thursday"; this asks parseDate,
// which is the same set of spellings the date rules themselves read.
func replaceDate(v *Validator, message, attribute, rule string, parameters []string) string {
	moment := param(parameters, 0)

	if _, isDate := parseDate("", moment); !isDate {
		return strings.ReplaceAll(message, ":date", v.GetDisplayableAttribute(moment))
	}
	return strings.ReplaceAll(message, ":date", v.GetDisplayableValue(attribute, moment))
}

// replaceDimensions answers to replaceDimensions: every named parameter fills
// the placeholder its own name spells, so "min_width=100" fills :min_width.
func replaceDimensions(v *Validator, message, attribute, rule string, parameters []string) string {
	for name, value := range v.ParseNamedParameters(parameters) {
		message = strings.ReplaceAll(message, ":"+name, value)
	}
	return message
}

// ReplaceRequiredIfDeclined answers to
// ReplacesAttributes::replaceRequiredIfDeclined, which the PHP declares public
// rather than protected -- it is called from outside the trait.
func (v *Validator) ReplaceRequiredIfDeclined(message, attribute, rule string, parameters []string) string {
	return replaceOther(v, message, attribute, rule, parameters)
}

// ReplaceProhibitedIfDeclined answers to
// ReplacesAttributes::replaceProhibitedIfDeclined, public for the reason
// ReplaceRequiredIfDeclined is.
func (v *Validator) ReplaceProhibitedIfDeclined(message, attribute, rule string, parameters []string) string {
	return replaceOther(v, message, attribute, rule, parameters)
}
