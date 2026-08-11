package validation

import (
	"context"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/str"
)

// This file answers to Illuminate\Validation\Concerns\ValidatesAttributes: one
// method per rule, at the PHP's spelling, reading the PHP's body.
//
// Every one takes (attribute, value, parameters) even where the PHP declares
// two arguments. The PHP calls them all through one line --
// `$this->$method($attribute, $value, $parameters, $this)` -- and tolerates the
// extra argument; a single Go signature is what lets the rule table hold the
// method itself rather than a closure around it, and so what keeps the table
// from being a second place where a rule's behaviour is written.
//
// `value` is `any` where the PHP says `mixed`, and `parameters` is []string
// where the PHP says array<int, int|string>: a rule string carries text.

// numericRules answers to Validator::$numericRules: the three rules that make
// every size rule on the field measure the value rather than the characters.
var numericRules = []string{"numeric", "integer", "decimal"}

// implicitRules answers to Validator::$implicitRules: the rules that run even
// when the value is blank, and whose failure stops the field.
var implicitRules = []string{
	"accepted", "accepted_if", "declined", "declined_if", "filled", "missing",
	"missing_if", "missing_unless", "missing_with", "missing_with_all",
	"present", "present_if", "present_unless", "present_with",
	"present_with_all", "required", "required_if", "required_if_accepted",
	"required_if_declined", "required_unless", "required_with",
	"required_with_all", "required_without", "required_without_all",
}

// excludeRules answers to Validator::$excludeRules: the five whose failure
// removes the field from the validated data instead of putting a message on it.
var excludeRules = []string{"exclude", "exclude_if", "exclude_unless", "exclude_with", "exclude_without"}

// acceptable and declinable answer to the arrays validateAccepted and
// validateDeclined compare against. The comparison is PHP's strict in_array, so
// the bool and the int are separate cases in accepted rather than extra strings.
var (
	acceptable = []string{"yes", "on", "1", "true"}
	declinable = []string{"no", "off", "0", "false"}
)

// phpExtensions answers to the list shouldBlockPhpUpload refuses.
var phpExtensions = []string{"php", "php3", "php4", "php5", "php7", "php8", "phtml", "phar"}

// ---------------------------------------------------------------------------
// Accepted and declined.
// ---------------------------------------------------------------------------

// ValidateAccepted answers to validateAccepted. It implies the attribute is
// required.
func (v *Validator) ValidateAccepted(attribute string, value any, parameters []string) bool {
	return v.ValidateRequired(attribute, value, nil) && isAccepted(value)
}

// ValidateAcceptedIf answers to validateAcceptedIf: accepted when the other
// attribute holds one of the given values.
func (v *Validator) ValidateAcceptedIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "accepted_if") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return v.ValidateRequired(attribute, value, nil) && isAccepted(value)
	}
	return true
}

// ValidateDeclined answers to validateDeclined. It implies the attribute is
// required.
func (v *Validator) ValidateDeclined(attribute string, value any, parameters []string) bool {
	return v.ValidateRequired(attribute, value, nil) && isDeclined(value)
}

// ValidateDeclinedIf answers to validateDeclinedIf.
func (v *Validator) ValidateDeclinedIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "declined_if") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return v.ValidateRequired(attribute, value, nil) && isDeclined(value)
	}
	return true
}

// isAccepted is the strict in_array of validateAccepted: the four strings, the
// integer 1 and the boolean true, and nothing that merely looks like them.
func isAccepted(value any) bool {
	switch n := value.(type) {
	case string:
		return slices.Contains(acceptable, n)
	case bool:
		return n
	case int:
		return n == 1
	case int64:
		return n == 1
	}
	return false
}

// isDeclined is the strict in_array of validateDeclined.
func isDeclined(value any) bool {
	switch n := value.(type) {
	case string:
		return slices.Contains(declinable, n)
	case bool:
		return !n
	case int:
		return n == 0
	case int64:
		return n == 0
	}
	return false
}

// ---------------------------------------------------------------------------
// The network.
// ---------------------------------------------------------------------------

