package testing

import (
	"fmt"

	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/view"
)

// TestComponent answers to Illuminate\Testing\TestComponent: a component that
// has been drawn, with the assertions worth making about the HTML it produced.
//
// It is TestView's smaller sibling, and the difference is what each one holds
// on to. TestView keeps the view, so it can assert about the data that was
// bound; TestComponent keeps the component, which the PHP exposes so that a
// test can reach the component's own properties and methods through __get and
// __call. Go has neither, so the component is a field and a test that wants
// something off it says so by name.
//
// Every assertion returns the component, so they chain the way the PHP's do:
//
//	rendered.AssertSee("Save").AssertDontSee("Delete")
type TestComponent struct {
	t T

	// Component answers to the public $component: the component before it was
	// drawn. It is exported because the PHP's is public, and because it is what
	// a test reaches through in place of __get.
	Component view.Component

	// rendered answers to $rendered: the contents the component produced, drawn
	// once in the constructor the way the PHP draws them once there.
	rendered string
}

// NewTestComponent answers to TestComponent::__construct.
//
// The PHP takes the component and the view it resolved to, and renders the
// second. Both are here for the same reason: the component is kept and the view
// is drawn.
//
// It returns (*TestComponent, error) where the PHP throws: $view->render()
// throws on a component that cannot be drawn, and view.View.Render answers with
// an error instead.
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

// AssertSee answers to TestComponent::assertSee: the text is in the rendered
// component.
//
// escape stands for the PHP's `$escape = true`, which is what makes
// AssertSee("Tom & Jerry") match a component that drew it as "Tom &amp; Jerry".
func (c *TestComponent) AssertSee(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringContainsString(c.t, escapeAll([]string{value}, escape)[0], c.rendered, "")
	return c
}

// AssertSeeInOrder answers to TestComponent::assertSeeInOrder: the strings are
// in the rendered component, each one after the last.
func (c *TestComponent) AssertSeeInOrder(values []string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertThat(c.t, escapeAll(values, escape), constraints.NewSeeInOrder(c.rendered), "")
	return c
}

// AssertSeeText answers to TestComponent::assertSeeText: the text is in the
// rendered component once the tags are taken off.
func (c *TestComponent) AssertSeeText(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringContainsString(c.t, escapeAll([]string{value}, escape)[0], stripTags(c.rendered), "")
	return c
}

// AssertSeeTextInOrder answers to TestComponent::assertSeeTextInOrder.
func (c *TestComponent) AssertSeeTextInOrder(values []string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertThat(c.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(c.rendered)), "")
	return c
}

// AssertDontSee answers to TestComponent::assertDontSee: the text is not in
// the rendered component.
func (c *TestComponent) AssertDontSee(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringNotContainsString(c.t, escapeAll([]string{value}, escape)[0], c.rendered, "")
	return c
}

// AssertDontSeeText answers to TestComponent::assertDontSeeText.
func (c *TestComponent) AssertDontSeeText(value string, escape ...bool) *TestComponent {
	c.t.Helper()

	assertStringNotContainsString(c.t, escapeAll([]string{value}, escape)[0], stripTags(c.rendered), "")
	return c
}

// ToString answers to TestComponent::__toString, which is the whole of what
// implementing Stringable buys the PHP: the rendered contents.
func (c *TestComponent) ToString() string { return c.rendered }
