package components

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/arandu-io/hesape/console/view"
)

// Component is what every console component holds: the output it renders into
// and the root it shortens paths against.
//
// The markup is written straight to the stream, with no template or output
// buffer in between.
type Component struct {
	output Output
	base   string
}

// NewComponent returns a component over an output.
//
// base is the application root, and it is what EnsureRelativePaths strips.
// Passing it in is what keeps a component testable.
func NewComponent(output Output, base string) Component {
	return Component{output: output, base: base}
}

// Output returns the stream the component renders into, so a component built
// from another one shares it.
func (c Component) Output() Output { return c.output }

// Alert renders a message in a box, in capitals.
type Alert struct{ Component }

// NewAlert returns the component.
func NewAlert(output Output, base string) Alert { return Alert{NewComponent(output, base)} }

// viewMargin is the columns on each side of a component that stay empty.
const viewMargin = 2

// Render writes the alert.
//
// The band is as wide as the terminal and three lines tall: a blank padding
// line, the message centered and inverted, and another blank padding line.
// The message is uppercased through [upperVisible] rather than
// strings.ToUpper, because the message has already been through
// EnsureDynamicContentIsHighlighted and a naive uppercase would mangle the
// escape sequences that mutator adds.
//
// There is a blank line above the band and none below: whatever renders next
// supplies its own leading margin, the way [Line.Render] does with its own
// trailing NewLine.
func (a Alert) Render(message string, verbosity ...Verbosity) {
	message = mutate(message,
		EnsureDynamicContentIsHighlighted,
		EnsurePunctuation,
		EnsureRelativePaths(a.base),
	)

	v := verbosityOf(verbosity)
	band := a.terminalWidth() - 2*viewMargin
	if band < 1 {
		band = 1
	}

	content := upperVisible(message)
	free := band - visibleWidth(content)
	if free < 0 {
		free = 0
	}
	// The odd column goes to the right.
	left, right := free/2, free-free/2

	margin := strings.Repeat(" ", viewMargin)
	padding := margin + paint(ansiOnYel, strings.Repeat(" ", band))

	// The right margin is not written. Two unpainted spaces at the end of a line
	// are invisible, and all they would add is trailing whitespace in a file the
	// output was redirected into.
	a.output.NewLine()
	a.output.Writeln(padding, v)
	a.output.Writeln(margin+paint(ansiOnYel, strings.Repeat(" ", left)+content+strings.Repeat(" ", right)), v)
	a.output.Writeln(padding, v)
}

// BulletList renders one line per element, each with a leading mark.
type BulletList struct{ Component }

// NewBulletList returns the component.
func NewBulletList(output Output, base string) BulletList {
	return BulletList{NewComponent(output, base)}
}

// Render writes the list.
//
// The dim colour covers the whole line, mark and element together, not just
// the mark.
func (b BulletList) Render(elements []string, verbosity ...Verbosity) {
	elements = mutateAll(elements,
		EnsureDynamicContentIsHighlighted,
		EnsureNoPunctuation,
		EnsureRelativePaths(b.base),
	)

	v := verbosityOf(verbosity)
	for _, element := range elements {
		b.output.Writeln("  "+paint(ansiDim, "⇂ "+element), v)
	}
}

// lineStyles is the four labels and their paint.
var lineStyles = map[string]struct {
	paint string
	title string
}{
	"info":    {ansiOnBlue, "INFO"},
	"success": {ansiOnGrn, "SUCCESS"},
	"warn":    {ansiOnYel, "WARN"},
	"error":   {ansiOnRed, "ERROR"},
}

// Line renders a labelled sentence: the chip, then the message.
//
// It is what Info, Success, Warn and Error are each one call to.
type Line struct{ Component }

// NewLineComponent returns the component.
//
// The constructor is not called NewLine because the package already has that
// name on Output, and a package with two NewLine of different shapes is a
// package where the completion list picks the wrong one.
func NewLineComponent(output Output, base string) Line { return Line{NewComponent(output, base)} }

