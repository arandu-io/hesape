package html

import (
	stdhtml "html"
	"html/template"
	"math/rand/v2"
	"strconv"
	"strings"
)

// HtmlBuilder builds escaped HTML fragments -- links, lists, obfuscated
// addresses -- as template.HTML values.
//
// Every method that produces markup returns template.HTML, which says the value
// is escaped and must not be escaped again. [HtmlBuilder.Decode] is the one
// exception and returns string, because what it produces is the opposite.
type HtmlBuilder struct {
	// url is the URL generator. It may be nil, and then every method that
	// touches a URL panics on the first call; the methods that would
	// dereference it say so.
	url UrlGenerator
}

// NewHtmlBuilder returns a builder over a URL generator.
//
// url may be nil. [HtmlBuilder.Entities], [HtmlBuilder.Decode],
// [HtmlBuilder.Attributes], [HtmlBuilder.Obfuscate], [HtmlBuilder.Email],
// [HtmlBuilder.Ol] and [HtmlBuilder.Ul] work without one; everything else
// needs it.
func NewHtmlBuilder(url UrlGenerator) *HtmlBuilder { return &HtmlBuilder{url: url} }

// Entities escapes value the same way every other method in this package
// does.
//
// See [escape] for what a fuller escaping scheme would do that this does
// not, and for why not double-encoding matters here.
func (h *HtmlBuilder) Entities(value string) template.HTML {
	return template.HTML(escape(value))
}

// Decode reverses HTML entity escaping, decoding value back to plain text.
//
// It returns string and not template.HTML, which is the one place in this
// package where the return type breaks the pattern. What comes out is
// markup that has been un-escaped: handing it back as template.HTML would
// tell the view layer that a string containing a live script tag is safe
// to write, which is the exact inversion of what this package is for.
func (h *HtmlBuilder) Decode(value string) string {
	return stdhtml.UnescapeString(value)
}

// Script builds a script tag pointing at an asset.
func (h *HtmlBuilder) Script(url string, attributes Attrs, secure bool) template.HTML {
	attributes = cloneAttrs(attributes)
	attributes["src"] = h.asset(url, secure)

	return template.HTML("<script" + string(h.Attributes(attributes)) + "></script>\n")
}

// Style builds a stylesheet link.
//
// media, type and rel default to all, text/css and stylesheet. An
// attribute the caller passed wins over the default on a collision.
func (h *HtmlBuilder) Style(url string, attributes Attrs, secure bool) template.HTML {
	merged := Attrs{"media": "all", "type": "text/css", "rel": "stylesheet"}
	for key, value := range attributes {
		merged[key] = value
	}
	merged["href"] = h.asset(url, secure)

	return template.HTML("<link" + string(h.Attributes(merged)) + ">\n")
}

// Image builds an img tag.
//
// # Two things worth knowing
//
// The resolved URL is escaped before being concatenated into a quoted
// attribute: an asset path carrying a double quote would otherwise close
// the attribute and open whatever follows it. It is the same hole as in
// [HtmlBuilder.Link].
//
// The alt attribute is always written, even when empty, so an image with
// no alt text says it has none rather than leaving a screen reader to read
// the file name out.
func (h *HtmlBuilder) Image(url, alt string, attributes Attrs, secure bool) template.HTML {
	attributes = cloneAttrs(attributes)
	attributes["alt"] = alt

	return template.HTML(`<img src="` + escape(h.asset(url, secure)) + `"` + string(h.Attributes(attributes)) + ">")
}

// Link builds an anchor tag.
//
// An empty title falls back to using the URL as the text.
//
// The resolved URL is escaped before being concatenated into a quoted
// attribute. Note what escaping does and does not buy: it stops the
// attribute being closed, and it does not stop a `javascript:` URL, which
// is a scheme rather than a syntax problem and is noted in the package
// comment.
func (h *HtmlBuilder) Link(url, title string, attributes Attrs, secure bool) template.HTML {
	url = h.to(url, secure)
	if title == "" {
		title = url
	}

	return template.HTML(`<a href="` + escape(url) + `"` + string(h.Attributes(attributes)) + ">" +
		escape(title) + "</a>")
}

