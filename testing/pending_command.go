package testing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/console"
)

// PendingCommandKernel runs one console command for a [PendingCommand].
//
// It takes a context, because running a command is I/O, and it takes the input
// as well as the output: a console.IO reads its answers from a reader and
// writes its prompts to a writer, so the seam is the two streams.
type PendingCommandKernel interface {
	Call(ctx context.Context, command string, parameters []string, in io.Reader, out io.Writer) error
}

// PendingCommand is a console command that has been described and not yet run.
//
// The kernel it runs on is an explicit field, so what the command runs on is
// visible at the call site.
//
// The expectations live here rather than on the test. Nothing else reads them,
// and [PendingCommand.Run] clears them once it has checked them.
type PendingCommand struct {
	// Test is the test being run.
	Test T

	// kernel is what the command runs on.
	kernel PendingCommandKernel

	// command is the name to run.
	command string

	// parameters are the arguments it runs with.
	parameters []string

	// expectedExitCode and unexpectedExitCode are the status assertions, made
	// when the command runs. At most one of them is set.
	expectedExitCode   *int
	unexpectedExitCode *int

	// hasExecuted records that the command has been run.
	hasExecuted bool

	expectedQuestions          []pendingCommandQuestion
	expectedChoices            []*pendingCommandChoice
	expectsOutput              *bool
	expectedOutput             []string
	expectedOutputSubstrings   []string
	unexpectedOutput           []string
	unexpectedOutputSubstrings []string
}

// pendingCommandQuestion is one expected question: the question that must be
// asked and the answer typed at it.
type pendingCommandQuestion struct {
	question string
	answer   string
}

// pendingCommandChoice is one expected choice: the options a question must
// offer, and the ones it did offer.
type pendingCommandChoice struct {
	question string
	expected []string
	strict   bool
	actual   []string
}

// NewPendingCommand describes a command to run on the given kernel. Nothing
// runs until [PendingCommand.Run].
func NewPendingCommand(t T, kernel PendingCommandKernel, command string, parameters []string) *PendingCommand {
	return &PendingCommand{
		Test:       t,
		kernel:     kernel,
		command:    command,
		parameters: parameters,
	}
}

// ExpectsQuestion records that the question must be asked when the command
// runs, and that this is the answer it gets.
//
// A bool is typed as yes or no, an int as itself, a slice as a comma separated
// list -- the three forms console.IO.Confirm and console.IO.Choice read.
func (c *PendingCommand) ExpectsQuestion(question string, answer any) *PendingCommand {
	c.Test.Helper()
	c.expectedQuestions = append(c.expectedQuestions, pendingCommandQuestion{
		question: question,
		answer:   pendingCommandAnswer(answer),
	})
	return c
}

// ExpectsConfirmation records a yes/no question and the answer to it. Any
// answer other than "yes", in any case, is taken as no.
func (c *PendingCommand) ExpectsConfirmation(question, answer string) *PendingCommand {
	c.Test.Helper()
	return c.ExpectsQuestion(question, strings.ToLower(answer) == "yes")
}

// ExpectsChoice records that the question must be asked, that it must offer
// these options, and that this is the one picked.
//
// strict asks for the options in this exact order; otherwise any order will
// do. Recording the same question twice replaces its options.
func (c *PendingCommand) ExpectsChoice(question string, answer any, answers []string, strict bool) *PendingCommand {
	c.Test.Helper()

	if choice := c.pendingCommandChoiceFor(question); choice != nil {
		choice.expected = answers
		choice.strict = strict
	} else {
		c.expectedChoices = append(c.expectedChoices, &pendingCommandChoice{
			question: question,
			expected: answers,
			strict:   strict,
		})
	}

	return c.ExpectsQuestion(question, answer)
}

// ExpectsSearch records a search: the question is asked twice, once for the
// search string and once for the choice it narrowed down to.
func (c *PendingCommand) ExpectsSearch(question string, answer any, search string, answers []string) *PendingCommand {
	c.Test.Helper()
	return c.
		ExpectsQuestion(question, search).
		ExpectsChoice(question, answer, answers, false)
}