// ValidateActiveUrl answers to validateActiveUrl: the host of the value has an
// A or an AAAA record.
//
// The name keeps the PHP's spelling of Url rather than the Go initialism (ADR
// 0044 allows the initialism, and it is not taken here because `url` is the
// rule somebody types and ValidateUrl would then read differently from its
// neighbour ValidateActiveUrl).
//
// This puts a DNS lookup on the request path. It is Laravel's rule and it does
// what Laravel's does; the deadline comes from the Validator's context, which
// is the request's.
func (v *Validator) ValidateActiveUrl(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	if !ok {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	records, err := v.GetDNSRecords(parsed.Hostname())
	return err == nil && len(records) > 0
}

// GetDNSRecords answers to getDnsRecords, with the initialism the Go spelling
// asks for (ADR 0044).
func (v *Validator) GetDNSRecords(hostname string) ([]string, error) {
	addrs, err := v.resolver().LookupIPAddr(v.Context(), hostname)
	if err != nil {
		return nil, err
	}
	records := make([]string, 0, len(addrs))
	for _, a := range addrs {
		records = append(records, a.IP.String())
	}
	return records, nil
}

// ---------------------------------------------------------------------------
// Shape of the text.
// ---------------------------------------------------------------------------

// ValidateAscii answers to validateAscii.
func (v *Validator) ValidateAscii(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	if !ok {
		return false
	}
	return str.IsASCII(s)
}

// ValidateBail answers to validateBail. It is a marker: Set.Validate reads it
// and stops the field at its first failure.
func (v *Validator) ValidateBail(attribute string, value any, parameters []string) bool { return true }

// ValidateAlpha answers to validateAlpha. With the parameter "ascii" it is the
// ASCII letters only, as the PHP's is.
func (v *Validator) ValidateAlpha(attribute string, value any, parameters []string) bool {
	return v.matchesAlphabet(value, parameters, alphaUnicode, alphaASCII)
}

// ValidateAlphaDash answers to validateAlphaDash.
func (v *Validator) ValidateAlphaDash(attribute string, value any, parameters []string) bool {
	return v.matchesAlphabet(value, parameters, dashUnicode, dashASCII)
}

// ValidateAlphaNum answers to validateAlphaNum.
func (v *Validator) ValidateAlphaNum(attribute string, value any, parameters []string) bool {
	return v.matchesAlphabet(value, parameters, numUnicode, numASCII)
}

func (v *Validator) matchesAlphabet(value any, parameters []string, unicoded, ascii *regexp.Regexp) bool {
	if !isStringOrNumber(value) {
		return false
	}
	if len(parameters) == 1 && parameters[0] == "ascii" {
		return ascii.MatchString(stringOf(value))
	}
	return unicoded.MatchString(stringOf(value))
}

// ValidateLowercase answers to validateLowercase.
func (v *Validator) ValidateLowercase(attribute string, value any, parameters []string) bool {
	s := stringOf(value)
	return strings.ToLower(s) == s
}

// ValidateUppercase answers to validateUppercase.
func (v *Validator) ValidateUppercase(attribute string, value any, parameters []string) bool {
	s := stringOf(value)
	return strings.ToUpper(s) == s
}

// ValidateStartsWith answers to validateStartsWith.
func (v *Validator) ValidateStartsWith(attribute string, value any, parameters []string) bool {
	return anyAffix(parameters, strings.HasPrefix, stringOf(value))
}

// ValidateDoesntStartWith answers to validateDoesntStartWith.
func (v *Validator) ValidateDoesntStartWith(attribute string, value any, parameters []string) bool {
	return !anyAffix(parameters, strings.HasPrefix, stringOf(value))
}

// ValidateEndsWith answers to validateEndsWith.
func (v *Validator) ValidateEndsWith(attribute string, value any, parameters []string) bool {
	return anyAffix(parameters, strings.HasSuffix, stringOf(value))
}

// ValidateDoesntEndWith answers to validateDoesntEndWith.
func (v *Validator) ValidateDoesntEndWith(attribute string, value any, parameters []string) bool {
	return !anyAffix(parameters, strings.HasSuffix, stringOf(value))
}

// ValidateString answers to validateString: is_string.
//
// It is not the tautology it looks like. A JSON body carries numbers, booleans
// and lists, and this is the rule that refuses them where text was asked for.
func (v *Validator) ValidateString(attribute string, value any, parameters []string) bool {
	_, ok := asString(value)
	return ok
}

// ---------------------------------------------------------------------------
// Arrays.
// ---------------------------------------------------------------------------

// ValidateArray answers to validateArray: the value is an array, and with
// parameters, an array with no key outside them.
func (v *Validator) ValidateArray(attribute string, value any, parameters []string) bool {
	if !isArray(value) {
		return false
	}
	if len(parameters) == 0 {
		return true
	}
	switch node := value.(type) {
	case Data:
		for key := range node {
			if !slices.Contains(parameters, key) {
				return false
			}
		}
	case map[string]any:
		for key := range node {
			if !slices.Contains(parameters, key) {
				return false
			}
		}
	case []any:
		for i := range node {
			if !slices.Contains(parameters, strconv.Itoa(i)) {
				return false
			}
		}
	}
	return true
}

// ValidateList answers to validateList: is_array && array_is_list. A Data is an
// array with keys, so it is not a list; a []any is.
func (v *Validator) ValidateList(attribute string, value any, parameters []string) bool {
	_, ok := asList(value)
	return ok
}

// ValidateRequiredArrayKeys answers to validateRequiredArrayKeys.
func (v *Validator) ValidateRequiredArrayKeys(attribute string, value any, parameters []string) bool {
	keyed, ok := value.(Data)
	if !ok {
		if m, isMap := value.(map[string]any); isMap {
			keyed = Data(m)
		} else {
			return false
		}
	}
	for _, key := range parameters {
		if _, exists := keyed[key]; !exists {
			return false
		}
	}
	return true
}

// ValidateContains answers to validateContains: the array holds every one of
// the given values.
func (v *Validator) ValidateContains(attribute string, value any, parameters []string) bool {
	list, ok := asList(value)
	if !ok {
		return false
	}
	for _, parameter := range parameters {
		found := false
		for _, member := range list {
			if stringOf(member) == parameter {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ValidateDistinct answers to validateDistinct: no value repeats.
//
// Laravel expands `foo.*` into one rule per member and calls this once per
// member, comparing each against its siblings. There are no wildcards here --
// RULE 15 keeps the rule string a closed set -- so the rule is written against
// the list itself and asked of it once, which is the same question. The
// parameters are the PHP's: `ignore_case` compares case-insensitively and
// `strict` compares types as well as values.
func (v *Validator) ValidateDistinct(attribute string, value any, parameters []string) bool {
	list, ok := asList(value)
	if !ok {
		return true
	}
	ignoreCase := slices.Contains(parameters, "ignore_case")
	strict := slices.Contains(parameters, "strict")

	for i, member := range list {
		for _, other := range list[i+1:] {
			switch {
			case ignoreCase:
				if strings.EqualFold(stringOf(member), stringOf(other)) {
					return false
				}
			case strict:
				if sameType(member, other) && sameValue(member, other) {
					return false
				}
			default:
				if stringOf(member) == stringOf(other) {
					return false
				}
			}
		}
	}
	return true
}

// GetDistinctValues answers to getDistinctValues: the values `distinct`
// compares. It is the list held at the attribute, for the reason
// ValidateDistinct gives.
func (v *Validator) GetDistinctValues(attribute string) []any {
	return v.ExtractDistinctValues(attribute)
}

// ExtractDistinctValues answers to extractDistinctValues.
func (v *Validator) ExtractDistinctValues(attribute string) []any {
	list, _ := asList(v.GetValue(attribute))
	return list
}

// ValidateInArray answers to validateInArray: the value is one of the values
// held by the other attribute.
func (v *Validator) ValidateInArray(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "in_array") {
		return false
	}
	other := v.GetValue(parameters[0])
	list, ok := asList(other)
	if !ok {
		return false
	}
	for _, member := range list {
		if sameValue(member, value) || stringOf(member) == stringOf(value) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Size, and the four comparisons.
// ---------------------------------------------------------------------------

// ValidateBetween answers to validateBetween. Both bounds are inclusive.
func (v *Validator) ValidateBetween(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "between") {
		return false
	}
	low, ok := number(strings.TrimSpace(parameters[0]))
	high, okHigh := number(strings.TrimSpace(parameters[1]))
	if !ok || !okHigh {
		return false
	}
	size := v.GetSize(attribute, value)
	return size >= low && size <= high
}

// ValidateMin answers to validateMin.
func (v *Validator) ValidateMin(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "min") {
		return false
	}
	low, ok := number(strings.TrimSpace(parameters[0]))
	return ok && v.GetSize(attribute, value) >= low
}

// ValidateMax answers to validateMax. An upload that did not finish fails
// before its size is asked for, as the PHP's does.
func (v *Validator) ValidateMax(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "max") {
		return false
	}
	if up, ok := value.(UploadedFile); ok && !up.IsValid() {
		return false
	}
	high, ok := number(strings.TrimSpace(parameters[0]))
	return ok && v.GetSize(attribute, value) <= high
}

// ValidateSize answers to validateSize.
//
// The size is four things under one name, exactly as getSize is: the characters
// of a string, the value of a number when the field declares numeric, integer
// or decimal, the members of an array, and the KILOBYTES of a file.
func (v *Validator) ValidateSize(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "size") {
		return false
	}
	want, ok := number(strings.TrimSpace(parameters[0]))
	return ok && v.GetSize(attribute, value) == want
}

// ValidateGt answers to validateGt.
func (v *Validator) ValidateGt(attribute string, value any, parameters []string) bool {
	return v.compareToAnother(attribute, value, parameters, "gt", func(c int) bool { return c > 0 })
}

// ValidateGte answers to validateGte.
func (v *Validator) ValidateGte(attribute string, value any, parameters []string) bool {
	return v.compareToAnother(attribute, value, parameters, "gte", func(c int) bool { return c >= 0 })
}

// ValidateLt answers to validateLt.
func (v *Validator) ValidateLt(attribute string, value any, parameters []string) bool {
	return v.compareToAnother(attribute, value, parameters, "lt", func(c int) bool { return c < 0 })
}

// ValidateLte answers to validateLte.
func (v *Validator) ValidateLte(attribute string, value any, parameters []string) bool {
	return v.compareToAnother(attribute, value, parameters, "lte", func(c int) bool { return c <= 0 })
}

// compareToAnother is the body the four comparisons share, in the PHP's order:
// a literal bound when the parameter names no field, a numeric comparison when
// the field declares numeric, a refusal when the two values are not the same
// kind of thing, and a comparison of sizes otherwise.
func (v *Validator) compareToAnother(attribute string, value any, parameters []string, name string, ok func(int) bool) bool {
	if !v.RequireParameterCount(1, parameters, name) {
		return false
	}
	other := v.GetValue(parameters[0])

	bound, isBound := number(strings.TrimSpace(parameters[0]))
	if other == nil && isBound {
		if _, numeric := numberOf(value); numeric {
			return ok(compareFloats(v.GetSize(attribute, value), bound))
		}
	}
	if isBound {
		// The parameter is a number and a field of that name does hold a value:
		// Laravel refuses rather than guessing which was meant.
		return false
	}
	if v.HasRule(attribute, numericRules) {
		mine, mineOK := numberOf(value)
		theirs, theirsOK := numberOf(other)
		if mineOK && theirsOK {
			return ok(compareFloats(mine, theirs))
		}
	}
	if !sameType(value, other) {
		return false
	}
	return ok(compareFloats(v.GetSize(attribute, value), v.GetSize(parameters[0], other)))
}

func compareFloats(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// GetSize answers to getSize: what min, max, size, between and the four
// comparisons measure.
//
// A number is its own size when the field declares numeric, integer or decimal;
// an array is how many members it has; a file is its KILOBYTES; anything else
// is how many characters it has. Characters, never bytes: a limit in bytes
// rejects valid input in every language that needs more than one byte for a
// letter.
func (v *Validator) GetSize(attribute string, value any) float64 {
	hasNumeric := v.HasRule(attribute, numericRules)
	if n, ok := numberOf(value); ok && hasNumeric {
		return n
	}
	if n, ok := countOf(value); ok {
		return float64(n)
	}
	if f, ok := asFile(value); ok {
		return float64(f.GetSize()) / 1024
	}
	return float64(len([]rune(stringOf(value))))
}

// ValidateDigits answers to validateDigits.
func (v *Validator) ValidateDigits(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "digits") {
		return false
	}
	want, ok := whole(parameters[0])
	s := stringOf(value)
	return ok && digitsOnly(s) && int64(len(s)) == want
}

// ValidateDigitsBetween answers to validateDigitsBetween.
func (v *Validator) ValidateDigitsBetween(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "digits_between") {
		return false
	}
	low, okLow := whole(parameters[0])
	high, okHigh := whole(parameters[1])
	s := stringOf(value)
	n := int64(len(s))
	return okLow && okHigh && digitsOnly(s) && n >= low && n <= high
}

// ValidateMaxDigits answers to validateMaxDigits.
func (v *Validator) ValidateMaxDigits(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "max_digits") {
		return false
	}
	high, ok := whole(parameters[0])
	s := stringOf(value)
	return ok && digitsOnly(s) && int64(len(s)) <= high
}

