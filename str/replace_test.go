package str_test

import (
	"testing"

	"github.com/arandu-io/hesape/str"
)

func TestReplaceRunsTheSearchesInOrder(t *testing.T) {
	cases := []struct {
		search  []string
		replace []string
		subject string
		want    string
	}{
		{[]string{"a"}, []string{"b"}, "aaa", "bbb"},
		{[]string{"a", "b"}, []string{"x"}, "ab", "x"},
		{[]string{""}, []string{"x"}, "ab", "ab"},
		{nil, nil, "ab", "ab"},
		{[]string{"A"}, []string{"x"}, "aA", "ax"},
	}
	for _, c := range cases {
		if got := str.Replace(c.search, c.replace, c.subject, true); got != c.want {
			t.Errorf("Replace(%v, %v, %q, true) = %q, want %q", c.search, c.replace, c.subject, got, c.want)
		}
	}
	if got := str.Replace([]string{"A"}, []string{"x"}, "aA", false); got != "xx" {
		t.Errorf("Replace with caseSensitive false = %q, want %q", got, "xx")
	}
}

func TestReplaceArraySpendsOneReplacementPerOccurrence(t *testing.T) {
	cases := []struct {
		search  string
		replace []string
		subject string
		want    string
	}{
		{"?", []string{"8:30", "9:00"}, "from ? to ?", "from 8:30 to 9:00"},
		{"?", []string{"8:30"}, "from ? to ?", "from 8:30 to ?"},
		{"?", nil, "from ? to ?", "from ? to ?"},
		{"", []string{"x"}, "from ? to ?", "from ? to ?"},
	}
	for _, c := range cases {
		if got := str.ReplaceArray(c.search, c.replace, c.subject); got != c.want {
			t.Errorf("ReplaceArray(%q, %v, %q) = %q, want %q", c.search, c.replace, c.subject, got, c.want)
		}
	}
}

func TestReplaceEnds(t *testing.T) {
	if got := str.ReplaceFirst("a", "X", "banana"); got != "bXnana" {
		t.Errorf("ReplaceFirst = %q", got)
	}
	if got := str.ReplaceLast("a", "X", "banana"); got != "bananX" {
		t.Errorf("ReplaceLast = %q", got)
	}
	if got := str.ReplaceStart("b", "X", "banana"); got != "Xanana" {
		t.Errorf("ReplaceStart = %q", got)
	}
	if got := str.ReplaceStart("a", "X", "banana"); got != "banana" {
		t.Errorf("ReplaceStart of a needle that does not open the subject = %q", got)
	}
	if got := str.ReplaceEnd("a", "X", "banana"); got != "bananX" {
		t.Errorf("ReplaceEnd = %q", got)
	}
	if got := str.ReplaceEnd("b", "X", "banana"); got != "banana" {
		t.Errorf("ReplaceEnd of a needle that does not close the subject = %q", got)
	}
	for _, empty := range []string{""} {
		if got := str.ReplaceFirst(empty, "X", "banana"); got != "banana" {
			t.Errorf("ReplaceFirst with an empty search = %q", got)
		}
	}
}

func TestSwapAppliesEveryPairInOnePass(t *testing.T) {
	if got := str.Swap(map[string]string{"a": "b", "b": "a"}, "ab"); got != "ba" {
		t.Errorf("Swap = %q, want %q", got, "ba")
	}
	if got := str.Swap(map[string]string{"ab": "X", "a": "Y"}, "ab"); got != "X" {
		t.Errorf("Swap did not prefer the longest key: %q", got)
	}
	if got := str.Swap(map[string]string{"": "X"}, "ab"); got != "ab" {
		t.Errorf("Swap with an empty key = %q", got)
	}
	if got := str.Swap(nil, "ab"); got != "ab" {
		t.Errorf("Swap with no pairs = %q", got)
	}
}

func TestRepeatRefusesANegativeCount(t *testing.T) {
	if got := str.Repeat("ab", -1); got != "" {
		t.Errorf("Repeat(%q, -1) = %q, want the empty string", "ab", got)
	}
	if got := str.Repeat("ab", 0); got != "" {
		t.Errorf("Repeat(%q, 0) = %q, want the empty string", "ab", got)
	}
	if got := str.Repeat("ab", 3); got != "ababab" {
		t.Errorf("Repeat(%q, 3) = %q", "ab", got)
	}
}

func TestWrapAndUnwrap(t *testing.T) {
	if got := str.Wrap("name", `"`); got != `"name"` {
		t.Errorf("Wrap = %q", got)
	}
	if got := str.Wrap("name", "[", "]"); got != "[name]" {
		t.Errorf("Wrap = %q", got)
	}
	if got := str.Unwrap(`"name"`, `"`); got != "name" {
		t.Errorf("Unwrap = %q", got)
	}
	if got := str.Unwrap("name", "[", "]"); got != "name" {
		t.Errorf("Unwrap of a string that is not wrapped = %q", got)
	}
	if got := str.Unwrap("", "["); got != "" {
		t.Errorf("Unwrap of the empty string = %q", got)
	}
}

