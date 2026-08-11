package html

import (
	stdhtml "html"
	"net/url"
	"strings"
	"testing"
)

// fakeUrls is a UrlGenerator that echoes what it is given, so a test can see
// exactly which value reached the markup.
type fakeUrls struct {
	current string
	routes  map[string]string
	err     error
}

func newFakeUrls() *fakeUrls {
	return &fakeUrls{current: "/current", routes: map[string]string{"invoices.show": "/invoices/7"}}
}

func (f *fakeUrls) To(path string, extra ...string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return "http://example.test" + join(path, extra)
}

func (f *fakeUrls) Secure(path string, extra ...string) string {
	if strings.HasPrefix(path, "https://") {
		return path
	}
	return "https://example.test" + join(path, extra)
}

func (f *fakeUrls) Asset(path string) string       { return "http://cdn.test/" + path }
func (f *fakeUrls) SecureAsset(path string) string { return "https://cdn.test/" + path }
func (f *fakeUrls) Current() string                { return f.current }

func (f *fakeUrls) Route(name string, parameters ...string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.routes[name] + join("", parameters), nil
}

func (f *fakeUrls) Action(action string, parameters ...string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "/" + action + join("", parameters), nil
}

func join(path string, extra []string) string {
	for _, segment := range extra {
		path += "/" + segment
	}
	return path
}

func newBuilder() *HtmlBuilder { return NewHtmlBuilder(newFakeUrls()) }

func TestEntitiesConvertsTheFive(t *testing.T) {
	got := string(newBuilder().Entities(`<b>Tom & "Jerry"'s</b>`))
	want := `&lt;b&gt;Tom &amp; &quot;Jerry&quot;&#039;s&lt;/b&gt;`

	if got != want {
		t.Fatalf("Entities = %q, want %q", got, want)
	}
}

// double_encode false is what keeps Mailto's obfuscated address readable.
func TestEntitiesDoesNotDoubleEncode(t *testing.T) {
	for _, value := range []string{"&amp;", "&#64;", "&#x40;", "&nbsp;"} {
		if got := string(newBuilder().Entities(value)); got != value {
			t.Errorf("Entities(%q) = %q, want it unchanged", value, got)
		}
	}
}

