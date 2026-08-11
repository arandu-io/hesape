package validation

import (
	"maps"
	"slices"
	"strings"
)

// This file is lang/en/validation.php, and the smallest Translator that reads
// it.
//
// It lives here rather than in hesape/translation/lang/en/validation.json only
// because that catalogue belongs to another package; the lines are the same
// lines, and a Validator handed a Translator never reads this table at all. When
// the catalogue there grows the full set, this table is deleted and
// englishTranslator with it.

// englishLines is lang/en/validation.php, verbatim, minus the three groups it
// ships empty -- custom, attributes and values -- which an application fills.
var englishLines = map[string]any{
	"accepted":        "The :attribute field must be accepted.",
	"accepted_if":     "The :attribute field must be accepted when :other is :value.",
	"active_url":      "The :attribute field must be a valid URL.",
	"after":           "The :attribute field must be a date after :date.",
	"after_or_equal":  "The :attribute field must be a date after or equal to :date.",
	"alpha":           "The :attribute field must only contain letters.",
	"alpha_dash":      "The :attribute field must only contain letters, numbers, dashes, and underscores.",
	"alpha_num":       "The :attribute field must only contain letters and numbers.",
	"any_of":          "The :attribute field is invalid.",
	"array":           "The :attribute field must be an array.",
	"ascii":           "The :attribute field must only contain single-byte alphanumeric characters and symbols.",
	"before":          "The :attribute field must be a date before :date.",
	"before_or_equal": "The :attribute field must be a date before or equal to :date.",
	"between": map[string]string{
		"array":   "The :attribute field must have between :min and :max items.",
		"file":    "The :attribute field must be between :min and :max kilobytes.",
		"numeric": "The :attribute field must be between :min and :max.",
		"string":  "The :attribute field must be between :min and :max characters.",
	},
	"boolean":           "The :attribute field must be true or false.",
	"can":               "The :attribute field contains an unauthorized value.",
	"confirmed":         "The :attribute field confirmation does not match.",
	"contains":          "The :attribute field is missing a required value.",
	"current_password":  "The password is incorrect.",
	"date":              "The :attribute field must be a valid date.",
	"date_equals":       "The :attribute field must be a date equal to :date.",
	"date_format":       "The :attribute field must match the format :format.",
	"decimal":           "The :attribute field must have :decimal decimal places.",
	"declined":          "The :attribute field must be declined.",
	"declined_if":       "The :attribute field must be declined when :other is :value.",
	"different":         "The :attribute field and :other must be different.",
	"digits":            "The :attribute field must be :digits digits.",
	"digits_between":    "The :attribute field must be between :min and :max digits.",
	"dimensions":        "The :attribute field has invalid image dimensions.",
	"distinct":          "The :attribute field has a duplicate value.",
	"doesnt_end_with":   "The :attribute field must not end with one of the following: :values.",
	"doesnt_start_with": "The :attribute field must not start with one of the following: :values.",
	"email":             "The :attribute field must be a valid email address.",
	"ends_with":         "The :attribute field must end with one of the following: :values.",
	"enum":              "The selected :attribute is invalid.",
	"exists":            "The selected :attribute is invalid.",
	"extensions":        "The :attribute field must have one of the following extensions: :values.",
	"file":              "The :attribute field must be a file.",
	"filled":            "The :attribute field must have a value.",
	"gt": map[string]string{
		"array":   "The :attribute field must have more than :value items.",
		"file":    "The :attribute field must be greater than :value kilobytes.",
		"numeric": "The :attribute field must be greater than :value.",
		"string":  "The :attribute field must be greater than :value characters.",
	},
	"gte": map[string]string{
		"array":   "The :attribute field must have :value items or more.",
		"file":    "The :attribute field must be greater than or equal to :value kilobytes.",
		"numeric": "The :attribute field must be greater than or equal to :value.",
		"string":  "The :attribute field must be greater than or equal to :value characters.",
	},
	"hex_color": "The :attribute field must be a valid hexadecimal color.",
	"image":     "The :attribute field must be an image.",
	"in":        "The selected :attribute is invalid.",
	"in_array":  "The :attribute field must exist in :other.",
	"integer":   "The :attribute field must be an integer.",
	"ip":        "The :attribute field must be a valid IP address.",
	"ipv4":      "The :attribute field must be a valid IPv4 address.",
	"ipv6":      "The :attribute field must be a valid IPv6 address.",
	"json":      "The :attribute field must be a valid JSON string.",
	"list":      "The :attribute field must be a list.",
	"lowercase": "The :attribute field must be lowercase.",
	"lt": map[string]string{
		"array":   "The :attribute field must have less than :value items.",
		"file":    "The :attribute field must be less than :value kilobytes.",
		"numeric": "The :attribute field must be less than :value.",
		"string":  "The :attribute field must be less than :value characters.",
	},
	"lte": map[string]string{
		"array":   "The :attribute field must not have more than :value items.",
		"file":    "The :attribute field must be less than or equal to :value kilobytes.",
		"numeric": "The :attribute field must be less than or equal to :value.",
		"string":  "The :attribute field must be less than or equal to :value characters.",
	},
	"mac_address": "The :attribute field must be a valid MAC address.",
	"max": map[string]string{
		"array":   "The :attribute field must not have more than :max items.",
		"file":    "The :attribute field must not be greater than :max kilobytes.",
		"numeric": "The :attribute field must not be greater than :max.",
		"string":  "The :attribute field must not be greater than :max characters.",
	},
	"max_digits": "The :attribute field must not have more than :max digits.",
	"mimes":      "The :attribute field must be a file of type: :values.",
	"mimetypes":  "The :attribute field must be a file of type: :values.",
	"min": map[string]string{
		"array":   "The :attribute field must have at least :min items.",
		"file":    "The :attribute field must be at least :min kilobytes.",
		"numeric": "The :attribute field must be at least :min.",
		"string":  "The :attribute field must be at least :min characters.",
	},
	"min_digits":       "The :attribute field must have at least :min digits.",
	"missing":          "The :attribute field must be missing.",
	"missing_if":       "The :attribute field must be missing when :other is :value.",
	"missing_unless":   "The :attribute field must be missing unless :other is :value.",
	"missing_with":     "The :attribute field must be missing when :values is present.",
	"missing_with_all": "The :attribute field must be missing when :values are present.",
	"multiple_of":      "The :attribute field must be a multiple of :value.",
	"not_in":           "The selected :attribute is invalid.",
	"not_regex":        "The :attribute field format is invalid.",
	"numeric":          "The :attribute field must be a number.",
	"password": map[string]string{
		"letters":       "The :attribute field must contain at least one letter.",
		"mixed":         "The :attribute field must contain at least one uppercase and one lowercase letter.",
		"numbers":       "The :attribute field must contain at least one number.",
		"symbols":       "The :attribute field must contain at least one symbol.",
		"uncompromised": "The given :attribute has appeared in a data leak. Please choose a different :attribute.",
	},
	"present":                "The :attribute field must be present.",
	"present_if":             "The :attribute field must be present when :other is :value.",
	"present_unless":         "The :attribute field must be present unless :other is :value.",
	"present_with":           "The :attribute field must be present when :values is present.",
	"present_with_all":       "The :attribute field must be present when :values are present.",
	"prohibited":             "The :attribute field is prohibited.",
	"prohibited_if":          "The :attribute field is prohibited when :other is :value.",
	"prohibited_if_accepted": "The :attribute field is prohibited when :other is accepted.",
	"prohibited_if_declined": "The :attribute field is prohibited when :other is declined.",
	"prohibited_unless":      "The :attribute field is prohibited unless :other is in :values.",
	"prohibits":              "The :attribute field prohibits :other from being present.",
	"regex":                  "The :attribute field format is invalid.",
	"required":               "The :attribute field is required.",
	"required_array_keys":    "The :attribute field must contain entries for: :values.",
	"required_if":            "The :attribute field is required when :other is :value.",
	"required_if_accepted":   "The :attribute field is required when :other is accepted.",
	"required_if_declined":   "The :attribute field is required when :other is declined.",
	"required_unless":        "The :attribute field is required unless :other is in :values.",
	"required_with":          "The :attribute field is required when :values is present.",
	"required_with_all":      "The :attribute field is required when :values are present.",
	"required_without":       "The :attribute field is required when :values is not present.",
	"required_without_all":   "The :attribute field is required when none of :values are present.",
	"same":                   "The :attribute field must match :other.",
	"size": map[string]string{
		"array":   "The :attribute field must contain :size items.",
		"file":    "The :attribute field must be :size kilobytes.",
		"numeric": "The :attribute field must be :size.",
		"string":  "The :attribute field must be :size characters.",
	},
	"starts_with": "The :attribute field must start with one of the following: :values.",
	"string":      "The :attribute field must be a string.",
	"timezone":    "The :attribute field must be a valid timezone.",
	"ulid":        "The :attribute field must be a valid ULID.",
	"unique":      "The :attribute has already been taken.",
	"uploaded":    "The :attribute failed to upload.",
	"uppercase":   "The :attribute field must be uppercase.",
	"url":         "The :attribute field must be a valid URL.",
	"uuid":        "The :attribute field must be a valid UUID.",
}