// Render writes the labelled line.
//
// The top margin is 2 minus the line endings already written, floored at
// zero: two labelled lines in a row get one blank line between them and
// never two.
//
// An unknown style panics. The four names are a closed set the caller writes
// as a literal, so an unknown one is a mistake in the command rather than
// anything a person typed, and a panic is what Go spends on that.
func (l Line) Render(style, message string, verbosity ...Verbosity) {
	message = mutate(message,
		EnsureDynamicContentIsHighlighted,
		EnsurePunctuation,
		EnsureRelativePaths(l.base),
	)

	chosen, known := lineStyles[style]
	if !known {
		panic("console: unknown line style " + strconv.Quote(style) + ", want info, success, warn or error")
	}

	if margin := 2 - l.output.NewLinesWritten(); margin > 0 {
		l.output.NewLine(margin)
	}
	l.output.Writeln("  "+chosen.paint+" "+chosen.title+" "+ansiReset+" "+message, verbosityOf(verbosity))
	l.output.NewLine()
}

// Info renders an informational line.
type Info struct{ Component }

// NewInfo returns the component.
func NewInfo(output Output, base string) Info { return Info{NewComponent(output, base)} }

// Render writes the line.
func (i Info) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(i.output, i.base).Render("info", message, verbosity...)
}

// Success renders a line that reports something worked.
type Success struct{ Component }

// NewSuccess returns the component.
func NewSuccess(output Output, base string) Success { return Success{NewComponent(output, base)} }

// Render writes the line.
func (s Success) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(s.output, s.base).Render("success", message, verbosity...)
}

// Warn renders a line about something that is off but did not stop the command.
type Warn struct{ Component }

// NewWarn returns the component.
func NewWarn(output Output, base string) Warn { return Warn{NewComponent(output, base)} }

// Render writes the line.
func (w Warn) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(w.output, w.base).Render("warn", message, verbosity...)
}

// Error renders a line about what went wrong.
type Error struct{ Component }

// NewError returns the component.
func NewError(output Output, base string) Error { return Error{NewComponent(output, base)} }

// Render writes the line.
func (e Error) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(e.output, e.base).Render("error", message, verbosity...)
}

// detailWidth is how wide a two column line may be before the dots stop.
const detailWidth = 150

// fallbackTerminalWidth is how wide a run with no terminal is.
//
// console.IO already falls back to 80 for the same reason; this is the floor
// under an Output that does not.
const fallbackTerminalWidth = 80

// TwoColumnDetail renders a label on the left and its value on the right, joined
// by dots.
type TwoColumnDetail struct{ Component }

// NewTwoColumnDetail returns the component.
func NewTwoColumnDetail(output Output, base string) TwoColumnDetail {
	return TwoColumnDetail{NewComponent(output, base)}
}

// Render writes the line.
func (t TwoColumnDetail) Render(first, second string, verbosity ...Verbosity) {
	mutators := []Mutator{
		EnsureDynamicContentIsHighlighted,
		EnsureNoPunctuation,
		EnsureRelativePaths(t.base),
	}
	first = mutate(first, mutators...)
	second = mutate(second, mutators...)

	width := t.width()
	dots := width - visibleWidth(first) - visibleWidth(second) - detailMargins(second)
	if dots < 1 {
		dots = 1
	}

	line := "  " + first + " " + ansiDim + strings.Repeat(".", dots) + ansiReset
	if second != "" {
		line += " " + second
	}
	t.output.Writeln(line, verbosityOf(verbosity))
}

// detailMargins is the columns a two column line spends on anything that is not
// the two halves or the dots.
//
// It is five columns for a line with no value and six for a line with one:
// four fixed margin columns, plus one for the gap before the dots and,
// when there is a value, one more for the gap before it.
func detailMargins(second string) int {
	if second == "" {
		return 2*viewMargin + 1
	}
	return 2*viewMargin + 2
}

