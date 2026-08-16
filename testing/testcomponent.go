package testing

import (
	"fmt"

	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/view"
)

// TestComponent is a component that has been drawn, with the assertions worth
// making about the HTML it produced.
//
// It is [TestView]'s smaller sibling, and the difference is what each one
// holds on to. TestView keeps the view, so it can assert about the data that
// was bound; TestComponent keeps the component itself, on an exported field,
// so a test that wants something off it reads it by name.
//
// Every assertion returns the component.
//
//	rendered.AssertSee("Save").AssertDontSee("Delete")
type TestComponent struct {
	t T

	// Component is the component before it was drawn.
	Component view.Component

	// rendered is the contents the component produced, drawn once by
	// [NewTestComponent] so that every assertion reads the same output.
	rendered string
}

// NewTestComponent renders the view and wraps it, keeping the component
// alongside it.
//
// It returns an error for a nil view and for a view that would not render, so
// a template failure is reported where it happened rather than as an empty
// page three assertions later.
func NewTestComponent(t T, component view.Component, v *view.View) (*TestComponent, error) {
	if v == nil {
		return nil, fmt.Errorf("testing: NewTestComponent needs a view to render, and got nil")
	}

	rendered, err := v.Render()
	if err != nil {
		return nil, err
	}

	return &TestComponent{t: t, Component: component, rendered: rendered}, nil
}

// AssertSee asserts the text is in the rendered component. The text is
// HTML-escaped first unless escape is given as false.
func (c *TestComponent) AssertSee(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringContainsString(c.t, escapeAll([]string{value}, escape)[0], c.rendered, "")
	return c
}

// AssertSeeInOrder asserts the strings are in the rendered component, each one
// after the last.
func (c *TestComponent) AssertSeeInOrder(values []string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertThat(c.t, escapeAll(values, escape), constraints.NewSeeInOrder(c.rendered), "")
	return c
}

// AssertSeeText asserts the text is in the rendered component once the tags
// are taken off.
func (c *TestComponent) AssertSeeText(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringContainsString(c.t, escapeAll([]string{value}, escape)[0], stripTags(c.rendered), "")
	return c
}

// AssertSeeTextInOrder asserts the strings are in the rendered component once
// the tags are taken off, each one after the last.
func (c *TestComponent) AssertSeeTextInOrder(values []string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertThat(c.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(c.rendered)), "")
	return c
}

// AssertDontSee asserts the text is not in the rendered component.
func (c *TestComponent) AssertDontSee(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringNotContainsString(c.t, escapeAll([]string{value}, escape)[0], c.rendered, "")
	return c
}

// AssertDontSeeText asserts the text is not in the rendered component once the
// tags are taken off.
func (c *TestComponent) AssertDontSeeText(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringNotContainsString(c.t, escapeAll([]string{value}, escape)[0], stripTags(c.rendered), "")
	return c
}

// ToString returns the rendered component.
func (c *TestComponent) ToString() string { return c.rendered }
