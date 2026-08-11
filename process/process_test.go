package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// helperEnv turns the test binary into the program these tests run.
//
// Every test here needs a program that exits with a chosen status, prints on a
// chosen stream, or stays quiet for a chosen time. Shelling out to sh would
// test the shell and skip Windows; a fixture binary would need building. The
// test binary is already on disk and already portable, so it stands in for all
// of them -- the shape os/exec's own tests use.
const helperEnv = "HESAPE_PROCESS_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		// syscall.Exit and not os.Exit: under -race the runtime spends a full
		// second finishing up before the process actually goes away, and a
		// second of silence after the last line is exactly what the idle timeout
		// is there to kill. Every output of the helper is written to an
		// unbuffered os.Stdout, so leaving that way loses nothing.
		syscall.Exit(helper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// helper is the stand-in program. It runs before the testing package parses
// flags, so its arguments are its own.
func helper(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helper: no command")
		return 2
	}
	switch args[0] {
	case "say":
		// say <stream> <text>
		out := io.Writer(os.Stdout)
		if args[1] == "err" {
			out = os.Stderr
		}
		fmt.Fprint(out, args[2])
	case "both":
		fmt.Fprint(os.Stdout, "out")
		fmt.Fprint(os.Stderr, "err")
	case "exit":
		code, _ := strconv.Atoi(args[1])
		fmt.Fprint(os.Stderr, "the reason it failed")
		return code
	case "sleep":
		ms, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case "drip":
		// drip <times> <milliseconds between>
		times, _ := strconv.Atoi(args[1])
		gap, _ := strconv.Atoi(args[2])
		for range times {
			fmt.Fprint(os.Stdout, ".")
			time.Sleep(time.Duration(gap) * time.Millisecond)
		}
	case "quiet":
		// quiet <milliseconds>: say one thing, then go silent. It is the shape
		// an idle timeout exists for -- a program that started, worked, and
		// stopped answering.
		ms, _ := strconv.Atoi(args[1])
		fmt.Fprint(os.Stdout, ".")
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case "env":
		fmt.Fprint(os.Stdout, os.Getenv(args[1]))
	case "cwd":
		dir, _ := os.Getwd()
		fmt.Fprint(os.Stdout, dir)
	case "cat":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "spew":
		n, _ := strconv.Atoi(args[1])
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", n)))
	default:
		fmt.Fprintln(os.Stderr, "helper: unknown command "+args[0])
		return 2
	}
	return 0
}

// helperCommand is a PendingProcess that runs the stand-in program.
//
// Every pending process needs a factory, because the factory is what holds the
// fakes and the recording; the ones here get a fresh one, which fakes nothing,
// so the command really runs.
func helperCommand(args ...string) *PendingProcess {
	return helperCommandOn(NewFactory(), args...)
}

// helperCommandOn is helperCommand on a factory the test already holds, for the
// tests where the factory is the thing under test.
func helperCommandOn(factory *Factory, args ...string) *PendingProcess {
	return factory.NewPendingProcess().
		Command(append([]string{os.Args[0]}, args...)...).
		Env(map[string]string{helperEnv: "1"})
}

func TestRunCapturesOutput(t *testing.T) {
	t.Parallel()

	c := helperCommand("both")
	line := c.String()

	res, err := c.Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "out" || res.ErrorOutput() != "err" {
		t.Fatalf("streams: stdout %q, stderr %q", res.Output(), res.ErrorOutput())
	}
	if res.ExitCode() != 0 {
		t.Fatalf("exit code %d, want 0", res.ExitCode())
	}
	if !res.Successful() || res.Failed() {
		t.Fatalf("a command that exited 0 reports Successful=%t, Failed=%t", res.Successful(), res.Failed())
	}
	if res.Command() != line {
		t.Fatalf("command %q, want the line that ran, %q", res.Command(), line)
	}
}

