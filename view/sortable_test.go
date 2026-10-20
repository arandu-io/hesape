package view_test

import (
	"os"
	"strings"
	"testing"
)

// The sortable behaviour reorders a list by drag and by keyboard, and says
// where an entry went. A list a person can reorder only with a mouse is a list
// a keyboard cannot reorder at all, so the keyboard half is held here as text:
// the buttons, the Alt+Arrow keys, the disabled ends and the live region.
func TestTheSortableListMovesByKeyboardAsWellAsByDrag(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile("assets/ui.js")
	if err != nil {
		t.Fatalf("reading the served script: %v", err)
	}
	text := string(script)
	start := strings.Index(text, "arandu.ui.define('sortable'")
	if start < 0 {
		t.Fatal("ui.js does not register the sortable behaviour")
	}
	end := strings.Index(text[start:], "\n\t});\n")
	if end < 0 {
		t.Fatal("the sortable behaviour has no end this test can find")
	}
	body := text[start : start+end]
	for _, want := range []string{
		"[data-sortable-item]",
		"[data-sortable-up]", "[data-sortable-down]",
		"event.altKey", "'ArrowUp'", "'ArrowDown'",
		".disabled = i === 0", ".disabled = i === all.length - 1",
		"[data-part=\"feedback\"]", "data-sortable-moved",
		"'dragstart'", "'dragover'", "'drop'", "'dragend'",
		"kyse:sorted",
		"removeEventListener('keydown'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sortable behaviour lost %q", want)
		}
	}
	for _, refused := range []string{"eval(", "new Function", "innerHTML"} {
		if strings.Contains(body, refused) {
			t.Errorf("the sortable behaviour uses %q, which the policy forbids or the markup must not need", refused)
		}
	}
}
