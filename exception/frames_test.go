package exception_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
)

// TestCaptureDoesNotCallEveryFrameApplicationCode: isApp asked whether the
// function name contained "/vendor/". Every frame outside this collection
// therefore looked like application code, so the debug page expanded the
// standard library and read its source.
func TestCaptureDoesNotCallEveryFrameApplicationCode(t *testing.T) {
	frames := exception.Capture(2, "example.com/app")

	if len(frames) == 0 {
		t.Fatal("Capture returned no frames")
	}
	for _, frame := range frames {
		if frame.IsApp {
			t.Fatalf("%s was marked application code, and the application is example.com/app", frame.Func)
		}
	}
}

// TestCaptureMarksTheApplicationsOwnFrames: the module path is the only thing in
// Go that says which code the application wrote.
func TestCaptureMarksTheApplicationsOwnFrames(t *testing.T) {
	frames := exception.Capture(2, "github.com/arandu-io/hesape/exception_test")

	var mine *exception.StackFrame
	for i, frame := range frames {
		if strings.Contains(frame.Func, "TestCaptureMarksTheApplicationsOwnFrames") {
			mine = &frames[i]
		}
	}
	if mine == nil {
		t.Fatal("the frame of the test itself is not in the stack")
	}
	if !mine.IsApp {
		t.Fatal("the frame of the application's own module was not marked as its code")
	}

	// Eleven lines are taken: five before the failing one, the line, and
	// five after. This took thirteen, six each side.
	if len(mine.Snippet) != 11 {
		t.Fatalf("the snippet is %d lines, want the 11 the PHP renders", len(mine.Snippet))
	}
	if mine.SnipTop != mine.Line-5 {
		t.Fatalf("the snippet starts at line %d, want five lines above %d", mine.SnipTop, mine.Line)
	}
}