// englishTranslator is the Translator a Validator falls back to inside
// GetDisplayableValue and the custom-message lookups: it answers out of
// englishLines and returns the key for anything it does not hold, which is the
// contract every check in formats.go is written against.
//
// A Validator built with no translator does not read the LINES -- getMessage
// asks v.trans directly and stops at the compiled rule set's own sentence. This
// is what answers the group lookups either way.
type englishTranslator struct{}

// Get answers to Translator::get over englishLines. The key is dot notation
// under "validation.", so "validation.min.string" reads the "string" line of the
// "min" group.
func (englishTranslator) Get(key string, replace map[string]any, locale string) any {
	rest, under := strings.CutPrefix(key, "validation.")
	if !under {
		return interpolate(key, replace)
	}
	head, tail, nested := strings.Cut(rest, ".")
	entry, held := englishLines[head]
	if !held {
		return key
	}
	if !nested {
		return interpolate(entry, replace)
	}
	group, isGroup := entry.(map[string]string)
	if !isGroup {
		return key
	}
	sentence, held := group[tail]
	if !held {
		return key
	}
	return interpolate(sentence, replace)
}

// Choice answers to Translator::choice. englishLines holds no line with a plural
// segment, so this is Get with :count filled in.
func (t englishTranslator) Choice(key string, number int, replace map[string]any, locale string) string {
	if replace == nil {
		replace = map[string]any{}
	}
	if _, given := replace["count"]; !given {
		replace["count"] = number
	}
	return line(t.Get(key, replace, locale), key)
}

// interpolate fills the :placeholders of a line, which is what
// Illuminate\Translation\Translator::makeReplacements does before returning one.
func interpolate(entry any, replace map[string]any) any {
	sentence, isSentence := entry.(string)
	if !isSentence || len(replace) == 0 {
		return entry
	}
	for _, name := range slices.Sorted(maps.Keys(replace)) {
		sentence = strings.ReplaceAll(sentence, ":"+name, stringOf(replace[name]))
	}
	return sentence
}
