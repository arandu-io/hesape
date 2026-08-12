package testing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// pendingCommandFakeT captures the failure instead of ending the test, so that
// a test can assert what an assertion says when it does not hold.
type pendingCommandFakeT struct {
	failed  bool
	message string
}

func (f *pendingCommandFakeT) Helper() {}

func (f *pendingCommandFakeT) Fatalf(format string, args ...any) {
	if !f.failed {
		f.failed = true
		f.message = fmt.Sprintf(format, args...)
	}
}

func (f *pendingCommandFakeT) Logf(string, ...any) {}

// pendingCommandFakeKernel is a PendingCommandKernel written as a function.
type pendingCommandFakeKernel func(ctx context.Context, command string, parameters []string, in io.Reader, out io.Writer) error

func (k pendingCommandFakeKernel) Call(ctx context.Context, command string, parameters []string, in io.Reader, out io.Writer) error {
	return k(ctx, command, parameters, in, out)
}

// pendingCommandKernelOf runs the body over a real console.IO wired to the
// streams the PendingCommand handed over, which is what the console does.
func pendingCommandKernelOf(body func(o *console.IO) error) pendingCommandFakeKernel {
	return func(_ context.Context, command string, parameters []string, in io.Reader, out io.Writer) error {
		return body(console.NewIO(command, parameters, out, out, in))
	}
}

func pendingCommandSilentKernel(err error) pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(*console.IO) error { return err })
}

func TestPendingCommandAssertExitCodePasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	code := NewPendingCommand(fake, pendingCommandSilentKernel(console.Exit(2, "no")), "app:work", nil).
		AssertExitCode(2).
		Run(context.Background())

	if code != 2 {
		t.Fatalf("status = %d, want 2", code)
	}
	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandAssertExitCodeFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(console.Exit(2, "no")), "app:work", nil).
		AssertExitCode(0).
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := "Expected status code 0 but received 2."; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandAssertNotExitCodePasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(nil), "app:work", nil).
		AssertNotExitCode(1).
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandAssertNotExitCodeFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(nil), "app:work", nil).
		AssertNotExitCode(0).
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := "Unexpected status code 0 was received."; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandAssertSuccessfulAndOkPass(t *testing.T) {
	t.Parallel()

	for name, assert := range map[string]func(*PendingCommand) *PendingCommand{
		"assertSuccessful": (*PendingCommand).AssertSuccessful,
		"assertOk":         (*PendingCommand).AssertOk,
	} {
		fake := &pendingCommandFakeT{}
		assert(NewPendingCommand(fake, pendingCommandSilentKernel(nil), "app:work", nil)).
			Run(context.Background())

		if fake.failed {
			t.Fatalf("%s: unexpected failure: %s", name, fake.message)
		}
	}
}

func TestPendingCommandAssertOkFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(errors.New("boom")), "app:work", nil).
		AssertOk().
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := "Expected status code 0 but received 1."; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandAssertFailedPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(errors.New("boom")), "app:work", nil).
		AssertFailed().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandAssertFailedFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(nil), "app:work", nil).
		AssertFailed().
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
}

// pendingCommandAskKernel asks one question and greets the answer.
func pendingCommandAskKernel() pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(o *console.IO) error {
		name, err := o.Ask("What is your name?", "")
		if err != nil {
			return err
		}
		o.Line("Hello, %s", name)
		return nil
	})
}

func TestPendingCommandExpectsQuestionPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandAskKernel(), "app:greet", nil).
		ExpectsQuestion("What is your name?", "Taylor").
		ExpectsOutput("Hello, Taylor").
		AssertSuccessful().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsQuestionFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandAskKernel(), "app:greet", nil).
		ExpectsQuestion("What is your quest?", "Taylor").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Question "What is your quest?" was not asked.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

// pendingCommandConfirmKernel asks for confirmation and says what it got.
func pendingCommandConfirmKernel() pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(o *console.IO) error {
		confirmed, err := o.Confirm("Do you wish to continue?", false)
		if err != nil {
			return err
		}
		if confirmed {
			o.Line("Confirmed")
			return nil
		}
		o.Line("Declined")
		return nil
	})
}

func TestPendingCommandExpectsConfirmationPasses(t *testing.T) {
	t.Parallel()

	for answer, expected := range map[string]string{"yes": "Confirmed", "no": "Declined", "": "Declined"} {
		fake := &pendingCommandFakeT{}
		NewPendingCommand(fake, pendingCommandConfirmKernel(), "app:work", nil).
			ExpectsConfirmation("Do you wish to continue?", answer).
			ExpectsOutput(expected).
			Run(context.Background())

		if fake.failed {
			t.Fatalf("answer %q: unexpected failure: %s", answer, fake.message)
		}
	}
}

func TestPendingCommandExpectsConfirmationFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandConfirmKernel(), "app:work", nil).
		ExpectsConfirmation("Do you wish to continue?", "no").
		ExpectsOutput("Confirmed").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Output "Confirmed" was not printed.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

// pendingCommandChoiceKernel offers two options and reports the one picked.
func pendingCommandChoiceKernel() pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(o *console.IO) error {
		picked, err := o.Choice("Which database?", []string{"postgres", "sqlite"}, "")
		if err != nil {
			return err
		}
		o.Line("Using %s", picked)
		return nil
	})
}

func TestPendingCommandExpectsChoicePasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandChoiceKernel(), "app:install", nil).
		ExpectsChoice("Which database?", "sqlite", []string{"sqlite", "postgres"}, false).
		ExpectsOutput("Using sqlite").
		AssertSuccessful().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsChoiceFailsOnDifferentOptions(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandChoiceKernel(), "app:install", nil).
		ExpectsChoice("Which database?", "sqlite", []string{"sqlite", "mysql"}, false).
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Question "Which database?" has different options.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandExpectsChoiceStrictFailsOnDifferentOrder(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandChoiceKernel(), "app:install", nil).
		ExpectsChoice("Which database?", "sqlite", []string{"sqlite", "postgres"}, true).
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Question "Which database?" has different options.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandExpectsSearchPasses(t *testing.T) {
	t.Parallel()

	kernel := pendingCommandKernelOf(func(o *console.IO) error {
		search, err := o.Ask("Which user?", "")
		if err != nil {
			return err
		}

		var found []string
		for _, user := range []string{"Taylor Otwell", "Jess Archer", "Tim MacDonald"} {
			if strings.Contains(user, search) {
				found = append(found, user)
			}
		}

		picked, err := o.Choice("Which user?", found, "")
		if err != nil {
			return err
		}
		o.Line("Picked %s", picked)
		return nil
	})

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, kernel, "app:user", nil).
		ExpectsSearch("Which user?", "Tim MacDonald", "Ti", []string{"Tim MacDonald"}).
		ExpectsOutput("Picked Tim MacDonald").
		AssertSuccessful().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

// pendingCommandTwoLineKernel prints two lines, in this order.
func pendingCommandTwoLineKernel() pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(o *console.IO) error {
		o.Line("first line")
		o.Line("second line")
		return nil
	})
}

func TestPendingCommandExpectsOutputPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutput("first line").
		ExpectsOutput("second line").
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsOutputFailsOutOfOrder(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutput("second line").
		ExpectsOutput("first line").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Output "first line" was not printed.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandExpectsOutputWithNoArgumentPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutput().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsOutputWithNoArgumentFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandSilentKernel(nil), "app:work", nil).
		ExpectsOutput().
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := "Expected output but none was printed."; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandDoesntExpectOutputPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		DoesntExpectOutput("third line").
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandDoesntExpectOutputFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		DoesntExpectOutput("second line").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Output "second line" was printed.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandDoesntExpectOutputWithNoArgumentFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		DoesntExpectOutput().
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if !strings.HasPrefix(fake.message, "Expected no output but received ") {
		t.Fatalf("message = %q", fake.message)
	}
}

func TestPendingCommandExpectsOutputToContainPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutputToContain("second").
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsOutputToContainFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutputToContain("third").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Output does not contain "third".`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

func TestPendingCommandDoesntExpectOutputToContainPasses(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		DoesntExpectOutputToContain("third").
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandDoesntExpectOutputToContainFails(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		DoesntExpectOutputToContain("second").
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if want := `Output "second" was printed.`; fake.message != want {
		t.Fatalf("message = %q, want %q", fake.message, want)
	}
}

// pendingCommandTableKernel prints one table, the way a command does.
func pendingCommandTableKernel(rows [][]string) pendingCommandFakeKernel {
	return pendingCommandKernelOf(func(o *console.IO) error {
		o.Table([]string{"Name", "Email"}, rows)
		return nil
	})
}

func TestPendingCommandExpectsTablePasses(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"Taylor", "taylor@example.com"},
		{"Jess", "jess@example.com"},
	}

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTableKernel(rows), "app:users", nil).
		ExpectsTable([]string{"Name", "Email"}, rows).
		AssertSuccessful().
		Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandExpectsTableFails(t *testing.T) {
	t.Parallel()

	printed := [][]string{{"Taylor", "taylor@example.com"}}
	expected := [][]string{{"Jess", "jess@example.com"}}

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, pendingCommandTableKernel(printed), "app:users", nil).
		ExpectsTable([]string{"Name", "Email"}, expected).
		Run(context.Background())

	if !fake.failed {
		t.Fatal("expected a failure and got none")
	}
	if !strings.HasPrefix(fake.message, "Output ") || !strings.HasSuffix(fake.message, " was not printed.") {
		t.Fatalf("message = %q", fake.message)
	}
}

func TestPendingCommandExecuteRunsTheCommand(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	command := NewPendingCommand(fake, pendingCommandSilentKernel(console.Exit(3, "no")), "app:work", nil)

	code := command.Execute(context.Background())

	if code != 3 {
		t.Fatalf("status = %d, want 3", code)
	}
	if !command.hasExecuted {
		t.Fatal("the command was not marked as executed")
	}
	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}

func TestPendingCommandRunFlushesExpectations(t *testing.T) {
	t.Parallel()

	fake := &pendingCommandFakeT{}
	command := NewPendingCommand(fake, pendingCommandTwoLineKernel(), "app:work", nil).
		ExpectsOutput("first line").
		ExpectsOutputToContain("second").
		DoesntExpectOutput("third line")

	command.Run(context.Background())

	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
	if command.expectedOutput != nil || command.expectedOutputSubstrings != nil ||
		command.unexpectedOutput != nil || command.expectedQuestions != nil ||
		command.expectedChoices != nil || command.expectsOutput != nil {
		t.Fatal("the expectations were not flushed")
	}
}

func TestPendingCommandPassesTheParametersToTheKernel(t *testing.T) {
	t.Parallel()

	var got []string
	kernel := pendingCommandFakeKernel(func(_ context.Context, command string, parameters []string, _ io.Reader, _ io.Writer) error {
		if command != "app:work" {
			t.Errorf("command = %q, want %q", command, "app:work")
		}
		got = parameters
		return nil
	})

	fake := &pendingCommandFakeT{}
	NewPendingCommand(fake, kernel, "app:work", []string{"--force", "5"}).
		AssertSuccessful().
		Run(context.Background())

	if len(got) != 2 || got[0] != "--force" || got[1] != "5" {
		t.Fatalf("parameters = %v", got)
	}
	if fake.failed {
		t.Fatalf("unexpected failure: %s", fake.message)
	}
}
