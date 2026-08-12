package testing

import (
	"fmt"
	"sort"

	"github.com/arandu-io/hesape/testing/constraints"
	"github.com/arandu-io/hesape/view"
)

// TestView answers to Illuminate\Testing\TestView: a view that has been drawn,
// with the assertions worth making about the HTML it produced and about the
// data it was handed.
//
// It keeps both halves the PHP keeps -- the view and the rendered string --
// because the two families of assertion read different things. AssertSee and
// the rest read the rendered HTML; AssertViewHas and the rest read the view's
// gathered data, which is the only place a binding the template never printed
// is still visible.
//
// Every assertion returns the view, so they chain the way the PHP's do:
//
//	rendered.AssertViewHas("user").AssertSee("Alice").AssertDontSee("Draft")
type TestView struct {
	t T

	// view answers to $view: the view before it was drawn.
	view *view.View

	// rendered answers to $rendered: the contents the view produced, drawn once
	// in the constructor the way the PHP draws them once there.
	rendered string
}

// NewTestView answers to TestView::__construct.
//
// It returns (*TestView, error) where the PHP throws: the constructor's one
// statement of substance is $view->render(), which throws on a template that
// cannot be drawn, and view.View.Render answers with an error instead.
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

// AssertViewHas answers to TestView::assertViewHas: the view was bound this
// piece of data.
//
// key stands for the PHP's `string|array $key`: a name, or the whole set of
// bindings, in which case it is AssertViewHasAll. The variadic value stands for
// `$value = null`: without one it asserts only that the name is bound.
//
// The PHP branches on what the value is, and three of its four branches are
// here. A func(any) bool is the Closure branch: the binding is handed to it and
// the assertion is what it answers. Anything else is compared with ==, which is
// assertEquals. The fourth branch -- Model and EloquentCollection compared with
// $model->is() -- has no counterpart, because this framework carries no Active
// Record to compare that way; such a binding falls to the == branch, where two
// records with the same fields are equal.
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

// AssertViewHasAll answers to TestView::assertViewHasAll: every one of these
// bindings is there.
//
// bindings stands for the PHP's `array $bindings`, whose keys the method reads
// twice over: a string key is a name with an expected value, an int key is a
// name on its own. Go has no map that holds both, so the two shapes are two
// arguments it accepts -- a map[string]any for name and value, a []string or
// []any for names alone.
//
// The names are visited in sorted order rather than in the order they were
// written, so that a run that fails names the same binding every time; Go's map
// iteration has no order to preserve.
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

// AssertViewMissing answers to TestView::assertViewMissing: the view was not
// bound this piece of data.
func (v *TestView) AssertViewMissing(key string) *TestView {
	v.t.Helper()

	data := v.view.GatherData()
	assertFalse(v.t, dataHas(data, key), fmt.Sprintf(
		"Failed asserting that the view [%s] has no data bound at [%s]; it is bound to %s.",
		v.view.GetName(), key, export(dataGet(data, key))))
	return v
}

// AssertViewEmpty answers to TestView::assertViewEmpty: the view drew nothing.
func (v *TestView) AssertViewEmpty() *TestView {
	v.t.Helper()

	assertEmpty(v.t, v.rendered, fmt.Sprintf(
		"Failed asserting that the view [%s] rendered empty; it rendered %s.",
		v.view.GetName(), export(v.rendered)))
	return v
}

// AssertSee answers to TestView::assertSee: the text is in the rendered view.
//
// escape stands for the PHP's `$escape = true`, which is what makes
// AssertSee("Tom & Jerry") match a view that drew it as "Tom &amp; Jerry".
func (v *TestView) AssertSee(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringContainsString(v.t, escapeAll([]string{value}, escape)[0], v.rendered, "")
	return v
}

// AssertSeeInOrder answers to TestView::assertSeeInOrder: the strings are in
// the rendered view, each one after the last.
func (v *TestView) AssertSeeInOrder(values []string, escape ...bool) *TestView {
	v.t.Helper()

	assertThat(v.t, escapeAll(values, escape), constraints.NewSeeInOrder(v.rendered), "")
	return v
}

// AssertSeeText answers to TestView::assertSeeText: the text is in the
// rendered view once the tags are taken off.
//
// It is the one to reach for when markup breaks the words up: "Hello
// <b>Alice</b>" contains "Hello Alice" only after the tags are gone.
func (v *TestView) AssertSeeText(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringContainsString(v.t, escapeAll([]string{value}, escape)[0], stripTags(v.rendered), "")
	return v
}

// AssertSeeTextInOrder answers to TestView::assertSeeTextInOrder.
func (v *TestView) AssertSeeTextInOrder(values []string, escape ...bool) *TestView {
	v.t.Helper()

	assertThat(v.t, escapeAll(values, escape), constraints.NewSeeInOrder(stripTags(v.rendered)), "")
	return v
}

// AssertDontSee answers to TestView::assertDontSee: the text is not in the
// rendered view.
func (v *TestView) AssertDontSee(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringNotContainsString(v.t, escapeAll([]string{value}, escape)[0], v.rendered, "")
	return v
}

// AssertDontSeeText answers to TestView::assertDontSeeText.
func (v *TestView) AssertDontSeeText(value string, escape ...bool) *TestView {
	v.t.Helper()

	assertStringNotContainsString(v.t, escapeAll([]string{value}, escape)[0], stripTags(v.rendered), "")
	return v
}

// ToString answers to TestView::__toString, which is the whole of what
// implementing Stringable buys the PHP: the rendered contents.
func (v *TestView) ToString() string { return v.rendered }

// testViewIsArray answers to `is_array($key)` over the shapes a PHP array
// arrives as here: the bindings with their values, or the names alone.
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
