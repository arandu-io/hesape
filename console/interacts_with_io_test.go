package console_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// io returns an IO over buffers, and the buffers, which is how every test below
// reads what a command printed.
func newConsoleIO(t *testing.T, input string) (*console.IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	var out, errOut bytes.Buffer
	return console.NewIO("test", nil, &out, &errOut, strings.NewReader(input)), &out, &errOut
}

func TestNewLineWritesAsManyLinesAsItWasAskedFor(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	o.NewLine()
	if got := out.String(); got != "\n" {
		t.Errorf("NewLine wrote %q, want one line ending", got)
	}

	out.Reset()
	o.NewLine(3)
	if got := out.String(); got != "\n\n\n" {
		t.Errorf("NewLine(3) wrote %q, want three line endings", got)
	}
}

func TestWriteAndWritelnHonourTheVerbosity(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	o.Writeln("normal")
	if !strings.Contains(out.String(), "normal") {
		t.Error("a normal line was not written at the normal level")
	}

	out.Reset()
	o.Writeln("chatter", console.VerbosityVerbose)
	if out.Len() != 0 {
		t.Errorf("a verbose line was written at the normal level: %q", out.String())
	}

	out.Reset()
	o.SetVerbosity(console.VerbosityVerbose)
	o.Writeln("chatter", console.VerbosityVerbose)
	if !strings.Contains(out.String(), "chatter") {
		t.Error("a verbose line was not written at the verbose level")
	}

	out.Reset()
	o.SetVerbosity(console.VerbosityQuiet)
	o.Writeln("anything")
	if out.Len() != 0 {
		t.Errorf("a quiet command wrote %q", out.String())
	}
}

func TestTheVerbosityQuestionsAnswerTheLevel(t *testing.T) {
	o, _, _ := newConsoleIO(t, "")

	o.SetVerbosity(console.VerbosityQuiet)
	if !o.IsQuiet() || o.IsVerbose() || o.IsDebug() {
		t.Error("a quiet command did not report itself quiet")
	}

	o.SetVerbosity(console.VerbosityVeryVerbose)
	if o.IsQuiet() || !o.IsVerbose() || !o.IsVeryVerbose() || o.IsDebug() {
		t.Error("a very verbose command reported the wrong level")
	}

	o.SetVerbosityNamed("vvv")
	if !o.IsDebug() {
		t.Error("the word vvv did not set the debug level")
	}

	o.SetVerbosityNamed("nonsense")
	if !o.IsDebug() {
		t.Error("an unknown word changed the level, and a typo must not silence a command")
	}
}

func TestNewLinesWrittenTracksWhatTheLastWriteLeftBehind(t *testing.T) {
	o, _, _ := newConsoleIO(t, "")

	// The count starts at one, for the line ending the shell wrote after the
	// command was typed.
	if got := o.NewLinesWritten(); got != 1 {
		t.Errorf("NewLinesWritten = %d before anything was written, want 1", got)
	}

	o.Write("no ending")
	if got := o.NewLinesWritten(); got != 0 {
		t.Errorf("NewLinesWritten = %d after a Write with no ending, want 0", got)
	}

	o.Writeln("one line")
	if got := o.NewLinesWritten(); got != 1 {
		t.Errorf("NewLinesWritten = %d after a Writeln, want 1", got)
	}
}

func TestAlertWritesTheMessageInCapitals(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	o.Alert("application in production")

	if !strings.Contains(out.String(), "APPLICATION IN PRODUCTION") {
		t.Errorf("Alert wrote %q, want the message in capitals", out.String())
	}
}

func TestTheComponentsRenderALabelledLine(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	o.OutputComponents().Info("the cache was cleared")

	got := out.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("the info component wrote %q, want an INFO label", got)
	}
	if !strings.Contains(got, "the cache was cleared.") {
		t.Errorf("the info component wrote %q, want the message with a full stop", got)
	}
}

func TestTheComponentsRenderABulletList(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	o.OutputComponents().BulletList([]string{"first.", "second."})

	got := out.String()
	if strings.Contains(got, "first.") {
		t.Errorf("the bullet list wrote %q, and a list element ends without punctuation", got)
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("the bullet list wrote %q, want both elements", got)
	}
}

func TestTheTaskComponentReportsHowItWent(t *testing.T) {
	console.ResolveTerminalWidthUsing(func() int { return 60 })
	defer console.ResolveTerminalWidthUsing(nil)

	o, out, _ := newConsoleIO(t, "")

	if err := o.OutputComponents().Task("migrating", func() (console.TaskResult, error) {
		return console.TaskSuccess, nil
	}); err != nil {
		t.Fatalf("Task: %v", err)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Errorf("a task that worked wrote %q, want DONE", out.String())
	}

	out.Reset()
	err := o.OutputComponents().Task("migrating", func() (console.TaskResult, error) {
		return console.TaskFailure, errNope
	})
	if err == nil {
		t.Error("Task swallowed the error the work returned")
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("a task that failed wrote %q, want FAIL", out.String())
	}
}

func TestTheTwoColumnDetailLinesUpTheColumns(t *testing.T) {
	console.ResolveTerminalWidthUsing(func() int { return 60 })
	defer console.ResolveTerminalWidthUsing(nil)

	o, out, _ := newConsoleIO(t, "")
	o.OutputComponents().TwoColumnDetail("2026_08_11_create_users_table", "DONE")

	got := out.String()
	if !strings.Contains(got, "2026_08_11_create_users_table") || !strings.Contains(got, "DONE") {
		t.Errorf("the detail line wrote %q, want both columns", got)
	}
	if !strings.Contains(got, "..") {
		t.Errorf("the detail line wrote %q, want the dots between the columns", got)
	}
}