// ValidateMinDigits answers to validateMinDigits.
func (v *Validator) ValidateMinDigits(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "min_digits") {
		return false
	}
	low, ok := whole(parameters[0])
	s := stringOf(value)
	return ok && digitsOnly(s) && int64(len(s)) >= low
}

// ValidateMultipleOf answers to validateMultipleOf.
//
// Zero on both sides fails, a zero numerator over a non-zero denominator
// passes, and a zero denominator fails -- the three cases the PHP writes out,
// because "is 0 a multiple of 0" has no answer worth guessing.
func (v *Validator) ValidateMultipleOf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "multiple_of") {
		return false
	}
	numerator, ok := numberOf(value)
	denominator, okDenominator := number(strings.TrimSpace(parameters[0]))
	if !ok || !okDenominator {
		return false
	}
	switch {
	case numerator == 0 && denominator == 0:
		return false
	case numerator == 0:
		return true
	case denominator == 0:
		return false
	}
	// Scaled to whole numbers before the remainder, so that 0.3 against 0.1 is
	// not the 0.09999999999999998 that float arithmetic answers.
	scale := math.Pow(10, float64(max(decimalsOf(stringOf(value)), decimalsOf(parameters[0]))))
	return math.Mod(math.Round(numerator*scale), math.Round(denominator*scale)) == 0
}

func decimalsOf(s string) int {
	if _, after, found := strings.Cut(s, "."); found {
		return len(after)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Types.
// ---------------------------------------------------------------------------

// ValidateNumeric answers to validateNumeric: is_numeric.
func (v *Validator) ValidateNumeric(attribute string, value any, parameters []string) bool {
	_, ok := numberOf(value)
	return ok
}

// ValidateInteger answers to validateInteger: FILTER_VALIDATE_INT.
func (v *Validator) ValidateInteger(attribute string, value any, parameters []string) bool {
	switch value.(type) {
	case int, int64:
		return true
	}
	_, ok := whole(strings.TrimSpace(stringOf(value)))
	return ok
}

// ValidateDecimal answers to validateDecimal: the count of digits after the
// point is the given number, or between the two given numbers.
func (v *Validator) ValidateDecimal(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "decimal") {
		return false
	}
	if !v.ValidateNumeric(attribute, value, nil) {
		return false
	}
	places, ok := decimalPlaces(stringOf(value))
	if !ok {
		return false
	}
	want, okWant := whole(parameters[0])
	if !okWant {
		return false
	}
	if len(parameters) < 2 {
		return int64(places) == want
	}
	high, okHigh := whole(parameters[1])
	return okHigh && int64(places) >= want && int64(places) <= high
}

// ValidateBoolean answers to validateBoolean. The comparison is strict, so only
// the six values the PHP lists pass: true, false, 0, 1, "0" and "1". A ticked
// checkbox sends "on", which is what `accepted` is for.
func (v *Validator) ValidateBoolean(attribute string, value any, parameters []string) bool {
	switch n := value.(type) {
	case bool:
		return true
	case int:
		return n == 0 || n == 1
	case int64:
		return n == 0 || n == 1
	case string:
		return n == "0" || n == "1"
	}
	return false
}

// ValidateJson answers to validateJson.
func (v *Validator) ValidateJson(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	return ok && str.IsJSON(s)
}

// ValidateNullable answers to validateNullable. It is a marker: Set.Validate
// reads it and stops the field when the value is null -- null, not empty, which
// is the whole of the difference from leaving `required` off.
func (v *Validator) ValidateNullable(attribute string, value any, parameters []string) bool {
	return true
}

// ValidateSometimes answers to validateSometimes. It is a marker: Set.Validate
// skips the field when its key was not sent at all.
func (v *Validator) ValidateSometimes(attribute string, value any, parameters []string) bool {
	return true
}

// ---------------------------------------------------------------------------
// Dates.
// ---------------------------------------------------------------------------

// ValidateDate answers to validateDate.
func (v *Validator) ValidateDate(attribute string, value any, parameters []string) bool {
	_, ok := parseDate("", stringOf(value))
	return ok
}

// ValidateDateFormat answers to validateDateFormat.
//
// The format is a GO layout -- date_format:2006-01-02, never date_format:Y-m-d.
// It is the one Laravel string that cannot be copied across, because the string
// means something else in the host language; compile.go refuses a PHP format at
// boot rather than accepting one that then rejects every date. Said here as ADR
// 0044 asks, because it is a change a reader has to know about.
func (v *Validator) ValidateDateFormat(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "date_format") {
		return false
	}
	s, ok := asString(value)
	if !ok {
		return false
	}
	t, err := time.Parse(parameters[0], s)
	// The round trip is the PHP's `$date->format($format) == $value`:
	// time.Parse accepts "2006-1-2" for the layout "2006-01-02", and a format
	// that accepts two spellings of one day is not a format.
	return err == nil && t.Format(parameters[0]) == s
}

