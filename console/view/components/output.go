package components

// Verbosity is how much a command is allowed to say.
//
// It is declared here, in the leaf, rather than in console: a component may not
// import the package that builds it, and one set of constants in the package
// that cannot import is better than two sets that agree by hand. console aliases
// this type, so console.Verbosity and components.Verbosity are one type and not
// two.
type Verbosity int

const (
	// VerbosityQuiet writes nothing at all: -q.
	VerbosityQuiet Verbosity = 16
	// VerbosityNormal is the default.
	VerbosityNormal Verbosity = 32
	// VerbosityVerbose is -v.
	VerbosityVerbose Verbosity = 64
	// VerbosityVeryVerbose is -vv.
	VerbosityVeryVerbose Verbosity = 128
	// VerbosityDebug is -vvv.
	VerbosityDebug Verbosity = 256
)

// Output is the terminal a component renders into.
//
// It is the minimal set of methods a component actually calls, kept as an
// interface so this package does not import console, which imports this
// one: a component is the leaf, and the IO is what satisfies it.
type Output interface {
	// Write puts text out without a line ending.
	Write(message string, verbosity ...Verbosity)

	// Writeln puts one line out.
	Writeln(message string, verbosity ...Verbosity)

	// NewLine writes count blank lines, one when count is omitted.
	NewLine(count ...int)

	// NewLinesWritten is how many line endings the last write left behind. The
	// Line component reads it to decide its top margin.
	NewLinesWritten() int

	// GetTerminalWidth is how many columns there are to fill.
	GetTerminalWidth() int

	// Ask puts a question and returns the answer, or def when it is empty.
	Ask(question, def string) (string, error)

	// AskWithCompletion is Ask with a list the terminal completes from.
	AskWithCompletion(question string, choices []string, def string) (string, error)

	// Secret asks for a value the terminal must not show.
	Secret(question string) (string, error)

	// Confirm asks a yes or no question.
	Confirm(question string, def bool) (bool, error)

	// Choice offers a numbered list and returns the option that was picked.
	Choice(question string, choices []string, def string) (string, error)
}

// ANSI, used for the label chips and the dots. There is no formatter between a
// component and the terminal, so the sequences are written here.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiOnBlue = "\x1b[44;97m"
	ansiOnGrn  = "\x1b[42;97m"
	ansiOnYel  = "\x1b[43;30m"
	ansiOnRed  = "\x1b[41;97m"
	ansiGreen  = "\x1b[32;1m"
	ansiYellow = "\x1b[33;1m"
	ansiRed    = "\x1b[31;1m"
)

// verbosityOf reads the optional trailing verbosity of a Render call.
func verbosityOf(verbosity []Verbosity) Verbosity {
	if len(verbosity) == 0 {
		return VerbosityNormal
	}
	return verbosity[0]
}
