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
// It answers the abstract Illuminate\Console\View\Components\Component. The
// renderView/compile pair it declares has no equivalent and never will: those
// two exist to include a PHP file and capture its output buffer, and here the
// markup is written straight to the stream.
type Component struct {
	output Output
	base   string
}

// NewComponent is Component::__construct: it returns a component over an output.
//
// base is the application root, and it is what EnsureRelativePaths strips. PHP
// reads it from the container; passing it is what keeps a component testable.
func NewComponent(output Output, base string) Component {
	return Component{output: output, base: base}
}

// Output has no Illuminate counterpart: Component::$output is protected and PHP
// declares no accessor for it, because a PHP component reaches the stream through
// inheritance. It returns the stream the component renders into, so a component
// built from another one shares it.
func (c Component) Output() Output { return c.output }

// Alert renders a message in a box, in capitals.
//
// It answers View\Components\Alert.
type Alert struct{ Component }

// NewAlert is Component::__construct, which Alert inherits: it returns the
// component.
func NewAlert(output Output, base string) Alert { return Alert{NewComponent(output, base)} }

// viewMargin is the mx-2 that all four of the PHP views carry: the columns on
// each side of a component that stay empty.
const viewMargin = 2

// Render is Alert::render: it writes the alert.
//
// The view it answers is
// `w-full mx-2 py-1 mt-1 bg-yellow text-black text-center uppercase`, and three
// of those eight classes were missing here. Two showed on the screen: w-full
// means the band is as wide as the terminal, and py-1 means it is three lines
// tall, so an alert used to be a yellow word where the PHP prints a yellow box.
// The third was uppercase, which was applied with strings.ToUpper over a message
// that had already been through EnsureDynamicContentIsHighlighted:
// Render("run [migrate] now") uppercased the terminator of the sequences that
// mutator adds, emitted "\x1b[1M" and "\x1b[0M", and the terminal printed them
// as text. See [upperVisible].
//
// mt-1 is the blank line above and there is no mb, so nothing is written below:
// [Line.Render] spends its trailing NewLine on the mb-1 its own view carries,
// and this one has none.
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
	// text-center. The odd column goes to the right, which is where PHP's own
	// integer division puts it.
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
//
// It answers View\Components\BulletList.
type BulletList struct{ Component }

// NewBulletList is Component::__construct, which BulletList inherits: it returns
// the component.
func NewBulletList(output Output, base string) BulletList {
	return BulletList{NewComponent(output, base)}
}

// Render is BulletList::render: it writes the list.
//
// The view is `<div class="text-gray mx-2">⇂ element</div>`, and text-gray is on
// the div: it covers the element as well as the mark. The reset used to land
// straight after the mark, so every element printed in the default colour and
// the list read as a column of grey ticks beside white text.
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

// NewLineComponent is Component::__construct, which Line inherits: it returns the
// component.
//
// The constructor is not called NewLine because the package already answers to
// that verb on Output, and a package with two NewLine of different shapes is a
// package where the completion list picks the wrong one.
func NewLineComponent(output Output, base string) Line { return Line{NewComponent(output, base)} }

// Render is Line::render: it writes the labelled line.
//
// The top margin is 2 minus the line endings already written, floored at zero,
// which is exactly the arithmetic the PHP view does: two labelled lines in a row
// get one blank line between them and never two.
//
// An unknown style panics, and it used to render. PHP indexes a four-entry array
// with the name -- `array_merge(static::$styles[$style], ...)` -- so
// Render("critical", ...) is a TypeError that ends the command there. This
// painted it blue, titled it CRITICAL and carried on, which is a command
// reporting a disaster in the colour of an announcement. The four names are a
// closed set the caller writes as a literal, so an unknown one is a mistake in
// the command rather than anything a person typed, and a panic is what Go spends
// on that.
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
//
// It answers View\Components\Info.
type Info struct{ Component }

// NewInfo is Component::__construct, which Info inherits: it returns the
// component.
func NewInfo(output Output, base string) Info { return Info{NewComponent(output, base)} }

// Render is Info::render: it writes the line.
func (i Info) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(i.output, i.base).Render("info", message, verbosity...)
}

// Success renders a line that reports something worked.
//
// It answers View\Components\Success.
type Success struct{ Component }

