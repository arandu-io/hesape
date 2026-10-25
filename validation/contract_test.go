package validation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/validation"
)

// This file is the parity contract: one entry per rule of Illuminate\Validation,
// each written the way that rule behaves there, with the value types a request
// really carries -- text from a form, numbers and booleans and lists from a JSON
// body, and an upload. An entry that fails is a difference between this package
// and the one it takes its vocabulary from, and every difference that is kept is
// kept on purpose and has its own test further down.

// contractCase is one rule with the values it must accept and the values it must
// refuse, as the single field "f".
type contractCase struct {
	rule string
	pass []any
	fail []any
}

// assertContract runs every case through a compiled set and reports the value
// that went the wrong way.
func assertContract(t *testing.T, cases []contractCase) {
	t.Helper()
	for _, c := range cases {
		set, err := validation.Compile(validation.Rules{"f": c.rule})
		if err != nil {
			t.Errorf("%s: Compile = %v", c.rule, err)
			continue
		}
		for _, value := range c.pass {
			if errs := runSet(set, validation.Data{"f": value}); errs.Any() {
				t.Errorf("%s: %#v was refused with %v", c.rule, value, errs)
			}
		}
		for _, value := range c.fail {
			if errs := runSet(set, validation.Data{"f": value}); !errs.Any() {
				t.Errorf("%s: %#v was accepted", c.rule, value)
			}
		}
	}
}

func runSet(set *validation.Set, data validation.Data) validation.Errors {
	v := validation.Make(data, set)
	if v.Passes() {
		return nil
	}
	return v.Errors()
}

func list(members ...any) []any { return members }

