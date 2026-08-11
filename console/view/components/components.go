package components

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/console/view"
)

// Component is what every console component holds: the output it renders into
// and the root it shortens paths against.
//
// It answers the abstract Illuminate\Console\View\Components\Component. The
// renderView/compile pair it declares has no equivalent and never will: those
// two exist to include a PHP file and capture its output buffer, and here the
// markup is written straight to the stream.
type Component struct {
	output Output
	base   string
}

// NewComponent returns a component over an output.
//
// base is the application root, and it is what EnsureRelativePaths strips. PHP
// reads it from the container; passing it is what keeps a component testable.
func NewComponent(output Output, base string) Component {
	return Component{output: output, base: base}
}

// Output returns the stream the component renders into, so a component built
// from another one shares it.
func (c Component) Output() Output { return c.output }

// Alert renders a message in a box, in capitals.
//
// It answers View\Components\Alert.
type Alert struct{ Component }

// NewAlert returns the component.
func NewAlert(output Output, base string) Alert { return Alert{NewComponent(output, base)} }

// Render writes the alert.
func (a Alert) Render(message string, verbosity ...Verbosity) {
	message = mutate(message,
		EnsureDynamicContentIsHighlighted,
		EnsurePunctuation,
		EnsureRelativePaths(a.base),
	)

	v := verbosityOf(verbosity)
	a.output.NewLine()
	a.output.Writeln("  "+ansiOnYel+"  "+strings.ToUpper(message)+"  "+ansiReset, v)
	a.output.NewLine()
}

// BulletList renders one line per element, each with a leading mark.
//
// It answers View\Components\BulletList.
type BulletList struct{ Component }

// NewBulletList returns the component.
func NewBulletList(output Output, base string) BulletList {
	return BulletList{NewComponent(output, base)}
}

// Render writes the list.
func (b BulletList) Render(elements []string, verbosity ...Verbosity) {
	elements = mutateAll(elements,
		EnsureDynamicContentIsHighlighted,
		EnsureNoPunctuation,
		EnsureRelativePaths(b.base),
	)

	v := verbosityOf(verbosity)
	for _, element := range elements {
		b.output.Writeln("  "+ansiDim+"⇂"+ansiReset+" "+element, v)
	}
}

// lineStyles is View\Components\Line::$styles: the four labels and their paint.
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
// It answers View\Components\Line, and it is what Info, Success, Warn and Error
// are each one call to.
type Line struct{ Component }

// NewLineComponent returns the component.
//
// The constructor is not called NewLine because the package already answers to
// that verb on Output, and a package with two NewLine of different shapes is a
// package where the completion list picks the wrong one.
func NewLineComponent(output Output, base string) Line { return Line{NewComponent(output, base)} }

// Render writes the labelled line.
//
// The top margin is 2 minus the line endings already written, floored at zero,
// which is exactly the arithmetic the PHP view does: two labelled lines in a row
// get one blank line between them and never two.
func (l Line) Render(style, message string, verbosity ...Verbosity) {
	message = mutate(message,
		EnsureDynamicContentIsHighlighted,
		EnsurePunctuation,
		EnsureRelativePaths(l.base),
	)

	chosen, known := lineStyles[style]
	if !known {
		chosen = lineStyles["info"]
		chosen.title = strings.ToUpper(style)
	}

	if margin := 2 - l.output.NewLinesWritten(); margin > 0 {
		l.output.NewLine(margin)
	}
	l.output.Writeln("  "+chosen.paint+" "+chosen.title+" "+ansiReset+" "+message, verbosityOf(verbosity))
	l.output.NewLine()
}

// Info renders an informational line.
//
// It answers View\Components\Info.
type Info struct{ Component }

// NewInfo returns the component.
func NewInfo(output Output, base string) Info { return Info{NewComponent(output, base)} }

// Render writes the line.
func (i Info) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(i.output, i.base).Render("info", message, verbosity...)
}

// Success renders a line that reports something worked.
//
// It answers View\Components\Success.
type Success struct{ Component }

// NewSuccess returns the component.
func NewSuccess(output Output, base string) Success { return Success{NewComponent(output, base)} }

// Render writes the line.
func (s Success) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(s.output, s.base).Render("success", message, verbosity...)
}

// Warn renders a line about something that is off but did not stop the command.
//
// It answers View\Components\Warn.
type Warn struct{ Component }

