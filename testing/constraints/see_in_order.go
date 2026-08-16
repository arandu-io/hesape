package constraints

import (
	"fmt"
	"html"
	"strings"
)

// Constraint is a reusable matcher. It is what an assertion evaluates a value
// against, and what it reads its failure message from.
//
// Implement it to write a comparison more than one assertion can share.
type Constraint interface {
	// Matches reports whether the value satisfies the constraint.
	Matches(other any) bool

	// FailureDescription is what a failure should say about the value. It is
	// read only after Matches has answered false.
	FailureDescription(other any) string

	// String names the constraint.
	String() string
}

// SeeInOrder matches a string in which the given values appear, each one after
// the last.
//
// It is not safe for concurrent use: Matches records which value failed, so a
// SeeInOrder is built for one assertion and used by it.
type SeeInOrder struct {
	// content is the string under test.
	content string

	// failedValue is the last value that did not appear where it had to. It is
	// written by Matches and read by FailureDescription.
	failedValue string
}

// NewSeeInOrder returns a constraint over the given content.
func NewSeeInOrder(content string) *SeeInOrder {
	return &SeeInOrder{content: content}
}

// Matches reports whether other, which must be a []string, appears in the
// content in that order. Anything else does not match.
//
// Both sides are entity-decoded first, so a page that writes an apostrophe as
// &#039; still matches the apostrophe a test wrote. An empty value is skipped
// rather than matched: it is at every position, so honouring it would let a
// stray "" pass an ordering that is wrong.
func (c *SeeInOrder) Matches(other any) bool {
	values, ok := other.([]string)
	if !ok {
		return false
	}

	decodedContent := html.UnescapeString(c.content)
	position := 0

	for _, value := range values {
		if value == "" {
			continue
		}

		decodedValue := html.UnescapeString(value)

		at := strings.Index(decodedContent[position:], decodedValue)
		if at < 0 {
			c.failedValue = value
			return false
		}

		position += at + len(decodedValue)
	}

	return true
}

// FailureDescription names the content and the value that was not found where
// it had to be. It is meaningful only after [SeeInOrder.Matches] returned
// false.
func (c *SeeInOrder) FailureDescription(other any) string {
	return fmt.Sprintf("Failed asserting that '%s' contains \"%s\" in specified order.",
		c.content, c.failedValue)
}

// String names the constraint.
func (c *SeeInOrder) String() string { return "SeeInOrder" }