// width is the column count a detail line fills: min(terminal, 150), over the 80
// [fallbackTerminalWidth] names.
func (c Component) width() int {
	if width := c.terminalWidth(); width < detailWidth {
		return width
	}
	return detailWidth
}

// terminalWidth is the column count a full-width component fills.
//
// It is [Component.width] without the 150 cap: a band that is meant to span
// the whole terminal should not stop short of it.
func (c Component) terminalWidth() int {
	if width := c.output.GetTerminalWidth(); width > 0 {
		return width
	}
	return fallbackTerminalWidth
}

// TaskFunc is the work a Task component runs.
//
// A non-nil error makes the line read FAIL whatever the result says.
type TaskFunc func() (view.TaskResult, error)

// Task renders a description, runs the work and reports how it went, on one
// line.
type Task struct{ Component }

// NewTask returns the component.
func NewTask(output Output, base string) Task { return Task{NewComponent(output, base)} }

// Render runs task and writes the line.
//
// The line is written after task returns, whatever it returned.
func (t Task) Render(description string, task TaskFunc, verbosity ...Verbosity) error {
	description = mutate(description,
		EnsureDynamicContentIsHighlighted,
		EnsureNoPunctuation,
		EnsureRelativePaths(t.base),
	)

	v := verbosityOf(verbosity)
	t.output.Write("  "+description+" ", v)

	started := time.Now()
	result, err := view.TaskSuccess, error(nil)
	if task != nil {
		result, err = task()
		if err != nil {
			result = view.TaskFailure
		}
	}

	runTime := ""
	if task != nil {
		runTime = " " + runTimeForHumans(started)
	}

	dots := t.width() - visibleWidth(description) - visibleWidth(runTime) - 10
	if dots < 0 {
		dots = 0
	}
	t.output.Write(ansiDim+strings.Repeat(".", dots)+runTime+ansiReset, v)

	switch result {
	case view.TaskFailure:
		t.output.Writeln(" "+ansiRed+"FAIL"+ansiReset, v)
	case view.TaskSkipped:
		t.output.Writeln(" "+ansiYellow+"SKIPPED"+ansiReset, v)
	default:
		t.output.Writeln(" "+ansiGreen+"DONE"+ansiReset, v)
	}
	return err
}

// Ask puts a question and returns the answer.
type Ask struct{ Component }

// NewAsk returns the component.
func NewAsk(output Output, base string) Ask { return Ask{NewComponent(output, base)} }

// Render asks.
func (a Ask) Render(question, def string) (string, error) { return a.output.Ask(question, def) }

// AskWithCompletion is Ask with a list the terminal completes from.
type AskWithCompletion struct{ Component }

// NewAskWithCompletion returns the component.
func NewAskWithCompletion(output Output, base string) AskWithCompletion {
	return AskWithCompletion{NewComponent(output, base)}
}

// Render asks.
func (a AskWithCompletion) Render(question string, choices []string, def string) (string, error) {
	return a.output.AskWithCompletion(question, choices, def)
}

// Secret asks for a value the terminal must not show.
//
// A terminal that will not stop echoing is an error here rather than a
// choice between showing the value or not.
type Secret struct{ Component }

// NewSecret returns the component.
func NewSecret(output Output, base string) Secret { return Secret{NewComponent(output, base)} }

// Render asks.
func (s Secret) Render(question string) (string, error) { return s.output.Secret(question) }

// Confirm asks a yes or no question.
type Confirm struct{ Component }

// NewConfirm returns the component.
func NewConfirm(output Output, base string) Confirm { return Confirm{NewComponent(output, base)} }

// Render asks.
func (c Confirm) Render(question string, def bool) (bool, error) {
	return c.output.Confirm(question, def)
}

// Choice offers a numbered list and returns the option that was picked.
//
// The prompt keeps asking until it gets an answer it understands.
type Choice struct{ Component }

// NewChoice returns the component.
func NewChoice(output Output, base string) Choice { return Choice{NewComponent(output, base)} }