// NewWarn returns the component.
func NewWarn(output Output, base string) Warn { return Warn{NewComponent(output, base)} }

// Render writes the line.
func (w Warn) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(w.output, w.base).Render("warn", message, verbosity...)
}

// Error renders a line about what went wrong.
//
// It answers View\Components\Error.
type Error struct{ Component }

// NewError returns the component.
func NewError(output Output, base string) Error { return Error{NewComponent(output, base)} }

// Render writes the line.
func (e Error) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(e.output, e.base).Render("error", message, verbosity...)
}

// detailWidth is how wide a two column line may be before the dots stop.
//
// PHP takes min(terminal width, 150); this takes the same, and falls back to a
// fixed width when there is no terminal so that a piped run and a redirected one
// produce the same bytes.
const detailWidth = 150

// TwoColumnDetail renders a label on the left and its value on the right, joined
// by dots.
//
// It answers View\Components\TwoColumnDetail.
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
	dots := width - visibleWidth(first) - visibleWidth(second) - 6
	if dots < 1 {
		dots = 1
	}

	line := "  " + first + " " + ansiDim + strings.Repeat(".", dots) + ansiReset
	if second != "" {
		line += " " + second
	}
	t.output.Writeln(line, verbosityOf(verbosity))
}

// width is the column count a detail line fills.
func (c Component) width() int {
	width := c.output.GetTerminalWidth()
	if width <= 0 || width > detailWidth {
		return detailWidth
	}
	return width
}

// TaskFunc is the work a Task component runs.
//
// PHP passes a callable that returns a TaskResult and throws on failure; the
// error is the throw, and a non-nil one makes the line read FAIL whatever the
// result says.
type TaskFunc func() (view.TaskResult, error)

// Task renders a description, runs the work and reports how it went, on one
// line.
//
// It answers View\Components\Task.
type Task struct{ Component }

// NewTask returns the component.
func NewTask(output Output, base string) Task { return Task{NewComponent(output, base)} }

// Render runs task and writes the line.
//
// The mechanical difference from the PHP: the callback returns (TaskResult,
// error) where PHP returns a result and throws, and the error is returned rather
// than rethrown -- but the line is written either way, which is the behaviour
// the finally block is there for.
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
//
// It answers View\Components\Ask. The $multiline argument has no equivalent: it
// sets a Symfony Question flag that only the Symfony question helper reads.
type Ask struct{ Component }

// NewAsk returns the component.
func NewAsk(output Output, base string) Ask { return Ask{NewComponent(output, base)} }

// Render asks.
func (a Ask) Render(question, def string) (string, error) { return a.output.Ask(question, def) }

// AskWithCompletion is Ask with a list the terminal completes from.
//
// It answers View\Components\AskWithCompletion.
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
// It answers View\Components\Secret. The $fallback argument has no equivalent:
// it decides whether Symfony may read the value in the clear when the terminal
// will not stop echoing, and here that case is an error rather than a choice.
type Secret struct{ Component }

// NewSecret returns the component.
func NewSecret(output Output, base string) Secret { return Secret{NewComponent(output, base)} }

// Render asks.
func (s Secret) Render(question string) (string, error) { return s.output.Secret(question) }

// Confirm asks a yes or no question.
//
// It answers View\Components\Confirm.
type Confirm struct{ Component }

// NewConfirm returns the component.
func NewConfirm(output Output, base string) Confirm { return Confirm{NewComponent(output, base)} }

// Render asks.
func (c Confirm) Render(question string, def bool) (bool, error) {
	return c.output.Confirm(question, def)
}

// Choice offers a numbered list and returns the option that was picked.
//
// It answers View\Components\Choice. The $attempts and $multiple arguments have
// no equivalent: both are Symfony ChoiceQuestion settings, and the prompt here
// keeps asking until it gets an answer it understands.
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
// column of dots beside it.
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
	if width == 0 {
		return utf8.RuneCountInString(s)
	}
	return width
}

// runTimeForHumans is InteractsWithTime::runTimeForHumans: how long the task
// took, in the largest unit that keeps it readable.
func runTimeForHumans(started time.Time) string {
	elapsed := time.Since(started)
	if elapsed < time.Second {
		return fmt.Sprintf("%dms", elapsed.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", elapsed.Seconds())
}
