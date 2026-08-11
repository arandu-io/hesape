package str_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/str"
)

func TestOfChains(t *testing.T) {
	got := str.Of("  Purchase Order  ").Trim().Snake("_").ToString()
	if got != "purchase_order" {
		t.Errorf("chain = %q, want %q", got, "purchase_order")
	}
	if got := str.Of("welcome_email").Studly().Append("Job").String(); got != "WelcomeEmailJob" {
		t.Errorf("chain = %q, want %q", got, "WelcomeEmailJob")
	}
	if got := str.Of("Laravel").Prepend("Hello ", "dear ").Value(); got != "Hello dear Laravel" {
		t.Errorf("Prepend = %q, want %q", got, "Hello dear Laravel")
	}
}

// TestAStringableIsAValue records that a chain never reaches back: the
// Stringable a caller holds is the one it keeps, whatever anyone does with it
// afterwards.
func TestAStringableIsAValue(t *testing.T) {
	original := str.Of("laravel")
	upper := original.Upper()
	if original.ToString() != "laravel" {
		t.Errorf("the original became %q", original.ToString())
	}
	if upper.ToString() != "LARAVEL" {
		t.Errorf("the chain produced %q, want %q", upper.ToString(), "LARAVEL")
	}
}

func TestStringableTerminals(t *testing.T) {
	s := str.Of("Laravel Framework")

	if got := s.Length(); got != 17 {
		t.Errorf("Length() = %d, want 17", got)
	}
	if !s.Contains([]string{"Frame"}, false) {
		t.Error("Contains(Frame) = false")
	}
	if !s.ContainsAll([]string{"Laravel", "Framework"}, false) {
		t.Error("ContainsAll = false")
	}
	if s.DoesntContain([]string{"Laravel"}, false) {
		t.Error("DoesntContain(Laravel) = true")
	}
	if !s.StartsWith("Laravel") || !s.EndsWith("Framework") {
		t.Error("StartsWith or EndsWith = false")
	}
	if s.DoesntStartWith("Laravel") || s.DoesntEndWith("Framework") {
		t.Error("DoesntStartWith or DoesntEndWith = true")
	}
	if !s.Exactly("Laravel Framework") {
		t.Error("Exactly = false")
	}
	if s.IsEmpty() || !s.IsNotEmpty() {
		t.Error("IsEmpty or IsNotEmpty is wrong")
	}
	if got := s.WordCount(); got != 2 {
		t.Errorf("WordCount() = %d, want 2", got)
	}
	if got := s.Position("Frame", 0); got != 8 {
		t.Errorf("Position(Frame) = %d, want 8", got)
	}
	if c, ok := s.CharAt(0); !ok || c != "L" {
		t.Errorf("CharAt(0) = %q, %v, want L, true", c, ok)
	}
	if _, ok := s.CharAt(100); ok {
		t.Error("CharAt(100) reported an index that is not there")
	}
	if got := str.Of("EmailNotificationSent").Ucsplit(); strings.Join(got, "|") != "Email|Notification|Sent" {
		t.Errorf("Ucsplit() = %v", got)
	}
}

func TestStringableExplodeAndSplit(t *testing.T) {
	cases := []struct {
		limit []int
		want  string
	}{
		{nil, "a|b|c|d"},
		{[]int{2}, "a|b,c,d"},
		{[]int{-1}, "a|b|c"},
		{[]int{-10}, ""},
		{[]int{0}, "a,b,c,d"},
	}
	for _, c := range cases {
		got := strings.Join(str.Of("a,b,c,d").Explode(",", c.limit...), "|")
		if got != c.want {
			t.Errorf("Explode(\",\", %v) = %q, want %q", c.limit, got, c.want)
		}
	}

	if got := str.Of("abcdef").Split(2); strings.Join(got, "|") != "ab|cd|ef" {
		t.Errorf("Split(2) = %v", got)
	}
	if got := str.Of("a1b2c").Split(regexp.MustCompile(`\d`)); strings.Join(got, "|") != "a|b|c" {
		t.Errorf("Split(regexp) = %v", got)
	}
	if got := str.Of("a1b2c").Split(`\d`, 2); strings.Join(got, "|") != "a|b2c" {
		t.Errorf("Split(pattern, 2) = %v", got)
	}
}

