package html

import (
	stdhtml "html"
	"html/template"
	"math/rand/v2"
	"strconv"
	"strings"
)

// HtmlBuilder answers to Illuminate\Html\HtmlBuilder.
//
// Every method that produces markup returns template.HTML, which says the value
// is escaped and must not be escaped again. [HtmlBuilder.Decode] is the one
// exception and returns string, because what it produces is the opposite.
type HtmlBuilder struct {
	// url is the URL generator. PHP allows null and then every method that
	// touches a URL is a fatal error on the first call; the constructor here
	// takes it the same way, and the methods that would dereference it say so.
	url UrlGenerator
}

// NewHtmlBuilder answers to HtmlBuilder::__construct.
//
// url may be nil, as PHP's may be null. [HtmlBuilder.Entities],
// [HtmlBuilder.Decode], [HtmlBuilder.Attributes], [HtmlBuilder.Obfuscate],
// [HtmlBuilder.Email], [HtmlBuilder.Ol] and [HtmlBuilder.Ul] work without one;
// everything else needs it.
func NewHtmlBuilder(url UrlGenerator) *HtmlBuilder { return &HtmlBuilder{url: url} }

// Entities answers to HtmlBuilder::entities.
//
// See [escape] for what htmlentities does that this does not, and for why
// double_encode false matters here.
func (h *HtmlBuilder) Entities(value string) template.HTML {
	return template.HTML(escape(value))
}

// Decode answers to HtmlBuilder::decode: html_entity_decode with ENT_QUOTES.
//
// It returns string and not template.HTML, and that is the one place in this
// package where the return type is not the mechanical translation. What comes
// out is markup that has been un-escaped: handing it back as template.HTML
// would tell the view layer that a string containing a live script tag is safe
// to write, which is the exact inversion of what this package is for.
func (h *HtmlBuilder) Decode(value string) string {
	return stdhtml.UnescapeString(value)
}

// Script answers to HtmlBuilder::script: a script tag pointing at an asset.
//
// The trailing newline is PHP's PHP_EOL.
func (h *HtmlBuilder) Script(url string, attributes Attrs, secure bool) template.HTML {
	attributes = cloneAttrs(attributes)
	attributes["src"] = h.asset(url, secure)

	return template.HTML("<script" + string(h.Attributes(attributes)) + "></script>\n")
}

// Style answers to HtmlBuilder::style: a stylesheet link.
//
// media, type and rel default to all, text/css and stylesheet. PHP writes
// `$attributes + $defaults`, where the union operator keeps the left side on a
// collision, so an attribute the caller passed wins over the default. So does
// this.
func (h *HtmlBuilder) Style(url string, attributes Attrs, secure bool) template.HTML {
	merged := Attrs{"media": "all", "type": "text/css", "rel": "stylesheet"}
	for key, value := range attributes {
		merged[key] = value
	}
	merged["href"] = h.asset(url, secure)

	return template.HTML("<link" + string(h.Attributes(merged)) + ">\n")
}

// Image answers to HtmlBuilder::image.
//
// # Two differences from PHP, both deliberate
//
// PHP writes `'<img src="'.$this->url->asset($url).'"'` -- the resolved URL is
// concatenated into a quoted attribute without being escaped, so an asset path
// carrying a double quote closes the attribute and opens whatever follows it.
// Here the URL is escaped. It is the same hole as in [HtmlBuilder.Link].
//
// PHP also drops the alt attribute entirely when $alt is null, because
// attributeElement omits a null. This always writes it, so an image with no
// alt text says it has none rather than leaving a screen reader to read the
// file name out.
func (h *HtmlBuilder) Image(url, alt string, attributes Attrs, secure bool) template.HTML {
	attributes = cloneAttrs(attributes)
	attributes["alt"] = alt

	return template.HTML(`<img src="` + escape(h.asset(url, secure)) + `"` + string(h.Attributes(attributes)) + ">")
}