// ExpectsOutput records that each line given must be printed, in this order.
//
// Called with no argument it records only that the command must print
// something. The variadic is what tells the omitted argument from the empty
// line.
func (c *PendingCommand) ExpectsOutput(output ...string) *PendingCommand {
	c.Test.Helper()

	if len(output) == 0 {
		expects := true
		c.expectsOutput = &expects
		return c
	}

	c.expectedOutput = append(c.expectedOutput, output...)
	return c
}

// DoesntExpectOutput records that none of the lines given may be printed.
//
// Called with no argument it records that the command must print nothing at
// all.
func (c *PendingCommand) DoesntExpectOutput(output ...string) *PendingCommand {
	c.Test.Helper()

	if len(output) == 0 {
		expects := false
		c.expectsOutput = &expects
		return c
	}

	c.unexpectedOutput = append(c.unexpectedOutput, output...)
	return c
}

// ExpectsOutputToContain records that the string must be somewhere in what the
// command printed, on any line.
func (c *PendingCommand) ExpectsOutputToContain(s string) *PendingCommand {
	c.Test.Helper()
	c.expectedOutputSubstrings = append(c.expectedOutputSubstrings, s)
	return c
}

// DoesntExpectOutputToContain records that the string must appear nowhere in
// what the command printed.
func (c *PendingCommand) DoesntExpectOutputToContain(s string) *PendingCommand {
	c.Test.Helper()
	c.unexpectedOutputSubstrings = append(c.unexpectedOutputSubstrings, s)
	return c
}

// ExpectsTable records that the command must print this table.
//
// The table is rendered here by the same renderer the command uses, and every
// line of it becomes an expected output line. A second renderer written for
// tests would be a second way to draw a table.
func (c *PendingCommand) ExpectsTable(headers []string, rows [][]string) *PendingCommand {
	c.Test.Helper()

	var rendered bytes.Buffer
	console.NewIO("", nil, &rendered, nil, nil).Table(headers, rows)

	for _, line := range pendingCommandLines(rendered.String()) {
		// Drop the empty line the render ends on.
		if line == "" {
			continue
		}
		c.ExpectsOutput(line)
	}
	return c
}

// AssertExitCode records that the command must exit with this status. The
// assertion is made when the command runs, which is where the status exists.
func (c *PendingCommand) AssertExitCode(exitCode int) *PendingCommand {
	c.Test.Helper()
	c.expectedExitCode = &exitCode
	return c
}

// AssertNotExitCode records that the command must exit with any status other
// than this one.
func (c *PendingCommand) AssertNotExitCode(exitCode int) *PendingCommand {
	c.Test.Helper()
	c.unexpectedExitCode = &exitCode
	return c
}

// AssertSuccessful records that the command must exit zero.
func (c *PendingCommand) AssertSuccessful() *PendingCommand {
	c.Test.Helper()
	return c.AssertExitCode(pendingCommandSuccess)
}

// AssertOk is [PendingCommand.AssertSuccessful] under a shorter name.
func (c *PendingCommand) AssertOk() *PendingCommand {
	c.Test.Helper()
	return c.AssertSuccessful()
}

// AssertFailed records that the command must exit with anything other than
// zero.
func (c *PendingCommand) AssertFailed() *PendingCommand {
	c.Test.Helper()
	return c.AssertNotExitCode(pendingCommandSuccess)
}

// Execute is [PendingCommand.Run] under another name.
func (c *PendingCommand) Execute(ctx context.Context) int {
	c.Test.Helper()
	return c.Run(ctx)
}

// Run executes the command, asserts the status and then every expectation that
// was recorded, clears them, and returns the status.
//
// A question the test did not script reaches the end of the input, and the
// console reports that it ran out of answers -- which arrives here as a failed
// status and as output, both of which are asserted.
func (c *PendingCommand) Run(ctx context.Context) int {
	c.Test.Helper()
	c.hasExecuted = true

	var captured bytes.Buffer
	err := c.kernel.Call(ctx, c.command, c.parameters, c.consoleInput(), &captured)
	exitCode := console.ExitCode(err)

	switch {
	case c.expectedExitCode != nil:
		assertEquals(c.Test, *c.expectedExitCode, exitCode, fmt.Sprintf(
			"Expected status code %d but received %d.", *c.expectedExitCode, exitCode))
	case c.unexpectedExitCode != nil:
		assertNotEquals(c.Test, *c.unexpectedExitCode, exitCode, fmt.Sprintf(
			"Unexpected status code %d was received.", *c.unexpectedExitCode))
	}

	c.verifyExpectations(captured.String())
	c.flushExpectations()

	return exitCode
}