// ValidateBefore answers to validateBefore.
func (v *Validator) ValidateBefore(attribute string, value any, parameters []string) bool {
	return v.CompareDates(attribute, value, parameters, "<")
}

// ValidateBeforeOrEqual answers to validateBeforeOrEqual.
func (v *Validator) ValidateBeforeOrEqual(attribute string, value any, parameters []string) bool {
	return v.CompareDates(attribute, value, parameters, "<=")
}

// ValidateAfter answers to validateAfter.
func (v *Validator) ValidateAfter(attribute string, value any, parameters []string) bool {
	return v.CompareDates(attribute, value, parameters, ">")
}

// ValidateAfterOrEqual answers to validateAfterOrEqual.
func (v *Validator) ValidateAfterOrEqual(attribute string, value any, parameters []string) bool {
	return v.CompareDates(attribute, value, parameters, ">=")
}

// ValidateDateEquals answers to validateDateEquals.
func (v *Validator) ValidateDateEquals(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "date_equals") {
		return false
	}
	return v.CompareDates(attribute, value, parameters, "=")
}

// CompareDates answers to compareDates: read both moments and answer what the
// operator says. A moment that cannot be read fails on either side -- a date
// rule that cannot read the date has not been passed.
//
// The other moment is a field this rule set declares, one of the three keywords
// today, tomorrow and yesterday, or a literal date. Laravel accepts anything
// strtotime does, which includes "next thursday" and "1 fortnight ago"; a rule
// whose accepted set nobody can enumerate cannot be reasoned about, so the
// keywords are the three and the rest is a date.
func (v *Validator) CompareDates(attribute string, value any, parameters []string, operator string) bool {
	if len(parameters) == 0 {
		return false
	}
	if !isStringOrNumber(value) {
		return false
	}
	mine, ok := parseDate(v.GetDateFormat(attribute), stringOf(value))
	if !ok {
		return false
	}
	theirs, ok := v.moment(attribute, parameters[0])
	if !ok {
		return false
	}
	return v.Compare(mine.Compare(theirs), 0, operator)
}

// GetDateFormat answers to getDateFormat: the layout the field's date_format
// declares, and empty when it declares none.
func (v *Validator) GetDateFormat(attribute string) string {
	if f, ok := v.set.byName[attribute]; ok {
		return f.layout
	}
	return ""
}

func (v *Validator) moment(attribute, argument string) (time.Time, bool) {
	if at, isKeyword := keywords[argument]; isKeyword {
		return at(v.Now()), true
	}
	if other, declared := v.set.byName[argument]; declared {
		return parseDate(other.layout, stringOf(v.GetValue(argument)))
	}
	return parseDate(v.GetDateFormat(attribute), argument)
}

// Compare answers to compare: the operator applied to the result of a
// comparison. The PHP takes the two values; Go has no operator on `mixed`, so
// it takes what comparing them said.
func (v *Validator) Compare(result, against int, operator string) bool {
	switch operator {
	case "<":
		return result < against
	case ">":
		return result > against
	case "<=":
		return result <= against
	case ">=":
		return result >= against
	case "=":
		return result == against
	}
	return false
}

// ValidateTimezone answers to validateTimezone.
func (v *Validator) ValidateTimezone(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	if !ok {
		return false
	}
	// "Local" is Go's spelling of "whatever this machine is set to", which is
	// not a zone a form can mean: it would pass on the developer's laptop and
	// on nothing else.
	if s == "Local" {
		return false
	}
	_, err := time.LoadLocation(s)
	return err == nil
}

// ---------------------------------------------------------------------------
// Identity and shape.
// ---------------------------------------------------------------------------

// ValidateEmail answers to validateEmail.
//
// The default is the PHP's RFC validation, done here as a shape check.
// Deliverability is proven by sending mail: `dns` asks whether the domain has a
// mail exchanger, which is as far as a lookup can go.
func (v *Validator) ValidateEmail(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	if !ok {
		return false
	}
	if !emailShape(s) {
		return false
	}
	for _, validation := range parameters {
		switch validation {
		case "rfc", "strict", "filter", "filter_unicode":
			// The shape check above is all four of them: this package has one
			// answer to "is this an address" and does not keep four that drift.
		case "dns":
			at := strings.LastIndexByte(s, '@')
			records, err := v.GetDNSRecords(s[at+1:])
			if err != nil || len(records) == 0 {
				return false
			}
		}
	}
	return true
}

// ValidateUrl answers to validateUrl. The parameters are the schemes allowed,
// and http and https are the default.
func (v *Validator) ValidateUrl(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	return ok && str.IsURL(s, parameters...)
}

// ValidateUuid answers to validateUuid.
func (v *Validator) ValidateUuid(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	return ok && str.IsUUID(s)
}

// ValidateUlid answers to validateUlid.
func (v *Validator) ValidateUlid(attribute string, value any, parameters []string) bool {
	s, ok := asString(value)
	return ok && str.IsULID(s)
}

// ValidateIp answers to validateIp.
func (v *Validator) ValidateIp(attribute string, value any, parameters []string) bool {
	_, ok := address(stringOf(value))
	return ok
}

// ValidateIpv4 answers to validateIpv4.
func (v *Validator) ValidateIpv4(attribute string, value any, parameters []string) bool {
	a, ok := address(stringOf(value))
	return ok && a.Is4()
}

// ValidateIpv6 answers to validateIpv6.
func (v *Validator) ValidateIpv6(attribute string, value any, parameters []string) bool {
	a, ok := address(stringOf(value))
	return ok && a.Is6()
}

// ValidateMacAddress answers to validateMacAddress.
func (v *Validator) ValidateMacAddress(attribute string, value any, parameters []string) bool {
	return macShape.MatchString(stringOf(value))
}

// ValidateHexColor answers to validateHexColor.
func (v *Validator) ValidateHexColor(attribute string, value any, parameters []string) bool {
	return hexColorShape.MatchString(stringOf(value))
}

