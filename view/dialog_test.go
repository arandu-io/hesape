package view_test

import (
	"strings"
	"testing"
)

// The dialog half of the client script is three promises a form dialog is
// built on, and each is a line that a tidy-up could take out without any page
// failing to render.
//
// A trigger that is a link to the form's own page opens the dialog on an
// ordinary click and is left alone when the click asks for a new tab: the
// person who held a modifier asked for the page, and swallowing that click
// takes the page away from them.
//
// Closing gives focus back to what opened it, including when the opener never
// had focus -- a click focuses neither a link nor a button in Safari -- and
// when nothing clicked it open, which is a dialog reopened after its form was
// refused.
//
// A form longer than the screen opens with the cursor on a field rather than on
// its scrolling body, which is where the element puts it on its own.
//
// And the dialog reopened after a refused form is opened as a modal even when
// it was drawn open, which is the state it is drawn in so that the form is
// readable on a page this script never ran on.
func TestTheDialogKeepsItsPromisesToAFormDialog(t *testing.T) {
	script := code(readUI(t))

	for _, want := range []struct{ code, why string }{
		{"if (!opensInPlace(event, open)) return;", "a click asking for a new tab is swallowed instead of following the link"},
		{"event.metaKey || event.ctrlKey || event.shiftKey || event.altKey", "a modifier held on the link does not leave it to the browser"},
		{"openers.set(panel, open);", "the opener is not remembered, so focus is not returned to it"},
		{"document.addEventListener('close', function (event) {", "nothing listens for a dialog closing"},
		{"}, true);", "close does not bubble, and a listener outside the capture phase never hears it"},
		{"openers.get(panel) || triggerOf(panel)", "a dialog nobody clicked open returns focus nowhere"},
		{"if (!panel || panel.tagName !== 'DIALOG' || panel.open) return;", "a dialog shut only to be reopened as a modal pulls focus out of itself"},
		{"if (panel.open) panel.close();", "a dialog drawn open throws on showModal instead of becoming a modal"},
		{"landOnAControl(panel);", "a form longer than the screen opens with the cursor on its scrolling body"},
		{"arandu.ui.define('form-dialog', {", "the behaviour the form dialog writes is not registered"},
		{"ctx.props.open !== true", "a form dialog opens whether or not its form was the one refused"},
		{`panel.querySelector('[aria-invalid="true"]')`, "the reopened dialog does not put the cursor on the refused field"},
	} {
		if !strings.Contains(script, want.code) {
			t.Errorf("ui.js does not carry %q: %s", want.code, want.why)
		}
	}
}
