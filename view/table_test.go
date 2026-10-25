package view_test

import (
	"os"
	"strings"
	"testing"
)

// tableSource is the part of the served script that is the data table's: the
// shared hooks and the behaviour, from the first declaration to the dialog
// section that follows.
func tableSource(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile("assets/ui.js")
	if err != nil {
		t.Fatalf("reading the served script: %v", err)
	}
	text := string(script)
	start := strings.Index(text, "var tableSelections = {};")
	end := strings.Index(text, "/* ---- dialog ---")
	if start < 0 || end < start {
		t.Fatal("ui.js has no data table section this test can find")
	}
	return text[start:end]
}

// The data table is a region the server draws, and the script does only what an
// address cannot. These guards hold the shape of that as text, because running
// the script is the job of the browser tests and these are what fails first when
// a part of it is taken out.
func TestTheDataTableBehaviourKeepsWhatAnAddressCannotDo(t *testing.T) {
	t.Parallel()
	body := tableSource(t)
	for name, want := range map[string]string{
		"it registers the behaviour":                         "arandu.ui.define('table'",
		"a menu opens from its summary":                      "details[data-menu] > summary",
		"a menu is placed beside its button":                 "function placeMenu(menu)",
		"a menu closes on Escape":                            "event.key === 'Escape'",
		"a search is one request after a pause":              "form.requestSubmit",
		"Enter cancels the pause":                            "document.addEventListener('submit'",
		"the box's own change event is not a request":        "'[data-part=\"search\"], [data-part=\"facet-search\"]'",
		"the selection outlives the region it is drawn in":   "tableSelections[root.id]",
		"the selection ends when the page leaves the list":   "!tableSwapping[root.id]",
		"the keys of other pages ride in the bulk form":      "input[data-bulk-key]",
		"an open menu is carried across the swap":            "htmx:beforeSwap",
		"and put back before the next frame":                 "htmx:afterSwap",
		"the address bar is made canonical":                  "history.replaceState(history.state, '', canonical",
		"a complete list takes the click before htmx":        "root.addEventListener('click', ctx.click, true)",
		"and keeps it from the link's own handler":           "var take = function () { event.preventDefault(); event.stopPropagation(); };",
		"the box's own change event is stopped in capture":   "event.stopPropagation();\n\t}, true);",
		"a hidden column is kept in the address of a list":   "searchParams.append(param, one)",
		"a page that scrolls closes a menu":                  "window.addEventListener('scroll'",
		"the header box is indeterminate for part of a page": "master.indeterminate = ticked > 0 && ticked < page.length",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: the data table behaviour lost %q", name, want)
		}
	}
}

// A class written by a script is a class the stylesheet was never compiled with,
// so the browser only switches what the markup already carries.
func TestTheDataTableBehaviourWritesNoClassNoMarkupAndNoStyleAttribute(t *testing.T) {
	t.Parallel()
	body := tableSource(t)
	for _, refused := range []string{
		"className", "classList", "innerHTML", "insertAdjacentHTML", "outerHTML",
		"setAttribute('style'", "cssText", "eval(", "new Function",
	} {
		if strings.Contains(body, refused) {
			t.Errorf("the data table behaviour uses %q: a script switches `hidden` and attributes, and the markup carries the classes", refused)
		}
	}
}