// ValidateRegex answers to validateRegex.
//
// The pattern is a GO pattern: RE2, with no backreference and no lookaround,
// and without the delimiters PHP's preg_ functions need. compile.go compiles it
// at boot, so an unclosed group is a boot failure naming the field rather than
// a 500 on the first form somebody submits.
func (v *Validator) ValidateRegex(attribute string, value any, parameters []string) bool {
	if !isStringOrNumber(value) {
		return false
	}
	if !v.RequireParameterCount(1, parameters, "regex") {
		return false
	}
	re := v.pattern(parameters[0])
	return re != nil && re.MatchString(stringOf(value))
}

// ValidateNotRegex answers to validateNotRegex.
func (v *Validator) ValidateNotRegex(attribute string, value any, parameters []string) bool {
	if !isStringOrNumber(value) {
		return false
	}
	if !v.RequireParameterCount(1, parameters, "not_regex") {
		return false
	}
	re := v.pattern(parameters[0])
	return re != nil && !re.MatchString(stringOf(value))
}

// pattern is the boot-compiled pattern of the rule being run, so a request
// compiles nothing.
func (v *Validator) pattern(source string) *regexp.Regexp {
	if v.currentRule != nil && v.currentRule.re != nil {
		return v.currentRule.re
	}
	re, err := regexp.Compile(source)
	if err != nil {
		return nil
	}
	return re
}

// ValidateIn answers to validateIn.
//
// A value that is an array on a field that also declares `array` passes when
// every member is one of the parameters, which is how Laravel validates a
// multi-select in one rule.
func (v *Validator) ValidateIn(attribute string, value any, parameters []string) bool {
	if list, ok := asList(value); ok && v.HasRule(attribute, []string{"array"}) {
		for _, member := range list {
			if isArray(member) {
				return false
			}
			if !slices.Contains(parameters, stringOf(member)) {
				return false
			}
		}
		return true
	}
	if isArray(value) {
		return false
	}
	return slices.Contains(parameters, stringOf(value))
}

// ValidateNotIn answers to validateNotIn.
func (v *Validator) ValidateNotIn(attribute string, value any, parameters []string) bool {
	return !v.ValidateIn(attribute, value, parameters)
}

// ValidateEnum answers to Illuminate\Validation\Rules\Enum.
//
// PHP names a backed enum class and asks it tryFrom; Go has no enum type to
// ask, so the cases are the parameters -- enum:draft,published,archived -- and
// the Enum rule object builds that string from a typed list. The name is the
// one written in the rule string, which is what ADR 0044 asks of it.
func (v *Validator) ValidateEnum(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "enum") {
		return false
	}
	if value == nil || isArray(value) {
		return false
	}
	return slices.Contains(parameters, stringOf(value))
}

// ---------------------------------------------------------------------------
// Presence.
// ---------------------------------------------------------------------------

// ValidateRequired answers to validateRequired.
//
// Absent is one of four things, and the four are the whole rule: null, a string
// that is nothing but whitespace, an array with no members, and a file that was
// never written. A form sending three spaces has not answered the question.
func (v *Validator) ValidateRequired(attribute string, value any, parameters []string) bool {
	switch {
	case value == nil:
		return false
	case isString(value):
		return strings.TrimSpace(value.(string)) != ""
	}
	if n, ok := countOf(value); ok {
		return n >= 1
	}
	if f, ok := asFile(value); ok {
		return f.GetPath() != ""
	}
	return true
}

// ValidatePresent answers to validatePresent: the key was sent, whatever it
// holds.
func (v *Validator) ValidatePresent(attribute string, value any, parameters []string) bool {
	return v.data.Has(attribute)
}

// ValidateFilled answers to validateFilled: a key that was sent holds an
// answer. A key that was not sent passes, which is the whole difference from
// required.
func (v *Validator) ValidateFilled(attribute string, value any, parameters []string) bool {
	if v.data.Has(attribute) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateMissing answers to validateMissing.
func (v *Validator) ValidateMissing(attribute string, value any, parameters []string) bool {
	return !v.data.Has(attribute)
}

// ValidateProhibited answers to validateProhibited.
func (v *Validator) ValidateProhibited(attribute string, value any, parameters []string) bool {
	return !v.ValidateRequired(attribute, value, nil)
}

// ValidateRequiredIf answers to validateRequiredIf.
//
// A key the form did not send at all passes: a form that does not carry the
// other field cannot be answering it.
func (v *Validator) ValidateRequiredIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "required_if") {
		return false
	}
	if !v.data.Has(parameters[0]) {
		return true
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredUnless answers to validateRequiredUnless.
func (v *Validator) ValidateRequiredUnless(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "required_unless") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if !looseContains(values, other) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredIfAccepted answers to validateRequiredIfAccepted.
func (v *Validator) ValidateRequiredIfAccepted(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "required_if_accepted") {
		return false
	}
	if v.ValidateAccepted(parameters[0], v.GetValue(parameters[0]), nil) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredIfDeclined answers to validateRequiredIfDeclined.
func (v *Validator) ValidateRequiredIfDeclined(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "required_if_declined") {
		return false
	}
	if v.ValidateDeclined(parameters[0], v.GetValue(parameters[0]), nil) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredWith answers to validateRequiredWith: required when ANY of
// the named attributes is present.
func (v *Validator) ValidateRequiredWith(attribute string, value any, parameters []string) bool {
	if !v.AllFailingRequired(parameters) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredWithAll answers to validateRequiredWithAll: required when
// ALL of the named attributes are present.
func (v *Validator) ValidateRequiredWithAll(attribute string, value any, parameters []string) bool {
	if !v.AnyFailingRequired(parameters) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredWithout answers to validateRequiredWithout: required when ANY
// of the named attributes is missing.
func (v *Validator) ValidateRequiredWithout(attribute string, value any, parameters []string) bool {
	if v.AnyFailingRequired(parameters) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateRequiredWithoutAll answers to validateRequiredWithoutAll: required
// when ALL of the named attributes are missing.
func (v *Validator) ValidateRequiredWithoutAll(attribute string, value any, parameters []string) bool {
	if v.AllFailingRequired(parameters) {
		return v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// AnyFailingRequired answers to anyFailingRequired.
func (v *Validator) AnyFailingRequired(attributes []string) bool {
	for _, key := range attributes {
		if !v.ValidateRequired(key, v.GetValue(key), nil) {
			return true
		}
	}
	return false
}

// AllFailingRequired answers to allFailingRequired.
func (v *Validator) AllFailingRequired(attributes []string) bool {
	for _, key := range attributes {
		if v.ValidateRequired(key, v.GetValue(key), nil) {
			return false
		}
	}
	return true
}

// ValidatePresentIf answers to validatePresentIf.
func (v *Validator) ValidatePresentIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "present_if") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return v.ValidatePresent(attribute, value, nil)
	}
	return true
}

// ValidatePresentUnless answers to validatePresentUnless.
func (v *Validator) ValidatePresentUnless(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "present_unless") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if !looseContains(values, other) {
		return v.ValidatePresent(attribute, value, nil)
	}
	return true
}

// ValidatePresentWith answers to validatePresentWith.
func (v *Validator) ValidatePresentWith(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "present_with") {
		return false
	}
	if v.data.HasAny(parameters) {
		return v.ValidatePresent(attribute, value, nil)
	}
	return true
}

// ValidatePresentWithAll answers to validatePresentWithAll.
func (v *Validator) ValidatePresentWithAll(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "present_with_all") {
		return false
	}
	if v.data.HasAll(parameters) {
		return v.ValidatePresent(attribute, value, nil)
	}
	return true
}

