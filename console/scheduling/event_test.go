package scheduling_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/console/scheduling"
)

// skipWithoutEcho skips a test that needs a real program to run, which is every
// test in here that proves what is and is not interpreted.
func skipWithoutEcho(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test runs /bin/echo, which is what this platform does not have")
	}
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skipf("the test runs /bin/echo: %v", err)
	}
	return "/bin/echo"
}

// TestAScheduledCommandDoesNotGoThroughAShell is the defect this package was
// changed for.
//
// The command line below carries four things a shell acts on: a semicolon that
// ends one command and begins another, a command substitution in each of the two
// spellings, and the word `touch` that all three of them would run. Every one of
// them would create the marker file if a shell read the line.
//
// Nothing reads it. The words are the arguments echo is given, and the proof is
// in two halves: the marker does not exist, and the characters came back out of
// echo as text.
func TestAScheduledCommandDoesNotGoThroughAShell(t *testing.T) {
	echo := skipWithoutEcho(t)

	dir := t.TempDir()
	marker := filepath.Join(dir, "interpreted")
	output := filepath.Join(dir, "out.log")

	command := echo + " safe ; touch " + marker +
		" $(touch " + marker + ") `touch " + marker + "`"

	event := scheduling.NewEvent(nil, command, nil).SendOutputTo(output)

	if err := event.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("%s exists: the command line was read by a shell and the second command ran", marker)
	}

	written, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading the output file: %v", err)
	}
	printed := string(written)

	for _, want := range []string{"safe", ";", "$(touch", "`touch", marker} {
		if !strings.Contains(printed, want) {
			t.Errorf("the command printed %q, which is missing the literal %q", printed, want)
		}
	}
	if event.ExitCode != 0 {
		t.Errorf("the command ended with %d, want 0", event.ExitCode)
	}
}

// TestAScheduledParameterWithMetacharactersIsOneArgument is the same defect
// reached the way an application reaches it: through Exec, whose parameters are
// escaped on the way into the command line and split back out of it here.
//
// The parameter holds a semicolon and spaces, and it must arrive at the program
// as the single argument it was written as.
func TestAScheduledParameterWithMetacharactersIsOneArgument(t *testing.T) {
	echo := skipWithoutEcho(t)

	dir := t.TempDir()
	marker := filepath.Join(dir, "interpreted")
	output := filepath.Join(dir, "out.log")

	parameter := "a; touch " + marker

	event := scheduling.NewSchedule(nil, nil, nil).
		Exec(echo, parameter).
		SendOutputTo(output)

	command, err := event.BuildCommand()
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if want := []string{echo, parameter}; !slices.Equal(command, want) {
		t.Fatalf("BuildCommand = %q, want %q: the parameter is one argument", command, want)
	}

	if err := event.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("%s exists: the escaped parameter was read as a second command", marker)
	}
	written, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading the output file: %v", err)
	}
	if got := strings.TrimRight(string(written), "\n"); got != parameter {
		t.Errorf("the command printed %q, want the parameter verbatim: %q", got, parameter)
	}
}

// TestTheOutputGoesToTheFileTheEventNamed is the redirection that used to be two
// characters of a shell line and is now what the runtime does with the streams.
func TestTheOutputGoesToTheFileTheEventNamed(t *testing.T) {
	echo := skipWithoutEcho(t)

	output := filepath.Join(t.TempDir(), "out.log")

	if err := scheduling.NewEvent(nil, echo+" first", nil).SendOutputTo(output).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := read(t, output); got != "first\n" {
		t.Errorf("the output file holds %q, want %q", got, "first\n")
	}

	// A second run replaces it, because SendOutputTo does not append.
	if err := scheduling.NewEvent(nil, echo+" second", nil).SendOutputTo(output).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := read(t, output); got != "second\n" {
		t.Errorf("the output file holds %q, want the run to have replaced it", got)
	}

	// AppendOutputTo keeps what is there.
	if err := scheduling.NewEvent(nil, echo+" third", nil).AppendOutputTo(output).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := read(t, output); got != "second\nthird\n" {
		t.Errorf("the output file holds %q, want the second run's output kept", got)
	}
}

// TestAFailedScheduledCommandIsAStatusAndNotAnError pins the contract the
// onFailure callbacks depend on: a command that ran and exited non-zero is the
// event's exit code, and Run returns nothing.
//
// The program here is a shell, and that is the point of the distinction: a shell
// a schedule asked for by name is a program like any other, and a shell nothing
// asked for is what this package stopped doing.
func TestAFailedScheduledCommandIsAStatusAndNotAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test runs /bin/sh as a program that exits non-zero")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("the test runs /bin/sh: %v", err)
	}

	failed := false
	event := scheduling.NewEvent(nil, `/bin/sh -c 'exit 3'`, nil).
		OnFailure(func(context.Context) error {
			failed = true
			return nil
		})

	if err := event.Run(t.Context()); err != nil {
		t.Fatalf("Run returned %v; a command that exited non-zero is a status", err)
	}
	if event.ExitCode != 3 {
		t.Errorf("the event ended with %d, want 3", event.ExitCode)
	}
	if !failed {
		t.Error("the onFailure callback did not run")
	}
}

// TestABackgroundedCommandIsFinishedWhereItWasStarted is the half of a
// backgrounded event that used to be a second process: Run returns before the
// command has ended, and the exit code, the after callbacks and the mutex
// release happen when it does.
func TestABackgroundedCommandIsFinishedWhereItWasStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test runs /bin/sh as a program that exits non-zero")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("the test runs /bin/sh: %v", err)
	}

	// The after callback is what says the event finished, and receiving from the
	// channel is what makes reading the event afterwards ordered.
	finished := make(chan struct{})

	event := scheduling.NewEvent(nil, `/bin/sh -c 'exit 7'`, nil).
		Then(func(context.Context) error {
			close(finished)
			return nil
		})
	event.RunInBackground()

	if err := event.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("the backgrounded command was never finished, and its mutex would be held until it expired")
	}

	if event.ExitCode != 7 {
		t.Errorf("the event ended with %d, want 7", event.ExitCode)
	}
}

// TestACommandLineThatEndsInsideAQuoteIsRefused: a line that cannot be read is a
// command that must not run. Under a shell the same line was a word that
// swallowed the rest of it.
func TestACommandLineThatEndsInsideAQuoteIsRefused(t *testing.T) {
	event := scheduling.NewEvent(nil, `app:work --name='unclosed`, nil)

	if _, err := event.BuildCommand(); err == nil {
		t.Fatal("BuildCommand accepted a command line that ends inside a quote")
	}
	if err := event.Run(t.Context()); err == nil {
		t.Fatal("Run started a command line that could not be read")
	}
}

// TestAQuotedArgumentSurvivesTheRoundTrip is escapeArgument and splitCommand
// being a pair: what compileParameters writes into the line is what comes back
// out of it.
func TestAQuotedArgumentSurvivesTheRoundTrip(t *testing.T) {
	for _, parameter := range []string{
		"plain",
		"two words",
		"it's quoted",
		"a; b && c | d > e",
		"$HOME `whoami` $(id)",
		"back\\slash",
		"",
	} {
		event := scheduling.NewSchedule(nil, nil, nil).Exec("app:work", parameter)

		command, err := event.BuildCommand()
		if err != nil {
			t.Fatalf("BuildCommand(%q): %v", parameter, err)
		}
		if want := []string{"app:work", parameter}; !slices.Equal(command, want) {
			t.Errorf("BuildCommand = %q, want %q", command, want)
		}
	}
}

// read is the output file, as a string.
func read(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(contents)
}
