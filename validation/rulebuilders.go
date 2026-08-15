package validation

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/arandu-io/hesape/auth"
)

// This file answers to Illuminate\Validation\Rules and to
// Illuminate\Validation\Rule.
//
// Rule is a class of static methods in the PHP because PHP has nowhere else to
// put a function; each of them is a constructor here, so Rule::in([...]) is
// NewIn(...) and Rule::password() is PasswordDefault().
//
// Most of these build the same rule string somebody could have typed, which is
// what Stringable means there and String means here:
//
//	validation.MustCompile(validation.Rules{
//		"role":  rules.In("admin", "editor").String(),
//		"price": NewNumeric().Min(0).Max(1000).String(),
//	})
//
// Two are spelled with the suffix Laravel itself puts on Rules\ArrayRule, and
// for the same reason -- the plain name is already taken: Rules\File is FileRule
// because File is the upload interface, and Rules\Email is EmailRule because
// Email is the one-value helper.

// ---------------------------------------------------------------------------
// Rules\In and Rules\NotIn.
// ---------------------------------------------------------------------------

// In builds the "in" rule -- the value must be one of a fixed list -- without
// the caller spelling the rule string by hand. String renders every value
// quoted, so one containing a comma survives the parameter list instead of
// splitting into two allowed values. Build one with NewIn.
//
// Answers Illuminate\Validation\Rules\In.
type In struct {
	rule   string
	values []string
}

// NewIn answers to the In constructor and to Rule::in.
func NewIn(values ...string) *In { return &In{rule: "in", values: values} }

// String answers to In::__toString.
func (r *In) String() string { return r.rule + ":" + quotedList(r.values) }

// NotIn builds the "not_in" rule -- the value must be none of a fixed list --
// with the same quoting In uses, so a listed value containing a comma is still
// one value. Build one with NewNotIn.
//
// Answers Illuminate\Validation\Rules\NotIn.
type NotIn struct {
	rule   string
	values []string
}

// NewNotIn answers to the NotIn constructor and to Rule::notIn.
func NewNotIn(values ...string) *NotIn { return &NotIn{rule: "not_in", values: values} }

// String answers to NotIn::__toString.
func (r *NotIn) String() string { return r.rule + ":" + quotedList(r.values) }

// quotedList is the escaping both do: every value quoted, and a quote inside one
// doubled, so that a value containing a comma survives the parameter list.
func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ",")
}

// ---------------------------------------------------------------------------
// Rules\ArrayRule.
// ---------------------------------------------------------------------------

// ArrayRule builds the "array" rule: the value must be an array, and when keys
// are given it may hold no key outside that list, which is how a nested object
// arriving from a form is kept to the shape it was meant to have. With no keys
// it renders the bare "array". The name carries the suffix here for the reason
// it carries one in the PHP -- "array" is a reserved word there, and the plain
// name is not available.
//
// Answers Illuminate\Validation\Rules\ArrayRule.
type ArrayRule struct{ keys []string }

// NewArrayRule answers to the ArrayRule constructor and to Rule::array.
func NewArrayRule(keys ...string) *ArrayRule { return &ArrayRule{keys: keys} }

// String answers to ArrayRule::__toString.
func (r *ArrayRule) String() string {
	if len(r.keys) == 0 {
		return "array"
	}
	return "array:" + strings.Join(r.keys, ",")
}

// ---------------------------------------------------------------------------
// Rules\ExcludeIf, Rules\ProhibitedIf and Rules\RequiredIf.
// ---------------------------------------------------------------------------

// ExcludeIf renders the "exclude" rule when its condition holds and an empty
// string when it does not, so a field can be dropped from a rule set without
// branching around the set itself. An excluded attribute is removed from the
// validated data rather than reported: it is not part of this request, which is
// a different thing from being wrong, and no message is put on it. Build one
// with NewExcludeIf.
//
// Answers Illuminate\Validation\Rules\ExcludeIf.
type ExcludeIf struct{ Condition bool }

// NewExcludeIf answers to the ExcludeIf constructor and to Rule::excludeIf.
//
// The PHP takes a Closure or a bool and refuses anything else with an
// InvalidArgumentException; a Go signature refuses it at compile time, and a
// caller with a closure calls it.
func NewExcludeIf(condition bool) *ExcludeIf { return &ExcludeIf{Condition: condition} }

// String answers to ExcludeIf::__toString: the rule when the condition holds,
// and nothing at all when it does not.
func (r *ExcludeIf) String() string {
	if r.Condition {
		return "exclude"
	}
	return ""
}

// ProhibitedIf renders the "prohibited" rule when its condition holds and an
// empty string when it does not. A prohibited attribute fails whenever it
// arrived with a value at all, which refuses a field that must not accompany
// this request instead of quietly ignoring it. Build one with NewProhibitedIf.
//
// Answers Illuminate\Validation\Rules\ProhibitedIf.
type ProhibitedIf struct{ Condition bool }

// NewProhibitedIf answers to the ProhibitedIf constructor and to
// Rule::prohibitedIf.
func NewProhibitedIf(condition bool) *ProhibitedIf { return &ProhibitedIf{Condition: condition} }

// String answers to ProhibitedIf::__toString.
func (r *ProhibitedIf) String() string {
	if r.Condition {
		return "prohibited"
	}
	return ""
}