// ValidateMissingIf answers to validateMissingIf.
func (v *Validator) ValidateMissingIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "missing_if") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return v.ValidateMissing(attribute, value, parameters)
	}
	return true
}

// ValidateMissingUnless answers to validateMissingUnless.
func (v *Validator) ValidateMissingUnless(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "missing_unless") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if !looseContains(values, other) {
		return v.ValidateMissing(attribute, value, parameters)
	}
	return true
}

// ValidateMissingWith answers to validateMissingWith.
func (v *Validator) ValidateMissingWith(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "missing_with") {
		return false
	}
	if v.data.HasAny(parameters) {
		return v.ValidateMissing(attribute, value, parameters)
	}
	return true
}

// ValidateMissingWithAll answers to validateMissingWithAll.
func (v *Validator) ValidateMissingWithAll(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "missing_with_all") {
		return false
	}
	if v.data.HasAll(parameters) {
		return v.ValidateMissing(attribute, value, parameters)
	}
	return true
}

// ValidateProhibitedIf answers to validateProhibitedIf.
func (v *Validator) ValidateProhibitedIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "prohibited_if") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if looseContains(values, other) {
		return !v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateProhibitedIfAccepted answers to validateProhibitedIfAccepted.
func (v *Validator) ValidateProhibitedIfAccepted(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "prohibited_if_accepted") {
		return false
	}
	if v.ValidateAccepted(parameters[0], v.GetValue(parameters[0]), nil) {
		return v.ValidateProhibited(attribute, value, nil)
	}
	return true
}

// ValidateProhibitedIfDeclined answers to validateProhibitedIfDeclined.
func (v *Validator) ValidateProhibitedIfDeclined(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "prohibited_if_declined") {
		return false
	}
	if v.ValidateDeclined(parameters[0], v.GetValue(parameters[0]), nil) {
		return v.ValidateProhibited(attribute, value, nil)
	}
	return true
}

// ValidateProhibitedUnless answers to validateProhibitedUnless.
func (v *Validator) ValidateProhibitedUnless(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "prohibited_unless") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	if !looseContains(values, other) {
		return !v.ValidateRequired(attribute, value, nil)
	}
	return true
}

// ValidateProhibits answers to validateProhibits: this attribute being present
// forbids the named ones.
func (v *Validator) ValidateProhibits(attribute string, value any, parameters []string) bool {
	if v.ValidateRequired(attribute, value, nil) {
		for _, parameter := range parameters {
			if v.ValidateRequired(parameter, v.GetValue(parameter), nil) {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Exclusion. These five never put a message on a field: their failure removes
// the field from the validated data instead. See Validator.AddFailure.
// ---------------------------------------------------------------------------

// ValidateExclude answers to validateExclude: always excluded.
func (v *Validator) ValidateExclude(attribute string, value any, parameters []string) bool {
	return false
}

// ValidateExcludeIf answers to validateExcludeIf.
func (v *Validator) ValidateExcludeIf(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "exclude_if") {
		return false
	}
	if !v.data.Has(parameters[0]) {
		return true
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	return !looseContains(values, other)
}

// ValidateExcludeUnless answers to validateExcludeUnless.
func (v *Validator) ValidateExcludeUnless(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(2, parameters, "exclude_unless") {
		return false
	}
	values, other := v.ParseDependentRuleParameters(parameters)
	return looseContains(values, other)
}

// ValidateExcludeWith answers to validateExcludeWith.
func (v *Validator) ValidateExcludeWith(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "exclude_with") {
		return false
	}
	return !v.data.Has(parameters[0])
}

// ValidateExcludeWithout answers to validateExcludeWithout.
func (v *Validator) ValidateExcludeWithout(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "exclude_without") {
		return false
	}
	return !v.AnyFailingRequired(parameters)
}

// ---------------------------------------------------------------------------
// Cross-field.
// ---------------------------------------------------------------------------

// ValidateSame answers to validateSame.
func (v *Validator) ValidateSame(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "same") {
		return false
	}
	return sameValue(value, v.GetValue(parameters[0]))
}

// ValidateDifferent answers to validateDifferent. An attribute the form did not
// send is not compared: a field that is not in the form is not the same as this
// one.
func (v *Validator) ValidateDifferent(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "different") {
		return false
	}
	for _, parameter := range parameters {
		if v.data.Has(parameter) && sameValue(value, v.GetValue(parameter)) {
			return false
		}
	}
	return true
}

// ValidateConfirmed answers to validateConfirmed: the value is repeated by
// <attribute>_confirmation, or by the attribute the parameter names.
func (v *Validator) ValidateConfirmed(attribute string, value any, parameters []string) bool {
	other := attribute + "_confirmation"
	if len(parameters) > 0 {
		other = parameters[0]
	}
	return v.ValidateSame(attribute, value, []string{other})
}

// ValidateCurrentPassword answers to validateCurrentPassword.
//
// Laravel resolves the guard and the hasher out of the container, which ADR
// 0002 refuses. The same question without one is CurrentPasswordChecker, given
// to the Validator with WithCurrentPassword; without one the rule fails
// closed, because a password check that passes for want of a checker is the
// worst outcome available.
func (v *Validator) ValidateCurrentPassword(attribute string, value any, parameters []string) bool {
	if v.passwords == nil {
		return false
	}
	guard := ""
	if len(parameters) > 0 {
		guard = parameters[0]
	}
	return v.passwords.CheckCurrentPassword(v.Context(), v.grant, guard, stringOf(value))
}

// ---------------------------------------------------------------------------
// Uploads.
// ---------------------------------------------------------------------------

// ValidateFile answers to validateFile.
func (v *Validator) ValidateFile(attribute string, value any, parameters []string) bool {
	return v.IsValidFileInstance(value)
}

// IsValidFileInstance answers to isValidFileInstance: a File, and an upload
// that finished.
func (v *Validator) IsValidFileInstance(value any) bool {
	if up, ok := value.(UploadedFile); ok && !up.IsValid() {
		return false
	}
	_, ok := asFile(value)
	return ok
}

// ValidateMimes answers to validateMimes: the extension GUESSED FROM THE
// CONTENT is one of the given ones, which is why a .png renamed to .jpg does
// not pass. jpg and jpeg are each other, as the PHP makes them.
func (v *Validator) ValidateMimes(attribute string, value any, parameters []string) bool {
	if !v.IsValidFileInstance(value) {
		return false
	}
	if v.ShouldBlockPhpUpload(value, parameters) {
		return false
	}
	allowed := parameters
	if slices.Contains(parameters, "jpg") || slices.Contains(parameters, "jpeg") {
		allowed = append(slices.Clone(parameters), "jpg", "jpeg")
	}
	f, _ := asFile(value)
	return f.GetPath() != "" && slices.Contains(allowed, f.GuessExtension())
}