// Link answers to HtmlBuilder::link.
//
// An empty title is PHP's null, and the URL is used as the text.
//
// PHP writes `'<a href="'.$url.'"'`, concatenating the resolved URL into a
// quoted attribute unescaped. Here it is escaped. Note what escaping does and
// does not buy: it stops the attribute being closed, and it does not stop a
// `javascript:` URL, which is a scheme rather than a syntax problem and is
// noted in the package comment.
func (h *HtmlBuilder) Link(url, title string, attributes Attrs, secure bool) template.HTML {
	url = h.to(url, secure)
	if title == "" {
		title = url
	}

	return template.HTML(`<a href="` + escape(url) + `"` + string(h.Attributes(attributes)) + ">" +
		escape(title) + "</a>")
}

// SecureLink answers to HtmlBuilder::secureLink: [HtmlBuilder.Link] over https.
func (h *HtmlBuilder) SecureLink(url, title string, attributes Attrs) template.HTML {
	return h.Link(url, title, attributes, true)
}

// LinkAsset answers to HtmlBuilder::linkAsset: a link whose target is an asset.
//
// It resolves the asset and hands the result to [HtmlBuilder.Link], which
// resolves it again. That is PHP's shape, and it is why UrlGenerator.To has to
// return an absolute URL unchanged.
func (h *HtmlBuilder) LinkAsset(url, title string, attributes Attrs, secure bool) template.HTML {
	url = h.asset(url, secure)
	if title == "" {
		title = url
	}

	return h.Link(url, title, attributes, secure)
}

// LinkSecureAsset answers to HtmlBuilder::linkSecureAsset.
func (h *HtmlBuilder) LinkSecureAsset(url, title string, attributes Attrs) template.HTML {
	return h.LinkAsset(url, title, attributes, true)
}

// LinkRoute answers to HtmlBuilder::linkRoute: a link to a named route.
//
// PHP throws InvalidArgumentException for a name that is not registered; this
// returns (template.HTML, error), which is the translation ADR 0044 allows.
func (h *HtmlBuilder) LinkRoute(name, title string, parameters []string, attributes Attrs) (template.HTML, error) {
	url, err := h.url.Route(name, parameters...)
	if err != nil {
		return "", err
	}
	return h.Link(url, title, attributes, false), nil
}

// LinkAction answers to HtmlBuilder::linkAction: a link to a controller action.
func (h *HtmlBuilder) LinkAction(action, title string, parameters []string, attributes Attrs) (template.HTML, error) {
	url, err := h.url.Action(action, parameters...)
	if err != nil {
		return "", err
	}
	return h.Link(url, title, attributes, false), nil
}

// Mailto answers to HtmlBuilder::mailto: a link to an address, obfuscated.
//
// An empty title is PHP's null, and then the title is the obfuscated address --
// which is then passed through [HtmlBuilder.Entities], and survives it intact
// only because entities does not double-encode. See [escape].
//
// The href is safe to write unescaped here, which it is not in
// [HtmlBuilder.Link]: everything in it came out of [HtmlBuilder.Obfuscate],
// which never emits a raw quote.
func (h *HtmlBuilder) Mailto(email, title string, attributes Attrs) template.HTML {
	obfuscated := string(h.Email(email))

	if title == "" {
		title = obfuscated
	}

	href := string(h.Obfuscate("mailto:")) + obfuscated

	return template.HTML(`<a href="` + href + `"` + string(h.Attributes(attributes)) + ">" +
		escape(title) + "</a>")
}

// Email answers to HtmlBuilder::email: an address obfuscated, with every
// at-sign that survived turned into &#64;.
func (h *HtmlBuilder) Email(email string) template.HTML {
	return template.HTML(strings.ReplaceAll(string(h.Obfuscate(email)), "@", "&#64;"))
}

