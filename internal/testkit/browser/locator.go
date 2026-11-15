package browser

import (
	"encoding/json"
	"strings"
)

// Locator finds an element the way a person does: by its role and accessible
// name, by its label, or by a CSS selector that says why nothing else could.
//
// Every lookup is strict: an action needs exactly one visible element, and
// two visible matches are a failure naming both rather than a guess. Names are
// compared exactly after collapsing whitespace, and a label whose text ends in
// the asterisk of a required field matches without it.
type Locator struct {
	kind string
	a, b string
}

// Role finds an element by its ARIA role, explicit or implied by its tag, and
// its accessible name. An empty name matches any element with the role. A
// text, email, tel, url or password input is a "textbox".
func Role(role, name string) Locator { return Locator{kind: "role", a: role, b: name} }

// label finds a form control by the text of its label, its aria-labelledby or
// its aria-label.
func label(text string) Locator { return Locator{kind: "label", a: text} }

// CSS finds elements by a selector. reason is why no role or label can say the
// same; an empty reason fails the step that uses the locator, because a class
// name is styling and styling changes.
func CSS(selector, reason string) Locator {
	return Locator{kind: "css", a: selector, b: strings.TrimSpace(reason)}
}

// String describes the locator the way failures name it.
func (l Locator) String() string {
	if l.kind == "role" {
		own := "role=" + l.a
		if l.b != "" {
			own += ` name="` + l.b + `"`
		}
		return own
	}
	return l.kind + `="` + l.a + `"`
}

// problem reports what is wrong with the locator before it reaches the page.
func (l Locator) problem() string {
	switch l.kind {
	case "":
		return "the zero Locator finds nothing; build one with Role or CSS"
	case "css":
		if l.b == "" {
			return "CSS(" + l.a + `, "") has no reason; a class is styling, so say why no role or label can find this element`
		}
	}
	return ""
}

// MarshalJSON encodes the locator for the page script.
func (l Locator) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind string `json:"kind"`
		A    string `json:"a"`
		B    string `json:"b,omitempty"`
	}
	return json.Marshal(wire{Kind: l.kind, A: l.a, B: l.b})
}
