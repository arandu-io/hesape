package html

import (
	"html/template"
	"maps"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/view"
)

// Attrs is the attribute map every method in this package takes last.
//
// It represents three cases as one map:
//
//	Attrs{"class": "btn"}          class="btn"
//	Attrs{"value": ""}             value=""
//	Attrs{"required": "required"}  required="required"
//	the key is absent              nothing is written
//
// The third is written with the key repeated as its own value, because that
// is what a Go map's key-value shape asks for, and it is the same thing a
// boolean HTML attribute renders as. The fourth -- an attribute that must
// not appear at all -- is spelled by leaving the key out of the map.
//
// # Order
//
// A Go map has no order, so the attributes come out sorted by name.
// Attribute order carries no meaning in HTML, and sorted is what makes a
// golden test possible.
type Attrs map[string]string

// Attributes builds an attribute string from a map, with a leading space
// when it is not empty.
//
// The value is escaped. The key is not escaped -- escaping a name would
// produce an attribute nobody asked for -- but a key that is not a legal
// HTML attribute name is dropped rather than concatenated as given:
// concatenating one lets the caller close the tag, and
// `Attrs{`x"><script>`: "1"}` would be a script tag.
//
// A key the browser or a client library would act on is dropped too: an
// event handler, the HTMX family, an Alpine directive, style, srcdoc and
// http-equiv. Escaping a value does not make any of those inert, and the list
// is view.ActiveAttribute's, so the builders here and the view layer refuse
// the same names. A request or a script belongs in a component that takes it
// as a field, not in a map of attributes.
func (h *HtmlBuilder) Attributes(attributes Attrs) template.HTML {
	if len(attributes) == 0 {
		return ""
	}

	elements := make([]string, 0, len(attributes))
	for _, key := range slices.Sorted(maps.Keys(attributes)) {
		if element := h.attributeElement(key, attributes[key]); element != "" {
			elements = append(elements, element)
		}
	}

	if len(elements) == 0 {
		return ""
	}
	return template.HTML(" " + strings.Join(elements, " "))
}

// attributeElement builds one key="value" pair, or "" when key is not a
// legal attribute name.
func (h *HtmlBuilder) attributeElement(key, value string) string {
	if !isAttributeName(key) || view.ActiveAttribute(key) != nil {
		return ""
	}
	return key + `="` + escape(value) + `"`
}

// isAttributeName reports whether key may be written as an HTML attribute name.
//
// HTML5 states this as a prohibition rather than an alphabet: anything except
// a control character, a space, a quote, an apostrophe, a greater-than, a
// slash, an equals sign, and the noncharacters. Reading it that way keeps the
// data-* hooks the client behaviours dispatch on, and the aria- and form
// attributes, working without an allow-list somebody has to remember to
// extend. What a browser would execute is a separate question, answered by
// view.ActiveAttribute.
func isAttributeName(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r <= 0x1f, r == 0x7f:
			return false
		case r == ' ', r == '"', r == '\'', r == '>', r == '/', r == '=':
			return false
		case r == 0xfffd:
			// The replacement character means the input was not valid UTF-8,
			// and an attribute name is not the place to find that out.
			return false
		}
	}
	return true
}

// cloneAttrs copies the caller's map before anything is written into it: a
// Go map is a reference, so without the copy every call to Script would
// leave an src in the caller's map, and the second image on the page would
// carry the first one's source.
func cloneAttrs(attributes Attrs) Attrs {
	clone := make(Attrs, len(attributes)+4)
	maps.Copy(clone, attributes)
	return clone
}

// escape converts the characters that can change the shape of an HTML
// document into their entity references.
//
// # What it does and does not convert
//
// It converts the five characters that can change the shape of a document --
// &, <, >, " and ' -- and leaves every other character alone. A fuller
// escaping scheme would also convert every character that has a named
// entity, so it would write an e-acute as &eacute; where this leaves it as
// the two UTF-8 bytes it already was. In a document served as UTF-8 the two
// render identically, and Go has no named entity table to encode with; the
// security-carrying half of a fuller scheme is the five, and that half is
// here in full.
//
// # Every ampersand, including one that opens an entity
//
// An ampersand is always encoded, so &amp; becomes &amp;amp; and shows as the
// text it was. Leaving a well-formed reference alone looks harmless and is
// not: the parser decodes it before anything reads the attribute, so
// "javascript&colon;alert(1)" reaches a scheme check without a colon in it and
// reaches the browser as a javascript: URL. A value that is already markup --
// what [HtmlBuilder.Obfuscate] produces -- is written as it stands by the
// caller that made it, and never passes through here.
func escape(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 16)

	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#039;")
		default:
			b.WriteByte(value[i])
		}
	}

	return b.String()
}