// Obfuscate answers to HtmlBuilder::obfuscate: a string rewritten so a
// spam-bot reading the response does not find an address in it.
//
// Each byte becomes its decimal entity, its hexadecimal entity, or itself, at
// random. The result renders as the original.
//
// # Two departures from the PHP, both because the PHP is wrong there
//
// PHP writes `if (ord($letter) > 128) return $letter;` -- a return, not a
// continue, from inside the loop (HtmlBuilder.php:358). The first byte over 128
// ends the function and throws away everything obfuscated so far, so any
// address with a non-ASCII character in it comes back as one byte of itself.
// Here a byte over 127 is written through and the loop carries on, which is
// what that line was trying to do.
//
// PHP's third case writes the byte raw, so a value carrying a double quote
// emits one -- and [HtmlBuilder.Mailto] puts this straight into a quoted href.
// Here the third case falls back to the decimal entity for the five characters
// that matter, so the output is always safe in an attribute and always renders
// the same.
//
// The output differs between calls, by design. What is stable is what a reader
// sees, which is what a test should assert.
func (h *HtmlBuilder) Obfuscate(value string) template.HTML {
	var b strings.Builder
	b.Grow(len(value) * 4)

	for i := 0; i < len(value); i++ {
		letter := value[i]

		if letter > 127 {
			b.WriteByte(letter)
			continue
		}

		switch rand.IntN(3) {
		case 0:
			b.WriteString("&#" + strconv.Itoa(int(letter)) + ";")
		case 1:
			b.WriteString("&#x" + strconv.FormatUint(uint64(letter), 16) + ";")
		default:
			if isMarkupByte(letter) {
				b.WriteString("&#" + strconv.Itoa(int(letter)) + ";")
			} else {
				b.WriteByte(letter)
			}
		}
	}

	return template.HTML(b.String())
}

// isMarkupByte reports whether writing the byte raw could change the shape of
// the document it lands in.
func isMarkupByte(c byte) bool {
	return c == '&' || c == '<' || c == '>' || c == '"' || c == '\''
}

// ListItem is one entry of the $list array [HtmlBuilder.Ol] and
// [HtmlBuilder.Ul] take.
//
// PHP takes an array whose keys carry meaning, and the three shapes it reads
// are these three:
//
//	ListItem{Label: "one"}                          <li>one</li>
//	ListItem{Items: [...]}                          a nested list, no <li> around it
//	ListItem{Label: "group", Items: [...]}          <li>group<ol>...</ol></li>
//
// The second is PHP's integer key and the third is PHP's string key -- see
// HtmlBuilder::nestedListing, which branches on is_int($key).
type ListItem struct {
	// Label is the text of a leaf, or the heading of a nested list.
	Label string

	// Items being non-nil makes this a nested list rather than a leaf.
	Items []ListItem
}

// Ol answers to HtmlBuilder::ol.
func (h *HtmlBuilder) Ol(list []ListItem, attributes Attrs) template.HTML {
	return h.listing("ol", list, attributes)
}

// Ul answers to HtmlBuilder::ul.
func (h *HtmlBuilder) Ul(list []ListItem, attributes Attrs) template.HTML {
	return h.listing("ul", list, attributes)
}

// listing answers to HtmlBuilder::listing. An empty list writes nothing, which
// is PHP's early return.
func (h *HtmlBuilder) listing(listType string, list []ListItem, attributes Attrs) template.HTML {
	if len(list) == 0 {
		return ""
	}

	var items strings.Builder
	for _, item := range list {
		items.WriteString(string(h.listingElement(listType, item)))
	}

	return template.HTML("<" + listType + string(h.Attributes(attributes)) + ">" +
		items.String() + "</" + listType + ">")
}

// listingElement answers to HtmlBuilder::listingElement.
func (h *HtmlBuilder) listingElement(listType string, item ListItem) template.HTML {
	if item.Items != nil {
		return h.nestedListing(listType, item)
	}
	return template.HTML("<li>" + escape(item.Label) + "</li>")
}

// nestedListing answers to HtmlBuilder::nestedListing.
//
// PHP writes `'<li>'.$key.$this->listing(...)` -- the heading of a nested list
// is concatenated raw, while the leaves one line above it go through e(). A
// list built from anything a person typed carries markup straight into the page
// there. Here the heading is escaped like the leaves.
func (h *HtmlBuilder) nestedListing(listType string, item ListItem) template.HTML {
	if item.Label == "" {
		return h.listing(listType, item.Items, nil)
	}
	return template.HTML("<li>" + escape(item.Label) + string(h.listing(listType, item.Items, nil)) + "</li>")
}

// to resolves a path through the generator, over https when secure.
func (h *HtmlBuilder) to(path string, secure bool) string {
	if secure {
		return h.url.Secure(path)
	}
	return h.url.To(path)
}

// asset resolves an asset path through the generator, over https when secure.
func (h *HtmlBuilder) asset(path string, secure bool) string {
	if secure {
		return h.url.SecureAsset(path)
	}
	return h.url.Asset(path)
}