func TestDeduplicate(t *testing.T) {
	cases := []struct {
		value     string
		character []string
		want      string
	}{
		{"the   space", nil, "the space"},
		{"the---dash", []string{"-"}, "the-dash"},
		{"a---b", []string{"--"}, "a--b"},
		{"a", []string{""}, "a"},
		{"", nil, ""},
	}
	for _, c := range cases {
		if got := str.Deduplicate(c.value, c.character...); got != c.want {
			t.Errorf("Deduplicate(%q, %v) = %q, want %q", c.value, c.character, got, c.want)
		}
	}
}

func TestBase64RoundTrip(t *testing.T) {
	if got := str.ToBase64("laravel"); got != "bGFyYXZlbA==" {
		t.Errorf("ToBase64 = %q", got)
	}
	if got, ok := str.FromBase64("bGFyYXZlbA==", true); !ok || got != "laravel" {
		t.Errorf("FromBase64 = %q, %v", got, ok)
	}
	if got, ok := str.FromBase64("", true); !ok || got != "" {
		t.Errorf("FromBase64 of the empty string = %q, %v", got, ok)
	}
	if _, ok := str.FromBase64("!!!", true); ok {
		t.Error("FromBase64 accepted a character outside the alphabet in strict mode")
	}
	if got, ok := str.FromBase64("bGFy YXZl bA==", false); !ok || got != "laravel" {
		t.Errorf("FromBase64 with whitespace = %q, %v", got, ok)
	}
}

func TestValidators(t *testing.T) {
	if !str.IsJSON(`{"a":1}`) || !str.IsJSON("[]") || !str.IsJSON("1") {
		t.Error("IsJSON refused valid JSON")
	}
	if str.IsJSON("") || str.IsJSON("{") || str.IsJSON("not json") {
		t.Error("IsJSON accepted something that is not JSON")
	}
	if !str.IsASCII("laravel") || str.IsASCII("café") {
		t.Error("IsASCII is wrong")
	}
	if str.IsASCII("") != true {
		t.Error("IsASCII of the empty string should hold, since no byte is out of range")
	}
	if !str.IsURL("https://example.com") || str.IsURL("not a url") || str.IsURL("") {
		t.Error("IsURL is wrong")
	}
	if !str.IsURL("http://example.com", "http") {
		t.Error("IsURL refused a protocol it was given")
	}
	if str.IsURL("ftp://example.com", "http") {
		t.Error("IsURL accepted a protocol it was not given")
	}
}

func TestParseCallback(t *testing.T) {
	if before, after := str.ParseCallback("Class@method", "handle"); before != "Class" || after != "method" {
		t.Errorf("ParseCallback = %q, %q", before, after)
	}
	if before, after := str.ParseCallback("Class", "handle"); before != "Class" || after != "handle" {
		t.Errorf("ParseCallback with no at sign = %q, %q", before, after)
	}
	if before, after := str.ParseCallback("", ""); before != "" || after != "" {
		t.Errorf("ParseCallback of the empty string = %q, %q", before, after)
	}
}

func TestNumbersKeepsTheDigits(t *testing.T) {
	if got := str.Numbers("(11) 98765-4321"); got != "11987654321" {
		t.Errorf("Numbers = %q", got)
	}
	if got := str.Numbers("no digits"); got != "" {
		t.Errorf("Numbers of a string with none = %q", got)
	}
}

func TestSubstrCountAndReplace(t *testing.T) {
	if got := str.SubstrCount("laravel laravel", "la", 0); got != 2 {
		t.Errorf("SubstrCount = %d, want 2", got)
	}
	if got := str.SubstrCount("laravel laravel", "la", 8); got != 1 {
		t.Errorf("SubstrCount with an offset = %d, want 1", got)
	}
	if got := str.SubstrCount("laravel", "", 0); got != 0 {
		t.Errorf("SubstrCount with an empty needle = %d, want 0", got)
	}
	if got := str.SubstrCount("laravel", "la", 100); got != 0 {
		t.Errorf("SubstrCount with an offset past the end = %d, want 0", got)
	}
	if got := str.SubstrReplace("1300", ":", 2); got != "13:" {
		t.Errorf("SubstrReplace = %q", got)
	}
	if got := str.SubstrReplace("1300", ":", 2, 0); got != "13:00" {
		t.Errorf("SubstrReplace with a length of zero = %q", got)
	}
	if got := str.SubstrReplace("1300", ":", -2, 1); got != "13:0" {
		t.Errorf("SubstrReplace with a negative offset = %q", got)
	}
}