// ValidateMimetypes answers to validateMimetypes. A parameter of the form
// "image/*" matches every type in the group, as the PHP's does.
func (v *Validator) ValidateMimetypes(attribute string, value any, parameters []string) bool {
	if !v.IsValidFileInstance(value) {
		return false
	}
	if v.ShouldBlockPhpUpload(value, parameters) {
		return false
	}
	f, _ := asFile(value)
	mime := f.GetMimeType()
	group, _, _ := strings.Cut(mime, "/")
	return f.GetPath() != "" &&
		(slices.Contains(parameters, mime) || slices.Contains(parameters, group+"/*"))
}

// ValidateExtensions answers to validateExtensions: the extension THE CLIENT
// SENT is one of the given ones. It is the counterpart of mimes, which asks the
// content instead.
func (v *Validator) ValidateExtensions(attribute string, value any, parameters []string) bool {
	if !v.IsValidFileInstance(value) {
		return false
	}
	if v.ShouldBlockPhpUpload(value, parameters) {
		return false
	}
	return slices.Contains(parameters, strings.ToLower(clientExtension(value)))
}

// ValidateImage answers to validateImage: mimes over the six image types, plus
// svg when the parameters allow it.
func (v *Validator) ValidateImage(attribute string, value any, parameters []string) bool {
	mimes := []string{"jpg", "jpeg", "png", "gif", "bmp", "webp"}
	if slices.Contains(parameters, "allow_svg") {
		mimes = append(mimes, "svg")
	}
	return v.ValidateMimes(attribute, value, mimes)
}

// ShouldBlockPhpUpload answers to shouldBlockPhpUpload: an upload whose name
// ends in a PHP extension is refused unless the rule asked for php by name.
func (v *Validator) ShouldBlockPhpUpload(value any, parameters []string) bool {
	if slices.Contains(parameters, "php") {
		return false
	}
	return slices.Contains(phpExtensions, strings.TrimSpace(strings.ToLower(clientExtension(value))))
}

// clientExtension is the name the upload came in with for an UploadedFile, and
// the file's own extension for anything else -- the fork shouldBlockPhpUpload
// makes.
func clientExtension(value any) string {
	if up, ok := value.(UploadedFile); ok {
		return up.GetClientOriginalExtension()
	}
	if f, ok := asFile(value); ok {
		return f.GetExtension()
	}
	return ""
}

// ValidateDimensions answers to validateDimensions.
//
// An SVG passes without being measured, as the PHP's does: it has no pixels to
// count. Everything else has its bytes read, which is a decode of input a
// stranger chose -- only the header is read, never the pixels.
func (v *Validator) ValidateDimensions(attribute string, value any, parameters []string) bool {
	if v.IsValidFileInstance(value) {
		if f, _ := asFile(value); f.GetMimeType() == "image/svg+xml" || f.GetMimeType() == "image/svg" {
			return true
		}
	}
	if !v.IsValidFileInstance(value) {
		return false
	}
	f, _ := asFile(value)
	width, height, ok := imageDimensions(f)
	if !ok {
		return false
	}
	if !v.RequireParameterCount(1, parameters, "dimensions") {
		return false
	}
	named := v.ParseNamedParameters(parameters)
	return !(v.FailsBasicDimensionChecks(named, width, height) ||
		v.FailsRatioCheck(named, width, height) ||
		v.FailsMinRatioCheck(named, width, height) ||
		v.FailsMaxRatioCheck(named, width, height))
}

// FailsBasicDimensionChecks answers to failsBasicDimensionChecks.
func (v *Validator) FailsBasicDimensionChecks(parameters map[string]string, width, height int) bool {
	fails := func(key string, against int, worse func(a, b int) bool) bool {
		raw, given := parameters[key]
		if !given {
			return false
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return true
		}
		return worse(n, against)
	}
	notEqual := func(a, b int) bool { return a != b }
	above := func(a, b int) bool { return a > b }
	below := func(a, b int) bool { return a < b }

	return fails("width", width, notEqual) ||
		fails("min_width", width, above) ||
		fails("max_width", width, below) ||
		fails("height", height, notEqual) ||
		fails("min_height", height, above) ||
		fails("max_height", height, below)
}

// FailsRatioCheck answers to failsRatioCheck, precision included: the tolerance
// widens with the image, so that a 3/2 photograph of 1201 by 800 is still 3/2.
func (v *Validator) FailsRatioCheck(parameters map[string]string, width, height int) bool {
	raw, given := parameters["ratio"]
	if !given {
		return false
	}
	want, ok := parseRatio(raw)
	if !ok || height == 0 {
		return true
	}
	precision := 1 / (max(float64(width+height)/2, float64(height)) + 1)
	return math.Abs(want-float64(width)/float64(height)) > precision
}

// FailsMinRatioCheck answers to failsMinRatioCheck.
func (v *Validator) FailsMinRatioCheck(parameters map[string]string, width, height int) bool {
	raw, given := parameters["min_ratio"]
	if !given {
		return false
	}
	want, ok := parseRatio(raw)
	if !ok || height == 0 {
		return true
	}
	return float64(width)/float64(height) > want
}

// FailsMaxRatioCheck answers to failsMaxRatioCheck.
func (v *Validator) FailsMaxRatioCheck(parameters map[string]string, width, height int) bool {
	raw, given := parameters["max_ratio"]
	if !given {
		return false
	}
	want, ok := parseRatio(raw)
	if !ok || height == 0 {
		return true
	}
	return float64(width)/float64(height) < want
}

// parseRatio reads "3/2" and "1.5", which are the two spellings the PHP's
// sscanf('%f/%d') accepts.
func parseRatio(raw string) (float64, bool) {
	numerator, denominator, divided := strings.Cut(raw, "/")
	n, err := strconv.ParseFloat(strings.TrimSpace(numerator), 64)
	if err != nil {
		return 0, false
	}
	if !divided {
		return n, true
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(denominator), 64)
	if err != nil || d == 0 {
		return 0, false
	}
	return n / d, true
}

// ParseNamedParameters answers to parseNamedParameters: "min_width=100" becomes
// one entry of a map.
func (v *Validator) ParseNamedParameters(parameters []string) map[string]string {
	named := make(map[string]string, len(parameters))
	for _, item := range parameters {
		key, value, _ := strings.Cut(item, "=")
		named[key] = value
	}
	return named
}

// ---------------------------------------------------------------------------
// The database. RULE 17 holds on a read: both rules take the Grant and the
// query is filtered by its tenant.
//
// The Grant is auth.Grant, which is a struct and cannot be compared to nil, so
// what both rules check is the tenant on it. That is the stronger check and the
// one that was meant: a Grant carrying no tenant cannot scope the count, and a
// count that is not scoped answers whether SOMEBODY holds the value.
// ---------------------------------------------------------------------------