// consoleInput is what the command reads its answers from: the scripted
// answers, one line each, in the order the questions were declared.
//
// Proving each question was actually asked, with the options it offered, is
// done separately in verifyExpectations against what the command printed,
// because a console.IO writes its prompt where every other line goes.
func (c *PendingCommand) consoleInput() io.Reader {
	var script strings.Builder
	for _, question := range c.expectedQuestions {
		script.WriteString(question.answer)
		script.WriteString("\n")
	}
	return strings.NewReader(script.String())
}

// verifyExpectations reports the first expectation the run did not meet, in a
// fixed order: questions, then choices, then output.
func (c *PendingCommand) verifyExpectations(output string) {
	c.Test.Helper()

	remainder, asked := c.verifyQuestions(output)
	if !asked {
		return
	}

	for _, choice := range c.expectedChoices {
		message := fmt.Sprintf("Question %q has different options.", choice.question)
		if choice.strict {
			assertEquals(c.Test, choice.expected, choice.actual, message)
		} else {
			assertEqualsCanonicalizing(c.Test, choice.expected, choice.actual, message)
		}
	}

	if c.expectsOutput != nil {
		switch {
		case !*c.expectsOutput && output != "":
			fail(c.Test, "Expected no output but received %q.", output)
			return
		case *c.expectsOutput && len(c.expectedOutput) == 0 &&
			len(c.expectedOutputSubstrings) == 0 && output == "":
			fail(c.Test, "Expected output but none was printed.")
			return
		}
	}

	lines := pendingCommandLines(remainder)

	from := 0
	for _, expected := range c.expectedOutput {
		at := pendingCommandLineIndex(lines, expected, from)
		if at < 0 {
			fail(c.Test, "Output %q was not printed.", expected)
			return
		}
		from = at + 1
	}

	for _, expected := range c.expectedOutputSubstrings {
		if !strings.Contains(output, expected) {
			fail(c.Test, "Output does not contain %q.", expected)
			return
		}
	}

	for _, unexpected := range c.unexpectedOutput {
		if pendingCommandLineIndex(lines, unexpected, 0) >= 0 {
			fail(c.Test, "Output %q was printed.", unexpected)
			return
		}
	}

	for _, unexpected := range c.unexpectedOutputSubstrings {
		if strings.Contains(output, unexpected) {
			fail(c.Test, "Output %q was printed.", unexpected)
			return
		}
	}
}

// verifyQuestions walks the output once, in order, proving that every expected
// question was asked and collecting the options each choice offered.
//
// It returns what is left of the output after the last question, and whether
// they were all asked -- so that the caller stops rather than going on to
// compare options nothing ever offered.
//
// # Why the remainder, and not the whole output
//
// A prompt is written without a newline, because in a terminal the newline
// comes from the person pressing return. Read from a pipe there is nobody to
// press it, so the prompt and whatever the command prints next land on the same
// line: asking "What is your name?" and then printing "Hello, Taylor" produces
//
//	What is your name? Hello, Taylor
//
// and an ExpectsOutput("Hello, Taylor") comparing whole lines finds nothing.
//
// Here there is one buffer, so the prompts are consumed off the front and the
// line comparison runs on what is left. Anything printed BEFORE a question is
// consumed with it.
func (c *PendingCommand) verifyQuestions(output string) (string, bool) {
	c.Test.Helper()

	rest := output
	for _, question := range c.expectedQuestions {
		at := strings.Index(rest, question.question)
		if at < 0 {
			fail(c.Test, "Question %q was not asked.", question.question)
			return rest, false
		}
		rest = rest[at+len(question.question):]

		// A choice writes its options on the lines under the question, so this
		// is where they can be read. A search asks the same question twice --
		// once bare, once with the options -- and the second pass is the one
		// that finds them.
		//
		// It reads them before the prompt is trimmed off, because the options
		// are part of the prompt.
		if choice := c.pendingCommandChoiceFor(question.question); choice != nil {
			if offered := pendingCommandOfferedChoices(rest); len(offered) > 0 {
				choice.actual = offered
			}
		}

		rest = pendingCommandTrimPrompt(rest)
	}
	return rest, true
}