// RequiredIf renders the "required" rule when its condition holds and an empty
// string when it does not, so a field is demanded only in the case the caller
// decides. The condition is a bool settled before the rule set is built; a
// condition that has to look at the request is required_if, which is a rule name
// and takes the other field as its parameter. Build one with NewRequiredIf.
//
// Answers Illuminate\Validation\Rules\RequiredIf.
type RequiredIf struct{ Condition bool }

// NewRequiredIf answers to the RequiredIf constructor and to Rule::requiredIf.
func NewRequiredIf(condition bool) *RequiredIf { return &RequiredIf{Condition: condition} }

// String answers to RequiredIf::__toString.
func (r *RequiredIf) String() string {
	if r.Condition {
		return "required"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Rules\Date.
// ---------------------------------------------------------------------------

// Date answers to Illuminate\Validation\Rules\Date: the date rules of one field,
// written without remembering their names.
type Date struct {
	format      string
	constraints []string
}

// NewDate answers to the Date constructor and to Rule::date.
func NewDate() *Date { return &Date{} }

// Format answers to Date::format. The layout is a GO layout --
// Format("2006-01-02"), never "Y-m-d" -- for the reason date_format takes one.
func (r *Date) Format(format string) *Date {
	r.format = format

	return r
}

// BeforeToday answers to Date::beforeToday.
func (r *Date) BeforeToday() *Date { return r.Before("today") }

// AfterToday answers to Date::afterToday.
func (r *Date) AfterToday() *Date { return r.After("today") }

// TodayOrBefore answers to Date::todayOrBefore.
func (r *Date) TodayOrBefore() *Date { return r.BeforeOrEqual("today") }

// TodayOrAfter answers to Date::todayOrAfter.
func (r *Date) TodayOrAfter() *Date { return r.AfterOrEqual("today") }

// Before answers to Date::before.
func (r *Date) Before(date string) *Date { return r.addRule("before:" + date) }

// After answers to Date::after.
func (r *Date) After(date string) *Date { return r.addRule("after:" + date) }

// BeforeOrEqual answers to Date::beforeOrEqual.
func (r *Date) BeforeOrEqual(date string) *Date { return r.addRule("before_or_equal:" + date) }

// AfterOrEqual answers to Date::afterOrEqual.
func (r *Date) AfterOrEqual(date string) *Date { return r.addRule("after_or_equal:" + date) }

// Between answers to Date::between.
func (r *Date) Between(from, to string) *Date { return r.After(from).Before(to) }

// BetweenOrEqual answers to Date::betweenOrEqual.
func (r *Date) BetweenOrEqual(from, to string) *Date {
	return r.AfterOrEqual(from).BeforeOrEqual(to)
}

func (r *Date) addRule(rules string) *Date {
	r.constraints = append(r.constraints, rules)

	return r
}

// String answers to Date::__toString.
func (r *Date) String() string {
	head := "date"
	if r.format != "" {
		head = "date_format:" + r.format
	}
	return strings.Join(append([]string{head}, r.constraints...), "|")
}

// ---------------------------------------------------------------------------
// Rules\Numeric.
// ---------------------------------------------------------------------------

// Numeric builds a whole chain of numeric rules for one field -- bounds, digit
// counts, decimal places, comparisons against another field -- without the
// caller remembering any of their names. It always begins with "numeric", and
// String renders the chain with repeats dropped, because several of the methods
// add "integer" of their own. Build one with NewNumeric.
//
// Answers Illuminate\Validation\Rules\Numeric.
type Numeric struct{ constraints []string }

// NewNumeric answers to the Numeric constructor and to Rule::numeric.
func NewNumeric() *Numeric { return &Numeric{constraints: []string{"numeric"}} }

// Between answers to Numeric::between.
func (r *Numeric) Between(min, max float64) *Numeric {
	return r.addRule("between:" + number64(min) + "," + number64(max))
}

// Decimal answers to Numeric::decimal.
func (r *Numeric) Decimal(min int, max ...int) *Numeric {
	rule := "decimal:" + strconv.Itoa(min)
	if len(max) > 0 {
		rule += "," + strconv.Itoa(max[0])
	}
	return r.addRule(rule)
}

// Different answers to Numeric::different.
func (r *Numeric) Different(field string) *Numeric { return r.addRule("different:" + field) }

// Digits answers to Numeric::digits.
func (r *Numeric) Digits(length int) *Numeric {
	return r.Integer().addRule("digits:" + strconv.Itoa(length))
}

// DigitsBetween answers to Numeric::digitsBetween.
func (r *Numeric) DigitsBetween(min, max int) *Numeric {
	return r.Integer().addRule("digits_between:" + strconv.Itoa(min) + "," + strconv.Itoa(max))
}

// GreaterThan answers to Numeric::greaterThan.
func (r *Numeric) GreaterThan(field string) *Numeric { return r.addRule("gt:" + field) }

// GreaterThanOrEqualTo answers to Numeric::greaterThanOrEqualTo.
func (r *Numeric) GreaterThanOrEqualTo(field string) *Numeric { return r.addRule("gte:" + field) }

// Integer answers to Numeric::integer.
func (r *Numeric) Integer() *Numeric { return r.addRule("integer") }

// LessThan answers to Numeric::lessThan.
func (r *Numeric) LessThan(field string) *Numeric { return r.addRule("lt:" + field) }

// LessThanOrEqualTo answers to Numeric::lessThanOrEqualTo.
func (r *Numeric) LessThanOrEqualTo(field string) *Numeric { return r.addRule("lte:" + field) }

// Max answers to Numeric::max.
func (r *Numeric) Max(value float64) *Numeric { return r.addRule("max:" + number64(value)) }

// MaxDigits answers to Numeric::maxDigits.
func (r *Numeric) MaxDigits(value int) *Numeric {
	return r.addRule("max_digits:" + strconv.Itoa(value))
}

// Min answers to Numeric::min.
func (r *Numeric) Min(value float64) *Numeric { return r.addRule("min:" + number64(value)) }

// MinDigits answers to Numeric::minDigits.
func (r *Numeric) MinDigits(value int) *Numeric {
	return r.addRule("min_digits:" + strconv.Itoa(value))
}

// MultipleOf answers to Numeric::multipleOf.
func (r *Numeric) MultipleOf(value float64) *Numeric {
	return r.addRule("multiple_of:" + number64(value))
}

// Same answers to Numeric::same.
func (r *Numeric) Same(field string) *Numeric { return r.addRule("same:" + field) }

// Exactly answers to Numeric::exactly.
func (r *Numeric) Exactly(value int) *Numeric {
	return r.Integer().addRule("size:" + strconv.Itoa(value))
}

func (r *Numeric) addRule(rules string) *Numeric {
	r.constraints = append(r.constraints, rules)

	return r
}

// String answers to Numeric::__toString, with the repeats dropped as the PHP's
// array_unique drops them -- Digits and Exactly both add `integer`.
func (r *Numeric) String() string {
	seen := make(map[string]struct{}, len(r.constraints))
	out := make([]string, 0, len(r.constraints))
	for _, constraint := range r.constraints {
		if _, repeated := seen[constraint]; repeated {
			continue
		}
		seen[constraint] = struct{}{}
		out = append(out, constraint)
	}
	return strings.Join(out, "|")
}

// number64 renders a bound without a trailing zero, as PHP renders one.
func number64(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// ---------------------------------------------------------------------------
// Rules\Dimensions.
// ---------------------------------------------------------------------------

// Dimensions builds the "dimensions" rule: the pixel width and height an
// uploaded image must have or stay within, and the aspect ratio it must match.
// Each method sets one constraint, and String renders them all as a single rule
// in a fixed order rather than the order they were set, since a Go map remembers
// none. Build one with NewDimensions.
//
// Answers Illuminate\Validation\Rules\Dimensions.
type Dimensions struct{ constraints map[string]string }

// NewDimensions answers to the Dimensions constructor and to Rule::dimensions.
func NewDimensions() *Dimensions { return &Dimensions{constraints: map[string]string{}} }

// Width answers to Dimensions::width.
func (r *Dimensions) Width(value int) *Dimensions { return r.set("width", value) }

// Height answers to Dimensions::height.
func (r *Dimensions) Height(value int) *Dimensions { return r.set("height", value) }

// MinWidth answers to Dimensions::minWidth.
func (r *Dimensions) MinWidth(value int) *Dimensions { return r.set("min_width", value) }

// MinHeight answers to Dimensions::minHeight.
func (r *Dimensions) MinHeight(value int) *Dimensions { return r.set("min_height", value) }

// MaxWidth answers to Dimensions::maxWidth.
func (r *Dimensions) MaxWidth(value int) *Dimensions { return r.set("max_width", value) }

// MaxHeight answers to Dimensions::maxHeight.
func (r *Dimensions) MaxHeight(value int) *Dimensions { return r.set("max_height", value) }

// Ratio answers to Dimensions::ratio.
func (r *Dimensions) Ratio(value float64) *Dimensions { return r.setRatio("ratio", value) }

// MinRatio answers to Dimensions::minRatio.
func (r *Dimensions) MinRatio(value float64) *Dimensions { return r.setRatio("min_ratio", value) }

// MaxRatio answers to Dimensions::maxRatio.
func (r *Dimensions) MaxRatio(value float64) *Dimensions { return r.setRatio("max_ratio", value) }

// RatioBetween answers to Dimensions::ratioBetween.
func (r *Dimensions) RatioBetween(min, max float64) *Dimensions {
	return r.setRatio("min_ratio", min).setRatio("max_ratio", max)
}

func (r *Dimensions) set(key string, value int) *Dimensions {
	r.constraints[key] = strconv.Itoa(value)

	return r
}

func (r *Dimensions) setRatio(key string, value float64) *Dimensions {
	r.constraints[key] = number64(value)

	return r
}

// dimensionOrder is the order the constraints are written in. A PHP array
// remembers the order they were set in and a Go map remembers none, so the
// spelling is fixed here rather than left to the map.
var dimensionOrder = []string{
	"width", "height", "min_width", "min_height",
	"max_width", "max_height", "ratio", "min_ratio", "max_ratio",
}

// String answers to Dimensions::__toString.
func (r *Dimensions) String() string {
	pairs := make([]string, 0, len(r.constraints))
	for _, key := range dimensionOrder {
		if value, set := r.constraints[key]; set {
			pairs = append(pairs, key+"="+value)
		}
	}
	return "dimensions:" + strings.Join(pairs, ",")
}

// ---------------------------------------------------------------------------
// Rules\File and Rules\ImageFile.
// ---------------------------------------------------------------------------

// FileRule answers to Illuminate\Validation\Rules\File. It carries the suffix
// because File is already the upload interface here, which is what
// Symfony\Component\HttpFoundation\File\File is there.
type FileRule struct {
	allowedMimetypes  []string
	allowedExtensions []string
	minimumFileSize   *int
	maximumFileSize   *int
	customRules       []string
	image             bool
	allowSvg          bool
}

// NewFileRule answers to the File constructor and to Rule::file.
func NewFileRule() *FileRule { return &FileRule{} }

// NewImageFile answers to the ImageFile constructor, to Rule::imageFile and to
// the static File::image.
func NewImageFile(allowSvg ...bool) *FileRule {
	r := &FileRule{image: true}
	if len(allowSvg) > 0 {
		r.allowSvg = allowSvg[0]
	}
	return r
}

// Types answers to the static File::types: the MIME types or extensions the
// upload may be.
func Types(mimetypes ...string) *FileRule {
	return &FileRule{allowedMimetypes: mimetypes}
}

// Extensions answers to File::extensions: the extension the BROWSER sent, which
// is not the same question Mimes asks.
func (r *FileRule) Extensions(extensions ...string) *FileRule {
	r.allowedExtensions = extensions

	return r
}

// Size answers to File::size: exactly this many kilobytes.
func (r *FileRule) Size(size int) *FileRule {
	r.minimumFileSize, r.maximumFileSize = &size, &size

	return r
}

// Between answers to File::between.
func (r *FileRule) Between(minSize, maxSize int) *FileRule {
	r.minimumFileSize, r.maximumFileSize = &minSize, &maxSize

	return r
}

// Min answers to File::min.
func (r *FileRule) Min(size int) *FileRule {
	r.minimumFileSize = &size

	return r
}

// Max answers to File::max.
func (r *FileRule) Max(size int) *FileRule {
	r.maximumFileSize = &size

	return r
}

// Dimensions answers to ImageFile::dimensions.
func (r *FileRule) Dimensions(dimensions *Dimensions) *FileRule {
	return r.Rules(dimensions.String())
}

// Rules answers to File::rules: more rules merged into the ones this builds.
func (r *FileRule) Rules(rules ...string) *FileRule {
	r.customRules = append(r.customRules, rules...)

	return r
}

// ToKilobytes answers to File::toKilobytes: "2mb" is 2000 kilobytes, as the PHP
// counts them -- a thousand, not 1024.
//
// The PHP throws on a suffix it does not know; the second value is how Go spells
// that, and an unsuffixed number is refused there too.
func ToKilobytes(size string) (int, bool) {
	size = strings.ToLower(strings.TrimSpace(size))

	for suffix, multiplier := range map[string]float64{
		"kb": 1, "mb": 1_000, "gb": 1_000_000, "tb": 1_000_000_000,
	} {
		rest, matched := strings.CutSuffix(size, suffix)
		if !matched {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			return 0, false
		}
		return int(value*multiplier + 0.5), true
	}
	return 0, false
}

// String answers to File::buildValidationRules, rendered as the chain it is.
func (r *FileRule) String() string {
	head := "file"
	if r.image {
		head = "image"
		if r.allowSvg {
			head = "image:allow_svg"
		}
	}
	rules := []string{head}

	rules = append(rules, r.buildMimetypes()...)

	if len(r.allowedExtensions) > 0 {
		lowered := make([]string, len(r.allowedExtensions))
		for i, extension := range r.allowedExtensions {
			lowered[i] = strings.ToLower(extension)
		}
		rules = append(rules, "extensions:"+strings.Join(lowered, ","))
	}

	switch {
	case r.minimumFileSize == nil && r.maximumFileSize == nil:
	case r.maximumFileSize == nil:
		rules = append(rules, "min:"+strconv.Itoa(*r.minimumFileSize))
	case r.minimumFileSize == nil:
		rules = append(rules, "max:"+strconv.Itoa(*r.maximumFileSize))
	case *r.minimumFileSize != *r.maximumFileSize:
		rules = append(rules, "between:"+strconv.Itoa(*r.minimumFileSize)+","+strconv.Itoa(*r.maximumFileSize))
	default:
		rules = append(rules, "size:"+strconv.Itoa(*r.minimumFileSize))
	}

	return strings.Join(append(rules, r.customRules...), "|")
}

// buildMimetypes answers to File::buildMimetypes: a type with a slash is a MIME
// type and everything else is an extension, so the two become two rules.
func (r *FileRule) buildMimetypes() []string {
	if len(r.allowedMimetypes) == 0 {
		return nil
	}

	var mimetypes, mimes []string
	for _, allowed := range r.allowedMimetypes {
		if strings.Contains(allowed, "/") {
			mimetypes = append(mimetypes, allowed)
			continue
		}
		mimes = append(mimes, allowed)
	}

	var rules []string
	if len(mimetypes) > 0 {
		rules = append(rules, "mimetypes:"+strings.Join(mimetypes, ","))
	}
	if len(mimes) > 0 {
		rules = append(rules, "mimes:"+strings.Join(mimes, ","))
	}
	return rules
}

// ---------------------------------------------------------------------------
// Rules\Email.
// ---------------------------------------------------------------------------

// EmailRule builds the "email" rule together with the list of checks it should
// run: the RFC reading, a stricter one, an MX record lookup on the domain, the
// spoofing check, and the native one. With nothing asked for it renders the bare
// "email", which is the shape check alone. Build one with NewEmailRule. It
// carries the suffix because Email is already the one-value helper here.
//
// Answers Illuminate\Validation\Rules\Email.
type EmailRule struct{ validations []string }

// NewEmailRule answers to the Email constructor and to Rule::email.
func NewEmailRule() *EmailRule { return &EmailRule{} }

// RfcCompliant answers to Email::rfcCompliant. Strict adds the stricter reading,
// which is Email::strict.
func (r *EmailRule) RfcCompliant(strict ...bool) *EmailRule {
	if len(strict) > 0 && strict[0] {
		return r.add("strict")
	}
	return r.add("rfc")
}

// Strict answers to Email::strict.
func (r *EmailRule) Strict() *EmailRule { return r.add("strict") }

// ValidateMxRecord answers to Email::validateMxRecord.
func (r *EmailRule) ValidateMxRecord() *EmailRule { return r.add("dns") }

// PreventSpoofing answers to Email::preventSpoofing.
func (r *EmailRule) PreventSpoofing() *EmailRule { return r.add("spoof") }

// WithNativeValidation answers to Email::withNativeValidation.
func (r *EmailRule) WithNativeValidation(allowUnicode ...bool) *EmailRule {
	if len(allowUnicode) > 0 && allowUnicode[0] {
		return r.add("filter_unicode")
	}
	return r.add("filter")
}

// Rules answers to Email::rules.
func (r *EmailRule) Rules(rules ...string) *EmailRule {
	r.validations = append(r.validations, rules...)

	return r
}

func (r *EmailRule) add(validation string) *EmailRule {
	r.validations = append(r.validations, validation)

	return r
}

// String renders the chain. With nothing asked for it is the bare `email`, which
// is the shape check.
func (r *EmailRule) String() string {
	if len(r.validations) == 0 {
		return "email"
	}
	return "email:" + strings.Join(r.validations, ",")
}

// ---------------------------------------------------------------------------
// Rules\Unique and Rules\Exists, over Rules\DatabaseRule.
// ---------------------------------------------------------------------------

// DatabaseRule holds what the two database rules have in common: the table and
// the column to look in, the extra conditions that narrow the search, and the
// query callbacks registered through Using. Unique and Exists embed it rather
// than repeat it, so the Where methods below are written once and read as
// methods on either; FormatWheres renders the conditions into the parameter list
// each of them builds. It is not useful on its own.
//
// Answers the Illuminate\Validation\Rules\DatabaseRule trait.
type DatabaseRule struct {
	table  string
	column string
	wheres [][2]string

	// using answers to $using: the query callbacks Using registers.
	using []func(query any)
}

// ResolveTableName answers to DatabaseRule::resolveTableName. The PHP resolves a
// model class name into its table; there are no models here (ADR: no Active
// Record), so a table name is a table name.
func (r *DatabaseRule) ResolveTableName(table string) string { return table }

// Where answers to DatabaseRule::where.
func (r *DatabaseRule) Where(column, value string) *DatabaseRule {
	r.wheres = append(r.wheres, [2]string{column, value})

	return r
}

// WhereNot answers to DatabaseRule::whereNot.
func (r *DatabaseRule) WhereNot(column, value string) *DatabaseRule {
	return r.Where(column, "!"+value)
}

// WhereNull answers to DatabaseRule::whereNull.
func (r *DatabaseRule) WhereNull(column string) *DatabaseRule { return r.Where(column, "NULL") }

// WhereNotNull answers to DatabaseRule::whereNotNull.
func (r *DatabaseRule) WhereNotNull(column string) *DatabaseRule {
	return r.Where(column, "NOT_NULL")
}

// WhereIn answers to DatabaseRule::whereIn.
func (r *DatabaseRule) WhereIn(column string, values ...string) *DatabaseRule {
	return r.Where(column, strings.Join(values, ","))
}

// WhereNotIn answers to DatabaseRule::whereNotIn.
func (r *DatabaseRule) WhereNotIn(column string, values ...string) *DatabaseRule {
	return r.Where(column, "!"+strings.Join(values, ","))
}

// WithoutTrashed answers to DatabaseRule::withoutTrashed.
func (r *DatabaseRule) WithoutTrashed(deletedAtColumn ...string) *DatabaseRule {
	return r.WhereNull(columnOr(deletedAtColumn, "deleted_at"))
}

// OnlyTrashed answers to DatabaseRule::onlyTrashed.
func (r *DatabaseRule) OnlyTrashed(deletedAtColumn ...string) *DatabaseRule {
	return r.WhereNotNull(columnOr(deletedAtColumn, "deleted_at"))
}

// FormatWheres answers to DatabaseRule::formatWheres.
func (r *DatabaseRule) FormatWheres() string {
	pairs := make([]string, len(r.wheres))
	for i, where := range r.wheres {
		pairs[i] = where[0] + `,"` + strings.ReplaceAll(where[1], `"`, `""`) + `"`
	}
	return strings.Join(pairs, ",")
}

func columnOr(given []string, def string) string {
	if len(given) > 0 && given[0] != "" {
		return given[0]
	}
	return def
}

// Unique builds the "unique" rule: no row in the table may already hold this
// value in the column. Ignore names the one row allowed to hold it, which is how
// a form that edits an existing record does not collide with itself. The check
// is a read of the table, so it runs only with a Grant carrying a tenant and
// counts only that tenant's rows. Build one with NewUnique.
//
// Answers Illuminate\Validation\Rules\Unique.
type Unique struct {
	DatabaseRule
	ignore   string
	idColumn string
}

// NewUnique answers to the Unique constructor and to Rule::unique. The column
// defaults to "NULL", which the PHP reads as "the attribute's own name".
func NewUnique(table string, column ...string) *Unique {
	return &Unique{
		DatabaseRule: DatabaseRule{table: table, column: columnOr(column, "NULL")},
		idColumn:     "id",
	}
}

// Ignore answers to Unique::ignore: the row this check is allowed to find,
// which is the row being edited.
func (r *Unique) Ignore(id string, idColumn ...string) *Unique {
	r.ignore = id
	r.idColumn = columnOr(idColumn, "id")

	return r
}

// String answers to Unique::__toString.
func (r *Unique) String() string {
	ignore := "NULL"
	if r.ignore != "" {
		ignore = `"` + r.ignore + `"`
	}
	return strings.TrimRight(strings.Join([]string{
		"unique:" + r.table, r.column, ignore, r.idColumn, r.FormatWheres(),
	}, ","), ",")
}

// Exists builds the "exists" rule: some row in the table must already hold this
// value in the column, which is how an identifier arriving from a form is
// checked before anything is written against it. The check is a read of the
// table, so it runs only with a Grant carrying a tenant and counts only that
// tenant's rows -- an identifier belonging to somebody else does not exist as
// far as this rule is concerned. Build one with NewExists.
//
// Answers Illuminate\Validation\Rules\Exists.
type Exists struct{ DatabaseRule }

// NewExists answers to the Exists constructor and to Rule::exists.
func NewExists(table string, column ...string) *Exists {
	return &Exists{DatabaseRule{table: table, column: columnOr(column, "NULL")}}
}

// String answers to Exists::__toString.
func (r *Exists) String() string {
	return strings.TrimRight(strings.Join([]string{
		"exists:" + r.table, r.column, r.FormatWheres(),
	}, ","), ",")
}

// ---------------------------------------------------------------------------
// Rules\Password.
// ---------------------------------------------------------------------------

// UncompromisedVerifier answers to
// Illuminate\Contracts\Validation\UncompromisedVerifier: whether a password has
// appeared in a data leak.
//
// Laravel's NotPwnedVerifier asks haveibeenpwned over its HTTP client, with the
// k-anonymity prefix so that the password itself never leaves. That is a network
// call and belongs with the client, so what is here is the question.
type UncompromisedVerifier interface {
	// Verify answers to UncompromisedVerifier::verify: false when the value has
	// appeared more times than the threshold allows.
	Verify(value string, threshold int) bool
}

// Password answers to Illuminate\Validation\Rules\Password.
//
// It is the one rule object most often reached for, and it cannot be a rule
// string: it holds a verifier and it says four different things depending on
// which part failed. It runs through Validator.After:
//
//	v.After(func(v *validation.Validator) {
//		v.ValidateUsingCustomRule("password", v.GetValue("password"), rule)
//	})
type Password struct {
	min           int
	max           int
	mixedCase     bool
	letters       bool
	numbers       bool
	symbols       bool
	uncompromised bool

	compromisedThreshold int
	verifier             UncompromisedVerifier

	customRules []string
	messages    []string

	validator *Validator
	data      Data
}

// defaultPasswordCallback answers to the static Password::$defaultCallback.
var defaultPasswordCallback func() *Password

// NewPassword answers to the Password constructor. A minimum below one is
// raised to one, as the PHP raises it.
func NewPassword(min int) *Password {
	return &Password{min: max(min, 1)}
}

// PasswordMin answers to the static Password::min.
func PasswordMin(size int) *Password { return NewPassword(size) }

// PasswordDefaults answers to Password::defaults called WITH a callback: the
// configuration every later PasswordDefault answers with.
func PasswordDefaults(callback func() *Password) { defaultPasswordCallback = callback }

// PasswordDefault answers to the static Password::default, and to
// Password::defaults called with none.
func PasswordDefault() *Password {
	if defaultPasswordCallback == nil {
		return PasswordMin(8)
	}
	if configured := defaultPasswordCallback(); configured != nil {
		return configured
	}
	return PasswordMin(8)
}

// PasswordRequired answers to the static Password::required: the default
// configuration, and the field marked required.
func PasswordRequired() (string, *Password) { return "required", PasswordDefault() }

// PasswordSometimes answers to the static Password::sometimes.
func PasswordSometimes() (string, *Password) { return "sometimes", PasswordDefault() }

// Max answers to Password::max.
func (p *Password) Max(size int) *Password {
	p.max = size

	return p
}

// Uncompromised answers to Password::uncompromised: refuse a password that has
// appeared in a data leak more times than the threshold allows.
//
// The verifier is a parameter because Laravel resolves it out of the container,
// which ADR 0001 refuses. A nil verifier FAILS the check rather than passing it:
// "we could not ask" is not "it is safe".
func (p *Password) Uncompromised(verifier UncompromisedVerifier, threshold ...int) *Password {
	p.uncompromised = true
	p.verifier = verifier
	if len(threshold) > 0 {
		p.compromisedThreshold = threshold[0]
	}

	return p
}

// MixedCase answers to Password::mixedCase.
func (p *Password) MixedCase() *Password {
	p.mixedCase = true

	return p
}

// Letters answers to Password::letters.
func (p *Password) Letters() *Password {
	p.letters = true

	return p
}

// Numbers answers to Password::numbers.
func (p *Password) Numbers() *Password {
	p.numbers = true

	return p
}

// Symbols answers to Password::symbols.
func (p *Password) Symbols() *Password {
	p.symbols = true

	return p
}

// Rules answers to Password::rules: more rules merged into the ones this
// enforces.
func (p *Password) Rules(rules ...string) *Password {
	p.customRules = append(p.customRules, rules...)

	return p
}

// AppliedRules answers to Password::appliedRules: what this rule is currently
// asking for, which is what a "your password must" list on the form is drawn
// from.
func (p *Password) AppliedRules() map[string]any {
	return map[string]any{
		"min":                  p.min,
		"max":                  p.max,
		"mixedCase":            p.mixedCase,
		"letters":              p.letters,
		"numbers":              p.numbers,
		"symbols":              p.symbols,
		"uncompromised":        p.uncompromised,
		"compromisedThreshold": p.compromisedThreshold,
		"customRules":          p.customRules,
	}
}

// SetValidator answers to Password::setValidator.
func (p *Password) SetValidator(validator *Validator) { p.validator = validator }

// SetData answers to Password::setData.
func (p *Password) SetData(data Data) { p.data = data }

// Passes answers to Password::passes.
//
// The PHP builds a sibling validator over `string|min:N` plus the custom rules
// and hangs the four character checks off its after hook. This does the same,
// with the sibling compiled from the same chain.
func (p *Password) Passes(attribute string, value any) bool {
	p.messages = nil

	chain := "string|min:" + strconv.Itoa(p.min)
	if p.max > 0 {
		chain += "|max:" + strconv.Itoa(p.max)
	}
	for _, rule := range p.customRules {
		if rule != "" {
			chain += "|" + rule
		}
	}

	data := p.data
	if data == nil {
		data = Data{}
	}
	data = data.Clone()
	data[attribute] = value

	set, err := Compile(Rules{attribute: chain})
	if err != nil {
		return p.fail(err.Error())
	}

	sibling := Make(data, set, p.siblingOptions()...)

	if sibling.Fails() {
		return p.fail(sibling.Errors().All()...)
	}

	text, isString := value.(string)
	if !isString {
		return true
	}

	if p.mixedCase && !hasMixedCase(text) {
		p.fail(p.line("validation.password.mixed",
			"The :attribute field must contain at least one uppercase and one lowercase letter."))
	}
	if p.letters && !containsRune(text, unicode.IsLetter) {
		p.fail(p.line("validation.password.letters",
			"The :attribute field must contain at least one letter."))
	}
	if p.symbols && !containsRune(text, isSymbolRune) {
		p.fail(p.line("validation.password.symbols",
			"The :attribute field must contain at least one symbol."))
	}
	if p.numbers && !containsRune(text, unicode.IsNumber) {
		p.fail(p.line("validation.password.numbers",
			"The :attribute field must contain at least one number."))
	}

	if len(p.messages) > 0 {
		return false
	}

	if p.uncompromised && (p.verifier == nil || !p.verifier.Verify(text, p.compromisedThreshold)) {
		return p.fail(p.line("validation.password.uncompromised",
			"The given :attribute has appeared in a data leak. Please choose a different :attribute."))
	}

	return true
}

// siblingOptions carries the overrides of the validator this rule belongs to
// into the sibling, so that both say the same things.
func (p *Password) siblingOptions() []ValidatorOption {
	if p.validator == nil {
		return nil
	}
	return []ValidatorOption{
		WithTranslator(p.validator.trans),
		WithCustomMessages(p.validator.CustomMessages()),
		WithCustomAttributes(p.validator.CustomAttributes()),
	}
}

// line reads one of the password lines out of the translator, falling back to
// the English sentence -- which is what Rules\Enum, Rules\Can and Rules\AnyOf do
// with their own line.
func (p *Password) line(key, fallback string) string {
	if p.validator == nil || p.validator.trans == nil {
		return fallback
	}
	if sentence := line(p.validator.GetTranslator().Get(key, nil, ""), key); sentence != key {
		return sentence
	}
	return fallback
}

// Message answers to Password::message.
func (p *Password) Message() []string { return p.messages }

// fail answers to Password::fail.
func (p *Password) fail(messages ...string) bool {
	p.messages = append(p.messages, messages...)

	return false
}

// hasMixedCase is the PHP's /(\p{Ll}+.*\p{Lu})|(\p{Lu}+.*\p{Ll})/u: at least one
// of each case, in either order.
func hasMixedCase(value string) bool {
	var lower, upper bool
	for _, r := range value {
		if unicode.IsLower(r) {
			lower = true
		}
		if unicode.IsUpper(r) {
			upper = true
		}
	}
	return lower && upper
}

// isSymbolRune is the PHP's /\p{Z}|\p{S}|\p{P}/u: a separator, a symbol or a
// punctuation mark.
func isSymbolRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Z, r) ||
		unicode.IsSymbol(r) || unicode.IsPunct(r)
}

func containsRune(value string, is func(rune) bool) bool {
	for _, r := range value {
		if is(r) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rules\Enum, Rules\Can and Rules\AnyOf.
// ---------------------------------------------------------------------------

// Enum answers to Illuminate\Validation\Rules\Enum: the value must be one of the
// cases of a type.
//
// PHP reads the cases off the enum class at runtime; Go has no enum and no
// reflection is used here, so the cases are given. Only and Except then narrow
// them exactly as the PHP narrows them.
type Enum struct {
	cases  []string
	only   []string
	except []string

	validator *Validator
}

// NewEnum answers to the Enum constructor and to Rule::enum.
func NewEnum(cases ...string) *Enum { return &Enum{cases: cases} }

// Only answers to Enum::only.
func (r *Enum) Only(values ...string) *Enum {
	r.only = values

	return r
}

// Except answers to Enum::except.
func (r *Enum) Except(values ...string) *Enum {
	r.except = values

	return r
}

// Passes answers to Enum::passes.
func (r *Enum) Passes(attribute string, value any) bool {
	if value == nil {
		return false
	}
	text := stringOf(value)

	var known bool
	for _, c := range r.cases {
		if c == text {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	return r.isDesirable(text)
}

// isDesirable answers to Enum::isDesirable.
func (r *Enum) isDesirable(value string) bool {
	switch {
	case len(r.only) > 0:
		return containsString(r.only, value)
	case len(r.except) > 0:
		return !containsString(r.except, value)
	}
	return true
}

// Message answers to Enum::message.
func (r *Enum) Message() []string {
	return []string{translatedOr(r.validator, "validation.enum", "The selected :attribute is invalid.")}
}

// SetValidator answers to Enum::setValidator.
func (r *Enum) SetValidator(validator *Validator) { r.validator = validator }

// Can answers to Illuminate\Validation\Rules\Can: the value is one the current
// subject is allowed to choose.
//
// Laravel asks the Gate facade. RULE 17 says the same thing with the Grant, so
// the question is asked through the Authorizer the caller gives -- there is no
// facade to reach for (ADR 0002).
type Can struct {
	ability   string
	arguments []string
	allows    func(g auth.Grant, ability string, arguments []string, value any) bool

	validator *Validator
}

// NewCan answers to the Can constructor and to Rule::can.
func NewCan(allows func(g auth.Grant, ability string, arguments []string, value any) bool, ability string, arguments ...string) *Can {
	return &Can{ability: ability, arguments: arguments, allows: allows}
}

// Passes answers to Can::passes.
//
// A nil check FAILS rather than passes: "nobody wired the authorizer" is not
// "everybody is allowed", and RULE 17 is the reason this rule exists.
func (r *Can) Passes(attribute string, value any) bool {
	if r.allows == nil || r.validator == nil {
		return false
	}
	return r.allows(r.validator.grant, r.ability, r.arguments, value)
}

// Message answers to Can::message.
func (r *Can) Message() []string {
	return []string{translatedOr(r.validator, "validation.can",
		"The :attribute field contains an unauthorized value.")}
}

// SetValidator answers to Can::setValidator.
func (r *Can) SetValidator(validator *Validator) { r.validator = validator }

// AnyOf answers to Illuminate\Validation\Rules\AnyOf: the value passes when it
// passes any one of the given rule sets.
type AnyOf struct {
	sets []*Set

	validator *Validator
}

// NewAnyOf answers to the AnyOf constructor and to Rule::anyOf.
func NewAnyOf(sets ...*Set) *AnyOf { return &AnyOf{sets: sets} }

// Passes answers to AnyOf::passes.
func (r *AnyOf) Passes(attribute string, value any) bool {
	for _, set := range r.sets {
		if set == nil {
			continue
		}
		data := Data{}
		if r.validator != nil {
			data = r.validator.GetData().Clone()
		}
		data[attribute] = value

		if Make(data, set).Passes() {
			return true
		}
	}
	return false
}

// Message answers to AnyOf::message.
func (r *AnyOf) Message() []string {
	return []string{translatedOr(r.validator, "validation.any_of", "The :attribute field is invalid.")}
}

// SetValidator answers to AnyOf::setValidator.
func (r *AnyOf) SetValidator(validator *Validator) { r.validator = validator }

// translatedOr is the `$message === 'validation.x' ? [...] : $message` the three
// rules above each write out.
func translatedOr(v *Validator, key, fallback string) string {
	if v == nil || v.trans == nil {
		return fallback
	}
	if sentence := line(v.GetTranslator().Get(key, nil, ""), key); sentence != key {
		return sentence
	}
	return fallback
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Illuminate\Validation\Rule, for the two that build no rule of their own.
// ---------------------------------------------------------------------------

// When answers to Rule::when: the rules an attribute gets when a condition
// holds, and the ones it gets when it does not.
func When(condition func(Data) bool, rules string, defaultRules ...string) *ConditionalRules {
	return NewConditionalRules(condition, rules, defaultRules...)
}

// Unless answers to Rule::unless, which the PHP defines as When with the
// condition turned around.
func Unless(condition func(Data) bool, rules string, defaultRules ...string) *ConditionalRules {
	return NewConditionalRules(func(data Data) bool {
		return condition == nil || !condition(data)
	}, rules, defaultRules...)
}

// ForEach answers to Rule::forEach: rules for one member of an array, decided by
// looking at that member.
func ForEach(callback func(value any, attribute string, data Data) Rules) *NestedRules {
	return NewNestedRules(callback)
}

// Using answers to DatabaseRule::using: a query callback the rule runs instead
// of the conditions it would otherwise build.
//
// The query is any because the builder belongs to hesape/database and this
// package does not import it -- a rule carries the callback and the repository
// that runs it knows what it holds.
func (r *DatabaseRule) Using(callback func(query any)) *DatabaseRule {
	r.using = append(r.using, callback)

	return r
}

// QueryCallbacks answers to DatabaseRule::queryCallbacks.
func (r *DatabaseRule) QueryCallbacks() []func(query any) { return r.using }

// IgnoreModel answers to Unique::ignoreModel: the row being edited, named by its
// key rather than by the record itself.
//
// The PHP takes an Eloquent model and reads $model->getKeyName() off it. There
// is no Active Record here (docs/01 rejects it), so the key and its column are
// what arrive -- which is the only thing the PHP reads off the model anyway.
func (r *Unique) IgnoreModel(key string, idColumn ...string) *Unique {
	return r.Ignore(key, idColumn...)
}
