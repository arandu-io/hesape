package console

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/arandu-io/hesape/console/view/components"
)

// detailWidth is the column the right-hand side of a two column line ends at.
//
// It is a constant and not the width of the terminal: a fixed column makes the
// output of two runs comparable, including the run that was piped into a file
// and the one that was not.
const detailWidth = 72

// ANSI, used for emphasis and nothing else. There is no colour when the output
// is not a terminal, so what a pipe receives is the same bytes a test asserts.
const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiDim    = "\x1b[2m"
)

// IO is the terminal a command was started from.
//
// It carries the three streams, the arguments that followed the command name
// and the flag set built from them. Everything a command prints or asks goes
// through it, which is what makes a command testable: give it a bytes.Buffer
// and a strings.Reader and the whole conversation is a value.
//
// An IO is not safe for concurrent use. It is the handle of the one goroutine
// running the command.
type IO struct {
	out io.Writer
	err io.Writer
	in  io.Reader

	// reader is created once. Creating one per prompt would drop whatever the
	// previous read had buffered, which is the whole of a piped answer.
	reader *bufio.Reader

	name  string
	args  []string
	flags *flag.FlagSet

	// input is the command line bound to what the signature declared, and it is
	// what Argument, Option and their has* companions read.
	input *Input

	// verbosity is how much the command is allowed to say, and factory is the
	// component set it renders with. Both are built on first use.
	verbosity Verbosity
	factory   *components.Factory

	// base is the application root, which the components strip from a path.
	base string

	// signals is what Trap installed, and what Untrap takes back off.
	signals *Signals

	// newLinesWritten is how many line endings the last write left behind. It
	// starts at 1 to account for the one the shell wrote after the command was
	// typed, exactly as OutputStyle does, and it is the whole of what
	// Illuminate\Console\Contracts\NewLineAware exists for.
	newLinesWritten int

	// tty is whether out is a terminal, and colour is whether it is a terminal
	// that was not asked to stay plain. NO_COLOR is honoured because a build
	// log that fills with escape sequences is a build log nobody reads.
	tty    bool
	colour bool
}