// An ampersand that does not open an entity is still an ampersand.
func TestEntitiesEncodesABareAmpersand(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a & b", "a &amp; b"},
		{"&", "&amp;"},
		{"&amp", "&amp;amp"},
		{"&#;", "&amp;#;"},
		{"&#x;", "&amp;#x;"},
	} {
		if got := string(newBuilder().Entities(c.in)); got != c.want {
			t.Errorf("Entities(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeReversesEntities(t *testing.T) {
	builder := newBuilder()
	original := `<b>Tom & "Jerry"'s</b>`

	if got := builder.Decode(string(builder.Entities(original))); got != original {
		t.Fatalf("Decode(Entities(x)) = %q, want %q", got, original)
	}
}

func TestAttributesAreSortedAndEscaped(t *testing.T) {
	got := string(newBuilder().Attributes(Attrs{"id": "x", "class": `a"b`, "required": "required"}))
	want := ` class="a&quot;b" id="x" required="required"`

	if got != want {
		t.Fatalf("Attributes = %q, want %q", got, want)
	}
}

func TestAttributesIsEmptyForAnEmptyMap(t *testing.T) {
	if got := string(newBuilder().Attributes(nil)); got != "" {
		t.Fatalf("Attributes(nil) = %q, want empty", got)
	}
}

// PHP concatenates the key. A key that closes the tag is a script tag there.
func TestAttributesDropsAnIllegalName(t *testing.T) {
	for _, key := range []string{`x"><script>`, "has space", "a=b", "a/b", "", "a>b", "a'b"} {
		if got := string(newBuilder().Attributes(Attrs{key: "1"})); got != "" {
			t.Errorf("Attributes with key %q = %q, want it dropped", key, got)
		}
	}
}

// HTMX and Alpine write attribute names the framework depends on.
func TestAttributesKeepsFrameworkNames(t *testing.T) {
	for _, key := range []string{"hx-post", "x-data", "@click", ":class", "data-variant", "aria-label"} {
		if got := string(newBuilder().Attributes(Attrs{key: "1"})); got != " "+key+`="1"` {
			t.Errorf("Attributes with key %q = %q, want it kept", key, got)
		}
	}
}

// A Go map is a reference. Writing src into the caller's map would leak into
// the next call.
func TestScriptDoesNotWriteIntoTheCallersMap(t *testing.T) {
	attributes := Attrs{"defer": "defer"}
	newBuilder().Script("app.js", attributes, false)

	if _, ok := attributes["src"]; ok {
		t.Fatal("Script wrote src into the caller's map")
	}
}

func TestScript(t *testing.T) {
	got := string(newBuilder().Script("app.js", nil, false))
	want := `<script src="http://cdn.test/app.js"></script>` + "\n"

	if got != want {
		t.Fatalf("Script = %q, want %q", got, want)
	}
}

func TestStyleDefaultsAndLetsTheCallerWin(t *testing.T) {
	got := string(newBuilder().Style("app.css", Attrs{"media": "print"}, true))
	want := `<link href="https://cdn.test/app.css" media="print" rel="stylesheet" type="text/css">` + "\n"

	if got != want {
		t.Fatalf("Style = %q, want %q", got, want)
	}
}

// HtmlBuilder.php:97 writes the resolved src into a quoted attribute unescaped.
func TestImageEscapesTheSource(t *testing.T) {
	got := string(newBuilder().Image(`a" onerror="alert(1)`, "", nil, false))

	if strings.Contains(got, `onerror="alert(1)"`) {
		t.Fatalf("Image let the src close the attribute: %s", got)
	}
	if !strings.Contains(got, "&quot;") {
		t.Fatalf("Image did not escape the src: %s", got)
	}
}

// PHP drops alt when it is null, leaving a screen reader to read the file name.
func TestImageAlwaysWritesAlt(t *testing.T) {
	if got := string(newBuilder().Image("logo.png", "", nil, false)); !strings.Contains(got, `alt=""`) {
		t.Fatalf("Image = %q, want an alt attribute", got)
	}
}

// HtmlBuilder.php:115 writes the resolved href into a quoted attribute unescaped.
func TestLinkEscapesTheHref(t *testing.T) {
	got := string(newBuilder().Link(`/a" onclick="alert(1)`, "Go", nil, false))

	if strings.Contains(got, `onclick="alert(1)"`) {
		t.Fatalf("Link let the href close the attribute: %s", got)
	}
}

func TestLinkEscapesTheTitleAndFallsBackToTheUrl(t *testing.T) {
	builder := newBuilder()

	if got := string(builder.Link("/a", "<b>x</b>", nil, false)); strings.Contains(got, "<b>") {
		t.Fatalf("Link did not escape the title: %s", got)
	}
	if got := string(builder.Link("/a", "", nil, false)); !strings.Contains(got, ">http://example.test/a<") {
		t.Fatalf("Link with no title = %q, want the URL as the text", got)
	}
}

func TestSecureLinkAndSecureAsset(t *testing.T) {
	builder := newBuilder()

	if got := string(builder.SecureLink("/a", "Go", nil)); !strings.Contains(got, `href="https://example.test/a"`) {
		t.Errorf("SecureLink = %q", got)
	}
	if got := string(builder.LinkSecureAsset("app.css", "", nil)); !strings.Contains(got, `href="https://cdn.test/app.css"`) {
		t.Errorf("LinkSecureAsset = %q", got)
	}
}

func TestLinkRouteAndLinkAction(t *testing.T) {
	builder := newBuilder()

	got, err := builder.LinkRoute("invoices.show", "Invoice", nil, nil)
	if err != nil {
		t.Fatalf("LinkRoute: %v", err)
	}
	if !strings.Contains(string(got), `href="http://example.test/invoices/7"`) {
		t.Errorf("LinkRoute = %q", got)
	}

	if _, err := builder.LinkAction("InvoiceController@show", "Invoice", nil, nil); err != nil {
		t.Fatalf("LinkAction: %v", err)
	}
}

// PHP throws for an unknown name; this returns the error.
func TestLinkRouteReportsAFailedLookup(t *testing.T) {
	urls := newFakeUrls()
	urls.err = errNoRoute
	builder := NewHtmlBuilder(urls)

	if _, err := builder.LinkRoute("absent", "x", nil, nil); err == nil {
		t.Fatal("LinkRoute with an unknown name returned no error")
	}
	if _, err := builder.LinkAction("absent", "x", nil, nil); err == nil {
		t.Fatal("LinkAction with an unknown name returned no error")
	}
}

var errNoRoute = url.InvalidHostError("no such route")

func TestOlAndUlEscapeTheLeaves(t *testing.T) {
	got := string(newBuilder().Ul([]ListItem{{Label: "<b>one</b>"}}, Attrs{"class": "list"}))
	want := `<ul class="list"><li>&lt;b&gt;one&lt;/b&gt;</li></ul>`

	if got != want {
		t.Fatalf("Ul = %q, want %q", got, want)
	}
}

// HtmlBuilder.php:305 concatenates the heading of a nested list raw.
func TestNestedListingEscapesTheHeading(t *testing.T) {
	got := string(newBuilder().Ol([]ListItem{
		{Label: `<img src=x onerror=alert(1)>`, Items: []ListItem{{Label: "a"}}},
	}, nil))

	if strings.Contains(got, "<img") {
		t.Fatalf("nested listing did not escape the heading: %s", got)
	}
	if !strings.Contains(got, "<li>&lt;img") {
		t.Fatalf("nested listing = %q", got)
	}
}

// An integer key in PHP nests without an <li> around it.
func TestNestedListingWithNoHeadingHasNoListItem(t *testing.T) {
	got := string(newBuilder().Ol([]ListItem{{Items: []ListItem{{Label: "a"}}}}, nil))
	want := "<ol><ol><li>a</li></ol></ol>"

	if got != want {
		t.Fatalf("Ol = %q, want %q", got, want)
	}
}

func TestEmptyListWritesNothing(t *testing.T) {
	if got := string(newBuilder().Ul(nil, Attrs{"class": "x"})); got != "" {
		t.Fatalf("Ul(nil) = %q, want empty", got)
	}
}

// Obfuscate is random. What is stable is what a reader sees.
func TestObfuscateRendersAsTheOriginal(t *testing.T) {
	builder := newBuilder()

	for _, value := range []string{"ada@example.com", "mailto:", "a.b-c_d+e@sub.example.co.uk"} {
		got := stdhtml.UnescapeString(string(builder.Obfuscate(value)))
		if got != value {
			t.Errorf("Obfuscate(%q) decoded to %q", value, got)
		}
	}
}

// HtmlBuilder.php:358 returns from inside the loop on the first byte over 128,
// throwing away everything obfuscated so far.
func TestObfuscateKeepsNonAsciiAndCarriesOn(t *testing.T) {
	value := "joão@example.com"

	got := stdhtml.UnescapeString(string(newBuilder().Obfuscate(value)))
	if got != value {
		t.Fatalf("Obfuscate(%q) decoded to %q -- the PHP returns one byte here", value, got)
	}
}

// PHP's third case writes the byte raw, and Mailto puts the result in an href.
func TestObfuscateNeverEmitsARawQuote(t *testing.T) {
	value := `a"b'c<d>e&f`

	for range 200 {
		got := string(newBuilder().Obfuscate(value))
		if strings.ContainsAny(got, `"'<>`) {
			t.Fatalf("Obfuscate emitted a raw markup character: %q", got)
		}
	}
}

func TestEmailHidesTheAtSign(t *testing.T) {
	got := string(newBuilder().Email("ada@example.com"))

	if strings.Contains(got, "@") {
		t.Fatalf("Email = %q, want no literal at-sign", got)
	}
	if decoded := stdhtml.UnescapeString(got); decoded != "ada@example.com" {
		t.Fatalf("Email decoded to %q", decoded)
	}
}

func TestMailtoRendersTheAddressAndKeepsTheHrefUsable(t *testing.T) {
	got := string(newBuilder().Mailto("ada@example.com", "", nil))

	if strings.Contains(got, "@") {
		t.Fatalf("Mailto = %q, want no literal at-sign", got)
	}

	decoded := stdhtml.UnescapeString(got)
	if !strings.Contains(decoded, `href="mailto:ada@example.com"`) {
		t.Fatalf("Mailto decoded to %q", decoded)
	}
	if !strings.Contains(decoded, ">ada@example.com</a>") {
		t.Fatalf("Mailto decoded to %q, want the address as the text", decoded)
	}
}

func TestMailtoEscapesAGivenTitle(t *testing.T) {
	got := string(newBuilder().Mailto("ada@example.com", "<b>Ada</b>", nil))

	if strings.Contains(got, "<b>") {
		t.Fatalf("Mailto did not escape the title: %s", got)
	}
}