// NewSuccess is Component::__construct, which Success inherits: it returns the
// component.
func NewSuccess(output Output, base string) Success { return Success{NewComponent(output, base)} }

// Render is Success::render: it writes the line.
func (s Success) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(s.output, s.base).Render("success", message, verbosity...)
}

// Warn renders a line about something that is off but did not stop the command.
//
// It answers View\Components\Warn.
type Warn struct{ Component }

// NewWarn is Component::__construct, which Warn inherits: it returns the
// component.
func NewWarn(output Output, base string) Warn { return Warn{NewComponent(output, base)} }

// Render is Warn::render: it writes the line.
func (w Warn) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(w.output, w.base).Render("warn", message, verbosity...)
}

// Error renders a line about what went wrong.
//
// It answers View\Components\Error.
type Error struct{ Component }

// NewError is Component::__construct, which Error inherits: it returns the
// component.
func NewError(output Output, base string) Error { return Error{NewComponent(output, base)} }

// Render is Error::render: it writes the line.
func (e Error) Render(message string, verbosity ...Verbosity) {
	NewLineComponent(e.output, e.base).Render("error", message, verbosity...)
}

// detailWidth is how wide a two column line may be before the dots stop.
//
// PHP takes min(terminal width, 150) -- Task reads it that way and the
// two-column view spells it max-w-150 -- and this takes the same.
const detailWidth = 150

// fallbackTerminalWidth is how wide a run with no terminal is.
//
// It is 80 because that is what Symfony's Terminal answers when it cannot
// measure, and Termwind asks Symfony. This used to fall back to detailWidth, so
// a run piped into a file produced a line seventy columns wider than the same
// command produces in Laravel. console.IO already answers 80 for the same
// reason; this is the floor under an Output that does not.
const fallbackTerminalWidth = 80

// TwoColumnDetail renders a label on the left and its value on the right, joined
// by dots.
//
// It answers View\Components\TwoColumnDetail.
type TwoColumnDetail struct{ Component }

// NewTwoColumnDetail is Component::__construct, which TwoColumnDetail inherits:
// it returns the component.
func NewTwoColumnDetail(output Output, base string) TwoColumnDetail {
	return TwoColumnDetail{NewComponent(output, base)}
}

