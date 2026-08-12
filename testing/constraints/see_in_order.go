package constraints

import (
	"fmt"
	"html"
	"strings"
)

// Constraint answers to PHPUnit\Framework\Constraint\Constraint, which the
// constraints in Illuminate\Testing extend.
//
// PHPUnit is not a dependency this collection carries, so the base class it
// gives them is written here as the three methods the subclasses override.
// assertThat is what calls them.
type Constraint interface {
	// Matches answers to Constraint::matches.
	Matches(other any) bool

	// FailureDescription answers to Constraint::failureDescription.
	FailureDescription(other any) string

	// String answers to Constraint::toString.
	String() string
}

// SeeInOrder answers to Illuminate\Testing\Constraints\SeeInOrder: the values
// appear in the content, each one after the last.
//
// It is the constraint behind TestResponse::assertSeeInOrder, and the reason
// that assertion is worth having: three assertSee calls pass on a page that
// shows the three in any order, and "the total is above the line items" is an
// order.
type SeeInOrder struct {
	// content answers to $content: the string under validation.
	content string

	// failedValue answers to $failedValue: the last value that did not appear
	// where it had to. It is written by Matches and read by
	// FailureDescription, which is the order PHPUnit calls them in.
	failedValue string
}

// NewSeeInOrder answers to SeeInOrder::__construct.
func NewSeeInOrder(content string) *SeeInOrder {
	return &SeeInOrder{content: content}
}

// Matches answers to SeeInOrder::matches.
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

// FailureDescription answers to SeeInOrder::failureDescription.
func (c *SeeInOrder) FailureDescription(other any) string {
	return fmt.Sprintf("Failed asserting that '%s' contains \"%s\" in specified order.",
		c.content, c.failedValue)
}

// String answers to SeeInOrder::toString, which returns the class name.
func (c *SeeInOrder) String() string { return "SeeInOrder" }
