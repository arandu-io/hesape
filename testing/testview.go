package testing

import (
	"fmt"
	"sort"

	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/view"
)

// TestView is a view that has been drawn, with the assertions worth making
// about the HTML it produced and about the data it was handed.
//
// AssertSee and the rest read the rendered HTML; AssertViewHas and the rest
// read the view's gathered data, which is the only place a binding the
// template never printed is still visible.
//
// Every assertion returns the view.
//
//	rendered.AssertViewHas("user").AssertSee("Alice").AssertDontSee("Draft")
type TestView struct {
	t T

	// view is the view before it was drawn.
	view *view.View

	// rendered is the contents the view produced, drawn once by
	// [NewTestView] so that every assertion reads the same output.
	rendered string
}

// NewTestView renders the view and wraps it for assertion.
//
// It returns an error for a nil view and for a view that would not render, so
// a template failure is reported where it happened rather than as an empty
// page three assertions later.
func NewTestView(t T, v *view.View) (*TestView, error) {
	if v == nil {
		return nil, fmt.Errorf("testing: NewTestView needs a view to render, and got nil")
	}

	rendered, err := v.Render()
	if err != nil {
		return nil, err
	}

	return &TestView{t: t, view: v, rendered: rendered}, nil
}

// AssertViewHas asserts the view was bound this piece of data, and this value
// when one is given.
//
// A value of func(any) bool is handed the binding and must report true.
// Anything else is compared loosely. A key that is really a set of bindings is
// handed to [TestView.AssertViewHasAll].
func (v *TestView) AssertViewHas(key any, value ...any) *TestView {
	v.t.Helper()

	if bindings, isArray := testViewIsArray(key); isArray {
		return v.AssertViewHasAll(bindings)
	}

	name := fmt.Sprint(key)
	data := v.view.GatherData()

	if len(value) == 0 || value[0] == nil {
		assertTrue(v.t, dataHas(data, name), fmt.Sprintf(
			"Failed asserting that the view [%s] has bound data at [%s]. Bound: %s.",
			v.view.GetName(), name, testViewKeys(data)))
		return v
	}

	actual := dataGet(data, name)

	if holds, isClosure := value[0].(func(any) bool); isClosure {
		assertTrue(v.t, holds(actual), fmt.Sprintf(
			"Failed asserting that the closure holds for the data bound at [%s], which is %s.",
			name, export(actual)))
		return v
	}

	assertEquals(v.t, value[0], actual, fmt.Sprintf(
		"Failed asserting that the data bound at [%s] matches expected %s; it is %s.",
		name, export(value[0]), export(actual)))
	return v
}

// AssertViewHasAll asserts every one of these bindings is there.
//
// The bindings are a map[string]any for name and value, or a []string or []any
// for names alone. Anything else fails the test.
//
// Map keys are visited in sorted order, so that a run that fails names the
// same binding every time.
func (v *TestView) AssertViewHasAll(bindings any) *TestView {
	v.t.Helper()

	switch b := bindings.(type) {
	case map[string]any:
		for _, key := range testViewSortedKeys(b) {
			if b[key] == nil {
				v.AssertViewHas(key)
				continue
			}
			v.AssertViewHas(key, b[key])
		}
	case []string:
		for _, key := range b {
			v.AssertViewHas(key)
		}
	case []any:
		for _, key := range b {
			v.AssertViewHas(fmt.Sprint(key))
		}
	default:
		fail(v.t, "AssertViewHasAll takes the bindings as map[string]any for name and value, "+
			"or as []string for names alone; it got %T.", bindings)
	}

	return v
}

// AssertViewMissing asserts the view was not bound this piece of data.
func (v *TestView) AssertViewMissing(key string) *TestView {
	v.t.Helper()

	data := v.view.GatherData()
	assertFalse(v.t, dataHas(data, key), fmt.Sprintf(
		"Failed asserting that the view [%s] has no data bound at [%s]; it is bound to %s.",
		v.view.GetName(), key, export(dataGet(data, key))))
	return v
}

// AssertViewEmpty asserts the view drew nothing.
func (v *TestView) AssertViewEmpty() *TestView {
	v.t.Helper()

	assertEmpty(v.t, v.rendered, fmt.Sprintf(
		"Failed asserting that the view [%s] rendered empty; it rendered %s.",
		v.view.GetName(), export(v.rendered)))
	return v
}

// AssertSee asserts the text is in the rendered view. The text is HTML-escaped
// first unless escape is given as false.
func (v *TestView) AssertSee(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringContainsString(v.t, escapeAll([]string{value}, escape)[0], v.rendered, "")
	return v
}

// AssertSeeInOrder asserts the strings are in the rendered view, each one
// after the last.
func (v *TestView) AssertSeeInOrder(values []string, escape ...bool) *TestView {
	v.t.Helper()

	assertThat(v.t, escapeAll(values, escape), constraints.NewSeeInOrder(v.rendered), "")
	return v
}

// AssertSeeText asserts the text is in the rendered view once the tags are
// taken off.
//
// It is the one to reach for when markup breaks the words up: "Hello
// <b>Alice</b>" contains "Hello Alice" only after the tags are gone.
func (v *TestView) AssertSeeText(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringContainsString(v.t, escapeAll([]string{value}, escape)[0], stripTags(v.rendered), "")
	return v
}

// AssertSeeTextInOrder asserts the strings are in the rendered view once the
// tags are taken off, each one after the last.
func (v *TestView) AssertSeeTextInOrder(values []string, escape ...bool) *TestView {
	v.t.Helper()

	assertThat(v.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(v.rendered)), "")
	return v
}

// AssertDontSee asserts the text is not in the rendered view.
func (v *TestView) AssertDontSee(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringNotContainsString(v.t, escapeAll([]string{value}, escape)[0], v.rendered, "")
	return v
}

// AssertDontSeeText asserts the text is not in the rendered view once the tags
// are taken off.
func (v *TestView) AssertDontSeeText(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringNotContainsString(v.t, escapeAll([]string{value}, escape)[0], stripTags(v.rendered), "")
	return v
}

// ToString returns the rendered view.
func (v *TestView) ToString() string { return v.rendered }

// testViewIsArray reads a key that is really a set of bindings: a map of names
// to values, or a list of names alone.
func testViewIsArray(key any) (any, bool) {
	switch k := key.(type) {
	case map[string]any:
		return k, true
	case []string:
		return k, true
	case []any:
		return k, true
	default:
		return nil, false
	}
}

// testViewSortedKeys returns a map's keys in order, so that a failing run
// reports the same binding on every run.
func testViewSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// testViewKeys names what a view was bound, for the failure message of an
// assertion that looked for something else.
func testViewKeys(data map[string]any) string {
	if len(data) == 0 {
		return "nothing"
	}
	return mustEncode(testViewSortedKeys(data))
}
