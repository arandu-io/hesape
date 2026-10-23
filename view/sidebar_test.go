package view_test

import (
	"strings"
	"testing"
)

// sidebarSection is the part of the served script, comments removed, between
// the sidebar heading and the next section.
func sidebarSection(t *testing.T) string {
	t.Helper()
	text := readUI(t)
	start := strings.Index(text, "/* ---- sidebar ---")
	if start < 0 {
		t.Fatal("ui.js has no sidebar section")
	}
	end := strings.Index(text[start+1:], "/* ---- ")
	if end < 0 {
		t.Fatal("the sidebar section has no end this test can find")
	}
	return code(text[start : start+1+end])
}

// TestTheSidebarToggleSwitchesTheModeAndRemembersIt holds the sidebar toggle
// as text; the browser half is in the e2e journey beside this file.
//
// The button names the sidebar by id, the switch happens only on a wide
// screen measured the way the vendored script measures it, the choice is
// stored under one key per sidebar, the button carries aria-expanded, and the
// overlay below the breakpoint is the vendored script's own toggle.
func TestTheSidebarToggleSwitchesTheModeAndRemembersIt(t *testing.T) {
	t.Parallel()

	body := sidebarSection(t)
	for _, want := range []string{
		"'[data-sidebar-toggle]'",
		"getAttribute('data-sidebar-toggle')",
		"'arandu.sidebar.'",
		"getAttribute('data-breakpoint')",
		"|| 768",
		"window.innerWidth >= breakpoint",
		"setAttribute('data-sidebar-mode', next)",
		"setAttribute('aria-expanded'",
		"sidebar.toggle()",
		"value === 'full' || value === 'slim'",
		"miss('sidebar'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sidebar section lost %q", want)
		}
	}
	for _, refused := range []string{"eval(", "new Function", "innerHTML", "SIDEBAR_MODES["} {
		if strings.Contains(body, refused) {
			t.Errorf("the sidebar section uses %q", refused)
		}
	}

	whole := code(readUI(t))
	if !strings.Contains(whole, "var sidebarToggle = from.closest('[data-sidebar-toggle]');\n\t\tif (sidebarToggle) toggleSidebar(sidebarToggle);") {
		t.Error("the delegated click listener does not reach the sidebar toggle")
	}
	if !strings.Contains(whole, "each(node, '[data-sidebar-mode]', restoreSidebar);") {
		t.Error("the sweep that runs on load and on every htmx:load does not restore the sidebar mode")
	}
}

// TestTheSidebarTouchesStorageOnlyInsideATry: storage throws in a private
// window and when site data is blocked, and a throw there would take the
// press, or the whole sweep, with it. Every use is the first statement of a
// try block, so the page works the same with no storage at all.
func TestTheSidebarTouchesStorageOnlyInsideATry(t *testing.T) {
	t.Parallel()

	lines := strings.Split(sidebarSection(t), "\n")
	uses := 0
	for i, ln := range lines {
		if !strings.Contains(ln, "localStorage") {
			continue
		}
		uses++
		if i == 0 || strings.TrimSpace(lines[i-1]) != "try {" {
			t.Errorf("storage is used outside a try block: %q", strings.TrimSpace(ln))
		}
	}
	if uses != 2 {
		t.Fatalf("the sidebar section uses storage %d times, want 2 (the write on a press and the read on a restore)", uses)
	}
}