// SecureLink is [HtmlBuilder.Link] over https.
func (h *HtmlBuilder) SecureLink(url, title string, attributes Attrs) template.HTML {
	return h.Link(url, title, attributes, true)
}

// LinkAsset builds a link whose target is an asset.
//
// It resolves the asset and hands the result to [HtmlBuilder.Link], which
// resolves it again -- which is why UrlGenerator.To has to return an
// absolute URL unchanged.
func (h *HtmlBuilder) LinkAsset(url, title string, attributes Attrs, secure bool) template.HTML {
	url = h.asset(url, secure)
	if title == "" {
		title = url
	}

	return h.Link(url, title, attributes, secure)
}

// LinkSecureAsset is [HtmlBuilder.LinkAsset] over https.
func (h *HtmlBuilder) LinkSecureAsset(url, title string, attributes Attrs) template.HTML {
	return h.LinkAsset(url, title, attributes, true)
}

// LinkRoute builds a link to a named route.
//
// It returns (template.HTML, error) rather than panicking for a name that
// is not registered.
func (h *HtmlBuilder) LinkRoute(name, title string, parameters []string, attributes Attrs) (template.HTML, error) {
	url, err := h.url.Route(name, parameters...)
	if err != nil {
		return "", err
	}
	return h.Link(url, title, attributes, false), nil
}

// LinkAction builds a link to a controller action.
func (h *HtmlBuilder) LinkAction(action, title string, parameters []string, attributes Attrs) (template.HTML, error) {
	url, err := h.url.Action(action, parameters...)
	if err != nil {
		return "", err
	}
	return h.Link(url, title, attributes, false), nil
}

// Mailto builds a link to an address, obfuscated.
//
// An empty title falls back to the obfuscated address -- which is then
// passed through [HtmlBuilder.Entities], and survives it intact only
// because [HtmlBuilder.Entities] does not double-encode. See [escape].
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

// Email obfuscates an address, with every at-sign that survived turned
// into &#64;.
func (h *HtmlBuilder) Email(email string) template.HTML {
	return template.HTML(strings.ReplaceAll(string(h.Obfuscate(email)), "@", "&#64;"))
}

// Obfuscate rewrites a string so a spam-bot reading the response does not
// find an address in it.
//
// Each byte becomes its decimal entity, its hexadecimal entity, or itself, at
// random. The result renders as the original.
//
// # Two guarantees this makes
//
// A byte over 127 is written through unmodified, and the loop carries on
// to the rest of the string: an address with a non-ASCII character in it
// does not come back truncated at the first one.
//
// The third case falls back to the decimal entity for the five characters
// that matter, rather than writing the byte raw: [HtmlBuilder.Mailto] puts
// this straight into a quoted href, so a value carrying a double quote
// must never emit one. The output is always safe in an attribute and
// always renders the same.
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

// ListItem is one entry of the list [HtmlBuilder.Ol] and [HtmlBuilder.Ul]
// take.
//
// There are three shapes:
//
//	ListItem{Label: "one"}                          <li>one</li>
//	ListItem{Items: [...]}                          a nested list, no <li> around it
//	ListItem{Label: "group", Items: [...]}          <li>group<ol>...</ol></li>
//
// The second and third are distinguished by whether Label is empty -- see
// [nestedListing].
type ListItem struct {
	// Label is the text of a leaf, or the heading of a nested list.
	Label string

	// Items being non-nil makes this a nested list rather than a leaf.
	Items []ListItem
}

// Ol builds an ordered list.
func (h *HtmlBuilder) Ol(list []ListItem, attributes Attrs) template.HTML {
	return h.listing("ol", list, attributes)
}

// Ul builds an unordered list.
func (h *HtmlBuilder) Ul(list []ListItem, attributes Attrs) template.HTML {
	return h.listing("ul", list, attributes)
}

// listing builds an ol or ul tag. An empty list writes nothing.
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

// listingElement builds one <li>, nested or a leaf.
func (h *HtmlBuilder) listingElement(listType string, item ListItem) template.HTML {
	if item.Items != nil {
		return h.nestedListing(listType, item)
	}
	return template.HTML("<li>" + escape(item.Label) + "</li>")
}

// nestedListing builds a nested list, with a heading when Label is set.
//
// The heading is escaped, the same as a leaf: a list built from anything a
// person typed must not carry markup straight into the page.
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
