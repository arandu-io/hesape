package console_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// newIO is the shape every test here starts from: two buffers and a script of
// answers.
func newIO(t *testing.T, args []string, input string) (*console.IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return console.NewIO("test", args, &out, &errOut, strings.NewReader(input)), &out, &errOut
}

// TestOutputStreams: what a person reads goes to the output, what went wrong
// goes to the error stream, and the prefix survives the pipe that the colour
// does not.
func TestOutputStreams(t *testing.T) {
	o, out, errOut := newIO(t, nil, "")

	o.Line("three rows deleted")
	o.Info("done in %dms", 12)
	o.Comment("the rest were already gone")
	o.NewLine()
	o.Warn("the %s disk is nearly full", "local")
	o.Error("could not reach the queue")

	wantOut := "three rows deleted\ndone in 12ms\nthe rest were already gone\n\n"
	if out.String() != wantOut {
		t.Errorf("output = %q, want %q", out.String(), wantOut)
	}
	wantErr := "warning: the local disk is nearly full\nerror: could not reach the queue\n"
	if errOut.String() != wantErr {
		t.Errorf("error stream = %q, want %q", errOut.String(), wantErr)
	}
}

// TestNoColourOffATerminal: a buffer is not a terminal, so the bytes a test
// asserts are the bytes a pipe receives.
func TestNoColourOffATerminal(t *testing.T) {
	o, out, _ := newIO(t, nil, "")
	o.Info("green on a terminal, plain here")
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("escape sequences written to something that is not a terminal: %q", out.String())
	}
}

func TestArgsAreWhatFollowedTheName(t *testing.T) {
	o, _, _ := newIO(t, []string{"--force", "invoices"}, "")

	if got := o.Args(); len(got) != 2 {
		t.Fatalf("Args before parsing = %v, want both", got)
	}

	fs := o.Flags()
	force := fs.Bool("force", false, "overwrite")
	if err := fs.Parse(o.Args()); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !*force {
		t.Error("--force was not read")
	}
	if got := o.Args(); len(got) != 1 || got[0] != "invoices" {
		t.Errorf("Args after parsing = %v, want [invoices]", got)
	}
}

// TestFlagsReportToTheErrorStream: a bad flag is an error the command returns,
// not an os.Exit inside a library.
func TestFlagsReportToTheErrorStream(t *testing.T) {
	o, out, errOut := newIO(t, []string{"--nope"}, "")
	fs := o.Flags()
	if err := fs.Parse(o.Args()); err == nil {
		t.Fatal("an unknown flag parsed without an error")
	}
	if errOut.Len() == 0 {
		t.Error("the usage went nowhere")
	}
	if out.Len() != 0 {
		t.Errorf("the usage went to the output stream: %q", out.String())
	}
}

func TestAsk(t *testing.T) {
	o, out, _ := newIO(t, nil, "invoices\n")
	answer, err := o.Ask("which table?", "users")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "invoices" {
		t.Errorf("answer = %q, want invoices", answer)
	}
	if !strings.Contains(out.String(), "[users]") {
		t.Errorf("the default was not offered: %q", out.String())
	}
}

func TestAskFallsBackToTheDefault(t *testing.T) {
	o, _, _ := newIO(t, nil, "\n")
	answer, err := o.Ask("which table?", "users")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "users" {
		t.Errorf("answer = %q, want users", answer)
	}
}

// TestAskOnInputThatEnded: silence is not consent. A prompt in a script that
// took an empty answer for a yes is how something gets deleted.
func TestAskOnInputThatEnded(t *testing.T) {
	o, _, _ := newIO(t, nil, "")
	if _, err := o.Ask("which table?", ""); err == nil {
		t.Fatal("Ask returned an answer from input that had ended")
	}
}

