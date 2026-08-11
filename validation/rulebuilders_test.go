package validation

import "testing"

func TestEachBuilderRendersTheRuleStringLaravelRenders(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"In", NewIn("admin", "editor").String(), `in:"admin","editor"`},
		{"In with a comma", NewIn(`a,b`).String(), `in:"a,b"`},
		{"In with a quote", NewIn(`say "hi"`).String(), `in:"say ""hi"""`},
		{"NotIn", NewNotIn("root").String(), `not_in:"root"`},
		{"ArrayRule", NewArrayRule().String(), "array"},
		{"ArrayRule with keys", NewArrayRule("name", "price").String(), "array:name,price"},
		{"ExcludeIf true", NewExcludeIf(true).String(), "exclude"},
		{"ExcludeIf false", NewExcludeIf(false).String(), ""},
		{"ProhibitedIf", NewProhibitedIf(true).String(), "prohibited"},
		{"RequiredIf", NewRequiredIf(true).String(), "required"},

		{"Date", NewDate().String(), "date"},
		{"Date with a layout", NewDate().Format("2006-01-02").String(), "date_format:2006-01-02"},
		{"Date afterToday", NewDate().AfterToday().String(), "date|after:today"},
		{"Date between", NewDate().Between("2026-01-01", "2026-12-31").String(),
			"date|after:2026-01-01|before:2026-12-31"},
		{"Date todayOrBefore", NewDate().TodayOrBefore().String(), "date|before_or_equal:today"},

		{"Numeric", NewNumeric().String(), "numeric"},
		{"Numeric min and max", NewNumeric().Min(0).Max(1000).String(), "numeric|min:0|max:1000"},
		{"Numeric digits", NewNumeric().Digits(4).String(), "numeric|integer|digits:4"},
		{"Numeric exactly drops the repeat", NewNumeric().Integer().Exactly(5).String(),
			"numeric|integer|size:5"},
		{"Numeric decimal", NewNumeric().Decimal(2).String(), "numeric|decimal:2"},
		{"Numeric decimal range", NewNumeric().Decimal(2, 4).String(), "numeric|decimal:2,4"},
		{"Numeric comparisons", NewNumeric().GreaterThan("low").LessThanOrEqualTo("high").String(),
			"numeric|gt:low|lte:high"},

		{"Dimensions", NewDimensions().Width(100).Height(200).String(),
			"dimensions:width=100,height=200"},
		{"Dimensions ratio", NewDimensions().RatioBetween(0.5, 2).String(),
			"dimensions:min_ratio=0.5,max_ratio=2"},

		{"FileRule", NewFileRule().String(), "file"},
		{"FileRule size", NewFileRule().Size(100).String(), "file|size:100"},
		{"FileRule between", NewFileRule().Between(10, 100).String(), "file|between:10,100"},
		{"FileRule min only", NewFileRule().Min(10).String(), "file|min:10"},
		{"FileRule max only", NewFileRule().Max(100).String(), "file|max:100"},
		{"FileRule types splits mime from extension", Types("image/png", "jpg").String(),
			"file|mimetypes:image/png|mimes:jpg"},
		{"FileRule extensions", NewFileRule().Extensions("PNG", "jpg").String(),
			"file|extensions:png,jpg"},
		{"ImageFile", NewImageFile().String(), "image"},
		{"ImageFile allowing svg", NewImageFile(true).String(), "image:allow_svg"},
		{"ImageFile with dimensions", NewImageFile().Dimensions(NewDimensions().MinWidth(100)).String(),
			"image|dimensions:min_width=100"},

		{"EmailRule", NewEmailRule().String(), "email"},
		{"EmailRule rfc", NewEmailRule().RfcCompliant().String(), "email:rfc"},
		{"EmailRule strict", NewEmailRule().RfcCompliant(true).String(), "email:strict"},
		{"EmailRule dns", NewEmailRule().ValidateMxRecord().String(), "email:dns"},

		// The PHP always writes the five positions and rtrims the trailing
		// commas, so the id column stays even when nothing is ignored.
		{"Unique", NewUnique("users", "email").String(), "unique:users,email,NULL,id"},
		{"Unique ignoring a row", NewUnique("users", "email").Ignore("7").String(),
			`unique:users,email,"7",id`},
		{"Unique with a where", func() string {
			u := NewUnique("users", "email")
			u.Where("account_id", "3")
			return u.String()
		}(), `unique:users,email,NULL,id,account_id,"3"`},
		{"Exists", NewExists("roles", "id").String(), "exists:roles,id"},
		{"Exists without trashed", func() string {
			e := NewExists("roles", "id")
			e.WithoutTrashed()
			return e.String()
		}(), `exists:roles,id,deleted_at,"NULL"`},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestEveryBuilderRendersSomethingTheCompilerAccepts(t *testing.T) {
	// The point of a builder is that it cannot misspell a rule. This is what
	// proves it: each chain goes through the same boot check a typed one does.
	chains := map[string]string{
		"role":    NewIn("admin", "editor").String(),
		"banned":  NewNotIn("root").String(),
		"price":   NewNumeric().Min(0).Max(1000).String(),
		"starts":  NewDate().Format("2006-01-02").AfterToday().String(),
		"avatar":  NewImageFile().Max(2048).Dimensions(NewDimensions().MaxWidth(2000)).String(),
		"report":  Types("application/pdf").Max(5000).String(),
		"contact": NewEmailRule().String(),
		"tags":    NewArrayRule().String(),
	}

	if _, err := Compile(chains); err != nil {
		t.Fatalf("a builder rendered something the compiler refused: %v", err)
	}
}