func TestStringableScan(t *testing.T) {
	if got := str.Of("filename.jpg").Scan("%[^.].%s"); len(got) != 0 {
		// An unsupported verb ends the scan, which is the shape sscanf has for
		// a format it cannot read.
		t.Logf("Scan with an unsupported verb returned %v", got)
	}
	got := str.Of("Paulo Lima 42").Scan("%s %s %d")
	if strings.Join(got, "|") != "Paulo|Lima|42" {
		t.Errorf("Scan = %v, want [Paulo Lima 42]", got)
	}
	if got := str.Of("age: 30").Scan("age: %d"); strings.Join(got, "|") != "30" {
		t.Errorf("Scan = %v, want [30]", got)
	}
	if got := str.Of("nope").Scan("age: %d"); len(got) != 0 {
		t.Errorf("Scan of a string that does not match = %v, want nothing", got)
	}
}

func TestStringableConversions(t *testing.T) {
	if got := str.Of("42abc").ToInteger(); got != 42 {
		t.Errorf("ToInteger() = %d, want 42", got)
	}
	if got := str.Of("abc").ToInteger(); got != 0 {
		t.Errorf("ToInteger() of a word = %d, want 0", got)
	}
	if got := str.Of("ff").ToInteger(16); got != 255 {
		t.Errorf("ToInteger(16) = %d, want 255", got)
	}
	if got := str.Of("-1.5e2xyz").ToFloat(); got != -150 {
		t.Errorf("ToFloat() = %v, want -150", got)
	}
	if got := str.Of("").ToFloat(); got != 0 {
		t.Errorf("ToFloat() of the empty string = %v, want 0", got)
	}
	for _, yes := range []string{"1", "true", "TRUE", "on", "yes", " yes "} {
		if !str.Of(yes).ToBoolean() {
			t.Errorf("ToBoolean(%q) = false", yes)
		}
	}
	for _, no := range []string{"0", "false", "off", "no", "", "maybe"} {
		if str.Of(no).ToBoolean() {
			t.Errorf("ToBoolean(%q) = true", no)
		}
	}
	if got := str.Of("laravel").ToBase64().FromBase64(true).ToString(); got != "laravel" {
		t.Errorf("base64 round trip = %q, want %q", got, "laravel")
	}
}

func TestStringableToDate(t *testing.T) {
	got, err := str.Of("2026-08-11 09:30:00").ToDate()
	if err != nil {
		t.Fatalf("ToDate() returned %v", err)
	}
	if !got.Equal(time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("ToDate() = %v", got)
	}

	got, err = str.Of("11/08/2026").ToDate("d/m/Y")
	if err != nil {
		t.Fatalf("ToDate(d/m/Y) returned %v", err)
	}
	if !got.Equal(time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ToDate(d/m/Y) = %v", got)
	}
	if _, err := str.Of("not a date").ToDate(); err == nil {
		t.Error("ToDate() of a word returned no error")
	}
	if _, err := str.Of("2026").ToDate("Q"); err == nil {
		t.Error("ToDate with an unknown format letter returned no error")
	}
}

func TestStringablePathHelpers(t *testing.T) {
	cases := []struct {
		value  string
		suffix []string
		want   string
	}{
		{"/var/www/app.go", nil, "app.go"},
		{"/var/www/app.go", []string{".go"}, "app"},
		{"/var/www/", nil, "www"},
		{"app.go", nil, "app.go"},
		{"", nil, ""},
		{".go", []string{".go"}, ".go"},
	}
	for _, c := range cases {
		if got := str.Of(c.value).Basename(c.suffix...).ToString(); got != c.want {
			t.Errorf("Basename(%q, %v) = %q, want %q", c.value, c.suffix, got, c.want)
		}
	}

	if got := str.Of("/var/www/app.go").Dirname().ToString(); got != "/var/www" {
		t.Errorf("Dirname() = %q, want %q", got, "/var/www")
	}
	if got := str.Of("/var/www/app.go").Dirname(2).ToString(); got != "/var" {
		t.Errorf("Dirname(2) = %q, want %q", got, "/var")
	}
	if got := str.Of("app.go").Dirname().ToString(); got != "." {
		t.Errorf("Dirname() of a bare name = %q, want %q", got, ".")
	}
	if got := str.Of(`App\Models\User`).ClassBasename().ToString(); got != "User" {
		t.Errorf("ClassBasename() = %q, want %q", got, "User")
	}
}