// ValidateExists answers to validateExists.
//
// Laravel resolves the presence verifier out of the container and queries with
// no notion of who is asking. Here the verifier arrives with the request,
// through WithPresence, together with the Grant -- because a rule that reads a
// table to answer "does this exist" is a read, and RULE 17 does not except
// reads. Without a verifier the rule fails closed and says so.
func (v *Validator) ValidateExists(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "exists") {
		return false
	}
	if v.presence == nil || auth.Tenant(v.grant) == "" {
		return false
	}
	_, table, _ := v.ParseTable(parameters[0])
	column := v.GetQueryColumn(parameters, attribute)
	extra := v.GetExtraConditions(sliceFrom(parameters, 2))

	if list, ok := asList(value); ok {
		unique := uniqueValues(list)
		if len(unique) == 0 {
			return true
		}
		count, err := v.presence.GetMultiCount(v.Context(), v.grant, table, column, unique, extra)
		return err == nil && count >= len(unique)
	}
	count, err := v.presence.GetCount(v.Context(), v.grant, table, column, value, nil, "", extra)
	return err == nil && count >= 1
}

// ValidateUnique answers to validateUnique.
//
// The parameters are the PHP's, in the PHP's order:
// unique:table,column,exceptID,idColumn,extraColumn,extraValue...
func (v *Validator) ValidateUnique(attribute string, value any, parameters []string) bool {
	if !v.RequireParameterCount(1, parameters, "unique") {
		return false
	}
	if v.presence == nil || auth.Tenant(v.grant) == "" {
		return false
	}
	_, table, idColumn := v.ParseTable(parameters[0])
	column := v.GetQueryColumn(parameters, attribute)

	var id any
	if len(parameters) > 2 {
		idColumn, id = v.GetUniqueIds(idColumn, parameters)
	}
	count, err := v.presence.GetCount(v.Context(), v.grant, table, column, value, id, idColumn, v.GetUniqueExtra(parameters))
	return err == nil && count == 0
}

// GetUniqueIds answers to getUniqueIds: the column and the value of the row the
// rule must ignore, which is the row being edited.
func (v *Validator) GetUniqueIds(idColumn string, parameters []string) (string, any) {
	if idColumn == "" {
		idColumn = "id"
		if len(parameters) > 3 && parameters[3] != "" {
			idColumn = parameters[3]
		}
	}
	return idColumn, v.PrepareUniqueId(parameters[2])
}

// PrepareUniqueId answers to prepareUniqueId. "[field]" reads the id out of the
// submitted data, "null" is null, and a whole number is a number.
func (v *Validator) PrepareUniqueId(id string) any {
	if match := bracketed.FindStringSubmatch(id); match != nil {
		return v.GetValue(match[1])
	}
	if strings.EqualFold(id, "null") {
		return nil
	}
	if n, ok := whole(id); ok {
		return n
	}
	return id
}

var bracketed = regexp.MustCompile(`\[(.*)\]`)

// GetUniqueExtra answers to getUniqueExtra.
func (v *Validator) GetUniqueExtra(parameters []string) map[string]string {
	if len(parameters) > 4 {
		return v.GetExtraConditions(parameters[4:])
	}
	return nil
}

// GetExtraConditions answers to getExtraConditions: the trailing parameters
// read in pairs, column then value.
func (v *Validator) GetExtraConditions(segments []string) map[string]string {
	extra := make(map[string]string, len(segments)/2)
	for i := 0; i+1 < len(segments); i += 2 {
		extra[segments[i]] = segments[i+1]
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// ParseTable answers to parseTable: "connection.table" splits, and a bare name
// is the table. The model branch of the PHP is not here -- there are no models
// (ADR: no Active Record), so a rule names the table.
func (v *Validator) ParseTable(table string) (connection, name, idColumn string) {
	if before, after, found := strings.Cut(table, "."); found {
		return before, after, ""
	}
	return "", table, ""
}

// GetQueryColumn answers to getQueryColumn.
func (v *Validator) GetQueryColumn(parameters []string, attribute string) string {
	if len(parameters) > 1 && parameters[1] != "" && parameters[1] != "NULL" {
		return parameters[1]
	}
	return v.GuessColumnForQuery(attribute)
}

// GuessColumnForQuery answers to guessColumnForQuery: the last segment of a
// dotted attribute, and the attribute itself otherwise.
func (v *Validator) GuessColumnForQuery(attribute string) string {
	if i := strings.LastIndexByte(attribute, '.'); i >= 0 {
		last := attribute[i+1:]
		if _, isNumber := whole(last); !isNumber {
			return last
		}
	}
	return attribute
}

func sliceFrom(list []string, at int) []string {
	if at >= len(list) {
		return nil
	}
	return list[at:]
}

func uniqueValues(list []any) []any {
	seen := make(map[string]struct{}, len(list))
	out := make([]any, 0, len(list))
	for _, item := range list {
		key := stringOf(item)
		if _, repeated := seen[key]; repeated {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

// ---------------------------------------------------------------------------
// The shared helpers of the trait.
// ---------------------------------------------------------------------------

// ParseDependentRuleParameters answers to parseDependentRuleParameters: the
// values a dependent rule compares against, and the other attribute's value.
//
// The PHP converts "true"/"false" to booleans and "null" to null here, before
// comparing; the conversion lives in the comparison instead, so that one place
// decides what "the other field says yes" means.
func (v *Validator) ParseDependentRuleParameters(parameters []string) ([]string, any) {
	if len(parameters) == 0 {
		return nil, nil
	}
	return parameters[1:], v.GetValue(parameters[0])
}

// RequireParameterCount answers to requireParameterCount.
//
// The PHP throws; here it reports, and the rule that asked returns false. The
// count is already proven at boot by compile.go, so this is the second lock on
// a door the first one closed -- and a rule invoked directly, outside a
// compiled set, still cannot read past the end of its parameters.
func (v *Validator) RequireParameterCount(count int, parameters []string, rule string) bool {
	return len(parameters) >= count
}

// isStringOrNumber is the guard several PHP bodies open with:
// `! is_string($value) && ! is_numeric($value)`.
func isStringOrNumber(value any) bool {
	if _, ok := asString(value); ok {
		return true
	}
	_, ok := numberOf(value)
	return ok
}

func isString(value any) bool {
	_, ok := asString(value)
	return ok
}

// sameType answers to isSameType: gettype of one equals gettype of the other.
func sameType(first, second any) bool { return phpType(first) == phpType(second) }

func phpType(value any) string {
	switch value.(type) {
	case nil:
		return "NULL"
	case bool:
		return "boolean"
	case int, int64:
		return "integer"
	case float32, float64:
		return "double"
	case string:
		return "string"
	case []any, Data, map[string]any:
		return "array"
	}
	return "object"
}

// Context is the context every rule that leaves the process runs under: the
// request's, so a DNS lookup or a count query carries the request's deadline.
func (v *Validator) Context() context.Context {
	if v.ctx == nil {
		return context.Background()
	}
	return v.ctx
}

// Now is the clock the relative date keywords read. It is settable so that a
// test of `after:today` does not have to be run before midnight.
func (v *Validator) Now() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

func (v *Validator) resolver() Resolver {
	if v.dns != nil {
		return v.dns
	}
	return defaultResolver
}
