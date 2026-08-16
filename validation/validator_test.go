package validation_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/validation"
)

func TestRequired(t *testing.T) {
	e := validation.Errors{}
	validation.Required(e, "name", "  ")
	validation.Required(e, "email", "someone@example.com")

	if len(e["name"]) != 1 {
		t.Errorf("a blank value must fail: %v", e)
	}
	if len(e["email"]) != 0 {
		t.Errorf("a filled value must pass: %v", e)
	}
}

// TestLengthCountsRunes: a limit measured in bytes rejects valid input in every
// language that needs more than one byte per character.
func TestLengthCountsRunes(t *testing.T) {
	e := validation.Errors{}
	validation.MinLen(e, "name", "José", 4)
	validation.MaxLen(e, "city", "São Paulo", 9)

	if e.Any() {
		t.Fatalf("accented input was rejected by a byte based limit: %v", e)
	}
}

func TestMinAndMaxLen(t *testing.T) {
	e := validation.Errors{}
	validation.MinLen(e, "short", "ab", 3)
	validation.MaxLen(e, "long", "abcd", 3)

	if len(e["short"]) != 1 || len(e["long"]) != 1 {
		t.Fatalf("errors = %v, want one per field", e)
	}
	if !strings.Contains(e["short"][0], "at least 3") {
		t.Errorf("the message must state the limit, got %q", e["short"][0])
	}
}

func TestEmail(t *testing.T) {
	valid := []string{"a@b.co", "someone.else@sub.example.com"}
	invalid := []string{"", "a", "@b.co", "a@", "a@b", "a b@c.co "}

	for _, v := range valid {
		e := validation.Errors{}
		validation.Email(e, "email", v)
		if e.Any() {
			t.Errorf("%q was rejected", v)
		}
	}
	for _, v := range invalid {
		e := validation.Errors{}
		validation.Email(e, "email", v)
		if !e.Any() {
			t.Errorf("%q was accepted", v)
		}
	}
}

func TestAddOnNilMapDoesNotPanic(t *testing.T) {
	var e validation.Errors

	e.Add("field", "message") // a caller that forgot to initialize must not crash a request

	if e.Any() {
		t.Fatal("a nil map must stay empty")
	}
}

// TestErrorMessageIsStable: fields come out sorted, so logs and golden files do
// not change between runs of the same input.
func TestErrorMessageIsStable(t *testing.T) {
	e := validation.Errors{}
	e.Add("zeta", "last")
	e.Add("alpha", "first")
	e.Add("alpha", "also first")

	got := e.Error()
	want := "validation failed: alpha (first, also first); zeta (last)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	for range 20 {
		if e.Error() != want {
			t.Fatal("Error() is not deterministic across calls")
		}
	}
}

// TestErrorsSatisfiesError lets a service return validation failures as a plain
// error, which is what keeps handlers free of type switches.
func TestErrorsSatisfiesError(t *testing.T) {
	var err error = validation.Errors{"email": {"is required"}}

	if !strings.Contains(err.Error(), "email") {
		t.Fatalf("Error() = %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Three defects, each with the input that proved it. Every test below failed
// before the fix and passes after it.
// ---------------------------------------------------------------------------

// TestWildcardDependentRuleReplacesAsterisksInItsParameters is the input the
// audit ran, and it failed OPEN: the rule set said foo.*.bar is required
// whenever foo.*.baz was sent, the request sent foo.0.baz, and nothing failed.
//
// Two steps of Validator::validateAttribute were missing. The attribute was
// never expanded against the data, so the rule ran against the literal key
// "foo.*.bar"; and the parameter was handed to required_with as the literal
// "foo.*.baz", which names nothing a request can hold. The whole of
// $dependentRules -- the family that decides whether a field is required -- was
// inert on any set written with a wildcard.
func TestWildcardDependentRuleReplacesAsterisksInItsParameters(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"foo.*.bar": "required_with:foo.*.baz"})

	v := validation.Make(validation.Data{"foo": []any{map[string]any{"baz": "x"}}}, set)
	if v.Passes() {
		t.Fatalf("foo.0.baz was sent and foo.0.bar was not, and the set passed")
	}
	if got := v.Errors().Get("foo.0.bar"); len(got) != 1 {
		t.Fatalf("errors = %v, want one message on foo.0.bar", v.Errors())
	}

	// The other half of the rule: with the sibling absent, nothing is required.
	absent := validation.Make(validation.Data{"foo": []any{map[string]any{"other": "x"}}}, set)
	if !absent.Passes() {
		t.Fatalf("foo.0.baz was not sent and foo.0.bar was demanded anyway: %v", absent.Errors())
	}

	// And the member that did send both passes while its sibling fails, which
	// is what expanding per member is for.
	mixed := validation.Make(validation.Data{"foo": []any{
		map[string]any{"baz": "x", "bar": "here"},
		map[string]any{"baz": "x"},
	}}, set)
	if mixed.Passes() {
		t.Fatal("the second member is missing bar and the set passed")
	}
	if got := mixed.Errors().Get("foo.0.bar"); len(got) != 0 {
		t.Errorf("the member that sent bar failed: %v", got)
	}
	if got := mixed.Errors().Get("foo.1.bar"); len(got) != 1 {
		t.Errorf("errors = %v, want one message on foo.1.bar", mixed.Errors())
	}
}

// TestWildcardFieldsAreExpandedAgainstTheData: a failure is reported under the
// key the request actually carries, not under the pattern that was written.
// "foo.*.bar is required" names nothing anybody can fix.
func TestWildcardFieldsAreExpandedAgainstTheData(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"items.*.price": "required|numeric"})

	v := validation.Make(validation.Data{"items": []any{
		validation.Data{"price": "10"},
		validation.Data{"price": "abc"},
	}}, set)

	if v.Passes() {
		t.Fatal("a non-numeric price passed")
	}
	if got := v.Errors().Get("items.1.price"); len(got) != 1 {
		t.Fatalf("errors = %v, want one message on items.1.price", v.Errors())
	}
	if got := v.Errors().Get("items.*.price"); len(got) != 0 {
		t.Errorf("a failure was reported under the pattern rather than the key: %v", got)
	}
}

