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

// sys is what these tests exercise, held as a Runner rather than a System: it
// is the interface the rest of the collection depends on, and a method that
// only exists on the concrete type is a method nothing can call.
var sys Runner = System{}

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

// helperCommand is a Command that runs the stand-in program.
func helperCommand(args ...string) Command {
	return Command{
		Name: os.Args[0],
		Args: args,
		Env:  map[string]string{helperEnv: "1"},
	}
}

func TestRunCapturesOutput(t *testing.T) {
	t.Parallel()

	res, err := sys.Run(t.Context(), helperCommand("both"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "out" || res.Stderr != "err" {
		t.Fatalf("streams: stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d, want 0", res.ExitCode)
	}
	if res.Duration <= 0 {
		t.Fatalf("duration %s, want something positive", res.Duration)
	}
	if res.Truncated {
		t.Fatal("nothing was capped, so nothing should be reported as truncated")
	}
	if res.Name != os.Args[0] {
		t.Fatalf("name %q, want the program that ran", res.Name)
	}
}

func TestRunReportsExitStatusWithWhatTheProgramSaid(t *testing.T) {
	t.Parallel()

	res, err := sys.Run(t.Context(), helperCommand("exit", "3"))

	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error %v (%T), want an *ExitError", err, err)
	}
	if exit.ExitCode != 3 {
		t.Fatalf("exit code %d, want 3", exit.ExitCode)
	}
	if !strings.Contains(exit.Error(), "the reason it failed") {
		t.Fatalf("message %q does not repeat what the program wrote", exit)
	}
	// The Result is filled anyway: a caller that wanted the output should not
	// have to take it back out of the error.
	if res.ExitCode != 3 || res.Stderr != "the reason it failed" {
		t.Fatalf("result %+v does not describe the run", res)
	}
}

func TestExitErrorMessageIsCutButTheFieldIsNot(t *testing.T) {
	t.Parallel()

	said := strings.Repeat("z", excerpt*2)
	err := &ExitError{Name: "loud", ExitCode: 1, Stderr: said}
	if len(err.Error()) > excerpt+64 {
		t.Fatalf("message is %d bytes, want an excerpt", len(err.Error()))
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Fatalf("message %q does not say it was cut", err.Error()[:32])
	}
	if err.Stderr != said {
		t.Fatal("the field lost bytes the message was only supposed to omit")
	}
}

func TestRunHonoursDir(t *testing.T) {
	t.Parallel()

	// EvalSymlinks because a temporary directory is under a symlink on macOS,
	// and the program reports where it actually is.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	c := helperCommand("cwd")
	c.Dir = dir
	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != dir {
		t.Fatalf("ran in %q, want %q", res.Stdout, dir)
	}
}

// No t.Parallel here or in the test below: t.Setenv changes this process's own
// environment, which is exactly what the command under test inherits.
func TestEnvIsAddedToTheOneThisProcessHas(t *testing.T) {
	t.Setenv("HESAPE_PROCESS_INHERITED", "from the parent")

	c := helperCommand("env", "HESAPE_PROCESS_INHERITED")
	c.Env["HESAPE_PROCESS_EXTRA"] = "added"
	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "from the parent" {
		t.Fatalf("inherited variable came through as %q", res.Stdout)
	}

	c.Args = []string{"env", "HESAPE_PROCESS_EXTRA"}
	if res, err = sys.Run(t.Context(), c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "added" {
		t.Fatalf("added variable came through as %q", res.Stdout)
	}
}

func TestEnvOverridesAVariableThatIsAlreadySet(t *testing.T) {
	t.Setenv("HESAPE_PROCESS_OVERRIDDEN", "the old value")

	c := helperCommand("env", "HESAPE_PROCESS_OVERRIDDEN")
	c.Env["HESAPE_PROCESS_OVERRIDDEN"] = "the new one"
	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != "the new one" {
		t.Fatalf("variable came through as %q", res.Stdout)
	}
}

func TestEnvRefusesANameThatIsNotOne(t *testing.T) {
	t.Parallel()

	c := helperCommand("cwd")
	c.Env["NOT=A=NAME"] = "x"
	if _, err := sys.Run(t.Context(), c); err == nil {
		t.Fatal("a name with an = in it was accepted, and it would have reached the program as a different variable")
	}
}

func TestStdinIsWrittenAndThenClosed(t *testing.T) {
	t.Parallel()

	c := helperCommand("cat")
	c.Stdin = "what the program reads"
	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stdout != c.Stdin {
		t.Fatalf("read back %q", res.Stdout)
	}
}

func TestEmptyStdinDoesNotWaitForATerminal(t *testing.T) {
	t.Parallel()

	// No Stdin at all: the program must see end of input rather than block.
	done := make(chan Result, 1)
	go func() {
		res, _ := sys.Run(context.Background(), helperCommand("cat"))
		done <- res
	}()
	select {
	case res := <-done:
		if res.Stdout != "" {
			t.Fatalf("read %q from an input that should be empty", res.Stdout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a command with no Stdin waited for input")
	}
}

func TestTimeoutKillsAndSaysSo(t *testing.T) {
	t.Parallel()

	c := helperCommand("sleep", "10000")
	c.Timeout = 50 * time.Millisecond

	start := time.Now()
	_, err := sys.Run(t.Context(), c)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("waited %s for a 50ms timeout", took)
	}

	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("error %v (%T), want a *TimeoutError", err, err)
	}
	if timeout.Idle {
		t.Fatal("a total timeout was reported as silence")
	}
	if timeout.After != c.Timeout {
		t.Fatalf("reported %s, want %s", timeout.After, c.Timeout)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("a timeout that does not unwrap to context.DeadlineExceeded")
	}
}

func TestIdleTimeoutKillsAProgramThatWentQuiet(t *testing.T) {
	t.Parallel()

	c := helperCommand("quiet", "60000")
	c.IdleTimeout = 300 * time.Millisecond

	res, err := sys.Run(t.Context(), c)

	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("error %v (%T), want a *TimeoutError", err, err)
	}
	if !timeout.Idle {
		t.Fatal("silence was reported as a total timeout, which tells the person to raise the wrong limit")
	}
	if !strings.Contains(timeout.Error(), "no output") {
		t.Fatalf("message %q does not say why", timeout)
	}
	// What it printed before going quiet: proof it was killed for stopping and
	// not for being slow to start.
	if res.Stdout != "." {
		t.Fatalf("output %q, want the one chunk it produced before the silence", res.Stdout)
	}
}

func TestIdleTimeoutLeavesAProgramThatKeepsPrinting(t *testing.T) {
	t.Parallel()

	// Twenty chunks, 50ms apart: a second in total, well past an idle limit of
	// 400ms, and never an eighth of it in silence.
	c := helperCommand("drip", "20", "50")
	c.IdleTimeout = 400 * time.Millisecond

	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("a program that reported progress the whole time was killed: %v", err)
	}
	if len(res.Stdout) != 20 {
		t.Fatalf("kept %d chunks of output", len(res.Stdout))
	}
}

