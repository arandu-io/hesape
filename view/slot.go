package view

import (
	"regexp"
	"strings"
)

// ComponentSlot is Illuminate\View\ComponentSlot.
//
// It is what a component receives between its opening and closing tag: the
// content, plus the attributes written on the slot itself.
type ComponentSlot struct {
	// Attributes is the slot's own attribute bag.
	Attributes *ComponentAttributeBag

	contents string
}

// NewComponentSlot is ComponentSlot::__construct.
func NewComponentSlot(contents string, attributes map[string]any) *ComponentSlot {
	slot := &ComponentSlot{contents: contents}
	slot.WithAttributes(attributes)
	return slot
}

// WithAttributes is ComponentSlot::withAttributes.
func (s *ComponentSlot) WithAttributes(attributes map[string]any) *ComponentSlot {
	s.Attributes = NewComponentAttributeBag(attributes)
	return s
}

// ToHTML is ComponentSlot::toHtml.
func (s *ComponentSlot) ToHTML() string { return s.contents }

// IsEmpty is ComponentSlot::isEmpty.
func (s *ComponentSlot) IsEmpty() bool { return s.contents == "" }

// IsNotEmpty is ComponentSlot::isNotEmpty.
func (s *ComponentSlot) IsNotEmpty() bool { return !s.IsEmpty() }

// commentPattern matches an HTML comment, including one that spans lines.
var commentPattern = regexp.MustCompile(`(?s)<!--.*?-->`)

// HasActualContent is ComponentSlot::hasActualContent.
//
// A slot holding nothing but a comment is empty for the purpose of deciding
// whether to draw the wrapper around it. The filter is variadic where PHP takes
// a nullable callable, and the default strips comments and trims.
func (s *ComponentSlot) HasActualContent(filter ...func(string) string) bool {
	strip := func(input string) string {
		return strings.TrimSpace(commentPattern.ReplaceAllString(input, ""))
	}
	if len(filter) > 0 && filter[0] != nil {
		strip = filter[0]
	}
	return strip(s.contents) != ""
}

// String is ComponentSlot::__toString.
func (s *ComponentSlot) String() string { return s.ToHTML() }