func keyed(pairs ...any) validation.Data {
	out := validation.Data{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}

// TestContractPresenceRules covers required, filled, present, missing and
// prohibited on every kind of blank there is: absent, null, the empty string,
// whitespace, an empty list.
func TestContractPresenceRules(t *testing.T) {
	assertContract(t, []contractCase{
		{"required", []any{"a", "0", 0, false, list("x"), keyed("a", 1)}, []any{nil, "", "  ", list(), validation.Data{}}},
		{"filled", []any{"a", 0, false}, []any{"", " ", list()}},
		{"present", []any{"a", "", nil, list()}, nil},
		{"prohibited", []any{"", " ", nil, list()}, []any{"a", 0, false, list("x")}},
		{"accepted", []any{"yes", "on", "1", "true", 1, true}, []any{"", "0", "no", "TRUE", 0, false, 2}},
		{"declined", []any{"no", "off", "0", "false", 0, false}, []any{"", "1", "yes", "FALSE", 1, true}},
	})
}

// TestContractAnImplicitRuleRunsOnABlankValueAndAnOrdinaryOneDoesNot is the
// difference between `required` and `min:3`: a blank value fails the first and
// is not asked about the second.
func TestContractAnImplicitRuleRunsOnABlankValueAndAnOrdinaryOneDoesNot(t *testing.T) {
	blank := []any{"", "   "}
	assertContract(t, []contractCase{
		{"min:3", blank, []any{"ab"}},
		{"email", blank, []any{"nope"}},
		{"integer", blank, []any{"x"}},
		{"alpha", blank, []any{"1"}},
		{"required|min:3", nil, append(blank, nil)},
		{"filled", nil, []any{"", "  "}},

		// A null that was sent is not blank text: it is asked about every rule
		// unless the field says it may be null.
		{"min:3", nil, []any{nil}},
		{"nullable|min:3", []any{nil, "", "abc"}, []any{"ab"}},
	})

	// Every implicit rule runs on a blank value: each is refused here for a
	// reason of its own.
	set := mustCompile(t, validation.Rules{
		"a": "required", "b": "accepted", "c": "declined", "d": "filled", "e": "present",
		"f": "required_if:a,x", "g": "required_with:a", "h": "required_without:a",
	})
	errs := runSet(set, validation.Data{"a": "", "b": "", "c": "", "d": "", "e": nil, "f": "", "g": "", "h": ""})
	for _, field := range []string{"a", "b", "c", "d", "h"} {
		if !errs.Has(field) {
			t.Errorf("%s: an implicit rule did not run on a blank value (%v)", field, errs)
		}
	}
	for _, field := range []string{"e", "f", "g"} {
		if errs.Has(field) {
			t.Errorf("%s: failed although its condition was not met (%v)", field, errs)
		}
	}
}

// TestContractSizeIsMeasuredByTheTypeOfTheField is min, max, size and between on
// the four things a size can be: characters, a value, members and kilobytes.
func TestContractSizeIsMeasuredByTheTypeOfTheField(t *testing.T) {
	assertContract(t, []contractCase{
		// A string counts characters, not bytes.
		{"min:3", []any{"abc", "José", "ação"}, []any{"ab", "é"}},
		{"max:3", []any{"abc", "ão!"}, []any{"abcd"}},
		{"size:3", []any{"abc", "ção"}, []any{"ab", "abcd"}},
		{"between:2,3", []any{"ab", "abc"}, []any{"a", "abcd"}},

		// A field that declares a numeric rule counts its value.
		{"numeric|min:3", []any{"3", 3, 3.5, 10}, []any{"2", 2.99, 1}},
		{"numeric|max:3", []any{"3", 2, "-5"}, []any{"3.01", 4}},
		{"integer|size:3", []any{"3", 3}, []any{"4", 30}},
		{"numeric|between:1.5,2.5", []any{"1.5", 2, "2.50"}, []any{"1.49", 3}},
		{"decimal:2|min:1", []any{"1.50"}, []any{"0.50"}},

		// An array counts its members.
		{"array|min:2", []any{list(1, 2), list(1, 2, 3)}, []any{list(1), list()}},
		{"array|max:2", []any{list(1, 2), list()}, []any{list(1, 2, 3)}},
		{"array|size:2", []any{list(1, 2)}, []any{list(1), list(1, 2, 3)}},
		{"array|between:1,2", []any{list(1), list(1, 2)}, []any{list(1, 2, 3)}},

		// The limit written with a fraction, on a field that measures a value.
		{"numeric|min:0.5", []any{"0.5", "1"}, []any{"0.49"}},
	})
}

// TestContractAFileIsMeasuredInKilobytes is the fourth thing a size can be.
func TestContractAFileIsMeasuredInKilobytes(t *testing.T) {
	small := contractFile{size: 100 * 1024}
	exact := contractFile{size: 2 * 1024}
	big := contractFile{size: 300 * 1024}
	assertContract(t, []contractCase{
		{"file|max:100", []any{small}, []any{big}},
		{"file|min:100", []any{small, big}, []any{exact}},
		{"file|size:2", []any{exact}, []any{small}},
		{"file|between:2,100", []any{exact, small}, []any{big}},
	})
}

type contractFile struct {
	size int64
	path string
}

func (f contractFile) GetPath() string                    { return cmpOr(f.path, "upload") }
func (f contractFile) GetRealPath() string                { return f.path }
func (f contractFile) GetSize() int64                     { return f.size }
func (f contractFile) GetMimeType() string                { return "application/octet-stream" }
func (f contractFile) GetExtension() string               { return "bin" }
func (f contractFile) GuessExtension() string             { return "bin" }
func (f contractFile) GetClientOriginalExtension() string { return "bin" }
func (f contractFile) IsValid() bool                      { return true }

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// TestContractTypeRules covers string, integer, numeric, boolean, array, list,
// json and the strict spelling of the three that have one.
func TestContractTypeRules(t *testing.T) {
	assertContract(t, []contractCase{
		{"string", []any{"a", "1"}, []any{1, true, list("a"), keyed("a", 1)}},
		{"integer", []any{"1", "-5", "+7", 3, int64(4), 2.0}, []any{"1.5", "01", "a", "1e3", 1.5, list()}},
		{"integer:strict", []any{3, int64(4), 2.0, uint(1)}, []any{"3", "1.5", 1.5, true}},
		{"numeric", []any{"1", "-1.5", ".5", "1e3", " 4 ", 2, 2.5}, []any{"a", "1,5", "0x1A", "NaN", true}},
		{"numeric:strict", []any{2, 2.5, int64(3)}, []any{"2", "2.5", "a"}},
		{"boolean", []any{true, false, 0, 1, "0", "1"}, []any{"true", "false", 2, "yes", list()}},
		{"boolean:strict", []any{true, false}, []any{0, 1, "0", "1", "true"}},
		{"array", []any{list(), list(1), keyed("a", 1)}, []any{"a", 1}},
		{"array:a,b", []any{keyed("a", 1), keyed("a", 1, "b", 2)}, []any{keyed("c", 1), keyed("a", 1, "c", 2)}},
		{"array_keys:a,b", []any{keyed("a", 1), keyed("a", 1, "b", 2)}, []any{keyed("c", 1), keyed("a", 1, "c", 2), "a"}},
		{"list", []any{list(), list("a", "b")}, []any{keyed("a", 1), "a"}},
		{"json", []any{`{"a":1}`, `[1,2]`, `"x"`, `5`, 5, true}, []any{`{a:1}`, `nope`, list(), keyed("a", 1)}},
		{"in_array_keys:a,b", []any{keyed("a", 1), keyed("b", 1, "c", 2)}, []any{keyed("c", 1), keyed(), "a"}},
		{"required_array_keys:a,b", []any{keyed("a", 1, "b", 2)}, []any{keyed("a", 1), "a"}},
	})
}

// TestContractMembershipRules covers in, not_in, contains, doesnt_contain and
// enum, and what each does with an array value.
func TestContractMembershipRules(t *testing.T) {
	assertContract(t, []contractCase{
		{"in:a,b", []any{"a", "b"}, []any{"c", "A", list("a"), keyed("a", 1)}},
		{"array|in:a,b", []any{list("a", "b"), list("a")}, []any{list("a", "z"), list(list("a"))}},
		{"not_in:a,b", []any{"c", "A"}, []any{"a", "b"}},
		{"in:1,2", []any{"1", 2}, []any{"3", 12}},
		{"contains:a,b", []any{list("a", "b", "c")}, []any{list("a"), "a"}},
		{"doesnt_contain:a,b", []any{list("c"), list(), list(1)}, []any{list("a"), list("c", "b"), "a"}},
		{"enum:draft,sent", []any{"draft", "sent"}, []any{"x", nil, list("draft")}},
	})
}

// TestContractStringShapeRules covers the rules about the characters of a text.
func TestContractStringShapeRules(t *testing.T) {
	assertContract(t, []contractCase{
		{"alpha", []any{"abc", "ação", "Ünï"}, []any{"abc1", "a b", "a-b", 12}},
		{"alpha:ascii", []any{"abc"}, []any{"ação"}},
		{"alpha_dash", []any{"a-b_c1", "ação-1", 12}, []any{"a b", "a.b"}},
		{"alpha_num", []any{"abc1", "ação", 12}, []any{"a-b", "a b"}},
		{"alpha_num:ascii", []any{"abc1"}, []any{"ação"}},
		{"ascii", []any{"abc", "a b!"}, []any{"ação", 12}},
		{"lowercase", []any{"abc", "ação 1"}, []any{"Abc", 12, list("a")}},
		{"uppercase", []any{"ABC", "AÇÃO 1"}, []any{"aBC", 12, list("A")}},
		{"starts_with:ab,cd", []any{"abx", "cdx"}, []any{"xab", 12, list("ab")}},
		{"starts_with:1", []any{12}, []any{21}},
		{"doesnt_start_with:ab", []any{"xab", 12}, []any{"abx", list("x")}},
		{"ends_with:ab,cd", []any{"xab", "xcd"}, []any{"abx", list("ab")}},
		{"doesnt_end_with:ab", []any{"abx", 12}, []any{"xab", list("x")}},
		{"digits:3", []any{"123", 123}, []any{"12", "1234", "12a", "+12"}},
		{"digits_between:2,3", []any{"12", "123"}, []any{"1", "1234", "1.2"}},
		{"min_digits:2", []any{"12", "123"}, []any{"1", "1a"}},
		{"max_digits:2", []any{"1", "12"}, []any{"123", "1a"}},
		{"hex_color", []any{"#fff", "#FFFFFF", "#ffff", "#ffffffff"}, []any{"fff", "#ff", "#ggg", "#fffff"}},
		{"mac_address", []any{"00:1A:2b:3C:4d:5E", "00-1A-2B-3C-4D-5E"}, []any{"00:1A:2B", "zz:zz:zz:zz:zz:zz"}},
		{"ip", []any{"127.0.0.1", "::1", "2001:db8::1"}, []any{"256.0.0.1", "nope", "fe80::1%eth0"}},
		{"ipv4", []any{"127.0.0.1"}, []any{"::1", "256.1.1.1"}},
		{"ipv6", []any{"::1"}, []any{"127.0.0.1"}},
		{"base64", []any{"YWJj", "YQ==", "AAAA"}, []any{"YQ", "not base64!", 12, list()}},
		{"encoding:utf-8", []any{"ação", "abc"}, []any{"\xff\xfe", 12}},
		{"encoding:ascii", []any{"abc"}, []any{"ação"}},
		{"regex:^[a-z]+$", []any{"abc", "abc"}, []any{"ABC", "ab1"}},
		{"not_regex:^[0-9]+$", []any{"abc", "1a"}, []any{"123"}},
		{"regex:^[0-9]+$", []any{"123", 123}, []any{"12a", list()}},
	})
}

// TestContractIdentityRules covers the rules that name a kind of identifier.
func TestContractIdentityRules(t *testing.T) {
	const v4 = "9b2f1b52-6c3e-4f4b-8a54-3a1f1a2b3c4d"
	const v7 = "018f3a52-6c3e-7f4b-8a54-3a1f1a2b3c4d"
	assertContract(t, []contractCase{
		{"uuid", []any{v4, v7, "00000000-0000-0000-0000-000000000000"}, []any{"nope", v4 + "0", 12}},
		{"uuid:4", []any{v4}, []any{v7}},
		{"uuid:7", []any{v7}, []any{v4}},
		{"uuid:nil", []any{"00000000-0000-0000-0000-000000000000"}, []any{v4}},
		{"uuid:max", []any{"ffffffff-ffff-ffff-ffff-ffffffffffff"}, []any{v4}},
		{"ulid", []any{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}, []any{"nope", v4}},
		{"email", []any{"a@b.co", "ana.silva+x@example.com.br"}, []any{"a@b", "a b@c.d", "@c.d", "a@", "a@b.co\n", 12}},
		{"url", []any{"https://example.com", "http://a.b/c?d=1", "ftp://a.b"}, []any{"example.com", "javascript:alert(1)", "http:/a.b", 12}},
		{"url:ftp", []any{"ftp://a.b"}, []any{"https://a.b"}},
		{"timezone", []any{"UTC", "America/Sao_Paulo", "Europe/Lisbon"}, []any{"US/Eastern", "Mars/Base", "Local", 12}},
		{"timezone:europe", []any{"Europe/Lisbon"}, []any{"America/Sao_Paulo", "UTC"}},
		{"timezone:all_with_bc", []any{"US/Eastern", "UTC"}, []any{"Mars/Base"}},
	})
}

// TestContractDateRules covers the rules about moments, in the layouts a form
// sends and in the one a field declares.
func TestContractDateRules(t *testing.T) {
	assertContract(t, []contractCase{
		{"date", []any{"2026-01-31", "2026-01-31 10:00:00", "2026-01-31T10:00:00Z"}, []any{"2026-02-30", "nope", 12, list()}},
		{"date_format:2006-01-02", []any{"2026-01-31"}, []any{"2026-1-31", "31/01/2026", list()}},
		{"date_format:2006-01-02,02/01/2006", []any{"2026-01-31", "31/01/2026"}, []any{"01/31/2026"}},
		{"after:2026-01-01", []any{"2026-01-02"}, []any{"2026-01-01", "2025-12-31", "nope"}},
		{"after_or_equal:2026-01-01", []any{"2026-01-01", "2026-01-02"}, []any{"2025-12-31"}},
		{"before:2026-01-01", []any{"2025-12-31"}, []any{"2026-01-01", "2026-01-02"}},
		{"before_or_equal:2026-01-01", []any{"2026-01-01", "2025-12-31"}, []any{"2026-01-02"}},
		{"date_equals:2026-01-01", []any{"2026-01-01"}, []any{"2026-01-02"}},
	})
}

// TestContractDependentRulesReadTheOtherFieldTheWayTheHostLanguageDoes pins what
// the value list of an _if or _unless rule means. The values are written as
// text, so the other field decides what they are: a boolean field reads "true"
// as the boolean, a null reads "null" as null, and two numeric texts are the
// same value when they are the same number.
func TestContractDependentRulesReadTheOtherFieldTheWayTheHostLanguageDoes(t *testing.T) {
	cases := []struct {
		name  string
		rules validation.Rules
		data  validation.Data
		fails bool
	}{
		{"a number equals its text", validation.Rules{"a": "required_if:t,1", "t": "sometimes"}, validation.Data{"t": 1, "a": ""}, true},
		{"numeric texts are one number", validation.Rules{"a": "required_if:t,1", "t": "sometimes"}, validation.Data{"t": "01", "a": ""}, true},
		{"another number is another value", validation.Rules{"a": "required_if:t,1", "t": "sometimes"}, validation.Data{"t": "2", "a": ""}, false},
		{"a boolean field reads true", validation.Rules{"a": "required_if:t,true", "t": "boolean"}, validation.Data{"t": true, "a": ""}, true},
		{"a boolean-ruled text reads true", validation.Rules{"a": "required_if:t,true", "t": "boolean"}, validation.Data{"t": "1", "a": ""}, true},
		{"a boolean-ruled text reads false", validation.Rules{"a": "required_if:t,false", "t": "boolean"}, validation.Data{"t": "0", "a": ""}, true},
		{"a boolean-ruled false is not true", validation.Rules{"a": "required_if:t,true", "t": "boolean"}, validation.Data{"t": "0", "a": ""}, false},
		{"a boolean is compared by type", validation.Rules{"a": "required_if:t,1", "t": "sometimes"}, validation.Data{"t": true, "a": ""}, false},
		{"null is spelled null", validation.Rules{"a": "required_if:t,null", "t": "sometimes"}, validation.Data{"t": nil, "a": ""}, true},
		{"unless holds when it is not listed", validation.Rules{"a": "required_unless:t,x,y", "t": "sometimes"}, validation.Data{"t": "z", "a": ""}, true},
		{"unless is quiet when it is listed", validation.Rules{"a": "required_unless:t,x,y", "t": "sometimes"}, validation.Data{"t": "y", "a": ""}, false},
		{"the other field was not sent", validation.Rules{"a": "required_if:t,1", "t": "sometimes"}, validation.Data{"a": ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := runSet(mustCompile(t, c.rules), c.data)
			if errs.Any() != c.fails {
				t.Errorf("fails = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// TestContractTheFourComparisonsReadANumberAsANumber. A form sends "20" and
// "100" as text, and measuring them by length would call 20 the greater.
func TestContractTheFourComparisonsReadANumberAsANumber(t *testing.T) {
	cases := []struct {
		rule        string
		value, with any
		fails       bool
	}{
		{"gt:other", "20", "100", true},
		{"gt:other", "100", "20", false},
		{"gte:other", "11", "20", true},
		{"gte:other", "20", "20", false},
		{"lt:other", "9", "10", false},
		{"lt:other", "10", "9", true},
		{"lte:other", "10", "10", false},
		{"gt:10", "25", nil, false},
		{"gt:10", "9", nil, true},
		{"lt:10", "9", nil, false},
		{"gte:10", "10", nil, false},
		{"lte:10", "11", nil, true},
		// Text that is not a number is compared by length.
		{"gt:other", "abcd", "abc", false},
		{"gt:other", "ab", "abc", true},
		// Two different kinds of thing are never comparable.
		{"gt:other", "5", list(1), true},
		// Members of an array.
		{"gt:other", list(1, 2, 3), list(1, 2), false},
		{"lt:other", list(1, 2, 3), list(1, 2), true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %v %v", c.rule, c.value, c.with), func(t *testing.T) {
			rules := validation.Rules{"f": c.rule, "other": "sometimes"}
			data := validation.Data{"f": c.value}
			if c.with != nil {
				data["other"] = c.with
			}
			errs := runSet(mustCompile(t, rules), data)
			if errs.Any() != c.fails {
				t.Errorf("fails = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// TestContractMultipleOfIsExact. Float arithmetic says 0.3 is not a multiple of
// 0.1, and a price of 19.90 against a step of 0.10 must be.
func TestContractMultipleOfIsExact(t *testing.T) {
	assertContract(t, []contractCase{
		{"multiple_of:0.1", []any{"0.3", "19.90", 0.3, "0"}, []any{"0.35", "a"}},
		{"multiple_of:5", []any{"10", 15, "-5", "0"}, []any{"11", "7.5"}},
		{"multiple_of:0", nil, []any{"5", "0"}},
	})
}

// TestContractDistinctComparesAWildcardAgainstItsSiblings. items.*.id is one
// rule asked of every member, and a member passes when no other member holds the
// same value.
func TestContractDistinctComparesAWildcardAgainstItsSiblings(t *testing.T) {
	rules := validation.Rules{"items.*.id": "distinct"}
	set := mustCompile(t, rules)

	unique := validation.Data{"items": list(keyed("id", "a"), keyed("id", "b"), keyed("id", "c"))}
	if errs := runSet(set, unique); errs.Any() {
		t.Errorf("distinct members were refused: %v", errs)
	}

	repeated := validation.Data{"items": list(keyed("id", "a"), keyed("id", "b"), keyed("id", "a"))}
	errs := runSet(set, repeated)
	if !errs.Has("items.0.id") || !errs.Has("items.2.id") || errs.Has("items.1.id") {
		t.Errorf("the repeated members are items.0.id and items.2.id, got %v", errs)
	}

	cased := validation.Data{"items": list(keyed("id", "a"), keyed("id", "A"))}
	if errs := runSet(set, cased); errs.Any() {
		t.Errorf("a different case is a different value without ignore_case: %v", errs)
	}
	if errs := runSet(mustCompile(t, validation.Rules{"items.*.id": "distinct:ignore_case"}), cased); !errs.Any() {
		t.Error("ignore_case did not see a repeat")
	}

	mixed := validation.Data{"items": list(keyed("id", 1), keyed("id", "1"))}
	if errs := runSet(set, mixed); !errs.Any() {
		t.Error("the number 1 and the text \"1\" are one value to the loose comparison")
	}
	if errs := runSet(mustCompile(t, validation.Rules{"items.*.id": "distinct:strict"}), mixed); errs.Any() {
		t.Errorf("strict compares types: %v", errs)
	}

	// A list is still distinct in itself.
	if errs := runSet(mustCompile(t, validation.Rules{"f": "distinct"}), validation.Data{"f": list("a", "a")}); !errs.Any() {
		t.Error("a list with a repeated member passed distinct")
	}
}

// TestContractInArrayReadsAWildcard. in_array:items.*.id is the id of every
// member.
func TestContractInArrayReadsAWildcard(t *testing.T) {
	set := mustCompile(t, validation.Rules{"pick": "in_array:items.*.id"})
	items := list(keyed("id", "a"), keyed("id", "b"))
	if errs := runSet(set, validation.Data{"pick": "b", "items": items}); errs.Any() {
		t.Errorf("a listed id was refused: %v", errs)
	}
	if errs := runSet(set, validation.Data{"pick": "z", "items": items}); !errs.Any() {
		t.Error("an id nobody sent was accepted")
	}
}

// TestContractAnUploadIsNotAString. The rules about text refuse an upload, and
// the rules about uploads refuse text.
func TestContractAnUploadIsNotAString(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(good, []byte("ação"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	assertContract(t, []contractCase{
		{"file", []any{contractFile{size: 1}}, []any{"a", 12, nil}},
		{"string", nil, []any{contractFile{size: 1}}},
		{"encoding:utf-8", []any{contractFile{size: 1, path: good}}, []any{contractFile{size: 1, path: bad}, contractFile{size: 1}}},
	})
}

// TestContractAnUnknownEncodingFailsAtBoot. Illuminate throws for a name PHP does
// not list; here the set does not compile.
func TestContractAnUnknownEncodingFailsAtBoot(t *testing.T) {
	_, err := validation.Compile(validation.Rules{"f": "encoding:klingon"})
	if err == nil || !strings.Contains(err.Error(), "klingon") {
		t.Errorf("Compile = %v, want a failure naming the encoding", err)
	}
}

// TestContractTheRulesIlluminateHasAreAllInTheCatalogue lists every rule of
// Illuminate\Validation\Concerns\ValidatesAttributes, by the name a rule string
// spells it, and requires each to compile. A rule added there is a line to add
// here, and a rule this package lacks is a failing entry.
func TestContractTheRulesIlluminateHasAreAllInTheCatalogue(t *testing.T) {
	chains := []string{
		"accepted", "accepted_if:o,x", "active_url", "after:2026-01-01", "after_or_equal:2026-01-01",
		"alpha", "alpha_dash", "alpha_num", "array", "array_keys:a", "ascii", "base64", "bail",
		"before:2026-01-01", "before_or_equal:2026-01-01", "between:1,2", "boolean", "confirmed",
		"contains:a", "current_password", "date", "date_equals:2026-01-01", "date_format:2006-01-02",
		"decimal:2", "declined", "declined_if:o,x", "different:o", "digits:2", "digits_between:1,2",
		"dimensions:min_width=1", "distinct", "doesnt_contain:a", "doesnt_end_with:a", "doesnt_start_with:a",
		"email", "encoding:utf-8", "ends_with:a", "exclude", "exclude_if:o,x", "exclude_unless:o,x",
		"exclude_with:o", "exclude_without:o", "exists:t", "extensions:jpg", "file", "filled", "gt:1",
		"gte:1", "hex_color", "image", "in:a", "in_array:o", "in_array_keys:a", "integer", "ip", "ipv4",
		"ipv6", "json", "list", "lowercase", "lt:1", "lte:1", "mac_address", "max:1", "max_digits:1",
		"mimes:jpg", "mimetypes:image/png", "min:1", "min_digits:1", "missing", "missing_if:o,x",
		"missing_unless:o,x", "missing_with:o", "missing_with_all:o", "multiple_of:1", "not_in:a",
		"not_regex:a", "nullable", "numeric", "present", "present_if:o,x", "present_unless:o,x",
		"present_with:o", "present_with_all:o", "prohibited", "prohibited_if:o,x", "prohibited_if_accepted:o",
		"prohibited_if_declined:o", "prohibited_unless:o,x", "prohibits:o", "regex:a", "required",
		"required_array_keys:a", "required_if:o,x", "required_if_accepted:o", "required_if_declined:o",
		"required_unless:o,x", "required_with:o", "required_with_all:o", "required_without:o",
		"required_without_all:o", "same:o", "size:1", "sometimes", "starts_with:a", "string", "timezone",
		"ulid", "unique:t", "uppercase", "url", "uuid",
	}
	for _, chain := range chains {
		if _, err := validation.Compile(validation.Rules{"f": chain, "o": "sometimes"}); err != nil {
			t.Errorf("%s: %v", chain, err)
		}
	}
	if len(chains) < 100 {
		t.Errorf("the contract lists %d rules; Illuminate has more", len(chains))
	}
}