// flushExpectations clears every recorded expectation, so a second run starts
// from nothing.
func (c *PendingCommand) flushExpectations() {
	c.expectedOutput = nil
	c.expectedOutputSubstrings = nil
	c.unexpectedOutput = nil
	c.unexpectedOutputSubstrings = nil
	c.expectedQuestions = nil
	c.expectedChoices = nil
	c.expectsOutput = nil
}

// pendingCommandChoiceFor finds the recorded choice for a question.
//
// The choices are a slice rather than a map so they keep declaration order,
// which is what makes a failure report the first choice that differs rather
// than an arbitrary one.
func (c *PendingCommand) pendingCommandChoiceFor(question string) *pendingCommandChoice {
	for _, choice := range c.expectedChoices {
		if choice.question == question {
			return choice
		}
	}
	return nil
}

// pendingCommandSuccess is the status console.ExitCode returns for a command
// that came back with no error.
const pendingCommandSuccess = 0

// pendingCommandAnswer types an answer the way a person would.
func pendingCommandAnswer(answer any) string {
	switch value := answer.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		if value {
			return "yes"
		}
		return "no"
	case []string:
		return strings.Join(value, ",")
	case int:
		return strconv.Itoa(value)
	default:
		return fmt.Sprint(value)
	}
}

// pendingCommandLines splits captured output the way a reader sees it, with the
// carriage return a Windows console leaves behind taken off.
func pendingCommandLines(output string) []string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// pendingCommandLineIndex finds the line that is exactly the expected one, at
// or after from.
func pendingCommandLineIndex(lines []string, expected string, from int) int {
	for i := from; i < len(lines); i++ {
		if lines[i] == expected {
			return i
		}
	}
	return -1
}

// pendingCommandTrimPrompt drops what is left of a prompt after its question.
//
// A prompt is written without a trailing newline, because in a terminal the
// newline comes from the person pressing return. Read from a pipe there is
// nobody to press it, so whatever the command prints next lands on the same
// line as the prompt -- and a whole-line comparison, which is what
// [PendingCommand.ExpectsOutput] does, then matches nothing.
//
// So the tail has to come off, and this knows the three shapes console.IO
// writes. There is exactly one console in this framework, and these are its
// prompts:
//
//	Ask      question [default]<space>
//	Confirm  question [y/N]<space>
//	Choice   question [default]\n  1) one\n  2) two\n><space>
//
// Anything that does not look like one of them is left alone, so a command that
// prints on the same line as its own question loses nothing it printed.
func pendingCommandTrimPrompt(rest string) string {
	rest = strings.TrimLeft(rest, " \t")

	// The default, or the y/N hint, both of which are bracketed.
	if strings.HasPrefix(rest, "[") {
		if end := strings.IndexByte(rest, ']'); end >= 0 {
			// Only when the bracket closes on the same line: an unclosed one is
			// output, not a prompt.
			if !strings.ContainsRune(rest[:end], '\n') {
				rest = strings.TrimLeft(rest[end+1:], " \t")
			}
		}
	}

	// A choice puts its options on the lines below, and its cursor after them.
	if strings.HasPrefix(rest, "\n") {
		lines := strings.Split(rest[1:], "\n")
		consumed := 0
		for _, line := range lines {
			if _, ok := pendingCommandOption(line); !ok {
				break
			}
			consumed += len(line) + 1
		}
		if consumed > 0 {
			after := rest[1+consumed:]
			if strings.HasPrefix(after, "> ") {
				after = after[2:]
			}
			rest = strings.TrimLeft(after, " \t")
		}
	}

	return rest
}

// pendingCommandOfferedChoices reads the options a choice printed under its
// question: the numbered lines console.IO.Choice writes, and nothing after
// them.
func pendingCommandOfferedChoices(after string) []string {
	lines := pendingCommandLines(after)
	if len(lines) == 0 {
		return nil
	}

	var offered []string
	for _, line := range lines[1:] {
		option, ok := pendingCommandOption(line)
		if !ok {
			break
		}
		offered = append(offered, option)
	}
	return offered
}

// pendingCommandOption reads one numbered option line, "  1) first", and
// reports whether the line was one.
func pendingCommandOption(line string) (string, bool) {
	rest := strings.TrimLeft(line, " \t")

	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || !strings.HasPrefix(rest[digits:], ") ") {
		return "", false
	}
	return rest[digits+2:], true
}
