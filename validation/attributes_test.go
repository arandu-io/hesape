package validation_test

import (
	"context"
	"net"
	"net/url"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/validation"
)

// ---------------------------------------------------------------------------
// The doubles. Every rule that leaves the process takes what it needs through
// an option, which is what lets these tests make no network call and open no
// connection.
// ---------------------------------------------------------------------------

// grantFor is the Grant a database rule runs under.
//
// It is a real auth.Grant and not a double: auth.Grant has unexported fields,
// so a test cannot invent one either, and SystemGrant is the door a job or a
// task comes through. The tenant is what the assertions below follow.
func grantFor(tenant string) auth.Grant {
	return auth.SystemGrant("validation.read", tenant)
}

// verifier is a PresenceVerifier over a table of rows already scoped by tenant,
// and it records the tenant it was asked for -- the scoping is only kept if the
// Grant actually reaches the query.
type verifier struct {
	rows    map[string][]string // tenant -> values held in the column
	asked   []string            // the tenants the rule asked about
	exclude any
	column  string
	extra   map[string]string
}

func (v *verifier) GetCount(ctx context.Context, g auth.Grant, collection, column string, value any, excludeID any, idColumn string, extra map[string]string) (int, error) {
	v.asked = append(v.asked, auth.Tenant(g))
	v.exclude, v.column, v.extra = excludeID, column, extra
	n := 0
	for _, held := range v.rows[auth.Tenant(g)] {
		if held == value.(string) {
			n++
		}
	}
	if excludeID != nil {
		n--
	}
	return max(n, 0), nil
}

func (v *verifier) GetMultiCount(ctx context.Context, g auth.Grant, collection, column string, values []any, extra map[string]string) (int, error) {
	v.asked = append(v.asked, auth.Tenant(g))
	n := 0
	for _, want := range values {
		for _, held := range v.rows[auth.Tenant(g)] {
			if held == want.(string) {
				n++
				break
			}
		}
	}
	return n, nil
}

type passwordChecker struct{ right string }

func (p passwordChecker) CheckCurrentPassword(ctx context.Context, g auth.Grant, guard, password string) bool {
	return password == p.right
}

type resolver struct{ known map[string]bool }