func TestToKilobytesCountsAThousandNotATwentyFourth(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"1kb", 1, true},
		{"2mb", 2000, true},
		{"1gb", 1_000_000, true},
		{"1tb", 1_000_000_000, true},
		{"1.5mb", 1500, true},
		{"100", 0, false},
		{"3pb", 0, false},
	}
	for _, c := range cases {
		got, ok := ToKilobytes(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: got (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// Rules\Password.
// ---------------------------------------------------------------------------

func TestPasswordChecksEachThingItWasAskedTo(t *testing.T) {
	cases := []struct {
		name  string
		rule  *Password
		value string
		want  bool
	}{
		{"long enough", PasswordMin(8), "abcdefgh", true},
		{"too short", PasswordMin(8), "abc", false},
		{"too long", PasswordMin(1).Max(4), "abcdefgh", false},
		{"letters", PasswordMin(1).Letters(), "1234", false},
		{"letters, satisfied", PasswordMin(1).Letters(), "12a4", true},
		{"mixed case", PasswordMin(1).MixedCase(), "abcdefgh", false},
		{"mixed case, satisfied", PasswordMin(1).MixedCase(), "abcDefgh", true},
		{"numbers", PasswordMin(1).Numbers(), "abcdefgh", false},
		{"numbers, satisfied", PasswordMin(1).Numbers(), "abcdefg1", true},
		{"symbols", PasswordMin(1).Symbols(), "abcdefgh", false},
		{"symbols, satisfied", PasswordMin(1).Symbols(), "abcdefg!", true},
	}

	for _, c := range cases {
		if got := c.rule.Passes("password", c.value); got != c.want {
			t.Errorf("%s: got %v, want %v (messages %v)", c.name, got, c.want, c.rule.Message())
		}
	}
}

func TestPasswordSaysWhichPartFailed(t *testing.T) {
	rule := PasswordMin(1).MixedCase().Numbers()

	if rule.Passes("password", "abcdefgh") {
		t.Fatal("a password with no capital and no number must not pass")
	}
	if len(rule.Message()) != 2 {
		t.Fatalf("want one message per failed part, got %v", rule.Message())
	}
}

func TestPasswordMinIsNeverBelowOne(t *testing.T) {
	if got := PasswordMin(0).AppliedRules()["min"]; got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
}

func TestPasswordDefaultsIsWhatDefaultAnswersWith(t *testing.T) {
	t.Cleanup(func() { PasswordDefaults(nil) })

	if got := PasswordDefault().AppliedRules()["min"]; got != 8 {
		t.Fatalf("with no callback the default is min 8, got %v", got)
	}

	PasswordDefaults(func() *Password { return PasswordMin(12).Symbols() })

	applied := PasswordDefault().AppliedRules()
	if applied["min"] != 12 || applied["symbols"] != true {
		t.Fatalf("got %v", applied)
	}
}

// leaked is an UncompromisedVerifier that says everything is.
type leaked struct{}

func (leaked) Verify(value string, threshold int) bool { return false }

func TestUncompromisedFailsClosedWithNoVerifier(t *testing.T) {
	// "we could not ask" is not "it is safe".
	rule := PasswordMin(1).Uncompromised(nil)
	if rule.Passes("password", "abcdefgh") {
		t.Fatal("a password nothing could check must not pass")
	}

	if PasswordMin(1).Uncompromised(leaked{}).Passes("password", "abcdefgh") {
		t.Fatal("a leaked password must not pass")
	}
}

func TestPasswordRunsThroughTheValidatorsAfterHook(t *testing.T) {
	v := Make(Data{"password": "abc"}, MustCompile(Rules{"password": "required"}))
	rule := PasswordMin(12)

	v.After(func(v *Validator) {
		v.ValidateUsingCustomRule("password", v.GetValue("password"), rule)
	})

	if v.Passes() {
		t.Fatal("a three-character password must fail a min of twelve")
	}
	if v.Errors().First("password") == "" {
		t.Fatal("the failure must reach the bag")
	}
}

// ---------------------------------------------------------------------------
// Rules\Enum, Rules\Can and Rules\AnyOf.
// ---------------------------------------------------------------------------

func TestEnumAcceptsOnlyItsCases(t *testing.T) {
	rule := NewEnum("draft", "published", "archived")

	if !rule.Passes("status", "draft") {
		t.Fatal("a case must pass")
	}
	if rule.Passes("status", "deleted") {
		t.Fatal("something that is not a case must not pass")
	}
	if rule.Passes("status", nil) {
		t.Fatal("null must not pass")
	}
	if rule.Only("published").Passes("status", "draft") {
		t.Fatal("Only must narrow the cases")
	}
	if NewEnum("draft", "published").Except("draft").Passes("status", "draft") {
		t.Fatal("Except must remove a case")
	}
}

func TestCanFailsClosedWithNoAuthorizer(t *testing.T) {
	rule := NewCan(nil, "assign-role")
	rule.SetValidator(Make(Data{}, MustCompile(Rules{"role": "required"})))

	if rule.Passes("role", "admin") {
		t.Fatal("a rule that could not ask must not allow")
	}
}

func TestAnyOfPassesWhenAnyOneSetDoes(t *testing.T) {
	rule := NewAnyOf(
		MustCompile(Rules{"contact": "email"}),
		MustCompile(Rules{"contact": "digits:11"}),
	)
	rule.SetValidator(Make(Data{}, MustCompile(Rules{"contact": "required"})))

	if !rule.Passes("contact", "someone@example.com") {
		t.Fatal("the first set accepts an address")
	}
	if !rule.Passes("contact", "11987654321") {
		t.Fatal("the second set accepts a phone number")
	}
	if rule.Passes("contact", "neither") {
		t.Fatal("neither set accepts this")
	}
}
