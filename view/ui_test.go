package view_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// readUI is the script as it is served: the same bytes go:embed puts in the
// binary, read from the same path.
func readUI(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("assets/ui.js")
	if err != nil {
		t.Fatalf("reading the client script: %v", err)
	}
	return string(body)
}

// code is the script with its comments removed, which is what a question about
// what the script *does* has to be asked of.
//
// The file's own header names `new AsyncFunction` in prose -- it is the reason
// this file exists rather than Alpine -- and a search over the raw bytes reports
// that sentence as a call. Blanking the prose is the difference between a test
// that holds a property and one that forbids writing about it.
//
// Only block comments and whole comment lines are removed. A `//` in the middle
// of a line is left alone: telling it from a `//` inside a string needs a
// tokenizer, and a stripper that got that wrong would blank real code and pass
// by finding nothing -- which is the one way this test could fail silently.
func code(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	inBlock := false
	for _, ln := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(ln)

		if inBlock {
			if i := strings.Index(ln, "*/"); i >= 0 {
				inBlock = false
				out.WriteString(ln[i+2:])
			}
			out.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			out.WriteByte('\n')
			continue
		}
		if i := strings.Index(ln, "/*"); i >= 0 && !strings.Contains(ln[:i], `'`) && !strings.Contains(ln[:i], `"`) {
			out.WriteString(ln[:i])
			if j := strings.Index(ln[i:], "*/"); j >= 0 {
				out.WriteString(ln[i+j+2:])
			} else {
				inBlock = true
			}
			out.WriteByte('\n')
			continue
		}
		out.WriteString(ln)
		out.WriteByte('\n')
	}
	return out.String()
}

// TestTheCommentStripperKeepsTheCode proves the instrument the test below
// depends on.
//
// A stripper that blanked too much would make that test pass by finding
// nothing, which is the one way it could fail without saying so. So: prose goes,
// code stays, and the script after stripping is still most of the script.
func TestTheCommentStripperKeepsTheCode(t *testing.T) {
	got := code(`/* eval( is named here in prose. */
var a = 1;
	// and here, on a line of its own: eval(
var b = eval(c);
d(); /* trailing */ e();
`)

	if strings.Contains(got, "prose") || strings.Contains(got, "on a line of its own") {
		t.Errorf("the stripper left a comment behind:\n%s", got)
	}
	for _, want := range []string{"var a = 1;", "var b = eval(c);", "d();", "e();"} {
		if !strings.Contains(got, want) {
			t.Errorf("the stripper dropped %q:\n%s", want, got)
		}
	}

	// And on the real file: the comments are more than a third of it and less
	// than all of it. A stripper that returned the empty string would satisfy
	// every assertion in the test below.
	raw := readUI(t)
	stripped := code(raw)
	if len(stripped) < len(raw)/3 {
		t.Errorf("the stripper left %d bytes of %d, which is not a script any more", len(stripped), len(raw))
	}
	if !strings.Contains(stripped, "document.addEventListener('click'") {
		t.Error("the stripper dropped the listeners, so the test below is checking a blank")
	}
}

// TestTheClientScriptEvaluatesNothing is the property the whole file exists for,
// and the one that is easiest to lose to a convenience.
//
// The policy is script-src 'self' with no unsafe-eval. Every way of turning a
// string into code throws under it, so a call written here does not open a hole
// -- it breaks the behaviour it was written for, on a page, silently, because
// the exception is thrown where the string was going to be run and not where it
// was written. Refusing them here is what makes the failure a build failure.
//
// The registry is why this is worth pinning now rather than later: it takes a
// name from an attribute, and a name from an attribute is one small step from a
// body from an attribute.
func TestTheClientScriptEvaluatesNothing(t *testing.T) {
	script := code(readUI(t))

	for _, forbidden := range []struct {
		pattern *regexp.Regexp
		what    string
	}{
		{regexp.MustCompile(`\beval\s*\(`), "eval"},
		{regexp.MustCompile(`new\s+Function\s*\(`), "new Function"},
		{regexp.MustCompile(`new\s+AsyncFunction`), "new AsyncFunction"},
		{regexp.MustCompile(`\bsetTimeout\s*\(\s*['"]`), "setTimeout with a string"},
		{regexp.MustCompile(`\bsetInterval\s*\(\s*['"]`), "setInterval with a string"},
		{regexp.MustCompile(`\binnerHTML\s*=`), "innerHTML"},
		{regexp.MustCompile(`\bouterHTML\s*=`), "outerHTML"},
		{regexp.MustCompile(`document\.write`), "document.write"},
	} {
		if at := forbidden.pattern.FindStringIndex(script); at != nil {
			t.Errorf("the client script uses %s, and the policy forbids it:\n%s",
				forbidden.what, strings.TrimSpace(line(script, at[0])))
		}
	}
}

