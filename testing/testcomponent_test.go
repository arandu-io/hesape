package testing

import (
	"errors"
	"io"
	"testing"

	"github.com/arandu-io/hesape/view"
)

const (
	testComponentButtonName = "hesape_testing_testcomponent_button"
	testComponentBrokenName = "hesape_testing_testcomponent_broken"
)

// testComponentHTML is what the component fixture draws: a label split by
// markup, an escaped ampersand, and two words in a known order.
const testComponentHTML = `<button><span>Save</span> <em>&amp; close</em></button>` +
	`<small>draft</small>`

func init() {
	view.Register(testComponentButtonName, func(w io.Writer, data any) error {
		_, err := io.WriteString(w, testComponentHTML)
		return err
	})
	view.Register(testComponentBrokenName, func(w io.Writer, data any) error {
		return errors.New("the fixture refuses to render")
	})
}

// testComponentFake is the component under test. view.Component asks for one
// method, and Render answering with the name of a registered view is one of
// the shapes it accepts.
type testComponentFake struct {
	Label string
}

func (c *testComponentFake) Render() any { return testComponentButtonName }

func testComponentRendered(t *testing.T, name string) (*TestComponent, *testViewFakeT) {
	t.Helper()

	fake := &testViewFakeT{}
	component := &testComponentFake{Label: "Save"}

	rendered, err := NewTestComponent(fake, component, view.NewFactory().Make(name, nil))
	if err != nil {
		t.Fatalf("NewTestComponent(%s): %v", name, err)
	}
	return rendered, fake
}

func TestComponentNewTestComponentReportsARenderFailure(t *testing.T) {
	t.Parallel()

	component := &testComponentFake{}

	broken := view.NewFactory().Make(testComponentBrokenName, nil)
	if _, err := NewTestComponent(&testViewFakeT{}, component, broken); err == nil {
		t.Fatal("expected a render error, and got none")
	}
	if _, err := NewTestComponent(&testViewFakeT{}, component, nil); err == nil {
		t.Fatal("expected an error for a nil view, and got none")
	}
}

func TestComponentKeepsTheComponent(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)

	component, ok := rendered.Component.(*testComponentFake)
	if !ok {
		t.Fatalf("Component held %T; expected *testComponentFake", rendered.Component)
	}
	if component.Label != "Save" {
		t.Fatalf("Component.Label is %q; expected %q", component.Label, "Save")
	}
	fake.assertPassed(t)
}

func TestComponentAssertSee(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertSee("Save")
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertSee("& close")
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertSee("Delete")
	fake.assertFailed(t)
}

func TestComponentAssertSeeInOrder(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeInOrder([]string{"Save", "close", "draft"})
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeInOrder([]string{"draft", "Save"})
	fake.assertFailed(t)
}

func TestComponentAssertSeeText(t *testing.T) {
	t.Parallel()

	// "<span>Save</span> <em>&amp; close</em>" reads as "Save &amp; close"
	// only once the tags are gone.
	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeText("Save & close")
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeText("Save and close")
	fake.assertFailed(t)
}

func TestComponentAssertSeeTextInOrder(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeTextInOrder([]string{"Save & close", "draft"})
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertSeeTextInOrder([]string{"draft", "Save & close"})
	fake.assertFailed(t)
}

func TestComponentAssertDontSee(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertDontSee("Delete")
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertDontSee("Save")
	fake.assertFailed(t)
}

func TestComponentAssertDontSeeText(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertDontSeeText("Save and close")
	fake.assertPassed(t)

	rendered, fake = testComponentRendered(t, testComponentButtonName)
	rendered.AssertDontSeeText("Save & close")
	fake.assertFailed(t)
}

func TestComponentToString(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	if got := rendered.ToString(); got != testComponentHTML {
		t.Fatalf("ToString returned %q; expected %q", got, testComponentHTML)
	}
	fake.assertPassed(t)
}

func TestComponentAssertionsChain(t *testing.T) {
	t.Parallel()

	rendered, fake := testComponentRendered(t, testComponentButtonName)
	rendered.AssertSee("Save").AssertSeeText("Save & close").AssertDontSee("Delete")
	fake.assertPassed(t)
}