func TestWithProgressBarFinishesTheBarEvenWhenTheWorkFailed(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	err := o.WithProgressBar(3, func(bar *console.Progress) error {
		bar.Advance(1)
		return errNope
	})
	if err == nil {
		t.Error("WithProgressBar swallowed the error the work returned")
	}
	if out.Len() == 0 {
		t.Error("the bar was never finished, and nothing was written")
	}
}

func TestConfirmToProceedTakesForceAsTheAnswer(t *testing.T) {
	_, arguments, options, err := console.Parse("db:wipe {--force}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	in := console.NewInput(arguments, options)
	if err := in.Parse([]string{"--force"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	o, out, _ := newConsoleIO(t, "")
	o.SetInput(in)

	proceed, err := o.ConfirmToProceed("Application In Production", true)
	if err != nil {
		t.Fatalf("ConfirmToProceed: %v", err)
	}
	if !proceed {
		t.Error("--force was given and the command still refused")
	}
	if out.Len() != 0 {
		t.Errorf("--force was given and the command still asked: %q", out.String())
	}
}

func TestConfirmToProceedDoesNotAskWhenThereIsNothingToConfirm(t *testing.T) {
	o, out, _ := newConsoleIO(t, "")

	proceed, err := o.ConfirmToProceed("Application In Production", false)
	if err != nil {
		t.Fatalf("ConfirmToProceed: %v", err)
	}
	if !proceed {
		t.Error("a command with nothing to confirm was refused")
	}
	if out.Len() != 0 {
		t.Errorf("a command with nothing to confirm asked anyway: %q", out.String())
	}
}

func TestConfirmToProceedStopsOnAnythingButYes(t *testing.T) {
	o, _, _ := newConsoleIO(t, "n\n")

	proceed, err := o.ConfirmToProceed("Application In Production", true)
	if err != nil {
		t.Fatalf("ConfirmToProceed: %v", err)
	}
	if proceed {
		t.Error("the answer was no and the command went ahead")
	}
}

func TestProhibitStopsACommandAndSaysSo(t *testing.T) {
	console.Prohibit("db:wipe")
	defer console.Prohibit("db:wipe", false)

	o, out, _ := newConsoleIO(t, "")
	if !console.IsProhibited("db:wipe", o) {
		t.Error("a prohibited command was allowed to run")
	}
	if !strings.Contains(out.String(), "prohibited") {
		t.Errorf("nothing was said about the prohibition: %q", out.String())
	}

	out.Reset()
	if !console.IsProhibited("db:wipe", o, true) {
		t.Error("the quiet check disagreed with the loud one")
	}
	if out.Len() != 0 {
		t.Errorf("the quiet check wrote %q", out.String())
	}

	if console.IsProhibited("db:seed", o) {
		t.Error("a command nobody prohibited was refused")
	}
}

func TestAskQuestionTakesTheDefaultWhenTheCommandMayNotPrompt(t *testing.T) {
	_, arguments, options, err := console.Parse("make:model {--no-interaction}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	in := console.NewInput(arguments, options)
	if err := in.Parse([]string{"--no-interaction"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	o, out, _ := newConsoleIO(t, "typed\n")
	o.SetInput(in)
	o.ConfigurePrompts()

	answer, err := o.AskQuestion("What is the name?", "Invoice")
	if err != nil {
		t.Fatalf("AskQuestion: %v", err)
	}
	if answer != "Invoice" {
		t.Errorf("answer = %q, want the default: a pipeline has nobody to type", answer)
	}
	if out.Len() != 0 {
		t.Errorf("a non-interactive command wrote a prompt: %q", out.String())
	}
}

func TestPromptForMissingArgumentsAsksForWhatIsRequired(t *testing.T) {
	_, arguments, options, err := console.Parse("make:model {name : What should the model be called?}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	in := console.NewInput(arguments, options)

	o, out, _ := newConsoleIO(t, "Invoice\n")
	o.SetInput(in)

	if err := o.PromptForMissingArguments(); err != nil {
		t.Fatalf("PromptForMissingArguments: %v", err)
	}
	if got := o.Argument("name").String(); got != "Invoice" {
		t.Errorf("name = %q, want Invoice", got)
	}
	if !strings.Contains(out.String(), "What should the model be called?") {
		t.Errorf("the prompt was %q, want the argument's description", out.String())
	}
}

func TestBufferedConsoleOutputKeepsWhatItPassedOn(t *testing.T) {
	var passed bytes.Buffer
	buffered := console.NewBufferedConsoleOutput(&passed)

	if _, err := buffered.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := passed.String(); got != "hello\n" {
		t.Errorf("the stream received %q, want hello", got)
	}
	if got := buffered.Fetch(); got != "hello\n" {
		t.Errorf("Fetch = %q, want hello", got)
	}
	if got := buffered.Fetch(); got != "" {
		t.Errorf("the second Fetch = %q, want empty: Fetch empties the buffer", got)
	}
}

func TestApplicationOutputIsWhatTheCalledCommandPrinted(t *testing.T) {
	var out, errOut bytes.Buffer

	app := console.NewApplication(&out, &errOut, strings.NewReader("")).Add(console.Command{
		Name: "app:speak",
		Run: func(_ context.Context, o *console.IO) error {
			o.Line("something happened")
			return nil
		},
	})

	if err := app.Call(t.Context(), "app:speak"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := app.Output(); !strings.Contains(got, "something happened") {
		t.Errorf("Output = %q, want what the command printed", got)
	}
	if !strings.Contains(out.String(), "something happened") {
		t.Error("the output was buffered instead of also reaching the terminal")
	}
}

// errNope is the failure the tests above hand to work that is meant to fail.
var errNope = errNopeType{}

type errNopeType struct{}

func (errNopeType) Error() string { return "nope" }
