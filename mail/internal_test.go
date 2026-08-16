package mail

import (
	"strings"
	"testing"
)

// The pieces the assertions and the builder are made of. They are tested from
// inside the package because they are the parts that are wrong quietly: a
// dedupe that keeps the first spelling, an escape that double-escapes, an
// in-order search that matches out of order.

func TestUniqueAddressesKeepsTheLastSpelling(t *testing.T) {
	got := uniqueAddresses([]Address{
		{Address: "a@example.test"},
		{Address: "b@example.test", Name: "Bee"},
		{Address: "a@example.test", Name: "Ada"},
	})

	if len(got) != 2 {
		t.Fatalf("kept %d addresses, want 2: %+v", len(got), got)
	}
	// The survivor also moves to where its last spelling was, which is why b
	// comes first here.
	if got[0].Address != "b@example.test" {
		t.Errorf("order is %+v, want the survivor where its last spelling was", got)
	}
	if got[1].Name != "Ada" {
		t.Errorf("name = %q, want the last spelling", got[1].Name)
	}
}

func TestAddressesOfTakesEveryShapeIlluminateTakes(t *testing.T) {
	for _, c := range []struct {
		name  string
		in    any
		with  []string
		count int
		first string
	}{
		{"a string", "a@example.test", nil, 1, "a@example.test"},
		{"a string and a name", "a@example.test", []string{"Ada"}, 1, "a@example.test"},
		{"an address", Address{Address: "a@example.test"}, nil, 1, "a@example.test"},
		{"a list of strings", []string{"a@example.test", "b@example.test"}, nil, 2, "a@example.test"},
		{"a list of addresses", []Address{{Address: "a@example.test"}}, nil, 1, "a@example.test"},
		{"a map of address to name", map[string]string{"a@example.test": "Ada"}, nil, 1, "a@example.test"},
		{"nothing", nil, nil, 0, ""},
		{"an empty string", "", nil, 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := addressesOf(c.in, c.with...)
			if len(got) != c.count {
				t.Fatalf("got %d addresses, want %d: %+v", len(got), c.count, got)
			}
			if c.count > 0 && got[0].Address != c.first {
				t.Errorf("first = %q, want %q", got[0].Address, c.first)
			}
		})
	}

	named := addressesOf("a@example.test", "Ada")
	if named[0].Name != "Ada" {
		t.Errorf("the name did not attach: %+v", named[0])
	}
}

func TestEscapedIsHtmlspecialcharsWithQuotes(t *testing.T) {
	got := escaped(`<a href="x">Ada & Co's</a>`)
	want := `&lt;a href=&quot;x&quot;&gt;Ada &amp; Co&#039;s&lt;/a&gt;`
	if got != want {
		t.Errorf("escaped = %q, want %q", got, want)
	}

	// The ampersand goes first, or every replacement after it is escaped again
	// and "&amp;lt;" reaches the assertion.
	if strings.Contains(escaped("<"), "&amp;lt;") {
		t.Error("the ampersand was replaced after the angle bracket")
	}

	if got := escaped("<b>", false); got != "<b>" {
		t.Errorf("escaped(_, false) = %q, want it untouched", got)
	}
}

func TestSeeInOrderNamesTheOneItCouldNotFind(t *testing.T) {
	if missing, ok := seeInOrder("one two three", []string{"one", "three"}); !ok {
		t.Errorf("in-order search failed on %q", missing)
	}
	missing, ok := seeInOrder("one two three", []string{"three", "one"})
	if ok {
		t.Fatal("out-of-order strings were reported as in order")
	}
	if missing != "one" {
		t.Errorf("missing = %q, want the one that was not found after the others", missing)
	}
}

func TestSubjectForHumanisesTheTypeName(t *testing.T) {
	if got := subjectFor(OrderShipped{}); got != "Order Shipped" {
		t.Errorf("subjectFor = %q, want the humanised type name", got)
	}
	if got := subjectFor(&OrderShipped{}); got != "Order Shipped" {
		t.Errorf("a pointer mailable gave %q", got)
	}
	if got := subjectFor(anonymous{}); got != "Anonymous" {
		t.Errorf("subjectFor = %q", got)
	}
}

type OrderShipped struct{}

func (OrderShipped) Envelope() Envelope { return Envelope{} }
func (OrderShipped) Content() Content   { return Content{} }

type anonymous struct{}

func (anonymous) Envelope() Envelope { return Envelope{} }
func (anonymous) Content() Content   { return Content{} }

func TestCollapseBlankLinesLeavesOneBlankLine(t *testing.T) {
	if got := collapseBlankLines("a\n\n\n\n\nb"); got != "a\n\nb" {
		t.Errorf("collapseBlankLines = %q", got)
	}
	if got := collapseBlankLines("a\r\n\r\nb"); got != "a\n\nb" {
		t.Errorf("carriage returns survived: %q", got)
	}
}

func TestReferencesStringWrapsEveryIdOnce(t *testing.T) {
	h := Headers{References: []string{"one@example.test", "<two@example.test>"}}
	if got := h.ReferencesString(); got != "<one@example.test> <two@example.test>" {
		t.Errorf("ReferencesString = %q", got)
	}
}

func TestParseViewTakesEveryShapeIlluminateTakes(t *testing.T) {
	if got, _ := parseView("mail.note"); got.HTML != "mail.note" {
		t.Errorf("a string view became %+v", got)
	}
	if got, _ := parseView([]string{"mail.html", "mail.text"}); got.HTML != "mail.html" || got.Text != "mail.text" {
		t.Errorf("the numeric array form became %+v", got)
	}
	if got, _ := parseView(ViewParts{Raw: "hi"}); got.Raw != "hi" {
		t.Errorf("ViewParts became %+v", got)
	}
	if _, err := parseView(42); err == nil {
		t.Error("an invalid view was accepted")
	}
}

func TestStripUnsafeLinksTakesEveryScheme(t *testing.T) {
	for _, in := range []string{
		`<a href="javascript:alert(1)">x</a>`,
		`<a href='JAVASCRIPT:alert(1)'>x</a>`,
		`<img src="data:text/html;base64,PHNjcmlwdD4=">`,
		`<a href="vbscript:msgbox">x</a>`,
	} {
		got := stripUnsafeLinks(in)
		for _, scheme := range []string{"javascript:", "JAVASCRIPT:", "data:", "vbscript:"} {
			if strings.Contains(got, scheme) {
				t.Errorf("%q survived in %q", scheme, got)
			}
		}
	}
}