// TestSizeComparesExactlyRatherThanInFloat64 is the audit's second input: two
// whole numbers a float64 cannot tell apart, one of them over the limit.
//
// GetSize compares at arbitrary precision. Comparing in float64 instead makes
// 9007199254740993 and 9007199254740992 the same number -- so a monetary or
// quota limit is passable by one unit of rounding.
func TestSizeComparesExactlyRatherThanInFloat64(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"amount": "numeric|max:9007199254740992"})

	over := validation.Make(validation.Data{"amount": "9007199254740993"}, set)
	if over.Passes() {
		t.Fatal("9007199254740993 passed max:9007199254740992 -- the comparison is still in float64")
	}

	at := validation.Make(validation.Data{"amount": "9007199254740992"}, set)
	if !at.Passes() {
		t.Fatalf("the limit itself was rejected: %v", at.Errors())
	}

	// The same in the other direction, so that min is not fixed by accident.
	low := validation.MustCompile(validation.Rules{"amount": "numeric|min:9007199254740993"})
	if under := validation.Make(validation.Data{"amount": "9007199254740992"}, low); under.Passes() {
		t.Fatal("9007199254740992 passed min:9007199254740993")
	}
}

// TestDateFormatTakesSeveralLayoutsAndReadsANumericValue is the third input. The
// rule walks every layout it was given and passes when any of them matches, and
// it reads a numeric value as the text it prints as.
//
// Taking at most one layout made the second one a boot failure, and asking only
// for text rejected a JSON body that sent 20240301 without quotes.
func TestDateFormatTakesSeveralLayoutsAndReadsANumericValue(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"d": "date_format:2006-01-02,2006-01-02 15:04:05"})

	for _, value := range []string{"2026-03-01", "2026-03-01 10:30:00"} {
		v := validation.Make(validation.Data{"d": value}, set)
		if !v.Passes() {
			t.Errorf("%q matched neither layout: %v", value, v.Errors())
		}
	}
	if v := validation.Make(validation.Data{"d": "01/03/2026"}, set); v.Passes() {
		t.Error("a value matching neither layout passed")
	}

	numeric := validation.MustCompile(validation.Rules{"d": "date_format:20060102"})
	if v := validation.Make(validation.Data{"d": 20260301}, numeric); !v.Passes() {
		t.Errorf("a JSON body that sent the date unquoted was rejected: %v", v.Errors())
	}
}

// TestAnUploadThatDidNotFinishFailsAsUploaded is the same method's other missing
// branch. A file rule or an implicit rule reports `uploaded` when the upload
// itself did not complete -- the file was too large for the server, and nothing
// was written. Without that branch a truncated upload is reported as whatever
// the next rule happens to think of the empty file.
func TestAnUploadThatDidNotFinishFailsAsUploaded(t *testing.T) {
	set := validation.MustCompile(validation.Rules{"avatar": "required|image|max:100"})

	v := validation.Make(validation.Data{"avatar": truncatedUpload{}}, set)

	if v.Passes() {
		t.Fatal("an upload that did not finish passed")
	}
	if got := v.Failed()["avatar"]; len(got) == 0 || got[0] != "uploaded" {
		t.Fatalf("failed rules = %v, want uploaded first", got)
	}
	if got := v.Errors().First("avatar"); got != "failed to upload" {
		t.Errorf("message = %q, want the sentence for uploaded", got)
	}
}

// truncatedUpload is an upload the server never finished writing, which is the
// only state `uploaded` exists to report.
type truncatedUpload struct{}

func (truncatedUpload) GetPath() string                    { return "" }
func (truncatedUpload) GetRealPath() string                { return "" }
func (truncatedUpload) GetSize() int64                     { return 0 }
func (truncatedUpload) GetMimeType() string                { return "" }
func (truncatedUpload) GetExtension() string               { return "" }
func (truncatedUpload) GuessExtension() string             { return "" }
func (truncatedUpload) GetClientOriginalExtension() string { return "" }
func (truncatedUpload) IsValid() bool                      { return false }
