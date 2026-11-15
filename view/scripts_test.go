package view_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// A script has to define what it uses before it uses it.
//
// The behaviour file registers every control it drives by calling one function,
// and that function is an assignment rather than a declaration -- so it exists
// from the line it is written on and not before. The registrations sat above
// it. The first one threw, and the throw took the rest of the file with it:
// nothing registered, no delegated attribute listened, and every page kept the
// controls it draws and lost everything they do.
//
// What made it hard to see is that the page looks right. The markup is the
// server's and arrives whole; only the behaviour is gone. It was found by
// somebody pressing the one control people press first, and reading the console.
//
// This is read statically rather than run, because running it would mean a
// JavaScript runtime in the test suite of a project that has none anywhere.
// What a static read can say is the whole of what went wrong here: an
// assignment used above the line it happens on.

// assignment matches a property being given a value: arandu.ui.define = ...
var assignment = regexp.MustCompile(`(?m)^\s*([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)+)\s*=\s*function`)

// TestEveryScriptDefinesWhatItUsesBeforeUsingIt is the guard.
func TestEveryScriptDefinesWhatItUsesBeforeUsingIt(t *testing.T) {
	for _, name := range []string{"assets/ui.js", "assets/theme.js"} {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		source := string(body)

		found := assignment.FindAllStringSubmatchIndex(source, -1)
		if len(found) == 0 {
			continue
		}

		checked := 0
		for _, at := range found {
			property := source[at[2]:at[3]]

			// Where it is given a value, and where it is first called.
			assigned := at[2]
			called := strings.Index(source, property+"(")
			if called < 0 {
				continue
			}

			checked++
			if called < assigned {
				t.Errorf("%s calls %s at byte %d and gives it a value at byte %d, so the first call throws and takes the rest of the file with it",
					name, property, called, assigned)
			}
		}

		if checked == 0 {
			t.Errorf("%s: nothing was checked, so this test cannot fail for it", name)
		}
	}
}

// TestTheBehaviourFileRegistersEverythingItDraws keeps the file that broke from
// silently registering nothing.
//
// The count is not the point; a file that registered one behaviour would pass
// any check that only asks whether the call works. What this holds is that the
// registrations are still there and still above nothing that would stop them.
func TestTheBehaviourFileRegistersEverythingItDraws(t *testing.T) {
	body, err := os.ReadFile("assets/ui.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)

	registrations := strings.Count(source, "arandu.ui.define('")
	if registrations < 10 {
		t.Errorf("the behaviour file registers %d behaviours, and it drives many more than that", registrations)
	}

	// And the definition is above all of them, which is the property the test
	// above checks byte by byte and this one states in one sentence.
	defined := strings.Index(source, "arandu.ui.define = function")
	first := strings.Index(source, "arandu.ui.define('")
	if defined < 0 || first < 0 {
		t.Fatal("the behaviour file no longer has the shape this describes")
	}
	if defined > first {
		t.Error(fmt.Sprintf("the first registration is at byte %d and the function it calls is given a value at byte %d", first, defined))
	}
}
