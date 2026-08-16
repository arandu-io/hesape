// This file is in the package rather than in components_test because
// formatRunTime has no exported entry point: Task.Render is the only caller and
// it measures a real clock, which is not something a test can pin.
package components

import (
	"strings"
	"testing"
	"time"
)

// recordingOutput is the terminal a component renders into, kept in memory.
//
// It answers what console.IO gives a component, reduced to the six methods the
// rendering half calls. width is what GetTerminalWidth reports, and a zero one
// is the run with no terminal that TestWidthFallsBackToEightyWithoutATerminal
// pins.
type recordingOutput struct {
	lines    []string
	pending  string
	newLines int
	width    int
}

func (o *recordingOutput) Write(message string, _ ...Verbosity) {
	o.pending += message
	o.newLines = 0
}

func (o *recordingOutput) Writeln(message string, _ ...Verbosity) {
	o.lines = append(o.lines, o.pending+message)
	o.pending = ""
	o.newLines = 1
}

func (o *recordingOutput) NewLine(count ...int) {
	times := 1
	if len(count) > 0 {
		times = count[0]
	}
	for range times {
		o.lines = append(o.lines, o.pending)
		o.pending = ""
	}
	o.newLines += times
}

func (o *recordingOutput) NewLinesWritten() int  { return o.newLines }
func (o *recordingOutput) GetTerminalWidth() int { return o.width }

func (o *recordingOutput) Ask(string, string) (string, error) { return "", nil }
func (o *recordingOutput) AskWithCompletion(string, []string, string) (string, error) {
	return "", nil
}
func (o *recordingOutput) Secret(string) (string, error)                   { return "", nil }
func (o *recordingOutput) Confirm(string, bool) (bool, error)              { return false, nil }
func (o *recordingOutput) Choice(string, []string, string) (string, error) { return "", nil }

// TestAlertDoesNotUppercaseTheEscapeSequences is the defect a person sees first:
// strings.ToUpper over a message that has been through
// EnsureDynamicContentIsHighlighted turns "\x1b[1m" into "\x1b[1M", which no
// terminal understands, and the alert prints as line noise. "run [migrate] now"
// is the input that proved it.
func TestAlertDoesNotUppercaseTheEscapeSequences(t *testing.T) {
	out := &recordingOutput{width: 60}
	NewAlert(out, "").Render("run [migrate] now")

	rendered := strings.Join(out.lines, "\n")
	for _, broken := range []string{"\x1b[1M", "\x1b[0M", "\x1b[43;30M"} {
		if strings.Contains(rendered, broken) {
			t.Errorf("the alert carries the mangled sequence %q:\n%q", broken, rendered)
		}
	}
	if !strings.Contains(rendered, "RUN ") || !strings.Contains(rendered, "[MIGRATE]") {
		t.Errorf("the message was not uppercased: %q", rendered)
	}
}

// TestAlertPaintsEveryColumnOfEveryLine verifies the whole band is painted:
// the terminal width less the two-column margin, three lines tall, with the
// message centered in the middle one.
func TestAlertPaintsEveryColumnOfEveryLine(t *testing.T) {
	out := &recordingOutput{width: 40}
	NewAlert(out, "").Render("something happened")

	if len(out.lines) != 4 {
		t.Fatalf("wrote %d lines, want the mt-1 blank and three painted ones:\n%q", len(out.lines), out.lines)
	}
	if out.lines[0] != "" {
		t.Errorf("the first line is the mt-1 margin and must be blank: %q", out.lines[0])
	}
	for i, line := range out.lines[1:] {
		if !strings.HasPrefix(line, "  "+ansiOnYel) {
			t.Errorf("painted line %d does not start the paint after the mx-2 margin: %q", i, line)
		}
		if width := visibleWidth(line); width != 40-2 {
			t.Errorf("painted line %d is %d columns wide, want %d", i, width, 40-2)
		}
	}
	if out.lines[1] != out.lines[3] {
		t.Errorf("the py-1 padding lines differ: %q and %q", out.lines[1], out.lines[3])
	}
	if strings.TrimSpace(stripANSI(out.lines[1])) != "" {
		t.Errorf("the py-1 padding line carries text: %q", out.lines[1])
	}
	// text-center: the same number of painted spaces on each side, give or take
	// the odd column.
	middle := stripANSI(out.lines[2])[2:]
	left := len(middle) - len(strings.TrimLeft(middle, " "))
	right := len(middle) - len(strings.TrimRight(middle, " "))
	if left-right > 1 || right-left > 1 {
		t.Errorf("the message is not centered: %d columns left, %d right", left, right)
	}
}

