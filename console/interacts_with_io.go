package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/arandu-io/hesape/console/view"
	"github.com/arandu-io/hesape/console/view/components"
)

// Verbosity is how much a command is allowed to say.
//
// It is an alias and not a declaration: the constants live in the components
// package, which may not import this one, and one type spelt twice is one type.
type Verbosity = components.Verbosity

// The five levels, re-exported so a command that already imports console does
// not have to import the components package to name one.
const (
	// VerbosityQuiet writes nothing at all: -q.
	VerbosityQuiet = components.VerbosityQuiet
	// VerbosityNormal is the default.
	VerbosityNormal = components.VerbosityNormal
	// VerbosityVerbose is -v.
	VerbosityVerbose = components.VerbosityVerbose
	// VerbosityVeryVerbose is -vv.
	VerbosityVeryVerbose = components.VerbosityVeryVerbose
	// VerbosityDebug is -vvv.
	VerbosityDebug = components.VerbosityDebug
)

// Write puts text out without a line ending.
//
// It is what the components render through. A verbosity above the one the
// command is running at writes nothing, which is what -q and -v decide.
func (o *IO) Write(message string, verbosity ...Verbosity) {
	if !o.shouldWrite(verbosity) {
		return
	}
	fmt.Fprint(o.out, message)
	o.newLinesWritten = trailingNewLineCount(message)
}

// Writeln puts one line out.
func (o *IO) Writeln(message string, verbosity ...Verbosity) {
	if !o.shouldWrite(verbosity) {
		return
	}
	fmt.Fprintln(o.out, message)
	o.newLinesWritten = trailingNewLineCount(message) + 1
}

// shouldWrite is the verbosity test both writers make.
func (o *IO) shouldWrite(verbosity []Verbosity) bool {
	level := VerbosityNormal
	if len(verbosity) > 0 {
		level = verbosity[0]
	}
	return o.verbosity != VerbosityQuiet && o.verbosity >= level
}

// trailingNewLineCount counts the line endings a message ends with.
//
// The Line component reads the running total to decide its top margin.
func trailingNewLineCount(message string) int {
	return len(message) - len(strings.TrimRight(message, "\n"))
}

// NewLinesWritten is how many line endings the last write left behind.
func (o *IO) NewLinesWritten() int { return o.newLinesWritten }

// NewLineWritten reports whether the last write ended a line.
//
// It is here because NewLineAware declares it.
func (o *IO) NewLineWritten() bool { return o.newLinesWritten > 0 }

// Alert writes a message in a box, in capitals.
func (o *IO) Alert(format string, a ...any) { o.OutputComponents().Alert(sprintf(format, a...)) }

// Question writes a line in the style a question is asked in.
//
// It writes, it does not ask: Ask is what waits for an answer.
func (o *IO) Question(format string, a ...any) { o.write(o.out, ansiDim, format, a...) }

// Anticipate puts a question and offers the choices as suggestions, without
// requiring one of them.
func (o *IO) Anticipate(question string, choices []string, def string) (string, error) {
	return o.AskWithCompletion(question, choices, def)
}

// AskWithCompletion puts a question and lists what the answer is likely to be.
//
// The completion is offered rather than enforced: an answer that is not in
// the list is returned as typed, which is the difference between this and
// Choice.
//
// choices is a slice rather than a callback: a caller that wants a computed
// list builds the slice before it asks.
func (o *IO) AskWithCompletion(question string, choices []string, def string) (string, error) {
	if len(choices) > 0 {
		o.Comment("one of: %s", strings.Join(choices, ", "))
	}
	return o.Ask(question, def)
}

// WithProgressBar runs the work while a bar advances over totalSteps.
//
// The bar is started before the callback and finished after it, whether or
// not the callback failed, so a bar never outlives the work it was drawn for.
//
// A Go method cannot be generic, so ranging over a collection is left to the
// caller: it advances the bar once per element by calling Advance from inside
// the callback.
func (o *IO) WithProgressBar(totalSteps int, callback func(bar *Progress) error) error {
	bar := o.Progress(totalSteps)
	defer bar.Finish()
	return callback(bar)
}

// OutputComponents is the component set the command renders with.
//
// It is built on first use, over this IO.
func (o *IO) OutputComponents() *components.Factory {
	if o.factory == nil {
		o.factory = components.NewFactory(o, o.base)
	}
	return o.factory
}

