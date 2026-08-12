package testing

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/view"
)

// The fixtures are registered once for the whole test binary, under names
// prefixed so that they cannot collide with another file's.
const (
	testViewPageName   = "hesape_testing_testview_page"
	testViewEmptyName  = "hesape_testing_testview_empty"
	testViewBrokenName = "hesape_testing_testview_broken"
)

// testViewPageHTML is what the page fixture draws. It carries a name split by
// markup, for the text assertions; an escaped ampersand, for the escaping; and
// three items in a known order, for the ordering assertions.
const testViewPageHTML = `<h1>Hello <b>Alice</b></h1>` +
	`<p>Tom &amp; Jerry</p>` +
	`<ul><li>first</li><li>second</li><li>third</li></ul>`

func init() {
	view.Register(testViewPageName, func(w io.Writer, data any) error {
		_, err := io.WriteString(w, testViewPageHTML)
		return err
	})
	view.Register(testViewEmptyName, func(w io.Writer, data any) error {
		return nil
	})
	view.Register(testViewBrokenName, func(w io.Writer, data any) error {
		return errors.New("the fixture refuses to render")
	})
}

// testViewFakeT captures a failure instead of ending the test, so that the
// failing half of every assertion can be exercised.
type testViewFakeT struct {
	failed   bool
	messages []string
	logs     []string
}

func (f *testViewFakeT) Helper() {}

func (f *testViewFakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

func (f *testViewFakeT) Logf(format string, args ...any) {
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

func (f *testViewFakeT) assertPassed(t *testing.T) {
	t.Helper()
	if f.failed {
		t.Fatalf("expected the assertion to pass; it failed with: %s", strings.Join(f.messages, " | "))
	}
}

func (f *testViewFakeT) assertFailed(t *testing.T) {
	t.Helper()
	if !f.failed {
		t.Fatal("expected the assertion to fail; it passed")
	}
}

// testViewData is what the page fixture is bound, and what the data assertions
// read.
func testViewData() map[string]any {
	return map[string]any{
		"title": "Invoices",
		"count": 3,
		"user":  map[string]any{"name": "Alice"},
	}
}

func testViewRendered(t *testing.T, name string) (*TestView, *testViewFakeT) {
	t.Helper()

	fake := &testViewFakeT{}
	rendered, err := NewTestView(fake, view.NewFactory().Make(name, testViewData()))
	if err != nil {
		t.Fatalf("NewTestView(%s): %v", name, err)
	}
	return rendered, fake
}

func TestViewNewTestViewReportsARenderFailure(t *testing.T) {
	t.Parallel()

	if _, err := NewTestView(&testViewFakeT{}, view.NewFactory().Make(testViewBrokenName, nil)); err == nil {
		t.Fatal("expected a render error, and got none")
	}
	if _, err := NewTestView(&testViewFakeT{}, nil); err == nil {
		t.Fatal("expected an error for a nil view, and got none")
	}
}

func TestViewAssertSee(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertSee("Hello")
	fake.assertPassed(t)

	// The escaping is what makes the raw ampersand match the escaped one.
	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertSee("Tom & Jerry")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertSee("Goodbye")
	fake.assertFailed(t)
}

func TestViewAssertSeeInOrder(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertSeeInOrder([]string{"first", "second", "third"})
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertSeeInOrder([]string{"third", "second", "first"})
	fake.assertFailed(t)
}

func TestViewAssertSeeText(t *testing.T) {
	t.Parallel()

	// "Hello <b>Alice</b>" reads as "Hello Alice" only once the tags are gone.
	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertSeeText("Hello Alice")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertSeeText("Hello Bob")
	fake.assertFailed(t)
}

func TestViewAssertSeeTextInOrder(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertSeeTextInOrder([]string{"Hello Alice", "Tom & Jerry", "first"})
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertSeeTextInOrder([]string{"first", "Hello Alice"})
	fake.assertFailed(t)
}

func TestViewAssertDontSee(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertDontSee("Goodbye")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertDontSee("Hello")
	fake.assertFailed(t)
}

func TestViewAssertDontSeeText(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertDontSeeText("Hello Bob")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertDontSeeText("Hello Alice")
	fake.assertFailed(t)
}

func TestViewAssertViewHas(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("title")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("nowhere")
	fake.assertFailed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("title", "Invoices")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("title", "Receipts")
	fake.assertFailed(t)

	// A dotted path walks into the bound data, which is Arr::get in the PHP.
	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("user.name", "Alice")
	fake.assertPassed(t)
}

func TestViewAssertViewHasWithAClosure(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("count", func(bound any) bool { return bound == 3 })
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("count", func(bound any) bool { return bound == 9 })
	fake.assertFailed(t)
}

func TestViewAssertViewHasWithAnArrayDefersToAssertViewHasAll(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewHas(map[string]any{"title": "Invoices", "count": 3})
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHas(map[string]any{"title": "Receipts"})
	fake.assertFailed(t)
}

func TestViewAssertViewHasAll(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewHasAll(map[string]any{"title": "Invoices", "count": 3})
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHasAll(map[string]any{"title": "Invoices", "count": 9})
	fake.assertFailed(t)

	// The names alone are the PHP's int-keyed entries.
	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHasAll([]string{"title", "count", "user"})
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHasAll([]string{"title", "nowhere"})
	fake.assertFailed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewHasAll("not an array")
	fake.assertFailed(t)
}

func TestViewAssertViewMissing(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewMissing("nowhere")
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewMissing("title")
	fake.assertFailed(t)
}

func TestViewAssertViewEmpty(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewEmptyName)
	rendered.AssertViewEmpty()
	fake.assertPassed(t)

	rendered, fake = testViewRendered(t, testViewPageName)
	rendered.AssertViewEmpty()
	fake.assertFailed(t)
}

func TestViewToString(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	if got := rendered.ToString(); got != testViewPageHTML {
		t.Fatalf("ToString returned %q; expected %q", got, testViewPageHTML)
	}
	fake.assertPassed(t)

	empty, fake := testViewRendered(t, testViewEmptyName)
	if got := empty.ToString(); got != "" {
		t.Fatalf("ToString returned %q; expected the empty string", got)
	}
	fake.assertPassed(t)
}

func TestViewAssertionsChain(t *testing.T) {
	t.Parallel()

	rendered, fake := testViewRendered(t, testViewPageName)
	rendered.AssertViewHas("title").AssertSee("Hello").AssertDontSee("Goodbye")
	fake.assertPassed(t)
}