// TestBulletListPaintsTheWholeLine verifies the dim colour covers the whole
// line, not just the leading mark.
func TestBulletListPaintsTheWholeLine(t *testing.T) {
	out := &recordingOutput{width: 80}
	NewBulletList(out, "").Render([]string{"the first", "the second"})

	if len(out.lines) != 2 {
		t.Fatalf("wrote %d lines, want one per element: %q", len(out.lines), out.lines)
	}
	for _, line := range out.lines {
		if !strings.HasPrefix(line, "  "+ansiDim+"⇂ ") {
			t.Errorf("the paint stops before the element: %q", line)
		}
		if strings.Index(line, ansiReset) != len(line)-len(ansiReset) {
			t.Errorf("the paint is reset before the end of the line: %q", line)
		}
	}
}

// TestTwoColumnDetailWithoutASecondColumn verifies a line with no value
// spends five columns on margins, not six.
func TestTwoColumnDetailWithoutASecondColumn(t *testing.T) {
	out := &recordingOutput{width: 150}
	NewTwoColumnDetail(out, "").Render("Cache", "")

	if got := strings.Count(out.lines[0], "."); got != 140 {
		t.Errorf("emitted %d dots, want 140: %q", got, out.lines[0])
	}
}

// TestTwoColumnDetailWithASecondColumn pins the other half of the same
// arithmetic, so that fixing the empty case cannot quietly move this one.
func TestTwoColumnDetailWithASecondColumn(t *testing.T) {
	out := &recordingOutput{width: 150}
	NewTwoColumnDetail(out, "").Render("Cache", "file")

	if got := strings.Count(out.lines[0], "."); got != 150-5-4-6 {
		t.Errorf("emitted %d dots, want %d: %q", got, 150-5-4-6, out.lines[0])
	}
}

// TestWidthFallsBackToEightyWithoutATerminal verifies a run with no terminal
// falls back to a width of 80.
func TestWidthFallsBackToEightyWithoutATerminal(t *testing.T) {
	out := &recordingOutput{width: 0}
	NewTwoColumnDetail(out, "").Render("Cache", "")

	if got := strings.Count(out.lines[0], "."); got != 80-5-5 {
		t.Errorf("emitted %d dots, want %d: %q", got, 80-5-5, out.lines[0])
	}
}

// TestLineRefusesAnUnknownStyle verifies an unknown style panics rather than
// rendering with a made-up title.
func TestLineRefusesAnUnknownStyle(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("an unknown line style rendered instead of aborting the command")
		}
	}()
	NewLineComponent(&recordingOutput{width: 80}, "").Render("critical", "the disk is full")
}

// TestTheFourKnownStylesStillRender guards the panic above from swallowing the
// styles that do exist.
func TestTheFourKnownStylesStillRender(t *testing.T) {
	for _, style := range []string{"info", "success", "warn", "error"} {
		out := &recordingOutput{width: 80}
		NewLineComponent(out, "").Render(style, "it happened")
		if !strings.Contains(out.lines[len(out.lines)-2], strings.ToUpper(style)) {
			t.Errorf("%s did not render its own chip: %q", style, out.lines)
		}
	}
}

// TestRunTimeForHumans verifies formatRunTime formats anything up to a
// second with two decimals and cascades everything above it into the
// largest units that fit.
func TestRunTimeForHumans(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{123456 * time.Microsecond, "123.46ms"},
		{90 * time.Second, "1m 30s"},
		{500 * time.Millisecond, "500.00ms"},
		{1000 * time.Millisecond, "1,000.00ms"},
		{1500 * time.Millisecond, "1s 500ms"},
		{2 * time.Hour, "2h"},
		{90*time.Minute + 30*time.Second, "1h 30m 30s"},
	} {
		if got := formatRunTime(tc.elapsed); got != tc.want {
			t.Errorf("formatRunTime(%s) = %q, want %q", tc.elapsed, got, tc.want)
		}
	}
}

// TestVisibleWidthOfPureEscapes verifies a string that is nothing but an
// escape sequence takes no columns.
func TestVisibleWidthOfPureEscapes(t *testing.T) {
	if got := visibleWidth(ansiReset); got != 0 {
		t.Errorf("visibleWidth(reset) = %d, want 0", got)
	}
	if got := visibleWidth(""); got != 0 {
		t.Errorf("visibleWidth(\"\") = %d, want 0", got)
	}
	if got := visibleWidth(ansiBold + "hello" + ansiReset); got != 5 {
		t.Errorf("visibleWidth = %d, want 5", got)
	}
}

// stripANSI drops the escape sequences, so an assertion can look at the columns
// a person sees.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