// Render asks.
func (c Choice) Render(question string, choices []string, def string) (string, error) {
	return c.output.Choice(question, choices, def)
}

// visibleWidth is how many columns a string takes, escape sequences excluded.
//
// Counting the bytes instead is how a coloured label ends up with a ragged
// column of dots beside it. It counts runes, not display cells, so a wide
// rune counts as one column even where a terminal would draw it as two.
func visibleWidth(s string) int {
	width, inEscape := 0, false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			width++
		}
	}
	return width
}

// paint wraps the text in an ANSI sequence and puts it back after every reset
// the text already carries.
//
// An escape sequence written by hand has one reset that switches everything
// off. Without this, the bold [migrate] that EnsureDynamicContentIsHighlighted
// adds would take the alert's yellow background down with it and the rest of
// the band would print plain.
func paint(sequence, s string) string {
	return sequence + strings.ReplaceAll(s, ansiReset, ansiReset+sequence) + ansiReset
}

// upperVisible uppercases the text and leaves the escape sequences alone.
//
// strings.ToUpper does not know the difference: Alert.Render("run [migrate]
// now") goes through EnsureDynamicContentIsHighlighted first, and ToUpper
// would turn the "m" that terminates every sequence that mutator adds into
// "M" -- "\x1b[1M" and "\x1b[0M", which no terminal understands and every
// terminal prints as text.
func upperVisible(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			out.WriteRune(r)
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			out.WriteRune(r)
			inEscape = true
		default:
			out.WriteRune(unicode.ToUpper(r))
		}
	}
	return out.String()
}

// cascadeUnits is the factor table, largest first, with the suffix each unit
// prints.
//
// A month is four weeks and a year is twelve of those, which is not a
// calendar and does not have to be -- it is the ladder a duration is broken
// up on.
var cascadeUnits = []struct {
	suffix       string
	milliseconds int64
}{
	{"y", 12 * 4 * 7 * 24 * 60 * 60 * 1000},
	{"mo", 4 * 7 * 24 * 60 * 60 * 1000},
	{"w", 7 * 24 * 60 * 60 * 1000},
	{"d", 24 * 60 * 60 * 1000},
	{"h", 60 * 60 * 1000},
	{"m", 60 * 1000},
	{"s", 1000},
	{"ms", 1},
}

// runTimeForHumans is how long the task took, in the largest unit that keeps
// it readable.
func runTimeForHumans(started time.Time) string { return formatRunTime(time.Since(started)) }

// formatRunTime is the half of runTimeForHumans that does not read a clock, so
// that a test can hand it a duration.
//
// Anything up to a second prints as milliseconds with two decimals; anything
// longer cascades into the largest units that fit, largest first.
func formatRunTime(elapsed time.Duration) string {
	milliseconds := float64(elapsed) / float64(time.Millisecond)
	if milliseconds <= 1000 {
		return numberFormat(milliseconds, 2) + "ms"
	}

	remaining := int64(math.Round(milliseconds))
	parts := make([]string, 0, len(cascadeUnits))
	for _, unit := range cascadeUnits {
		count := remaining / unit.milliseconds
		if count == 0 {
			continue
		}
		parts = append(parts, strconv.FormatInt(count, 10)+unit.suffix)
		remaining -= count * unit.milliseconds
	}
	return strings.Join(parts, " ")
}

// numberFormat renders a number with a comma every three digits and a full
// stop before the decimals.
//
// It is here because runTimeForHumans is the one caller and the separator shows:
// a task that takes exactly a second prints "1,000.00ms".
func numberFormat(value float64, decimals int) string {
	rendered := strconv.FormatFloat(value, 'f', decimals, 64)
	sign := ""
	if strings.HasPrefix(rendered, "-") {
		sign, rendered = "-", rendered[1:]
	}
	whole, fraction, hasFraction := strings.Cut(rendered, ".")

	var out strings.Builder
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteByte(whole[i])
	}
	if hasFraction {
		out.WriteByte('.')
		out.WriteString(fraction)
	}
	return sign + out.String()
}