func (r resolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r.known[host] {
		return []net.IPAddr{{IP: net.IPv4(93, 184, 216, 34)}}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// upload is an UploadedFile with the four answers the file rules ask for.
type upload struct {
	path       string
	size       int64
	mime       string
	guessed    string
	client     string
	valid      bool
	width      int
	height     int
	dimensions bool
}

func (u upload) GetPath() string                    { return u.path }
func (u upload) GetRealPath() string                { return u.path }
func (u upload) GetSize() int64                     { return u.size }
func (u upload) GetMimeType() string                { return u.mime }
func (u upload) GetExtension() string               { return u.client }
func (u upload) GuessExtension() string             { return u.guessed }
func (u upload) GetClientOriginalExtension() string { return u.client }
func (u upload) IsValid() bool                      { return u.valid }
func (u upload) Dimensions() (int, int, bool)       { return u.width, u.height, u.dimensions }

func photo() upload {
	return upload{
		path: "/tmp/photo.jpg", size: 2048, mime: "image/jpeg", guessed: "jpeg",
		client: "jpg", valid: true, width: 1200, height: 800, dimensions: true,
	}
}

// data runs a chain over a Data, which is the shape a rule that is not about
// text needs: url.Values cannot hold a null, a list or an upload.
func data(t *testing.T, rules validation.Rules, d validation.Data, opts ...validation.ValidatorOption) validation.Errors {
	t.Helper()
	set, err := validation.Compile(rules)
	if err != nil {
		t.Fatalf("Compile(%v) = %v", rules, err)
	}
	v := validation.Make(d, set, opts...)
	v.Passes()
	return v.Errors()
}

// ---------------------------------------------------------------------------
// The four shapes `required` calls absent.
// ---------------------------------------------------------------------------

func TestRequiredCallsFourDifferentThingsAbsent(t *testing.T) {
	rules := validation.Rules{"f": "required"}

	for name, value := range map[string]any{
		"null":               nil,
		"blank string":       "   ",
		"empty list":         []any{},
		"file never written": upload{path: "", valid: true},
	} {
		if errs := data(t, rules, validation.Data{"f": value}); !errs.Any() {
			t.Errorf("required accepted %s", name)
		}
	}

	for name, value := range map[string]any{
		"zero":        "0",
		"false":       false,
		"a list":      []any{"a"},
		"a real file": photo(),
	} {
		if errs := data(t, rules, validation.Data{"f": value}); errs.Any() {
			t.Errorf("required rejected %s: %v", name, errs)
		}
	}
}

// ---------------------------------------------------------------------------
// `size` is four behaviours under one name, which is exactly what getSize is.
// ---------------------------------------------------------------------------

func TestSizeMeasuresFourDifferentThings(t *testing.T) {
	// Characters in a string.
	if errs := data(t, validation.Rules{"f": "size:3"}, validation.Data{"f": "abc"}); errs.Any() {
		t.Errorf("three characters failed size:3: %v", errs)
	}
	// The value of a number, because the field declares integer.
	if errs := data(t, validation.Rules{"f": "integer|size:3"}, validation.Data{"f": "3"}); errs.Any() {
		t.Errorf("the number three failed integer|size:3: %v", errs)
	}
	if errs := data(t, validation.Rules{"f": "integer|size:3"}, validation.Data{"f": "123"}); !errs.Any() {
		t.Error("the number 123 passed integer|size:3, so it was measured as characters")
	}
	// Members of an array.
	if errs := data(t, validation.Rules{"f": "array|size:2"}, validation.Data{"f": []any{"a", "b"}}); errs.Any() {
		t.Errorf("two members failed array|size:2: %v", errs)
	}
	// KILOBYTES of a file: 2048 bytes is 2.
	if errs := data(t, validation.Rules{"f": "file|size:2"}, validation.Data{"f": photo()}); errs.Any() {
		t.Errorf("2048 bytes failed file|size:2: %v", errs)
	}
	if errs := data(t, validation.Rules{"f": "file|max:1"}, validation.Data{"f": photo()}); !errs.Any() {
		t.Error("2048 bytes passed file|max:1, so the size was not read in kilobytes")
	}
}

// TestBetweenIsInclusiveOnBothSides, which is the whole difference between it
// and a pair of comparisons.
func TestBetweenIsInclusiveOnBothSides(t *testing.T) {
	set := mustCompile(t, validation.Rules{"f": "integer|between:2,4"})
	for _, v := range []string{"2", "3", "4"} {
		if _, errs := set.Validate(url.Values{"f": {v}}); errs.Any() {
			t.Errorf("%q failed between:2,4: %v", v, errs)
		}
	}
	for _, v := range []string{"1", "5"} {
		if _, errs := set.Validate(url.Values{"f": {v}}); !errs.Any() {
			t.Errorf("%q passed between:2,4", v)
		}
	}
}

// TestConfirmedLooksForTheFieldWithConfirmationAfterIt, which is the one thing
// about the rule nobody remembers and nothing else reports.
func TestConfirmedLooksForTheFieldWithConfirmationAfterIt(t *testing.T) {
	set := mustCompile(t, validation.Rules{"password": "required|confirmed"})

	if _, errs := set.Validate(url.Values{
		"password": {"correct horse"}, "password_confirmation": {"correct horse"},
	}); errs.Any() {
		t.Errorf("a matching confirmation was rejected: %v", errs)
	}
	if _, errs := set.Validate(url.Values{
		"password": {"correct horse"}, "password_confirmation": {"battery staple"},
	}); !errs.Any() {
		t.Error("a confirmation that does not match was accepted")
	}
	if _, errs := set.Validate(url.Values{"password": {"correct horse"}}); !errs.Any() {
		t.Error("a missing confirmation was accepted")
	}
}

// ---------------------------------------------------------------------------
// nullable and the exclude family: the two rules that do something other than
// put a message on a field.
// ---------------------------------------------------------------------------

// TestNullableStopsOnNullAndNotOnEmpty. They are two different answers, and
// only one of them is "the client said there is nothing here".
func TestNullableStopsOnNullAndNotOnEmpty(t *testing.T) {
	rules := validation.Rules{"f": "nullable|integer"}

	if errs := data(t, rules, validation.Data{"f": nil}); errs.Any() {
		t.Errorf("null was rejected on a nullable field: %v", errs)
	}
	if errs := data(t, rules, validation.Data{"f": "12"}); errs.Any() {
		t.Errorf("a whole number was rejected: %v", errs)
	}
	if errs := data(t, rules, validation.Data{"f": "abc"}); !errs.Any() {
		t.Error("nullable let a value through that is not a whole number")
	}
}

// TestTheExcludeFamilyRemovesTheFieldRatherThanFailingIt, which is why none of
// the five ever puts a message on a form.
func TestTheExcludeFamilyRemovesTheFieldRatherThanFailingIt(t *testing.T) {
	set := mustCompile(t, validation.Rules{
		"role":       "required|in:admin,member",
		"department": "exclude_unless:role,member|required",
	})

	in, errs := set.Validate(url.Values{"role": {"admin"}, "department": {"whatever"}})
	if errs.Any() {
		t.Fatalf("an excluded field failed instead of being dropped: %v", errs)
	}
	if in.Has("department") {
		t.Error("an excluded field is still in the validated input")
	}

	in, errs = set.Validate(url.Values{"role": {"member"}, "department": {"support"}})
	if errs.Any() {
		t.Fatalf("Validate: %v", errs)
	}
	if in.String("department") != "support" {
		t.Errorf("department = %q, want it kept when the condition does not hold", in.String("department"))
	}

	// `exclude` on its own always drops the field, message or not.
	set = mustCompile(t, validation.Rules{"internal": "exclude"})
	in, errs = set.Validate(url.Values{"internal": {"1"}})
	if errs.Any() || in.Has("internal") {
		t.Errorf("exclude did not drop the field: errs=%v has=%v", errs, in.Has("internal"))
	}
}

// ---------------------------------------------------------------------------
// The conditional families. Nineteen spellings of one idea, and each of them
// says something the other eighteen do not.
// ---------------------------------------------------------------------------

func TestTheConditionalFamiliesEachAskADifferentQuestion(t *testing.T) {
	for _, c := range []struct {
		name  string
		rules validation.Rules
		form  url.Values
		fails bool
	}{
		{"accepted_if fires", validation.Rules{"terms": "accepted_if:plan,pro", "plan": "sometimes"},
			url.Values{"plan": {"pro"}, "terms": {"no"}}, true},
		{"accepted_if quiet", validation.Rules{"terms": "accepted_if:plan,pro", "plan": "sometimes"},
			url.Values{"plan": {"free"}, "terms": {"no"}}, false},
		{"declined_if fires", validation.Rules{"ads": "declined_if:plan,pro", "plan": "sometimes"},
			url.Values{"plan": {"pro"}, "ads": {"yes"}}, true},

		{"required_if_accepted fires", validation.Rules{"address": "required_if_accepted:ship", "ship": "sometimes"},
			url.Values{"ship": {"on"}}, true},
		{"required_if_accepted quiet", validation.Rules{"address": "required_if_accepted:ship", "ship": "sometimes"},
			url.Values{"ship": {"off"}}, false},
		{"required_if_declined fires", validation.Rules{"reason": "required_if_declined:renew", "renew": "sometimes"},
			url.Values{"renew": {"no"}}, true},

		{"required_with_all fires", validation.Rules{"c": "required_with_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{"a": {"1"}, "b": {"1"}}, true},
		{"required_with_all quiet", validation.Rules{"c": "required_with_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{"a": {"1"}}, false},
		{"required_without_all fires", validation.Rules{"c": "required_without_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{}, true},
		{"required_without_all quiet", validation.Rules{"c": "required_without_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{"a": {"1"}}, false},

		{"prohibited_if fires", validation.Rules{"coupon": "prohibited_if:plan,free", "plan": "sometimes"},
			url.Values{"plan": {"free"}, "coupon": {"X"}}, true},
		{"prohibited_unless fires", validation.Rules{"coupon": "prohibited_unless:plan,pro", "plan": "sometimes"},
			url.Values{"plan": {"free"}, "coupon": {"X"}}, true},
		{"prohibits fires", validation.Rules{"card": "prohibits:invoice", "invoice": "sometimes"},
			url.Values{"card": {"4242"}, "invoice": {"yes"}}, true},
		{"prohibits quiet", validation.Rules{"card": "prohibits:invoice", "invoice": "sometimes"},
			url.Values{"card": {"4242"}}, false},

		{"missing_if fires", validation.Rules{"gift": "missing_if:plan,free", "plan": "sometimes"},
			url.Values{"plan": {"free"}, "gift": {"1"}}, true},
		{"missing_unless fires", validation.Rules{"gift": "missing_unless:plan,pro", "plan": "sometimes"},
			url.Values{"plan": {"free"}, "gift": {"1"}}, true},
		{"missing_with fires", validation.Rules{"b": "missing_with:a", "a": "sometimes"},
			url.Values{"a": {"1"}, "b": {"1"}}, true},
		{"missing_with_all fires", validation.Rules{"c": "missing_with_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{"a": {"1"}, "b": {"1"}, "c": {"1"}}, true},

		{"present_if fires", validation.Rules{"note": "present_if:kind,other", "kind": "sometimes"},
			url.Values{"kind": {"other"}}, true},
		{"present_if quiet on empty", validation.Rules{"note": "present_if:kind,other", "kind": "sometimes"},
			url.Values{"kind": {"other"}, "note": {""}}, false},
		{"present_unless fires", validation.Rules{"note": "present_unless:kind,plain", "kind": "sometimes"},
			url.Values{"kind": {"other"}}, true},
		{"present_with fires", validation.Rules{"b": "present_with:a", "a": "sometimes"},
			url.Values{"a": {"1"}}, true},
		{"present_with_all fires", validation.Rules{"c": "present_with_all:a,b", "a": "sometimes", "b": "sometimes"},
			url.Values{"a": {"1"}, "b": {"1"}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := mustCompile(t, c.rules)
			_, errs := set.Validate(c.form)
			if errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The rules that need a value that is not text.
// ---------------------------------------------------------------------------

func TestTheArrayRules(t *testing.T) {
	for _, c := range []struct {
		name  string
		rules validation.Rules
		d     validation.Data
		fails bool
	}{
		{"array on a list", validation.Rules{"f": "array"}, validation.Data{"f": []any{"a"}}, false},
		{"array on a string", validation.Rules{"f": "array"}, validation.Data{"f": "a"}, true},
		{"array with allowed keys", validation.Rules{"f": "array:name,email"},
			validation.Data{"f": validation.Data{"name": "Ana"}}, false},
		{"array with a key outside them", validation.Rules{"f": "array:name"},
			validation.Data{"f": validation.Data{"name": "Ana", "role": "admin"}}, true},

		{"list on a list", validation.Rules{"f": "list"}, validation.Data{"f": []any{"a"}}, false},
		{"list on a keyed array", validation.Rules{"f": "list"},
			validation.Data{"f": validation.Data{"0": "a"}}, true},

		{"distinct with no repeat", validation.Rules{"f": "distinct"},
			validation.Data{"f": []any{"a", "b"}}, false},
		{"distinct with a repeat", validation.Rules{"f": "distinct"},
			validation.Data{"f": []any{"a", "a"}}, true},
		{"distinct ignoring case", validation.Rules{"f": "distinct:ignore_case"},
			validation.Data{"f": []any{"a", "A"}}, true},
		{"distinct minding case", validation.Rules{"f": "distinct"},
			validation.Data{"f": []any{"a", "A"}}, false},

		{"contains what it must", validation.Rules{"f": "contains:a,b"},
			validation.Data{"f": []any{"a", "b", "c"}}, false},
		{"contains what it must not", validation.Rules{"f": "contains:a,z"},
			validation.Data{"f": []any{"a", "b"}}, true},

		{"required_array_keys satisfied", validation.Rules{"f": "required_array_keys:name"},
			validation.Data{"f": validation.Data{"name": "Ana"}}, false},
		{"required_array_keys short", validation.Rules{"f": "required_array_keys:name,email"},
			validation.Data{"f": validation.Data{"name": "Ana"}}, true},

		{"in_array finds it", validation.Rules{"f": "in_array:allowed", "allowed": "sometimes"},
			validation.Data{"f": "b", "allowed": []any{"a", "b"}}, false},
		{"in_array does not", validation.Rules{"f": "in_array:allowed", "allowed": "sometimes"},
			validation.Data{"f": "z", "allowed": []any{"a", "b"}}, true},

		{"string on a string", validation.Rules{"f": "string"}, validation.Data{"f": "a"}, false},
		{"string on a number", validation.Rules{"f": "string"}, validation.Data{"f": 12}, true},
		{"string on a list", validation.Rules{"f": "string"}, validation.Data{"f": []any{"a"}}, true},

		{"in over a multi-select", validation.Rules{"f": "array|in:a,b"},
			validation.Data{"f": []any{"a", "b"}}, false},
		{"in over a multi-select with a stranger", validation.Rules{"f": "array|in:a,b"},
			validation.Data{"f": []any{"a", "z"}}, true},

		{"enum accepts a case", validation.Rules{"f": "enum:draft,published"},
			validation.Data{"f": "draft"}, false},
		{"enum refuses a stranger", validation.Rules{"f": "enum:draft,published"},
			validation.Data{"f": "deleted"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if errs := data(t, c.rules, c.d); errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

func TestTheNumericExtras(t *testing.T) {
	for _, c := range []struct {
		chain string
		value string
		fails bool
	}{
		{"max_digits:4", "1234", false},
		{"max_digits:4", "12345", true},
		{"max_digits:4", "12a", true},
		{"min_digits:2", "12", false},
		{"min_digits:2", "1", true},
		{"numeric|multiple_of:3", "9", false},
		{"numeric|multiple_of:3", "10", true},
		{"numeric|multiple_of:0.1", "0.3", false},
		{"numeric|multiple_of:0", "1", true},
		{"date_equals:2026-01-01", "2026-01-01", false},
		{"date_equals:2026-01-01", "2026-01-02", true},
	} {
		t.Run(c.chain+"/"+c.value, func(t *testing.T) {
			if errs := run(t, c.chain, c.value); errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

func TestTheFileRules(t *testing.T) {
	png := upload{path: "/tmp/a.png", size: 100, mime: "image/png", guessed: "png", client: "png", valid: true, width: 1, height: 1, dimensions: true}
	script := upload{path: "/tmp/a.php", size: 100, mime: "text/x-php", guessed: "php", client: "php", valid: true}
	unfinished := upload{path: "/tmp/a.png", size: 100, mime: "image/png", guessed: "png", client: "png", valid: false}

	for _, c := range []struct {
		name  string
		rules validation.Rules
		value any
		fails bool
	}{
		{"file on a file", validation.Rules{"f": "file"}, png, false},
		{"file on a string", validation.Rules{"f": "file"}, "/tmp/a.png", true},
		{"file on an upload that did not finish", validation.Rules{"f": "file"}, unfinished, true},

		{"image on a png", validation.Rules{"f": "image"}, png, false},
		{"image on a php file", validation.Rules{"f": "image"}, script, true},

		// mimes asks the CONTENT, so a .png named .jpg is still a png.
		{"mimes reads the content", validation.Rules{"f": "mimes:png"},
			upload{path: "/tmp/a.jpg", size: 1, mime: "image/png", guessed: "png", client: "jpg", valid: true}, false},
		{"mimes refuses the wrong content", validation.Rules{"f": "mimes:gif"}, png, true},
		{"mimes treats jpg and jpeg as each other", validation.Rules{"f": "mimes:jpg"}, photo(), false},

		// extensions asks the NAME THE CLIENT SENT, which is the other question.
		{"extensions reads the client name", validation.Rules{"f": "extensions:jpg"}, photo(), false},
		{"extensions refuses another", validation.Rules{"f": "extensions:png"}, photo(), true},

		{"mimetypes exact", validation.Rules{"f": "mimetypes:image/png"}, png, false},
		{"mimetypes by group", validation.Rules{"f": "mimetypes:image/*"}, png, false},
		{"mimetypes refuses another group", validation.Rules{"f": "mimetypes:text/*"}, png, true},

		// A .php upload is blocked whatever the rule asked for, unless it asked
		// for php by name.
		{"a php upload is blocked", validation.Rules{"f": "mimes:php"}, script, false},
		{"a php upload is blocked by another rule", validation.Rules{"f": "extensions:jpg,php"}, script, false},

		{"dimensions met", validation.Rules{"f": "dimensions:min_width=1000,ratio=3/2"}, photo(), false},
		{"dimensions too narrow", validation.Rules{"f": "dimensions:min_width=2000"}, photo(), true},
		{"dimensions wrong ratio", validation.Rules{"f": "dimensions:ratio=1/1"}, photo(), true},
		{"dimensions on an svg", validation.Rules{"f": "dimensions:width=10"},
			upload{path: "/tmp/a.svg", size: 1, mime: "image/svg+xml", guessed: "svg", client: "svg", valid: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if errs := data(t, c.rules, validation.Data{"f": c.value}); errs.Any() != c.fails {
				t.Errorf("errs.Any() = %v, want %v (%v)", errs.Any(), c.fails, errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The rules that leave the process.
// ---------------------------------------------------------------------------

// TestUniqueAndExistsAskTheStoreWithTheGrant: a read is authorized
// too, and a count of rows is a read -- without the tenant it answers whether
// SOMEBODY has that address.
func TestUniqueAndExistsAskTheStoreWithTheGrant(t *testing.T) {
	rows := &verifier{rows: map[string][]string{
		"acme":  {"taken@acme.test"},
		"other": {"free@acme.test"},
	}}
	acme := grantFor("acme")

	unique := validation.Rules{"email": "required|email|unique:users"}
	if errs := data(t, unique, validation.Data{"email": "taken@acme.test"},
		validation.WithPresence(acme, rows)); !errs.Any() {
		t.Error("unique passed on an address this tenant already holds")
	}
	if errs := data(t, unique, validation.Data{"email": "free@acme.test"},
		validation.WithPresence(acme, rows)); errs.Any() {
		t.Errorf("unique failed on an address only another tenant holds: %v", errs)
	}
	for _, asked := range rows.asked {
		if asked != "acme" {
			t.Fatalf("the query was made for tenant %q, not the one the Grant carries", asked)
		}
	}

	exists := validation.Rules{"email": "required|exists:users,email"}
	if errs := data(t, exists, validation.Data{"email": "taken@acme.test"},
		validation.WithPresence(acme, rows)); errs.Any() {
		t.Errorf("exists failed on a row this tenant holds: %v", errs)
	}
	if errs := data(t, exists, validation.Data{"email": "nobody@acme.test"},
		validation.WithPresence(acme, rows)); !errs.Any() {
		t.Error("exists passed on a row nobody holds")
	}
	if rows.column != "email" {
		t.Errorf("column = %q, want the one the rule named", rows.column)
	}
}

// TestUniqueIgnoresTheRowBeingEdited, which is what the third parameter is for:
// without it every edit of a row fails against itself.
func TestUniqueIgnoresTheRowBeingEdited(t *testing.T) {
	rows := &verifier{rows: map[string][]string{"acme": {"taken@acme.test"}}}
	errs := data(t,
		validation.Rules{"email": "required|unique:users,email,7"},
		validation.Data{"email": "taken@acme.test"},
		validation.WithPresence(grantFor("acme"), rows),
	)
	if errs.Any() {
		t.Errorf("unique failed against the row it was told to ignore: %v", errs)
	}
	if rows.exclude != int64(7) {
		t.Errorf("excludeID = %#v, want the number 7", rows.exclude)
	}
}

// TestADatabaseRuleWithoutAVerifierFailsClosed. The worst outcome available is
// a uniqueness check that passes because nothing was wired.
func TestADatabaseRuleWithoutAVerifierFailsClosed(t *testing.T) {
	for _, rule := range []string{"unique:users", "exists:users"} {
		if errs := data(t, validation.Rules{"email": rule}, validation.Data{"email": "a@b.co"}); !errs.Any() {
			t.Errorf("%q passed with no verifier and no Grant", rule)
		}
	}
}

func TestCurrentPasswordAsksTheChecker(t *testing.T) {
	rules := validation.Rules{"password": "required|current_password"}
	checker := passwordChecker{right: "correct horse"}

	if errs := data(t, rules, validation.Data{"password": "correct horse"},
		validation.WithCurrentPassword(checker)); errs.Any() {
		t.Errorf("the right password was rejected: %v", errs)
	}
	if errs := data(t, rules, validation.Data{"password": "battery staple"},
		validation.WithCurrentPassword(checker)); !errs.Any() {
		t.Error("the wrong password was accepted")
	}
	// No checker is a rule that cannot answer, and it does not answer yes.
	if errs := data(t, rules, validation.Data{"password": "correct horse"}); !errs.Any() {
		t.Error("current_password passed with no checker")
	}
}

func TestActiveUrlAsksTheResolver(t *testing.T) {
	rules := validation.Rules{"site": "required|active_url"}
	dns := resolver{known: map[string]bool{"example.com": true}}

	if errs := data(t, rules, validation.Data{"site": "https://example.com/a"},
		validation.WithResolver(dns)); errs.Any() {
		t.Errorf("a host with a record was rejected: %v", errs)
	}
	if errs := data(t, rules, validation.Data{"site": "https://nowhere.invalid"},
		validation.WithResolver(dns)); !errs.Any() {
		t.Error("a host with no record was accepted")
	}

	// email:dns is the same lookup on the domain of the address.
	if errs := data(t, validation.Rules{"a": "email:dns"}, validation.Data{"a": "someone@example.com"},
		validation.WithResolver(dns)); errs.Any() {
		t.Errorf("email:dns rejected a domain that resolves: %v", errs)
	}
	if errs := data(t, validation.Rules{"a": "email:dns"}, validation.Data{"a": "someone@nowhere.invalid"},
		validation.WithResolver(dns)); !errs.Any() {
		t.Error("email:dns accepted a domain that does not resolve")
	}
}

// TestAMultiValueInputIsAList, which is what `tags[]` sends and what
// `array` needs in order ever to pass.
func TestAMultiValueInputIsAList(t *testing.T) {
	set := mustCompile(t, validation.Rules{"tags": "array|between:1,3|in:go,web,db"})

	in, errs := set.Validate(url.Values{"tags": {"go", "web"}})
	if errs.Any() {
		t.Fatalf("a two-value select was rejected: %v", errs)
	}
	if got := in.Strings("tags"); !reflect.DeepEqual(got, []string{"go", "web"}) {
		t.Errorf("Strings = %v", got)
	}
	if _, errs := set.Validate(url.Values{"tags": {"go", "web", "db", "extra"}}); !errs.Any() {
		t.Error("four values passed between:1,3, so the members were not counted")
	}
}