func TestConfirm(t *testing.T) {
	for name, tc := range map[string]struct {
		input string
		def   bool
		want  bool
	}{
		"yes":              {input: "y\n", def: false, want: true},
		"the word yes":     {input: "YES\n", def: false, want: true},
		"no":               {input: "n\n", def: true, want: false},
		"empty is default": {input: "\n", def: true, want: true},
		"typo then answer": {input: "maybe\ny\n", def: false, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			o, _, _ := newIO(t, nil, tc.input)
			got, err := o.Confirm("drop the table?", tc.def)
			if err != nil {
				t.Fatalf("Confirm: %v", err)
			}
			if got != tc.want {
				t.Errorf("Confirm = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChoice(t *testing.T) {
	options := []string{"postgres", "mysql", "sqlite"}

	for name, tc := range map[string]struct {
		input string
		def   string
		want  string
	}{
		"by number":        {input: "2\n", want: "mysql"},
		"by name":          {input: "sqlite\n", want: "sqlite"},
		"empty is default": {input: "\n", def: "postgres", want: "postgres"},
		"out of range":     {input: "9\n1\n", want: "postgres"},
	} {
		t.Run(name, func(t *testing.T) {
			o, _, _ := newIO(t, nil, tc.input)
			got, err := o.Choice("which driver?", options, tc.def)
			if err != nil {
				t.Fatalf("Choice: %v", err)
			}
			if got != tc.want {
				t.Errorf("Choice = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChoiceNeedsOptions(t *testing.T) {
	o, _, _ := newIO(t, nil, "1\n")
	if _, err := o.Choice("which driver?", nil, ""); err == nil {
		t.Fatal("a choice with nothing to choose from was accepted")
	}
}

// TestSecretOffATerminal: there is no echo to turn off on a pipe, so the value
// is read as it comes and no platform code runs.
func TestSecretOffATerminal(t *testing.T) {
	o, _, _ := newIO(t, nil, "hunter2\n")
	secret, err := o.Secret("password:")
	if err != nil {
		t.Fatalf("Secret: %v", err)
	}
	if secret != "hunter2" {
		t.Errorf("Secret = %q, want hunter2", secret)
	}
}

func TestTableLinesTheColumnsUp(t *testing.T) {
	o, out, _ := newIO(t, nil, "")
	o.Table([]string{"NAME", "BATCH"}, [][]string{
		{"create_users_table", "1"},
		{"add_invoice_status", "2"},
		{"short"},
	})

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("wrote %d lines, want 4:\n%s", len(lines), out.String())
	}
	// Every column starts at the same offset, which is the whole point.
	at := strings.Index(lines[0], "BATCH")
	for _, line := range lines[1:3] {
		if !strings.HasPrefix(line[at:], strings.Fields(line)[1]) {
			t.Errorf("the second column of %q does not start at %d", line, at)
		}
	}
	// A row with fewer cells than headers is padded rather than dropped.
	if !strings.HasPrefix(lines[3], "short") {
		t.Errorf("the short row was mangled: %q", lines[3])
	}
}

func TestTwoColumnDetail(t *testing.T) {
	o, out, _ := newIO(t, nil, "")
	o.TwoColumnDetail("create_users_table", "ran")

	line := strings.TrimRight(out.String(), "\n")
	if !strings.HasPrefix(line, "create_users_table ") || !strings.HasSuffix(line, " ran") {
		t.Fatalf("line = %q", line)
	}
	if !strings.Contains(line, "....") {
		t.Errorf("the two halves are not joined by dots: %q", line)
	}
}

// TestTwoColumnDetailOverflows: a label longer than the line still reads, and
// still ends with its value.
func TestTwoColumnDetailOverflows(t *testing.T) {
	o, out, _ := newIO(t, nil, "")
	o.TwoColumnDetail(strings.Repeat("x", 90), "ran")
	if !strings.HasSuffix(strings.TrimRight(out.String(), "\n"), " . ran") {
		t.Errorf("line = %q", out.String())
	}
}

func TestTaskReportsAndReturns(t *testing.T) {
	o, out, _ := newIO(t, nil, "")

	if err := o.Task("clearing the cache", func() error { return nil }); err != nil {
		t.Fatalf("Task: %v", err)
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Errorf("the good task did not say so: %q", out.String())
	}

	broken := errors.New("the store is down")
	if err := o.Task("clearing the cache", func() error { return broken }); !errors.Is(err, broken) {
		t.Errorf("Task returned %v, want the error unchanged", err)
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("the failed task did not say so: %q", out.String())
	}
}

// TestProgressOffATerminal: a bar redrawn into a log file is a thousand lines
// of the same sentence, so off a terminal only the last one is written.
func TestProgressOffATerminal(t *testing.T) {
	o, out, _ := newIO(t, nil, "")

	p := o.Progress(4)
	p.Advance(1)
	p.Advance(1)
	if out.Len() != 0 {
		t.Fatalf("the bar was drawn into something that is not a terminal: %q", out.String())
	}

	p.Finish()
	p.Finish()

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("wrote %d lines, want 1:\n%s", len(lines), out.String())
	}
	if !strings.HasSuffix(lines[0], "4/4") {
		t.Errorf("the finished bar = %q, want it complete", lines[0])
	}
}