func TestMaxOutputCapsWhatIsKeptAndSaysThatItDid(t *testing.T) {
	t.Parallel()

	var seen int
	var mu sync.Mutex
	c := helperCommand("spew", "5000")
	c.MaxOutput = 100
	c.OnOutput = func(_ Stream, chunk []byte) {
		mu.Lock()
		seen += len(chunk)
		mu.Unlock()
	}

	res, err := sys.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("a loud program was turned into a failure: %v", err)
	}
	if len(res.Stdout) != 100 {
		t.Fatalf("kept %d bytes, want the cap of 100", len(res.Stdout))
	}
	if !res.Truncated {
		t.Fatal("output was cut and the Result does not say so")
	}
	mu.Lock()
	defer mu.Unlock()
	if seen != 5000 {
		t.Fatalf("OnOutput saw %d bytes; the cap is on what is kept, not on what is read", seen)
	}
}

func TestOnOutputSaysWhichStream(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	got := map[Stream]string{}
	c := helperCommand("both")
	c.OnOutput = func(stream Stream, chunk []byte) {
		mu.Lock()
		got[stream] += string(chunk)
		mu.Unlock()
	}

	if _, err := sys.Run(t.Context(), c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got[Stdout] != "out" || got[Stderr] != "err" {
		t.Fatalf("chunks arrived as %v", got)
	}
}

func TestCancellingTheContextIsTheCallersCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	c := helperCommand("sleep", "10000")
	// Limits of this package's own are set too, to prove which one is reported:
	// the caller gave up first, so neither of these is the answer.
	c.Timeout = time.Minute
	c.IdleTimeout = time.Minute

	invoked, err := sys.Start(ctx, c)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	_, err = invoked.Wait()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v (%T), want context.Canceled", err, err)
	}
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		t.Fatal("a cancelled command was reported as a timeout")
	}
}

func TestUnknownProgramNamesItself(t *testing.T) {
	t.Parallel()

	_, err := sys.Run(t.Context(), Command{Name: "hesape-no-such-program"})
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

	if _, err := sys.Run(t.Context(), Command{}); err == nil {
		t.Fatal("a command with no name was accepted")
	}
	if _, err := sys.Run(t.Context(), Command{Name: "   "}); err == nil {
		t.Fatal("a command whose name is whitespace was accepted")
	}
}

func TestStartExposesTheProcessAndWaitIsRepeatable(t *testing.T) {
	t.Parallel()

	invoked, err := sys.Start(t.Context(), helperCommand("sleep", "50"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if invoked.PID() <= 0 {
		t.Fatalf("pid %d", invoked.PID())
	}
	if !invoked.Running() {
		t.Fatal("a command that was just started is not running")
	}

	first, firstErr := invoked.Wait()
	second, secondErr := invoked.Wait()
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

	invoked, err := sys.Start(t.Context(), helperCommand("sleep", "10000"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := invoked.Signal(os.Kill); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	res, err := invoked.Wait()
	if err == nil {
		t.Fatal("a killed program came back as a success")
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit code %d, want -1 for a program that was killed", res.ExitCode)
	}
	if err := invoked.Signal(os.Kill); err == nil {
		t.Fatal("signalling a program that has already been reaped was accepted")
	}
}

func TestCommandString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		command Command
		want    string
	}{
		{Command{Name: "go", Args: []string{"build", "./..."}}, "go build ./..."},
		{Command{Name: "tailwindcss", Args: []string{"--input", "-"}}, "tailwindcss --input -"},
		{Command{Name: "git", Args: []string{"commit", "-m", "two words"}}, `git commit -m "two words"`},
		{Command{Name: "sh", Args: []string{""}}, `sh ""`},
		{Command{Name: "go"}, "go"},
	}
	for _, c := range cases {
		if got := c.command.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestNilContextIsRefused(t *testing.T) {
	t.Parallel()

	// A nil context is what the test is about: passing one is a mistake, and the
	// mistake has to come back as an error rather than as a panic from inside
	// os/exec, where it names neither the command nor the caller.
	if _, err := sys.Start(nil, helperCommand("cwd")); err == nil { //nolint:staticcheck
		t.Fatal("a nil context was accepted, and it would have panicked somewhere further in")
	}
}
