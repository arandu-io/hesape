package qr

import (
	"fmt"
	"strings"
)

// MarkupError reports caller-supplied markup this package refuses to place in
// the rendered symbol.
type MarkupError struct {
	// Reason says what was refused.
	Reason string
	// Offset is the byte position in the markup where it was refused.
	Offset int
}

func (e *MarkupError) Error() string {
	return fmt.Sprintf("qr: reserved center content rejected at byte %d: %s", e.Offset, e.Reason)
}

// allowedElements are the SVG elements caller-supplied markup may use. Every
// element that loads a resource, runs code, or carries a stylesheet is absent
// on purpose.
var allowedElements = map[string]bool{
	"circle":         true,
	"defs":           true,
	"desc":           true,
	"ellipse":        true,
	"g":              true,
	"line":           true,
	"linearGradient": true,
	"path":           true,
	"polygon":        true,
	"polyline":       true,
	"radialGradient": true,
	"rect":           true,
	"stop":           true,
	"text":           true,
	"title":          true,
	"tspan":          true,
}

// allowedAttributes are the attributes caller-supplied markup may set. The
// list holds geometry, paint, and text placement, and nothing that names a
// resource or carries declarations.
var allowedAttributes = map[string]bool{
	"cx":                  true,
	"cy":                  true,
	"d":                   true,
	"dx":                  true,
	"dy":                  true,
	"fill":                true,
	"fill-opacity":        true,
	"fill-rule":           true,
	"font-family":         true,
	"font-size":           true,
	"font-style":          true,
	"font-weight":         true,
	"gradientTransform":   true,
	"gradientUnits":       true,
	"height":              true,
	"id":                  true,
	"letter-spacing":      true,
	"offset":              true,
	"opacity":             true,
	"points":              true,
	"preserveAspectRatio": true,
	"r":                   true,
	"rx":                  true,
	"ry":                  true,
	"stop-color":          true,
	"stop-opacity":        true,
	"stroke":              true,
	"stroke-dasharray":    true,
	"stroke-linecap":      true,
	"stroke-linejoin":     true,
	"stroke-opacity":      true,
	"stroke-width":        true,
	"text-anchor":         true,
	"transform":           true,
	"viewBox":             true,
	"width":               true,
	"x":                   true,
	"x1":                  true,
	"x2":                  true,
	"y":                   true,
	"y1":                  true,
	"y2":                  true,
}

// validateMarkup checks caller-supplied SVG markup against the element and
// attribute lists, and refuses any value that would reach outside the
// document or carry style declarations.
//
// It is a gate, not a sanitiser: it never rewrites the markup.
func validateMarkup(s string) error {
	i := 0
	depth := 0
	for i < len(s) {
		if s[i] != '<' {
			if s[i] == '>' {
				return &MarkupError{Reason: "stray > outside a tag", Offset: i}
			}
			i++
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return &MarkupError{Reason: "unterminated comment", Offset: i}
			}
			i += 4 + end + 3
		case strings.HasPrefix(s[i:], "<!"), strings.HasPrefix(s[i:], "<?"):
			return &MarkupError{Reason: "declarations and processing instructions are not allowed", Offset: i}
		case strings.HasPrefix(s[i:], "</"):
			name, next, err := scanName(s, i+2, i)
			if err != nil {
				return err
			}
			if !allowedElements[name] {
				return &MarkupError{Reason: fmt.Sprintf("element %q is not allowed", name), Offset: i}
			}
			next = skipSpace(s, next)
			if next >= len(s) || s[next] != '>' {
				return &MarkupError{Reason: "malformed closing tag", Offset: i}
			}
			depth--
			if depth < 0 {
				return &MarkupError{Reason: "closing tag without an opening tag", Offset: i}
			}
			i = next + 1
		default:
			name, next, err := scanName(s, i+1, i)
			if err != nil {
				return err
			}
			if !allowedElements[name] {
				return &MarkupError{Reason: fmt.Sprintf("element %q is not allowed", name), Offset: i}
			}
			next, selfClosing, err := scanAttributes(s, next)
			if err != nil {
				return err
			}
			if !selfClosing {
				depth++
			}
			i = next
		}
	}
	if depth != 0 {
		return &MarkupError{Reason: "unclosed element", Offset: len(s)}
	}
	return nil
}

// scanName reads an element or attribute name starting at i.
func scanName(s string, i, tagStart int) (name string, next int, err error) {
	start := i
	for i < len(s) && isNameByte(s[i]) {
		i++
	}
	if i == start {
		return "", 0, &MarkupError{Reason: "missing name", Offset: tagStart}
	}
	return s[start:i], i, nil
}

func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' ||
		b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' ||
		b == '-' || b == '_' || b == ':' || b == '.'
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// scanAttributes reads the attribute list of an opening tag and the closing
// bracket, checking every name and value.
func scanAttributes(s string, i int) (next int, selfClosing bool, err error) {
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			return 0, false, &MarkupError{Reason: "unterminated tag", Offset: len(s)}
		}
		if s[i] == '>' {
			return i + 1, false, nil
		}
		if strings.HasPrefix(s[i:], "/>") {
			return i + 2, true, nil
		}
		start := i
		name, after, err := scanName(s, i, start)
		if err != nil {
			return 0, false, err
		}
		lower := strings.ToLower(name)
		switch {
		case strings.HasPrefix(lower, "on"):
			return 0, false, &MarkupError{Reason: fmt.Sprintf("event handler attribute %q is not allowed", name), Offset: start}
		case lower == "style":
			return 0, false, &MarkupError{Reason: "style attributes are not allowed", Offset: start}
		case lower == "href" || strings.HasSuffix(lower, ":href"):
			return 0, false, &MarkupError{Reason: "attributes that name a resource are not allowed", Offset: start}
		case !allowedAttributes[name]:
			return 0, false, &MarkupError{Reason: fmt.Sprintf("attribute %q is not allowed", name), Offset: start}
		}

		after = skipSpace(s, after)
		if after >= len(s) || s[after] != '=' {
			return 0, false, &MarkupError{Reason: fmt.Sprintf("attribute %q has no value", name), Offset: start}
		}
		after = skipSpace(s, after+1)
		if after >= len(s) || (s[after] != '"' && s[after] != '\'') {
			return 0, false, &MarkupError{Reason: fmt.Sprintf("attribute %q has an unquoted value", name), Offset: start}
		}
		quote := s[after]
		end := strings.IndexByte(s[after+1:], quote)
		if end < 0 {
			return 0, false, &MarkupError{Reason: fmt.Sprintf("attribute %q has an unterminated value", name), Offset: start}
		}
		value := s[after+1 : after+1+end]
		if err := checkAttributeValue(name, value, start); err != nil {
			return 0, false, err
		}
		i = after + 1 + end + 1
	}
}

// checkAttributeValue refuses values that would fetch something, run
// something, or break out of the attribute.
func checkAttributeValue(name, value string, offset int) error {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"javascript:", "data:", "<", ">", "@import", "expression("} {
		if strings.Contains(lower, forbidden) {
			return &MarkupError{
				Reason: fmt.Sprintf("attribute %q contains %q", name, forbidden),
				Offset: offset,
			}
		}
	}
	// A url() reference may only point inside this document.
	rest := lower
	for {
		k := strings.Index(rest, "url(")
		if k < 0 {
			break
		}
		rest = rest[k+4:]
		trimmed := strings.TrimLeft(rest, " \t'\"")
		if !strings.HasPrefix(trimmed, "#") {
			return &MarkupError{
				Reason: fmt.Sprintf("attribute %q references something outside this document", name),
				Offset: offset,
			}
		}
	}
	return nil
}