// Render is TwoColumnDetail::render: it writes the line.
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
// The view is `<div class="flex mx-2 max-w-150">`, holding the first span, the
// dot span with an ml-1 on it, and -- only when the value is not empty -- the
// value span with an ml-1 of its own. So the margins are 4 for the mx-2 and 1
// for each ml-1 that is actually rendered, which is five columns for a line with
// no value and six for a line with one.
//
// This was a flat 6, and TwoColumnDetail("Cache", "") emitted 139 dots where the
// PHP emits 140: it was paying for a span the view does not render.
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
// It is [Component.width] without the 150 cap, because the alert view says
// w-full and carries no max-w.
func (c Component) terminalWidth() int {
	if width := c.output.GetTerminalWidth(); width > 0 {
		return width
	}
	return fallbackTerminalWidth
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

// NewTask is Component::__construct, which Task inherits: it returns the
// component.
func NewTask(output Output, base string) Task { return Task{NewComponent(output, base)} }

// Render is Task::render: it runs task and writes the line.
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

// NewAsk is Component::__construct, which Ask inherits: it returns the component.
func NewAsk(output Output, base string) Ask { return Ask{NewComponent(output, base)} }

// Render is Ask::render: it asks.
func (a Ask) Render(question, def string) (string, error) { return a.output.Ask(question, def) }

// AskWithCompletion is Ask with a list the terminal completes from.
//
// It answers View\Components\AskWithCompletion.
type AskWithCompletion struct{ Component }

// NewAskWithCompletion is Component::__construct, which AskWithCompletion
// inherits: it returns the component.
func NewAskWithCompletion(output Output, base string) AskWithCompletion {
	return AskWithCompletion{NewComponent(output, base)}
}

// Render is AskWithCompletion::render: it asks.
func (a AskWithCompletion) Render(question string, choices []string, def string) (string, error) {
	return a.output.AskWithCompletion(question, choices, def)
}

// Secret asks for a value the terminal must not show.
//
// It answers View\Components\Secret. The $fallback argument has no equivalent:
// it decides whether Symfony may read the value in the clear when the terminal
// will not stop echoing, and here that case is an error rather than a choice.
type Secret struct{ Component }

// NewSecret is Component::__construct, which Secret inherits: it returns the
// component.
func NewSecret(output Output, base string) Secret { return Secret{NewComponent(output, base)} }

// Render is Secret::render: it asks.
func (s Secret) Render(question string) (string, error) { return s.output.Secret(question) }

// Confirm asks a yes or no question.
//
// It answers View\Components\Confirm.
type Confirm struct{ Component }

// NewConfirm is Component::__construct, which Confirm inherits: it returns the
// component.
func NewConfirm(output Output, base string) Confirm { return Confirm{NewComponent(output, base)} }

// Render is Confirm::render: it asks.
func (c Confirm) Render(question string, def bool) (bool, error) {
	return c.output.Confirm(question, def)
}

// Choice offers a numbered list and returns the option that was picked.
//
// It answers View\Components\Choice. The $attempts and $multiple arguments have
// no equivalent: both are Symfony ChoiceQuestion settings, and the prompt here
// keeps asking until it gets an answer it understands.
type Choice struct{ Component }

// NewChoice is Component::__construct, which Choice inherits: it returns the
// component.
func NewChoice(output Output, base string) Choice { return Choice{NewComponent(output, base)} }

// Render is Choice::render: it asks.
func (c Choice) Render(question string, choices []string, def string) (string, error) {
	return c.output.Choice(question, choices, def)
}

// visibleWidth is how many columns a string takes, escape sequences excluded.
//
// Counting the bytes instead is how a coloured label ends up with a ragged
// column of dots beside it. It is PHP's
// mb_strlen(preg_replace("/\<[\w=#\/\;,:.&,%?]+\>|\\e\[\d+m/", '$1', $s)), which
// counts runes and not display cells, so a wide rune counts one on both sides.
//
// It used to fall back to the rune count when it had counted nothing, which made
// a string that is only an escape sequence four columns wide instead of none --
// four phantom columns in the dot arithmetic of a line whose value had already
// been reset.
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
// It has no Illuminate counterpart, and it is what Termwind does for free: a
// nested span there closes back into its parent's style, while an escape
// sequence written by hand has one reset that switches everything off. Without
// it, the bold [migrate] that EnsureDynamicContentIsHighlighted adds takes the
// alert's yellow background down with it and the rest of the band prints plain.
func paint(sequence, s string) string {
	return sequence + strings.ReplaceAll(s, ansiReset, ansiReset+sequence) + ansiReset
}

// upperVisible uppercases the text and leaves the escape sequences alone.
//
// It is Termwind's `uppercase`, which runs over the text of an element and never
// over the styling around it. strings.ToUpper does not know the difference:
// Alert.Render("run [migrate] now") goes through
// EnsureDynamicContentIsHighlighted first, and ToUpper turned the "m" that
// terminates every sequence that mutator adds into "M" -- "\x1b[1M" and
// "\x1b[0M", which no terminal understands and every terminal prints as text.
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

// cascadeUnits is CarbonInterval::cascade's factor table, largest first, with
// the suffixes forHumans(short: true) prints.
//
// The factors are Carbon's own: a month is four weeks and a year is twelve of
// those, which is not a calendar and does not have to be -- it is the ladder a
// duration is broken up on.
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

// runTimeForHumans is InteractsWithTime::runTimeForHumans: how long the task
// took, in the largest unit that keeps it readable.
func runTimeForHumans(started time.Time) string { return formatRunTime(time.Since(started)) }

// formatRunTime is the half of runTimeForHumans that does not read a clock, so
// that a test can hand it a duration.
//
// PHP is two lines: anything up to a second is number_format($ms, 2).'ms', and
// anything above it is CarbonInterval::milliseconds($ms)->cascade()->forHumans(short: true).
// Neither was here. 123.456ms printed as "123ms", which throws away the two
// decimals a benchmark is read for, and 90s printed as "90.00s" where the PHP
// prints "1m 30s".
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

// numberFormat is PHP's number_format with its default separators: a comma every
// three digits and a full stop before the decimals.
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