// NewIO returns the terminal for one command.
//
// The registry builds one per run, and a test builds one directly: that is the
// supported way to run a command's Run function without a process.
//
// name is what the command is called, and it is what the flag set reports in a
// usage message. args is what followed the command name, unparsed.
func NewIO(name string, args []string, out, errOut io.Writer, in io.Reader) *IO {
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	if in == nil {
		in = strings.NewReader("")
	}

	o := &IO{
		out:             out,
		err:             errOut,
		in:              in,
		reader:          bufio.NewReader(in),
		name:            name,
		args:            args,
		verbosity:       VerbosityNormal,
		newLinesWritten: 1,
	}

	if f, ok := out.(*os.File); ok && isTerminal(f) {
		o.tty = true
		o.colour = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	return o
}

// Flags returns the flag set of this command, created on first use.
//
// The command declares its options on it and parses the arguments:
//
//	fs := o.Flags()
//	force := fs.Bool("force", false, "overwrite what is already there")
//	if err := fs.Parse(o.Args()); err != nil {
//		return err
//	}
//
// It is ContinueOnError, so a bad flag is an error the command returns rather
// than an os.Exit inside a library, and its usage goes to the error stream.
func (o *IO) Flags() *flag.FlagSet {
	if o.flags == nil {
		o.flags = flag.NewFlagSet(o.name, flag.ContinueOnError)
		o.flags.SetOutput(o.err)
	}
	return o.flags
}

// Args are the arguments that followed the command name.
//
// Before the flag set is parsed they are all of them; after, they are what is
// left, which is what a command that takes both flags and operands wants.
func (o *IO) Args() []string {
	if o.flags != nil && o.flags.Parsed() {
		return o.flags.Args()
	}
	return o.args
}

// Line writes one line to the output, verbatim.
//
// It is the default, and it is what carries the answer the command was run
// for. Info, Comment, Warn and Error differ from it in emphasis and in stream,
// never in content: when the output is not a terminal, Line, Info and Comment
// are the same bytes.
func (o *IO) Line(format string, a ...any) { o.write(o.out, "", format, a...) }

// Info writes a line that reports progress or a fact worth reading.
func (o *IO) Info(format string, a ...any) { o.write(o.out, ansiGreen, format, a...) }

// Comment writes an aside: the detail that helps but is not the answer.
func (o *IO) Comment(format string, a ...any) { o.write(o.out, ansiDim, format, a...) }

// Warn writes to the error stream, prefixed, for something that is off but did
// not stop the command.
//
// The prefix is there and not only the colour, because a warning that is
// invisible once the output is piped is a warning nobody acts on.
func (o *IO) Warn(format string, a ...any) {
	o.write(o.err, ansiYellow, "warning: "+format, a...)
}

// Error writes to the error stream, prefixed, for what went wrong.
//
// Returning the error is still what ends the command: this only says it out
// loud, for the case where the command carries on and reports at the end.
func (o *IO) Error(format string, a ...any) {
	o.write(o.err, ansiRed, "error: "+format, a...)
}

// NewLine writes count blank lines to the output, and one when count is left
// out.
//
// It answers InteractsWithIO::newLine. The mechanical difference is the
// variadic, which is how a PHP default argument is spelt in Go.
func (o *IO) NewLine(count ...int) {
	n := 1
	if len(count) > 0 {
		n = count[0]
	}
	for range n {
		fmt.Fprintln(o.out)
	}
	o.newLinesWritten += n
}

// write is the one place a line is formatted, painted and terminated.
func (o *IO) write(w io.Writer, colour, format string, a ...any) {
	if o.verbosity == VerbosityQuiet {
		return
	}
	text := format
	if len(a) > 0 {
		text = fmt.Sprintf(format, a...)
	}
	fmt.Fprintln(w, o.paint(colour, text))
	o.newLinesWritten = trailingNewLineCount(text) + 1
}

// paint wraps text in an escape sequence, or does not.
func (o *IO) paint(colour, text string) string {
	if !o.colour || colour == "" {
		return text
	}
	return colour + text + ansiReset
}

// Table writes rows under headers, in columns that line up.
//
// The columns are sized to the content, which is what makes the output usable
// in a terminal and greppable out of one. A row shorter than the headers is
// padded, and a longer one keeps its extra cells: dropping them would hide the
// data the command was run to see.
func (o *IO) Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		// The header is not painted. tabwriter measures a cell in runes and
		// counts an escape sequence as content, so a coloured header widens
		// its column by the bytes nobody can see.
		fmt.Fprintln(w, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		cells := row
		if n := len(headers) - len(cells); n > 0 {
			cells = append(append([]string{}, cells...), make([]string, n)...)
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
}

// TwoColumnDetail writes a label on the left and its value on the right, joined
// by dots.
//
// It is the shape a status listing takes -- a migration and whether it ran, a
// scheduled task and when it runs next -- and having it here is what keeps
// every command from inventing its own padding.
func (o *IO) TwoColumnDetail(left, right string) { o.detail(left, right, "") }

// detail is TwoColumnDetail with the emphasis of the right-hand side chosen.
//
// The dots are counted from the plain text, in runes: an accent is one column
// and an escape sequence is none, and measuring the painted string instead is
// how a status line ends up ragged on the terminal it was coloured for.
func (o *IO) detail(left, right, colour string) {
	dots := detailWidth - utf8.RuneCountInString(left) - utf8.RuneCountInString(right) - 2
	if dots < 1 {
		dots = 1
	}
	fmt.Fprintf(o.out, "%s %s %s\n", left, o.paint(ansiDim, strings.Repeat(".", dots)), o.paint(colour, right))
}

// Task runs fn and reports whether it worked, on one line.
//
// The line is written when fn returns, so the outcome is on the same line as
// the description. The error is returned unchanged: this reports it, it does
// not swallow it.
func (o *IO) Task(description string, fn func() error) error {
	err := fn()
	if err != nil {
		o.detail(description, "FAIL", ansiRed)
		return err
	}
	o.detail(description, "DONE", ansiGreen)
	return nil
}

// Progress starts a bar over total steps.
//
// On a terminal it redraws one line as the work advances. Everywhere else it
// writes nothing until Finish, because a progress bar written into a log file
// is a thousand lines of the same sentence.
func (o *IO) Progress(total int) *Progress {
	p := &Progress{io: o, total: total}
	p.render()
	return p
}

// Progress is one progress bar.
type Progress struct {
	io      *IO
	total   int
	current int
	done    bool
}

// Advance moves the bar on by n steps. It never passes the total.
func (p *Progress) Advance(n int) {
	if p == nil || p.done {
		return
	}
	p.current += n
	if p.total > 0 && p.current > p.total {
		p.current = p.total
	}
	p.render()
}

// Finish completes the bar and ends the line.
//
// Calling it twice does nothing the second time, so a deferred Finish beside an
// explicit one is not two bars.
func (p *Progress) Finish() {
	if p == nil || p.done {
		return
	}
	p.current = p.total
	p.done = true
	if !p.io.tty {
		fmt.Fprintf(p.io.out, "%s\n", p.bar())
		return
	}
	fmt.Fprintf(p.io.out, "\r%s\n", p.bar())
}

// render draws the bar in place, and only where in place means something.
func (p *Progress) render() {
	if !p.io.tty {
		return
	}
	fmt.Fprintf(p.io.out, "\r%s", p.bar())
}

// bar is the bar itself: filled blocks, then the count.
func (p *Progress) bar() string {
	const width = 30
	filled := width
	if p.total > 0 {
		filled = p.current * width / p.total
	}
	if filled > width {
		filled = width
	}
	return fmt.Sprintf("[%s%s] %d/%d",
		strings.Repeat("#", filled), strings.Repeat("-", width-filled), p.current, p.total)
}

// Ask puts a question and returns the answer, or def when the answer is empty.
//
// It answers InteractsWithIO::ask, which is one call to askQuestion in the PHP
// too: the prompt is written and the answer read in one place, so a command
// cannot ask in a shape the rest of the output does not use.
func (o *IO) Ask(question, def string) (string, error) { return o.AskQuestion(question, def) }

// Secret asks for a value the terminal must not show: a password, a token.
//
// When the input is a terminal the echo is turned off for the duration and put
// back afterwards, whatever happens. When it is not -- a pipe, a test, a CI job
// -- there is no echo to turn off and the value is read as it comes.
//
// A terminal that will not stop echoing is an error rather than a value typed
// in the clear, because a secret on the screen is a secret in the scrollback.
func (o *IO) Secret(question string) (string, error) {
	fmt.Fprintf(o.out, "%s ", question)

	f, ok := o.in.(*os.File)
	if !ok || !isTerminal(f) {
		return o.readLine()
	}

	restore, ok := hideEcho(f)
	if !ok {
		fmt.Fprintln(o.out)
		return "", errors.New("console: this terminal will not stop echoing, and the value would be typed in the clear; pass it in the environment instead")
	}
	defer restore()

	answer, err := o.readLine()
	// The newline the person typed was not echoed either, so the next line
	// would start beside the question.
	fmt.Fprintln(o.out)
	return answer, err
}

// Confirm asks a yes or no question, and keeps asking until it gets one.
//
// An answer that is neither is a typo, not a no: acting on it would be acting
// on something the person did not say.
func (o *IO) Confirm(question string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		fmt.Fprintf(o.out, "%s %s ", question, hint)
		answer, err := o.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		o.Warn("answer y or n")
	}
}

// Choice offers a numbered list and returns the option that was picked.
//
// It accepts the number or the option itself, because the number is what is
// quick to type and the option is what is in the person's head.
func (o *IO) Choice(question string, options []string, def string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("console: a choice needs at least one option")
	}
	for {
		if def != "" {
			fmt.Fprintf(o.out, "%s [%s]\n", question, def)
		} else {
			fmt.Fprintf(o.out, "%s\n", question)
		}
		for i, option := range options {
			fmt.Fprintf(o.out, "  %d) %s\n", i+1, option)
		}
		fmt.Fprint(o.out, "> ")

		answer, err := o.readLine()
		if err != nil {
			return "", err
		}
		if answer == "" && def != "" {
			return def, nil
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		for _, option := range options {
			if option == answer {
				return option, nil
			}
		}
		o.Warn("pick a number from 1 to %d, or type the option", len(options))
	}
}

// readLine reads one answer.
//
// Input that ends where an answer was expected is an error and not an empty
// answer: a command that took silence for consent is how a prompt in a script
// deletes something.
func (o *IO) readLine() (string, error) {
	line, err := o.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimRight(line, "\r\n"), nil
		}
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("console: the input ended while %s was waiting for an answer", o.name)
		}
		return "", fmt.Errorf("console: reading the answer: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
