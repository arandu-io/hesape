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
// It answers Symfony's OutputInterface::write, which is what the components
// render through. A verbosity above the one the command is running at writes
// nothing, which is what -q and -v decide.
func (o *IO) Write(message string, verbosity ...Verbosity) {
	if !o.shouldWrite(verbosity) {
		return
	}
	fmt.Fprint(o.out, message)
	o.newLinesWritten = trailingNewLineCount(message)
}

// Writeln puts one line out.
//
// It answers OutputInterface::writeln.
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
// It answers OutputStyle::trailingNewLineCount, and the Line component reads the
// running total to decide its top margin.
func trailingNewLineCount(message string) int {
	return len(message) - len(strings.TrimRight(message, "\n"))
}

// NewLinesWritten is how many line endings the last write left behind.
//
// It answers OutputStyle::newLinesWritten, the method
// Illuminate\Console\Contracts\NewLineAware declares.
func (o *IO) NewLinesWritten() int { return o.newLinesWritten }

// NewLineWritten reports whether the last write ended a line.
//
// It answers OutputStyle::newLineWritten, which the PHP marks deprecated in
// favour of NewLinesWritten. It is here because NewLineAware declares it.
func (o *IO) NewLineWritten() bool { return o.newLinesWritten > 0 }

// Alert writes a message in a box, in capitals.
//
// It answers InteractsWithIO::alert.
func (o *IO) Alert(format string, a ...any) { o.OutputComponents().Alert(sprintf(format, a...)) }

// Question writes a line in the style a question is asked in.
//
// It answers InteractsWithIO::question. It writes, it does not ask: Ask is what
// waits for an answer.
func (o *IO) Question(format string, a ...any) { o.write(o.out, ansiDim, format, a...) }

// Anticipate puts a question and offers the choices as suggestions, without
// requiring one of them.
//
// It answers InteractsWithIO::anticipate, which is one call to
// askWithCompletion in the PHP too.
func (o *IO) Anticipate(question string, choices []string, def string) (string, error) {
	return o.AskWithCompletion(question, choices, def)
}

// AskWithCompletion puts a question and lists what the answer is likely to be.
//
// It answers InteractsWithIO::askWithCompletion. The completion is offered
// rather than enforced: an answer that is not in the list is returned as typed,
// which is the difference between this and Choice.
//
// The mechanical difference from the PHP: the choices are a slice, where PHP
// takes an array or a callable. A callable that computes the list is the caller
// building the slice before it asks.
func (o *IO) AskWithCompletion(question string, choices []string, def string) (string, error) {
	if len(choices) > 0 {
		o.Comment("one of: %s", strings.Join(choices, ", "))
	}
	return o.Ask(question, def)
}

// WithProgressBar runs the work while a bar advances over totalSteps.
//
// It answers InteractsWithIO::withProgressBar. The bar is started before the
// callback and finished after it, whether or not the callback failed, so a bar
// never outlives the work it was drawn for.
//
// PHP takes an iterable as well as a count, and advances the bar once per
// element. A Go method cannot be generic, so the collection form is the caller
// ranging inside the callback and calling Advance -- which is what the PHP does
// on the caller's behalf.
func (o *IO) WithProgressBar(totalSteps int, callback func(bar *Progress) error) error {
	bar := o.Progress(totalSteps)
	defer bar.Finish()
	return callback(bar)
}

// OutputComponents is the component set the command renders with.
//
// It answers InteractsWithIO::outputComponents, and the $components property it
// is the getter for. It is built on first use, over this IO.
func (o *IO) OutputComponents() *components.Factory {
	if o.factory == nil {
		o.factory = components.NewFactory(o, o.base)
	}
	return o.factory
}

// GetOutput returns the stream the command writes to.
//
// It answers InteractsWithIO::getOutput.
func (o *IO) GetOutput() io.Writer { return o.out }

// SetOutput points the command at another stream.
//
// It answers InteractsWithIO::setOutput. The component factory is dropped with
// it, so the next OutputComponents call builds one over the new stream rather
// than going on writing to the old one.
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
//
// It answers InteractsWithIO::setInput.
func (o *IO) SetInput(in *Input) { o.input = in }

// SetBase records the application root, which the components strip from a path
// before printing it.
//
// It answers what Mutators\EnsureRelativePaths reads out of the container.
func (o *IO) SetBase(base string) {
	o.base = base
	o.factory = nil
}

// Verbosity is the level the command is running at.
func (o *IO) Verbosity() Verbosity { return o.verbosity }

// SetVerbosity sets the level.
//
// It answers InteractsWithIO::setVerbosity.
func (o *IO) SetVerbosity(level Verbosity) { o.verbosity = level }

// SetVerbosityNamed sets the level from one of the words a command may write
// instead of a constant: v, vv, vvv, quiet, normal.
//
// It answers the string half of setVerbosity's mixed argument, which
// parseVerbosity resolves. An unknown word leaves the level alone, exactly as
// the PHP does: a typo in a log call must not silence the command.
func (o *IO) SetVerbosityNamed(level string) { o.verbosity = ParseVerbosity(level, o.verbosity) }

// IsQuiet reports whether the command was told to say nothing: -q.
func (o *IO) IsQuiet() bool { return o.verbosity == VerbosityQuiet }

// IsVerbose reports whether the command was told to say more: -v.
func (o *IO) IsVerbose() bool { return o.verbosity >= VerbosityVerbose }

// IsVeryVerbose reports whether the command was told to say much more: -vv.
func (o *IO) IsVeryVerbose() bool { return o.verbosity >= VerbosityVeryVerbose }

// IsDebug reports whether the command was told to say everything: -vvv.
func (o *IO) IsDebug() bool { return o.verbosity >= VerbosityDebug }

// HasArgument reports whether the argument is declared in the signature.
//
// It answers InteractsWithIO::hasArgument.
func (o *IO) HasArgument(name string) bool { return o.input != nil && o.input.HasArgument(name) }

// Argument returns the value of one argument.
//
// It answers InteractsWithIO::argument. A command with no signature has no
// arguments, and this returns the zero Value rather than failing: the same
// answer the PHP gives for a key that was never declared.
func (o *IO) Argument(name string) Value {
	if o.input == nil {
		return Value{}
	}
	return o.input.Argument(name)
}

// Arguments returns every argument, keyed by name.
//
// It answers InteractsWithIO::arguments, and argument() called with no key.
func (o *IO) Arguments() map[string]Value {
	if o.input == nil {
		return map[string]Value{}
	}
	return o.input.Arguments()
}

// HasOption reports whether the option is declared in the signature.
//
// It answers InteractsWithIO::hasOption.
func (o *IO) HasOption(name string) bool { return o.input != nil && o.input.HasOption(name) }

// Option returns the value of one option.
//
// It answers InteractsWithIO::option.
func (o *IO) Option(name string) Value {
	if o.input == nil {
		return Value{}
	}
	return o.input.Option(name)
}

// Options returns every option, keyed by name.
//
// It answers InteractsWithIO::options, and option() called with no key.
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
// It answers OutputStyle::askQuestion, the one place a question is written and
// an answer is read: Ask, Anticipate and AskWithCompletion all end up here, the
// way every ask in the PHP ends up in this method.
//
// A command that may not prompt -- --no-interaction, or a pipeline with nobody
// at the keyboard -- takes the default and does not wait. Waiting is how a
// deploy hangs at three in the morning.
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
