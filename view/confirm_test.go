package view_test

import (
	"strings"
	"testing"
)

// A question htmx asks with hx-confirm is drawn by the script as a dialog, and
// the browser's own box is never reached. Each line is one promise a tidy-up
// could take out while every page still rendered, because the page would only
// start showing "<address> says" over a dialog nobody styled.
func TestTheConfirmationKeepsItsPromises(t *testing.T) {
	script := code(readUI(t))

	for _, want := range []struct{ code, why string }{
		{"document.addEventListener('htmx:confirm', function (event) {", "nothing takes the question before htmx asks it with window.confirm"},
		{"if (event.defaultPrevented || !detail || !detail.question", "an element with no question, or one another handler already answered, is held by a dialog"},
		{"event.preventDefault();\n\t\tvar elt = detail.elt;", "htmx still asks the browser's box after the dialog"},
		{"detail.issueRequest(true)", "the request sent after a yes asks the browser's box again"},
		{"csrfRenew().then(function () { detail.issueRequest(true); });", "a request sent after a yes skips the renewal of a lapsed token"},
		{"dialog.close('confirm')", "the button that goes through with it does not say so to the dialog"},
		{"dialog.returnValue === 'confirm'", "Escape and the way out are read as a yes"},
		{"dialog.remove();", "a dialog left on the page is swapped out from under a boosted body"},
		{"showModal(dialog);", "the question is drawn as something the page behind it can still reach"},
		{"dialog.setAttribute('role', 'alertdialog')", "a destructive question is not announced as an alert"},
		{"out.setAttribute('autofocus', '')", "the cursor of an irreversible question starts on the way through"},
		{"node.textContent = text", "the question is written as markup"},
		{"arandu.ui.confirm = confirmAsk;", "a page script has no way to ask without window.confirm"},
	} {
		if !strings.Contains(script, want.code) {
			t.Errorf("ui.js does not carry %q: %s", want.code, want.why)
		}
	}

	// The question is written with the DOM. A string of markup would put what
	// an element said into the page as HTML, and a style attribute is refused
	// by the policy the script is served under.
	for _, refused := range []string{"window.confirm(", "innerHTML", "insertAdjacentHTML", "outerHTML", ".style.cssText", "setAttribute('style'"} {
		for _, line := range strings.Split(confirmationBlock(t, script), "\n") {
			if strings.Contains(line, refused) {
				t.Errorf("the confirmation writes %q: %s", refused, strings.TrimSpace(line))
			}
		}
	}
}

// confirmationBlock is the part of the script that draws the question.
func confirmationBlock(t *testing.T, script string) string {
	t.Helper()
	start := strings.Index(script, "var CONFIRM_WORDS")
	end := strings.Index(script, "arandu.ui.confirm = confirmAsk;")
	if start < 0 || end < start {
		t.Fatal("ui.js has no confirmation block")
	}
	return script[start:end]
}