// GetOutput returns the stream the command writes to.
func (o *IO) GetOutput() io.Writer { return o.out }

// SetOutput points the command at another stream.
//
// The component factory is dropped with it, so the next OutputComponents
// call builds one over the new stream rather than going on writing to the
// old one.
func (o *IO) SetOutput(out io.Writer) {
	if out == nil {
		out = io.Discard
	}
	o.out = out
	o.factory = nil
}

// Input returns the command line bound to the signature, or nil when the
// command declared none.
func (o *IO) Input() *Input { return o.input }

// SetInput binds a parsed command line to this IO.
func (o *IO) SetInput(in *Input) { o.input = in }

// SetBase records the application root, which the components strip from a path
// before printing it.
func (o *IO) SetBase(base string) {
	o.base = base
	o.factory = nil
}

// Verbosity is the level the command is running at.
func (o *IO) Verbosity() Verbosity { return o.verbosity }

// SetVerbosity sets the level.
func (o *IO) SetVerbosity(level Verbosity) { o.verbosity = level }

// SetVerbosityNamed sets the level from one of the words: v, vv, vvv, quiet,
// normal.
//
// An unknown word leaves the level alone: a typo in a log call must not
// silence the command.
func (o *IO) SetVerbosityNamed(level string) { o.verbosity = ParseVerbosity(level, o.verbosity) }

// IsQuiet reports whether the command was told to say nothing, which is -q.
func (o *IO) IsQuiet() bool { return o.verbosity == VerbosityQuiet }

// IsVerbose reports whether the command was told to say more, which is -v.
func (o *IO) IsVerbose() bool { return o.verbosity >= VerbosityVerbose }

// IsVeryVerbose reports whether the command was told to say much more, which
// is -vv.
func (o *IO) IsVeryVerbose() bool { return o.verbosity >= VerbosityVeryVerbose }

// IsDebug reports whether the command was told to say everything, which is
// -vvv.
func (o *IO) IsDebug() bool { return o.verbosity >= VerbosityDebug }

// HasArgument reports whether the argument is declared in the signature.
func (o *IO) HasArgument(name string) bool { return o.input != nil && o.input.HasArgument(name) }

// Argument returns the value of one argument.
//
// A command with no signature has no arguments, and this returns the zero
// Value rather than failing.
func (o *IO) Argument(name string) Value {
	if o.input == nil {
		return Value{}
	}
	return o.input.Argument(name)
}

// Arguments returns every argument, keyed by name.
func (o *IO) Arguments() map[string]Value {
	if o.input == nil {
		return map[string]Value{}
	}
	return o.input.Arguments()
}

// HasOption reports whether the option is declared in the signature.
func (o *IO) HasOption(name string) bool { return o.input != nil && o.input.HasOption(name) }

// Option returns the value of one option.
func (o *IO) Option(name string) Value {
	if o.input == nil {
		return Value{}
	}
	return o.input.Option(name)
}

// Options returns every option, keyed by name.
func (o *IO) Options() map[string]Value {
	if o.input == nil {
		return map[string]Value{}
	}
	return o.input.Options()
}

// TaskResult is how a task ended, re-exported so a command that renders one does
// not have to import the view package for the constant.
type TaskResult = view.TaskResult

// The three outcomes a task line reports.
const (
	// TaskSuccess prints DONE.
	TaskSuccess = view.TaskSuccess
	// TaskFailure prints FAIL.
	TaskFailure = view.TaskFailure
	// TaskSkipped prints SKIPPED.
	TaskSkipped = view.TaskSkipped
)

// sprintf is the formatting every line method does, in one place.
func sprintf(format string, a ...any) string {
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

// AskQuestion puts a question and returns the answer, or def when the answer is
// empty.
//
// It is the one place a question is written and an answer is read: Ask,
// Anticipate and AskWithCompletion all end up here.
//
// A command that may not prompt -- --no-interaction, or a pipeline with
// nobody at the keyboard -- takes the default and does not wait. Waiting is
// how a deploy hangs at three in the morning.
func (o *IO) AskQuestion(question, def string) (string, error) {
	if !o.Interactive() {
		return def, nil
	}

	fmt.Fprint(o.out, QuestionHelper{}.WritePrompt(question, def))
	o.newLinesWritten = 0

	answer, err := o.readLine()
	if err != nil {
		return "", err
	}
	if answer == "" {
		return def, nil
	}
	return answer, nil
}