// TestTheRegistryIsAMapAndNotAParser holds the shape an application depends on:
// two functions to register with, and a lookup rather than anything that reads
// what the attribute says.
func TestTheRegistryIsAMapAndNotAParser(t *testing.T) {
	script := readUI(t)

	for _, want := range []string{
		"arandu.ui.action = function (name, fn)",
		"arandu.ui.define = function (name, hooks)",
		"data-kyse-behavior",
		"data-kyse-props",
		"data-kyse-on-",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the client script does not carry %q", want)
		}
	}

	// A behaviour is looked up with hasOwnProperty rather than by indexing the
	// map, because a name like "constructor" or "toString" is present on every
	// object in JavaScript and would answer with a function nobody registered.
	if !strings.Contains(script, "Object.prototype.hasOwnProperty.call(map, name)") {
		t.Error("a name is resolved without hasOwnProperty, so an inherited property answers as a registration")
	}
}

// TestEveryDelegatedEventReachesTheRegistry is the half that is easy to get
// half-right: adding an event to the documented set and forgetting to dispatch
// it leaves an attribute the server writes and nothing reads.
//
// The set is the events a person causes on a control: click, input, change,
// keydown and submit. Each has a listener on document, and each of those
// listeners calls dispatch with its own name.
func TestEveryDelegatedEventReachesTheRegistry(t *testing.T) {
	script := readUI(t)

	for _, event := range []string{"click", "input", "change", "keydown", "submit"} {
		listener := `document.addEventListener('` + event + `'`
		if !strings.Contains(script, listener) {
			t.Errorf("no listener for %q", event)
		}
		if !strings.Contains(script, `dispatch(event, '`+event+`')`) {
			t.Errorf("the %q listener does not reach the registry", event)
		}
	}
}

// TestTheLifecycleIsHookedToHtmx pins the three moments a behaviour is told
// about, and that they are htmx's own events rather than a poll.
//
// beforeCleanupElement is the one that cannot be replaced by a sweep: it is the
// last moment an element still exists, and a behaviour that took a timer or an
// observer has nowhere else to give it back.
func TestTheLifecycleIsHookedToHtmx(t *testing.T) {
	script := readUI(t)

	for hook, call := range map[string]string{
		"htmx:load":                 "mount(event.target)",
		"htmx:afterSettle":          "update(event.target)",
		"htmx:beforeCleanupElement": "destroy(event.target)",
	} {
		if !strings.Contains(script, `document.addEventListener('`+hook+`'`) {
			t.Errorf("%s is not hooked", hook)
		}
		if !strings.Contains(script, call) {
			t.Errorf("%s does not call %s", hook, call)
		}
	}

	// Mounting is idempotent by a marker on the element, because htmx:load
	// fires for the swapped element and for everything inside it, and a
	// behaviour mounted twice takes its timer twice.
	if !strings.Contains(script, `getAttribute('data-kyse-mounted') === 'true'`) {
		t.Error("mounting is not guarded, so a behaviour mounts more than once")
	}
}

// line returns the source line containing the byte at, for a message somebody
// can act on without opening the file and counting.
func line(src string, at int) string {
	start := strings.LastIndexByte(src[:at], '\n') + 1
	end := strings.IndexByte(src[at:], '\n')
	if end < 0 {
		return src[start:]
	}
	return src[start : at+end]
}