func TestRunReportsAFailedExitAsAResultAndNotAsAnError(t *testing.T) {
	t.Parallel()

	res, err := helperCommand("exit", "3").Run(t.Context(), nil, nil)

	// A command that exits non-zero is not an error, and that is the behaviour
	// under test rather than a shortcut: Laravel's run() does not throw for it,
	// failed() reports it, and Throw is what raises it.
	if err != nil {
		t.Fatalf("a command that exited 3 came back as an error: %v", err)
	}
	if res.ExitCode() != 3 {
		t.Fatalf("exit code %d, want 3", res.ExitCode())
	}
	if !res.Failed() {
		t.Fatal("a command that exited 3 reports itself as successful")
	}
	if res.ErrorOutput() != "the reason it failed" {
		t.Fatalf("error output %q, want what the program wrote", res.ErrorOutput())
	}
	if !res.SeeInErrorOutput("reason") {
		t.Fatalf("SeeInErrorOutput found nothing in %q", res.ErrorOutput())
	}

	// The result is what carries the failure, and Throw is the one place it
	// becomes an error -- with what the program said still in the message.
	same, err := res.Throw(nil)
	var exception *ProcessFailedException
	if !errors.As(err, &exception) {
		t.Fatalf("error %v (%T), want a *ProcessFailedException", err, err)
	}
	if same != res {
		t.Fatal("Throw handed back a result that is not the one it was called on")
	}
	if exception.Code != 3 {
		t.Fatalf("exception code %d, want the exit code 3", exception.Code)
	}
	if !strings.Contains(exception.Error(), "the reason it failed") {
		t.Fatalf("message %q does not repeat what the program wrote", exception)
	}
}

func TestProcessFailedExceptionSaysTheCommandTheCodeAndBothStreams(t *testing.T) {
	t.Parallel()

	result := NewFakeProcessResult("git push", 128, "everything up to date", "no such remote")
	exception := NewProcessFailedException(result)

	for _, want := range []string{
		`The command "git push" failed.`,
		"Exit Code: 128",
		"Output:",
		"everything up to date",
		"Error Output:",
		"no such remote",
	} {
		if !strings.Contains(exception.Error(), want) {
			t.Errorf("message does not contain %q:\n%s", want, exception.Error())
		}
	}
	// The result is attached whole, so a caller that wants the output does not
	// have to take it back out of the message.
	if exception.Result != result {
		t.Fatal("the exception does not carry the result it was built from")
	}

	// A failure with no status is still a failure, and 0 would say the opposite.
	if code := NewProcessFailedException(NewFakeProcessResult("hush", 0, "", "")).Code; code != 1 {
		t.Fatalf("code %d for a failure with no status, want 1", code)
	}
}

func TestRunHonoursThePathItWasGiven(t *testing.T) {
	t.Parallel()

	// EvalSymlinks because a temporary directory is under a symlink on macOS,
	// and the program reports where it actually is.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	res, err := helperCommand("cwd").Path(dir).Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != dir {
		t.Fatalf("ran in %q, want %q", res.Output(), dir)
	}
}

