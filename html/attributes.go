package html

import (
	"html/template"
	"maps"
	"slices"
	"strings"
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
// The legal set is HTML5's, stated as what it forbids, so hx-post,
// data-copy-text, @click and :class all pass.
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
	if !isAttributeName(key) {
		return ""
	}
	return key + `="` + escape(value) + `"`
}

// isAttributeName reports whether key may be written as an HTML attribute name.
//
// HTML5 states this as a prohibition rather than an alphabet: anything except
// a control character, a space, a quote, an apostrophe, a greater-than, a
// slash, an equals sign, and the noncharacters. Reading it that way is what
// keeps the framework's own attributes -- HTMX's hx-*, the data-* hooks the
// client behaviours dispatch on, and the punctuation-led names a library may
// still ask for -- working without an allow-list somebody has to remember to
// extend.
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
// document into their entity references, without double-encoding one that
// is already there.
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
// # Not double-encoding, which is load-bearing
//
// An ampersand that already opens a well-formed entity reference is left as it
// stands, so &amp; stays &amp; instead of becoming &amp;amp;. It is not a
// detail: [HtmlBuilder.Mailto] runs an already-obfuscated address, full of
// &#64; and &#x40;, back through this, and every one of them would be shown
// to the reader as text if this re-encoded.
func escape(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 16)

	for i := 0; i < len(value); {
		switch value[i] {
		case '&':
			if n := entityLength(value[i:]); n > 0 {
				b.WriteString(value[i : i+n])
				i += n
				continue
			}
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
		i++
	}

	return b.String()
}

// entityLength reports the length of the entity reference s opens, or zero.
//
// s begins with an ampersand. The three shapes are the three an HTML entity
// reference can take: &name;, &#1234; and &#x4a;.
func entityLength(s string) int {
	if len(s) < 3 {
		return 0
	}

	i := 1
	switch {
	case s[i] == '#':
		i++
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i++
			start := i
			for i < len(s) && isHexDigit(s[i]) {
				i++
			}
			if i == start {
				return 0
			}
		} else {
			start := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i == start {
				return 0
			}
		}
	case isAlpha(s[i]):
		for i < len(s) && (isAlpha(s[i]) || (s[i] >= '0' && s[i] <= '9')) {
			i++
		}
	default:
		return 0
	}

	if i < len(s) && s[i] == ';' {
		return i + 1
	}
	return 0
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