func TestStringableConditionals(t *testing.T) {
	upper := func(s str.Stringable) str.Stringable { return s.Upper() }
	lower := func(s str.Stringable) str.Stringable { return s.Lower() }

	if got := str.Of("laravel").When(true, upper).ToString(); got != "LARAVEL" {
		t.Errorf("When(true) = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("LARAVEL").When(false, upper, lower).ToString(); got != "laravel" {
		t.Errorf("When(false) with a default = %q, want %q", got, "laravel")
	}
	if got := str.Of("laravel").When(false, upper).ToString(); got != "laravel" {
		t.Errorf("When(false) with no default = %q, want the subject", got)
	}
	if got := str.Of("laravel").Unless(false, upper).ToString(); got != "LARAVEL" {
		t.Errorf("Unless(false) = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenContains([]string{"rav"}, upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenContains = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("").WhenEmpty(func(s str.Stringable) str.Stringable { return str.Of("filled") }).ToString(); got != "filled" {
		t.Errorf("WhenEmpty = %q, want %q", got, "filled")
	}
	if got := str.Of("laravel").WhenNotEmpty(upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenNotEmpty = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenStartsWith([]string{"lar"}, upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenStartsWith = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenEndsWith([]string{"vel"}, upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenEndsWith = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenDoesntStartWith([]string{"sym"}, upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenDoesntStartWith = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenExactly("laravel", upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenExactly = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenNotExactly("symfony", upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenNotExactly = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenIs([]string{"lara*"}, upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenIs = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of("laravel").WhenIsASCII(upper).ToString(); got != "LARAVEL" {
		t.Errorf("WhenIsASCII = %q, want %q", got, "LARAVEL")
	}
	if got := str.Of(str.UUID()).WhenIsUUID(func(s str.Stringable) str.Stringable { return str.Of("uuid") }).ToString(); got != "uuid" {
		t.Errorf("WhenIsUUID did not fire")
	}
	if got := str.Of(str.ULID()).WhenIsULID(func(s str.Stringable) str.Stringable { return str.Of("ulid") }).ToString(); got != "ulid" {
		t.Errorf("WhenIsULID did not fire")
	}
	if got := str.Of("abc123").WhenTest([]*regexp.Regexp{regexp.MustCompile(`\d`)}, upper).ToString(); got != "ABC123" {
		t.Errorf("WhenTest = %q, want %q", got, "ABC123")
	}

	tapped := ""
	str.Of("laravel").Tap(func(s str.Stringable) { tapped = s.ToString() })
	if tapped != "laravel" {
		t.Errorf("Tap saw %q, want %q", tapped, "laravel")
	}
	if got := str.Of("laravel").Pipe(func(s str.Stringable) string { return s.ToString() + "!" }).ToString(); got != "laravel!" {
		t.Errorf("Pipe = %q, want %q", got, "laravel!")
	}
}

// TestEveryStringableMethodForwardsToItsFunction spot-checks that the chain and
// the function agree, because a Stringable that quietly does something else is
// the failure this file exists to catch.
func TestEveryStringableMethodForwardsToItsFunction(t *testing.T) {
	subject := "Purchase Order 12"
	cases := []struct {
		name     string
		chained  string
		function string
	}{
		{"Camel", str.Of(subject).Camel().ToString(), str.Camel(subject)},
		{"Kebab", str.Of(subject).Kebab().ToString(), str.Kebab(subject)},
		{"Snake", str.Of(subject).Snake("_").ToString(), str.Snake(subject, "_")},
		{"Studly", str.Of(subject).Studly().ToString(), str.Studly(subject)},
		{"Pascal", str.Of(subject).Pascal().ToString(), str.Pascal(subject)},
		{"Title", str.Of(subject).Title().ToString(), str.Title(subject)},
		{"Headline", str.Of(subject).Headline().ToString(), str.Headline(subject)},
		{"Apa", str.Of(subject).Apa().ToString(), str.Apa(subject)},
		{"Slug", str.Of(subject).Slug("-").ToString(), str.Slug(subject, "-")},
		{"Squish", str.Of(subject).Squish().ToString(), str.Squish(subject)},
		{"Numbers", str.Of(subject).Numbers().ToString(), str.Numbers(subject)},
		{"Reverse", str.Of(subject).Reverse().ToString(), str.Reverse(subject)},
		{"Lower", str.Of(subject).Lower().ToString(), str.Lower(subject)},
		{"Upper", str.Of(subject).Upper().ToString(), str.Upper(subject)},
		{"Ucfirst", str.Of(subject).Ucfirst().ToString(), str.Ucfirst(subject)},
		{"Lcfirst", str.Of(subject).Lcfirst().ToString(), str.Lcfirst(subject)},
		{"Plural", str.Of(subject).Plural().ToString(), str.Plural(subject)},
		{"Singular", str.Of(subject).Singular().ToString(), str.Singular(subject)},
		{"Counted", str.Of("rule").Counted(3).ToString(), str.Counted("rule", 3)},
		{"Take", str.Of(subject).Take(4).ToString(), str.Take(subject, 4)},
		{"Substr", str.Of(subject).Substr(2, 5).ToString(), str.Substr(subject, 2, 5)},
		{"Mask", str.Of(subject).Mask("*", 2, 4).ToString(), str.Mask(subject, "*", 2, 4)},
		{"Limit", str.Of(subject).Limit(5, "...", false).ToString(), str.Limit(subject, 5, "...", false)},
		{"Words", str.Of(subject).Words(2, "...").ToString(), str.Words(subject, 2, "...")},
		{"PadLeft", str.Of(subject).PadLeft(30, "_").ToString(), str.PadLeft(subject, 30, "_")},
		{"PadRight", str.Of(subject).PadRight(30, "_").ToString(), str.PadRight(subject, 30, "_")},
		{"PadBoth", str.Of(subject).PadBoth(30, "_").ToString(), str.PadBoth(subject, 30, "_")},
		{"Start", str.Of(subject).Start("/").ToString(), str.Start(subject, "/")},
		{"Finish", str.Of(subject).Finish("/").ToString(), str.Finish(subject, "/")},
		{"After", str.Of(subject).After(" ").ToString(), str.After(subject, " ")},
		{"AfterLast", str.Of(subject).AfterLast(" ").ToString(), str.AfterLast(subject, " ")},
		{"Before", str.Of(subject).Before(" ").ToString(), str.Before(subject, " ")},
		{"BeforeLast", str.Of(subject).BeforeLast(" ").ToString(), str.BeforeLast(subject, " ")},
		{"Between", str.Of(subject).Between("P", "2").ToString(), str.Between(subject, "P", "2")},
		{"BetweenFirst", str.Of(subject).BetweenFirst("P", "2").ToString(), str.BetweenFirst(subject, "P", "2")},
		{"ChopStart", str.Of(subject).ChopStart("Purchase ").ToString(), str.ChopStart(subject, "Purchase ")},
		{"ChopEnd", str.Of(subject).ChopEnd("12").ToString(), str.ChopEnd(subject, "12")},
		{"Trim", str.Of(subject).Trim().ToString(), str.Trim(subject)},
		{"Ltrim", str.Of(subject).Ltrim().ToString(), str.Ltrim(subject)},
		{"Rtrim", str.Of(subject).Rtrim().ToString(), str.Rtrim(subject)},
		{"Repeat", str.Of(subject).Repeat(2).ToString(), str.Repeat(subject, 2)},
		{"Wrap", str.Of(subject).Wrap("[", "]").ToString(), str.Wrap(subject, "[", "]")},
		{"Unwrap", str.Of(subject).Unwrap("[", "]").ToString(), str.Unwrap(subject, "[", "]")},
		{"Deduplicate", str.Of(subject).Deduplicate().ToString(), str.Deduplicate(subject)},
		{"ToBase64", str.Of(subject).ToBase64().ToString(), str.ToBase64(subject)},
		{"ASCII", str.Of("Ação").ASCII().ToString(), str.ASCII("Ação")},
		{"Transliterate", str.Of("Ação").Transliterate("?", false).ToString(), str.Transliterate("Ação", "?", false)},
		{"WordWrap", str.Of(subject).WordWrap(5, "\n", false).ToString(), str.WordWrap(subject, 5, "\n", false)},
		{"StripTags", str.Of("<b>hi</b>").StripTags().ToString(), "hi"},
		{"Initials", str.Of("Paulo Ricardo Lima").Initials(true).ToString(), str.Initials("Paulo Ricardo Lima", true)},
		{"Ucwords", str.Of("hello world").Ucwords().ToString(), str.Ucwords("hello world")},
		{"PluralStudly", str.Of("UserGroup").PluralStudly().ToString(), str.PluralStudly("UserGroup")},
		{"PluralPascal", str.Of("UserGroup").PluralPascal().ToString(), str.PluralPascal("UserGroup")},
		{"Replace", str.Of(subject).Replace([]string{"Order"}, []string{"Invoice"}, true).ToString(), str.Replace([]string{"Order"}, []string{"Invoice"}, subject, true)},
		{"ReplaceArray", str.Of("? and ?").ReplaceArray("?", []string{"a", "b"}).ToString(), str.ReplaceArray("?", []string{"a", "b"}, "? and ?")},
		{"ReplaceFirst", str.Of(subject).ReplaceFirst("r", "R").ToString(), str.ReplaceFirst("r", "R", subject)},
		{"ReplaceLast", str.Of(subject).ReplaceLast("r", "R").ToString(), str.ReplaceLast("r", "R", subject)},
		{"ReplaceStart", str.Of(subject).ReplaceStart("P", "p").ToString(), str.ReplaceStart("P", "p", subject)},
		{"ReplaceEnd", str.Of(subject).ReplaceEnd("12", "13").ToString(), str.ReplaceEnd("12", "13", subject)},
		{"Remove", str.Of(subject).Remove([]string{" "}, true).ToString(), str.Remove([]string{" "}, subject, true)},
		{"Swap", str.Of(subject).Swap(map[string]string{"Order": "Invoice"}).ToString(), str.Swap(map[string]string{"Order": "Invoice"}, subject)},
		{"SubstrReplace", str.Of(subject).SubstrReplace("X", 0, 8).ToString(), str.SubstrReplace(subject, "X", 0, 8)},
		{"ConvertCase", str.Of(subject).ConvertCase(str.CaseUpper).ToString(), str.ConvertCase(subject, str.CaseUpper)},
		{"Markdown", str.Of("# hi").Markdown().ToString(), str.Markdown("# hi")},
		{"InlineMarkdown", str.Of("*hi*").InlineMarkdown().ToString(), str.InlineMarkdown("*hi*")},
		{"NewLine", str.Of(subject).NewLine(2).ToString(), subject + "\n\n"},
	}
	for _, c := range cases {
		if c.chained != c.function {
			t.Errorf("Stringable.%s = %q, but the function gives %q", c.name, c.chained, c.function)
		}
	}

	pattern := regexp.MustCompile(`\d+`)
	if got, want := str.Of(subject).Match(pattern).ToString(), str.Match(pattern, subject); got != want {
		t.Errorf("Stringable.Match = %q, want %q", got, want)
	}
	if got, want := len(str.Of(subject).MatchAll(pattern)), len(str.MatchAll(pattern, subject)); got != want {
		t.Errorf("Stringable.MatchAll returned %d, want %d", got, want)
	}
	if got, want := str.Of(subject).ReplaceMatches(pattern, "N", -1).ToString(), str.ReplaceMatches(pattern, "N", subject, -1); got != want {
		t.Errorf("Stringable.ReplaceMatches = %q, want %q", got, want)
	}
	if got, want := str.Of(subject).SubstrCount("r", 0), str.SubstrCount(subject, "r", 0); got != want {
		t.Errorf("Stringable.SubstrCount = %d, want %d", got, want)
	}
	before, after := str.Of("Class@method").ParseCallback("")
	if before != "Class" || after != "method" {
		t.Errorf("Stringable.ParseCallback = %q, %q", before, after)
	}
	excerpt, ok := str.Of("This is my name").Excerpt("my", 3, "...")
	if !ok || excerpt != "...is my na..." {
		t.Errorf("Stringable.Excerpt = %q, %v", excerpt, ok)
	}
}