// No t.Parallel here or in the test below: t.Setenv changes this process's own
// environment, which is exactly what the command under test inherits.
func TestEnvIsAddedToTheOneThisProcessHas(t *testing.T) {
	t.Setenv("HESAPE_PROCESS_INHERITED", "from the parent")

	c := helperCommand("env", "HESAPE_PROCESS_INHERITED").
		Env(map[string]string{helperEnv: "1", "HESAPE_PROCESS_EXTRA": "added"})

	res, err := c.Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "from the parent" {
		t.Fatalf("inherited variable came through as %q", res.Output())
	}

	// The same pending process with another command: what Run is given replaces
	// what Command set.
	res, err = c.Run(t.Context(), []string{os.Args[0], "env", "HESAPE_PROCESS_EXTRA"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "added" {
		t.Fatalf("added variable came through as %q", res.Output())
	}
}

func TestEnvOverridesAVariableThatIsAlreadySet(t *testing.T) {
	t.Setenv("HESAPE_PROCESS_OVERRIDDEN", "the old value")

	res, err := helperCommand("env", "HESAPE_PROCESS_OVERRIDDEN").
		Env(map[string]string{helperEnv: "1", "HESAPE_PROCESS_OVERRIDDEN": "the new one"}).
		Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the new one" {
		t.Fatalf("variable came through as %q", res.Output())
	}
}

func TestEnvRefusesANameThatIsNotOne(t *testing.T) {
	t.Parallel()

	c := helperCommand("cwd").Env(map[string]string{helperEnv: "1", "NOT=A=NAME": "x"})
	if _, err := c.Run(t.Context(), nil, nil); err == nil {
		t.Fatal("a name with an = in it was accepted, and it would have reached the program as a different variable")
	}
}

func TestStdinIsWrittenAndThenClosed(t *testing.T) {
	t.Parallel()

	const written = "what the program reads"

	res, err := helperCommand("cat").Input(written).Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != written {
		t.Fatalf("read back %q", res.Output())
	}
}

func TestEmptyStdinDoesNotWaitForATerminal(t *testing.T) {
	t.Parallel()

	// No Input at all: the program must see end of input rather than block.
	done := make(chan ProcessResult, 1)
	go func() {
		res, _ := helperCommand("cat").Run(context.Background(), nil, nil)
		done <- res
	}()
	select {
	case res := <-done:
		if res.Output() != "" {
			t.Fatalf("read %q from an input that should be empty", res.Output())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a command with no Input waited for input")
	}
}

func TestTimeoutKillsAndSaysSo(t *testing.T) {
	t.Parallel()

	limit := 50 * time.Millisecond

	start := time.Now()
	_, err := helperCommand("sleep", "10000").Timeout(limit).Run(t.Context(), nil, nil)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("waited %s for a 50ms timeout", took)
	}

	var timeout *ProcessTimedOutException
	if !errors.As(err, &timeout) {
		t.Fatalf("error %v (%T), want a *ProcessTimedOutException", err, err)
	}
	if timeout.Idle {
		t.Fatal("a total timeout was reported as silence")
	}
	if timeout.Timeout != limit {
		t.Fatalf("reported %s, want %s", timeout.Timeout, limit)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("a timeout that does not unwrap to context.DeadlineExceeded")
	}
}

func TestIdleTimeoutKillsAProgramThatWentQuiet(t *testing.T) {
	t.Parallel()

	limit := 300 * time.Millisecond

	res, err := helperCommand("quiet", "60000").IdleTimeout(limit).Run(t.Context(), nil, nil)

	var timeout *ProcessTimedOutException
	if !errors.As(err, &timeout) {
		t.Fatalf("error %v (%T), want a *ProcessTimedOutException", err, err)
	}
	if !timeout.Idle {
		t.Fatal("silence was reported as a total timeout, which tells the person to raise the wrong limit")
	}
	if timeout.Timeout != limit {
		t.Fatalf("reported %s, want the idle limit %s", timeout.Timeout, limit)
	}
	if !strings.Contains(timeout.Error(), "idle timeout") {
		t.Fatalf("message %q does not say why", timeout)
	}
	// What it printed before going quiet: proof it was killed for stopping and
	// not for being slow to start.
	if res.Output() != "." {
		t.Fatalf("output %q, want the one chunk it produced before the silence", res.Output())
	}
}

func TestIdleTimeoutLeavesAProgramThatKeepsPrinting(t *testing.T) {
	t.Parallel()

	// Twenty chunks, 50ms apart: a second in total, well past an idle limit of
	// 400ms, and never an eighth of it in silence.
	res, err := helperCommand("drip", "20", "50").
		IdleTimeout(400*time.Millisecond).
		Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("a program that reported progress the whole time was killed: %v", err)
	}
	if len(res.Output()) != 20 {
		t.Fatalf("kept %d chunks of output", len(res.Output()))
	}
}

