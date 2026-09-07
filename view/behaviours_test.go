package view_test

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// Every behaviour this ecosystem emits is registered by the script it serves.
//
// A component that writes data-kyse-behavior="x" and no script that answers to
// "x" is a component with a hole in it: the markup renders, the panel never
// opens, and the console says the behaviour is missing on every page that draws
// it. The application cannot be the one to fix that -- it did not ask for the
// attribute, the component wrote it -- so the answer belongs beside the script.
//
// kyse's password box shipped that way. It emitted the name from the day it was
// written and nothing in ui.js, basecoat.bundle.js, theme.js or htmx.min.js
// contained the word; every project that drew a sign-up form implemented the
// behaviour by hand. Found by the Corujão.ai team, twice.

// emitted is a name a component writes into the markup as its behaviour. The
// constant is what the component and the registration have to agree on, so it
// is what this reads.
var emitted = regexp.MustCompile(`(?m)^const ([A-Za-z]+)Behavior = "([a-z-]+)"`)

// registered is a name ui.js answers to.
var registered = regexp.MustCompile(`arandu\.ui\.define\('([a-z-]+)'`)

func TestEveryBehaviourTheComponentsEmitIsRegistered(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile("assets/ui.js")
	if err != nil {
		t.Fatalf("reading the served script: %v", err)
	}

	answered := map[string]bool{}
	for _, match := range registered.FindAllStringSubmatch(string(script), -1) {
		answered[match[1]] = true
	}
	if len(answered) == 0 {
		t.Fatal("ui.js registers no behaviour at all, so this test proved nothing")
	}

	// The components live in kyse, which this package does not import: a view
	// runtime that depended on a component library would be the wrong way round.
	// So the names are the ones this script owns, listed here, and the test that
	// keeps the list honest is in kyse -- it reads its own constants and fails
	// if one is not in this file.
	for _, name := range []string{"password"} {
		if !answered[name] {
			names := make([]string, 0, len(answered))
			for have := range answered {
				names = append(names, have)
			}
			sort.Strings(names)
			t.Errorf("ui.js does not register %q, and a component emits it: "+
				"the markup renders, nothing mounts, and every project writes the behaviour by hand. registered = %v",
				name, names)
		}
	}
}

// TestTheServedScriptIsTheOnlyPlaceBehavioursAreRegistered keeps the answer in
// one file.
//
// A behaviour registered in two of the served scripts is two answers to one
// name, and which one wins depends on the order they load.
func TestTheServedScriptIsTheOnlyPlaceBehavioursAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"basecoat.bundle.js", "theme.js", "htmx.min.js"} {
		body, err := os.ReadFile("assets/" + name)
		if err != nil {
			continue
		}
		if found := registered.FindAllStringSubmatch(string(body), -1); len(found) > 0 {
			t.Errorf("%s registers %d behaviour(s), and ui.js is where they live", name, len(found))
		}
	}
}