func TestALoudProgramsOutputIsKeptWhole(t *testing.T) {
	t.Parallel()

	// The package used to cap what it kept and flag the result as truncated.
	// Illuminate does neither, and ADR 0044 makes the surface Illuminate's, so
	// this is the test that says what happens instead: everything is kept, and
	// the handler is handed everything too.
	var mu sync.Mutex
	var heard int
	res, err := helperCommand("spew", "5000").Run(t.Context(), nil, func(_ Stream, buffer string) {
		mu.Lock()
		heard += len(buffer)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("a loud program was turned into a failure: %v", err)
	}
	if len(res.Output()) != 5000 {
		t.Fatalf("kept %d bytes of the 5000 the program printed", len(res.Output()))
	}
	mu.Lock()
	defer mu.Unlock()
	if heard != 5000 {
		t.Fatalf("the handler heard %d bytes of the 5000 the program printed", heard)
	}
}

func TestTheOutputHandlerSaysWhichStream(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	got := map[Stream]string{}

	_, err := helperCommand("both").Run(t.Context(), nil, func(stream Stream, buffer string) {
		mu.Lock()
		got[stream] += buffer
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got[Out] != "out" || got[Err] != "err" {
		t.Fatalf("chunks arrived as %v", got)
	}
}

func TestQuietlyKeepsNothingButStillHandsOverWhatWasPrinted(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	got := map[Stream]string{}

	res, err := helperCommand("both").Quietly().Run(t.Context(), nil, func(stream Stream, buffer string) {
		mu.Lock()
		got[stream] += buffer
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "" || res.ErrorOutput() != "" {
		t.Fatalf("a quiet command kept stdout %q and stderr %q", res.Output(), res.ErrorOutput())
	}
	mu.Lock()
	defer mu.Unlock()
	if got[Out] != "out" || got[Err] != "err" {
		t.Fatalf("the handler heard %v; quietly is about what is kept, not about what is read", got)
	}
}

func TestCancellingTheContextIsTheCallersCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	// Limits of this package's own are set too, to prove which one is reported:
	// the caller gave up first, so neither of these is the answer.
	c := helperCommand("sleep", "10000").Timeout(time.Minute).IdleTimeout(time.Minute)

	invoked, err := c.Start(ctx, nil, nil)
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	cancel()
	_, err = invoked.Wait(nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v (%T), want context.Canceled", err, err)
	}
	var timeout *ProcessTimedOutException
	if errors.As(err, &timeout) {
		t.Fatal("a cancelled command was reported as a timeout")
	}
}

func TestUnknownProgramNamesItself(t *testing.T) {
	t.Parallel()

	_, err := NewFactory().Run(t.Context(), []string{"hesape-no-such-program"}, nil)
	if err == nil {
		t.Fatal("a program that does not exist ran")
	}
	if !strings.Contains(err.Error(), "hesape-no-such-program") {
		t.Fatalf("message %q does not name the program", err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error %v does not unwrap to exec.ErrNotFound", err)
	}
}

func TestCommandWithNoNameIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := NewFactory().Run(t.Context(), nil, nil); err == nil {
		t.Fatal("a command with no name was accepted")
	}
	if _, err := NewFactory().Run(t.Context(), []string{"   "}, nil); err == nil {
		t.Fatal("a command whose name is whitespace was accepted")
	}
}

func TestStartExposesTheProcessAndWaitIsRepeatable(t *testing.T) {
	t.Parallel()

	invoked, err := helperCommand("sleep", "50").Start(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if invoked.ID() <= 0 {
		t.Fatalf("pid %d", invoked.ID())
	}
	if !invoked.Running() {
		t.Fatal("a command that was just started is not running")
	}

	first, firstErr := invoked.Wait(nil)
	second, secondErr := invoked.Wait(nil)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("Wait: %v / %v", firstErr, secondErr)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two Waits, two answers: %+v and %+v", first, second)
	}
	if invoked.Running() {
		t.Fatal("a command that was waited on is still reported as running")
	}
}

func TestSignalReachesTheProgram(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows delivers no signal but Kill, and killing is what cancelling the context already does")
	}

	invoked, err := helperCommand("sleep", "10000").Start(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := invoked.Signal(os.Kill); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	// A killed program is a failed result and not an error, for the reason a
	// non-zero exit is one: the failure lives on the result, and Throw is what
	// raises it.
	res, err := invoked.Wait(nil)
	if err != nil {
		t.Fatalf("waiting on a killed program: %v", err)
	}
	if !res.Failed() {
		t.Fatal("a killed program came back as a success")
	}
	if res.ExitCode() != -1 {
		t.Fatalf("exit code %d, want -1 for a program that was killed", res.ExitCode())
	}
	if err := invoked.Signal(os.Kill); err == nil {
		t.Fatal("signalling a program that has already been reaped was accepted")
	}
}

func TestTheCommandLineIsRenderedTheWayAPersonWouldTypeIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		command []string
		want    string
	}{
		{[]string{"go", "build", "./..."}, "go build ./..."},
		{[]string{"tailwindcss", "--input", "-"}, "tailwindcss --input -"},
		{[]string{"git", "commit", "-m", "two words"}, `git commit -m "two words"`},
		{[]string{"sh", ""}, `sh ""`},
		{[]string{"go"}, "go"},
	}
	for _, c := range cases {
		if got := NewFactory().NewPendingProcess().Command(c.command...).String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestNilContextIsRefused(t *testing.T) {
	t.Parallel()

	// A nil context is what the test is about: passing one is a mistake, and the
	// mistake has to come back as an error rather than as a panic from inside
	// os/exec, where it names neither the command nor the caller.
	if _, err := helperCommand("cwd").Start(nil, nil, nil); err == nil { //nolint:staticcheck
		t.Fatal("a nil context was accepted, and it would have panicked somewhere further in")
	}
}

func TestAFakeAnswersTheCommandsItsPatternMatches(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "git *", Result: "nothing to commit"})

	res, err := factory.Run(t.Context(), []string{"git", "status"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// A faked string is normalised the way a program's output ends: exactly one
	// trailing newline, however the test wrote it.
	if res.Output() != "nothing to commit\n" {
		t.Fatalf("output %q, want the faked one", res.Output())
	}
	if res.Command() != "git status" {
		t.Fatalf("command %q, want the line that was asked for", res.Command())
	}
	if !res.Successful() {
		t.Fatalf("a fake with no exit code came back failed, exit code %d", res.ExitCode())
	}
	factory.AssertRan(t, "git status")

	// A handler may be a function of the process, which is how one fake answers
	// differently for the commands it matched.
	byArgument := NewFactory()
	byArgument.Fake(FakeHandler{Command: "git *", Result: func(p *PendingProcess) any {
		return "the fake saw " + p.String()
	}})
	res, err = byArgument.Run(t.Context(), []string{"git", "fetch"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the fake saw git fetch\n" {
		t.Fatalf("output %q; the handler was not handed the process it answered for", res.Output())
	}
}

func TestTheFirstHandlerThatMatchesIsTheOneThatAnswers(t *testing.T) {
	t.Parallel()

	specificFirst := NewFactory()
	specificFirst.Fake(
		FakeHandler{Command: "git *", Result: "the specific one"},
		FakeHandler{Command: "*", Result: "the catch-all"},
	)

	res, err := specificFirst.Run(t.Context(), []string{"git", "status"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the specific one\n" {
		t.Fatalf("output %q, want the handler registered first", res.Output())
	}
	res, err = specificFirst.Run(t.Context(), []string{"hg", "status"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the catch-all\n" {
		t.Fatalf("output %q, want the catch-all for a command the first pattern misses", res.Output())
	}

	// The other order, which is the mistake people make: a "*" registered first
	// swallows every pattern after it.
	catchAllFirst := NewFactory()
	catchAllFirst.Fake(
		FakeHandler{Command: "*", Result: "the catch-all"},
		FakeHandler{Command: "git *", Result: "the specific one"},
	)
	res, err = catchAllFirst.Run(t.Context(), []string{"git", "status"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the catch-all\n" {
		t.Fatalf("output %q; a \"*\" registered first is supposed to win", res.Output())
	}
}

func TestFakingWithNoHandlersAnswersEverythingWithNothing(t *testing.T) {
	t.Parallel()

	factory := NewFactory().Fake()

	res, err := factory.Run(t.Context(), []string{"rm", "-rf", "/"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "" || res.ErrorOutput() != "" {
		t.Fatalf("output %q and error output %q, want nothing from a blank fake", res.Output(), res.ErrorOutput())
	}
	if !res.Successful() {
		t.Fatalf("a blank fake came back failed, exit code %d", res.ExitCode())
	}
	if !factory.IsRecording() {
		t.Fatal("a factory that has been faked does not report itself as recording")
	}
	factory.AssertRan(t, "rm *")
}

func TestAStrayCommandIsRefusedInsteadOfRun(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "git *", Result: "ok"})
	factory.PreventStrayProcesses()

	if !factory.PreventingStrayProcesses() {
		t.Fatal("PreventStrayProcesses was called and the factory does not report it")
	}

	_, err := helperCommandOn(factory, "say", "out", "this must not run").Run(t.Context(), nil, nil)
	var stray *StrayProcessError
	if !errors.As(err, &stray) {
		t.Fatalf("error %v (%T), want a *StrayProcessError", err, err)
	}
	if !strings.Contains(stray.Error(), "without a matching fake") {
		t.Fatalf("message %q does not say what went wrong", stray)
	}
	if !strings.Contains(stray.Error(), "this must not run") {
		t.Fatalf("message %q does not name the command that had no fake", stray)
	}
	factory.AssertNothingRan(t)

	// And with the guard off the same command runs for real, which is what says
	// the guard is what stopped it and not the fake.
	factory.AllowStrayProcesses()
	res, err := helperCommandOn(factory, "say", "out", "and now it does").Run(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("Run with stray processes allowed: %v", err)
	}
	if res.Output() != "and now it does" {
		t.Fatalf("output %q, want what the program printed", res.Output())
	}
}

func TestTheAssertionsSeeEveryCommandThatWasFaked(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "*", Result: "ok"})
	factory.AssertNothingRan(t)

	for range 2 {
		if _, err := factory.Run(t.Context(), []string{"git", "fetch"}, nil); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if _, err := factory.Run(t.Context(), []string{"go", "build", "./..."}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	factory.AssertRan(t, "git fetch")
	factory.AssertRan(t, "go *")
	factory.AssertRanTimes(t, "git fetch", 2)
	factory.AssertRanTimes(t, "*", 3)
	factory.AssertNotRan(t, "rm *")
	factory.AssertDidntRun(t, "npm install")

	want := []string{"git fetch", "git fetch", "go build ./..."}
	if got := factory.Recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded %q, want %q -- in the order they ran", got, want)
	}
}

func TestTheCountBehindTheAssertionsMatchesPatternsLikeAFakeDoes(t *testing.T) {
	t.Parallel()

	// countRan and ranReport are what the four assertions are made of, and they
	// are exercised here rather than through the assertions themselves: those
	// take a *testing.T and report a mismatch by failing this test, so the only
	// half of them a test can reach is the half that passes.
	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "*", Result: ""})

	if got := factory.ranReport(); got != "no process ran" {
		t.Fatalf("report before anything ran is %q", got)
	}

	for _, command := range [][]string{{"git", "status"}, {"git", "push", "origin", "main"}} {
		if _, err := factory.Run(t.Context(), command, nil); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}

	for _, c := range []struct {
		pattern string
		want    int
	}{
		{"git *", 2},
		{"git status", 1},
		{"*", 2},
		{"", 2},
		{"hg *", 0},
		{"git", 0},
	} {
		if got := factory.countRan(c.pattern); got != c.want {
			t.Errorf("countRan(%q) = %d, want %d", c.pattern, got, c.want)
		}
	}

	report := factory.ranReport()
	if !strings.HasPrefix(report, "2 ran:") {
		t.Fatalf("report %q does not open with how many ran", report)
	}
	for _, want := range []string{"git status", "git push origin main"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not name %q:\n%s", want, report)
		}
	}
}

func TestARecordedCommandReadsBackTheWayTheFakePatternSawIt(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: `git commit -m "two words"`, Result: "committed"})

	res, err := factory.Run(t.Context(), []string{"git", "commit", "-m", "two words"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.SeeInOutput("committed") {
		t.Fatalf("output %q: a pattern with a quoted argument did not answer", res.Output())
	}
	// The same string on both sides. A pattern that a fake answered and that an
	// assertion then says never ran is the one thing matchesCommand exists to
	// prevent.
	factory.AssertRan(t, `git commit -m "two words"`)
	if got := factory.Recorded(); len(got) != 1 || got[0] != res.Command() {
		t.Fatalf("recorded %q, want the command line the result reports, %q", got, res.Command())
	}
}

func TestASequenceAnswersDifferentlyEachTimeTheCommandRuns(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "git pull", Result: factory.Sequence(
		NewFakeProcessResult("", 1, "", "not a repository"),
		"already up to date",
	)})

	first, err := factory.Run(t.Context(), []string{"git", "pull"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if first.ExitCode() != 1 || first.ErrorOutput() != "not a repository\n" {
		t.Fatalf("first run: exit code %d, error output %q", first.ExitCode(), first.ErrorOutput())
	}

	second, err := factory.Run(t.Context(), []string{"git", "pull"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !second.Successful() || second.Output() != "already up to date\n" {
		t.Fatalf("second run: exit code %d, output %q", second.ExitCode(), second.Output())
	}

	// Run out, and the next command is an error: a sequence that quietly repeats
	// its last answer is a test that passes for a reason nobody wrote down.
	if _, err := factory.Run(t.Context(), []string{"git", "pull"}, nil); !errors.Is(err, ErrEmptyProcessSequence) {
		t.Fatalf("error %v, want the sequence to say it ran out", err)
	}
	factory.AssertRanTimes(t, "git pull", 2)
}

func TestASequenceThatRanOutCanBeToldToAnswerAnyway(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	sequence := factory.Sequence("the only answer").DontFailWhenEmpty()
	factory.Fake(FakeHandler{Command: "date", Result: sequence})

	if _, err := factory.Run(t.Context(), []string{"date"}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sequence.IsEmpty() {
		t.Fatal("a sequence of one still reports something left after one run")
	}

	res, err := factory.Run(t.Context(), []string{"date"}, nil)
	if err != nil {
		t.Fatalf("a sequence told not to fail when empty failed anyway: %v", err)
	}
	if !res.Successful() || res.Output() != "" {
		t.Fatalf("exit code %d and output %q, want an empty success", res.ExitCode(), res.Output())
	}

	// WhenEmpty says what to answer with instead of nothing.
	named := NewFactory()
	named.Fake(FakeHandler{Command: "date", Result: named.Sequence().WhenEmpty("the standing answer")})
	res, err = named.Run(t.Context(), []string{"date"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output() != "the standing answer\n" {
		t.Fatalf("output %q, want what WhenEmpty was given", res.Output())
	}
}

func TestResultBuildsTheOutcomeAFakeAnswersWith(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(
		FakeHandler{Command: "go test*", Result: factory.Result("", "FAIL\tprocess", 1)},
		FakeHandler{Command: "go *", Result: factory.Result("all good", "", 0)},
	)

	failed, err := factory.Run(t.Context(), []string{"go", "test", "./..."}, nil)
	if err != nil {
		t.Fatalf("a faked non-zero exit came back as an error: %v", err)
	}
	if !failed.Failed() || failed.ExitCode() != 1 {
		t.Fatalf("exit code %d, want the faked 1", failed.ExitCode())
	}
	if !failed.SeeInErrorOutput("FAIL") {
		t.Fatalf("error output %q does not carry what the fake was given", failed.ErrorOutput())
	}
	if _, err := failed.Throw(nil); err == nil {
		t.Fatal("Throw on a faked failure returned no error")
	}

	ok, err := factory.Run(t.Context(), []string{"go", "vet", "./..."}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ok.SeeInOutput("all good") {
		t.Fatalf("output %q, want the second handler's", ok.Output())
	}
	if _, err := ok.Throw(nil); err != nil {
		t.Fatalf("Throw on a successful result returned %v", err)
	}

	// One result registered once is handed to every command that matches, and
	// each of them reports its own command line rather than the first one's.
	if failed.Command() != "go test ./..." || ok.Command() != "go vet ./..." {
		t.Fatalf("commands %q and %q, want each result to name the command it answered", failed.Command(), ok.Command())
	}
}

func TestDescribeReplaysAProcessThatIsWatchedWhileItRuns(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	factory.Fake(FakeHandler{Command: "deploy *", Result: factory.Describe().
		ID(4242).
		Output("uploading").
		ErrorOutput("a slow mirror").
		Output("done").
		RunsFor(2).
		ExitCode(1)})

	var seen []string
	invoked, err := factory.Start(t.Context(), []string{"deploy", "production"}, func(stream Stream, buffer string) {
		seen = append(seen, string(stream)+": "+strings.TrimSuffix(buffer, "\n"))
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if invoked.ID() != 4242 {
		t.Fatalf("id %d, want the described 4242", invoked.ID())
	}

	rounds := 0
	for invoked.Running() {
		rounds++
		if rounds > 10 {
			t.Fatal("a fake described as running for two rounds never stopped")
		}
	}
	if rounds != 2 {
		t.Fatalf("the watching loop went round %d times, want the described 2", rounds)
	}

	res, err := invoked.Wait(nil)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.ExitCode() != 1 || !res.Failed() {
		t.Fatalf("exit code %d, want the described 1", res.ExitCode())
	}
	if res.Output() != "uploading\ndone\n" {
		t.Fatalf("output %q, want both described lines of standard output", res.Output())
	}
	if res.ErrorOutput() != "a slow mirror\n" {
		t.Fatalf("error output %q, want the described line", res.ErrorOutput())
	}
	if res.Command() != "deploy production" {
		t.Fatalf("command %q, want the line that was started", res.Command())
	}

	// The output does not arrive on its own, because nothing is running: it
	// arrives because the loop asked, and it arrives in the order it was
	// described, across both streams.
	want := []string{"out: uploading", "err: a slow mirror", "out: done"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("the handler was handed %q, want %q", seen, want)
	}

	// A started fake is recorded when it starts, with the result it will end
	// with, so an assertion does not have to wait for it.
	factory.AssertRan(t, "deploy *")
	factory.AssertRanTimes(t, "deploy production", 1)
}
