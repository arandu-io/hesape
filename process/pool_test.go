package process

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
)

// helperArgs is the command line of the stand-in program, for the pool tests
// that add processes through Pool.Command rather than through a PendingProcess.
func helperArgs(args ...string) []string {
	return append([]string{os.Args[0]}, args...)
}

// withHelperEnv puts the stand-in program's switch in the environment of a
// process a pool built, which is what makes it the helper rather than the test
// binary rerunning the suite.
func withHelperEnv(p *PendingProcess) *PendingProcess {
	return p.Env(map[string]string{helperEnv: "1"})
}

func TestConcurrentlyRunsEveryProcess(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	results, err := factory.Concurrently(context.Background(), func(pool *Pool) {
		withHelperEnv(pool.As("first").Command(helperArgs("say", "out", "one")...))
		withHelperEnv(pool.Command(helperArgs("say", "out", "two")...))
	}, nil)
	if err != nil {
		t.Fatalf("Concurrently: %v", err)
	}

	collected := results.Collect()
	if len(collected) != 2 {
		t.Fatalf("Collect: %d results, want 2", len(collected))
	}
	if got := collected[0].Output(); got != "one" {
		t.Errorf("the first result is %q, want %q", got, "one")
	}
	if got := collected[1].Output(); got != "two" {
		t.Errorf("the second result is %q, want %q", got, "two")
	}
	for i, result := range collected {
		if !result.Successful() {
			t.Errorf("result %d failed: %s", i, result.ErrorOutput())
		}
	}
}

func TestPoolKeysAnUnnamedProcessByItsPosition(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	keys := map[string]string{}

	factory := NewFactory()
	_, err := factory.Concurrently(context.Background(), func(pool *Pool) {
		withHelperEnv(pool.As("build").Command(helperArgs("say", "out", "a")...))
		withHelperEnv(pool.Command(helperArgs("say", "out", "b")...))
		withHelperEnv(pool.Command(helperArgs("say", "out", "c")...))
	}, func(stream Stream, buffer string, key string) {
		// Four programs write at once, so the handler is called from four
		// goroutines and the test holds the lock the handler contract does not.
		mu.Lock()
		defer mu.Unlock()
		keys[key] += buffer
	})
	if err != nil {
		t.Fatalf("Concurrently: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]string{"build": "a", "0": "b", "1": "c"}
	if len(keys) != len(want) {
		t.Fatalf("the handler saw %v, want %v", keys, want)
	}
	for key, buffer := range want {
		if keys[key] != buffer {
			t.Errorf("key %q carried %q, want %q", key, keys[key], buffer)
		}
	}
}

func TestPoolStartDoesNotWaitAndCountsWhatItStarted(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	started, err := factory.Pool(func(pool *Pool) {
		withHelperEnv(pool.Command(helperArgs("sleep", "50")...))
		withHelperEnv(pool.Command(helperArgs("sleep", "50")...))
	}).Start(context.Background(), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := started.Count(); got != 2 {
		t.Fatalf("Count is %d, want 2", got)
	}

	results, err := started.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := len(results.Collect()); got != 2 {
		t.Fatalf("Collect: %d results, want 2", got)
	}
	if running := started.Running(); len(running) != 0 {
		t.Errorf("Running after Wait: %d, want none", len(running))
	}
}

func TestSignalReachesTheProcessesThatAreStillRunning(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	started, err := factory.Pool(func(pool *Pool) {
		withHelperEnv(pool.Command(helperArgs("sleep", "10000")...))
		withHelperEnv(pool.Command(helperArgs("sleep", "10000")...))
	}).Start(context.Background(), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	signalled, err := started.Signal(os.Kill)
	if err != nil {
		t.Fatalf("Signal: %v", err)
	}
	if len(signalled) != 2 {
		t.Fatalf("Signal reached %d processes, want 2", len(signalled))
	}

	results, err := started.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	// A killed program is a failed result and not an error, which is the rule
	// the whole package is built on.
	for i, result := range results.Collect() {
		if result.Successful() {
			t.Errorf("result %d survived the kill", i)
		}
	}
}

func TestSignalSkipsTheProcessesThatAlreadyFinished(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	started, err := factory.Pool(func(pool *Pool) {
		withHelperEnv(pool.Command(helperArgs("say", "out", "done")...))
	}).Start(context.Background(), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := started.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	signalled, err := started.Signal(os.Kill)
	if err != nil {
		t.Fatalf("Signal: %v", err)
	}
	if len(signalled) != 0 {
		t.Errorf("Signal reached %d finished processes, want none", len(signalled))
	}
}

func TestPoolCallbackRunsAtStartAndNotAtDefinition(t *testing.T) {
	t.Parallel()

	defined := 0
	factory := NewFactory()
	pool := factory.Pool(func(pool *Pool) {
		defined++
		withHelperEnv(pool.Command(helperArgs("say", "out", "again")...))
	})
	if defined != 0 {
		t.Fatalf("the callback ran while the pool was being built")
	}

	for i := range 2 {
		results, err := pool.Run(context.Background())
		if err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		if got := len(results.Collect()); got != 1 {
			t.Fatalf("Run %d collected %d results, want 1", i, got)
		}
	}
	if defined != 2 {
		t.Errorf("the callback ran %d times, want 2", defined)
	}
}

func TestPoolReportsAProcessThatCouldNotStart(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	_, err := factory.Pool(func(pool *Pool) {
		pool.Command()
	}).Start(context.Background(), nil)
	if err == nil {
		t.Fatal("a pool holding a command with no name started")
	}
}

func TestPipeFeedsEachProcessTheOutputOfTheOneBefore(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	result, err := factory.Pipe(context.Background(), func(pipe *Pipe) {
		withHelperEnv(pipe.Command(helperArgs("say", "out", "carried")...))
		withHelperEnv(pipe.Command(helperArgs("cat")...))
	}, nil)
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if got := result.Output(); got != "carried" {
		t.Errorf("the pipe answered %q, want %q", got, "carried")
	}
}

func TestPipeStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	result, err := factory.Pipe(context.Background(), func(pipe *Pipe) {
		withHelperEnv(pipe.Command(helperArgs("exit", "3")...))
		withHelperEnv(pipe.Command(helperArgs("say", "out", "never")...))
	}, nil)
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if !result.Failed() {
		t.Fatal("the failed result did not come back")
	}
	if got := result.ExitCode(); got != 3 {
		t.Errorf("the pipe answered exit code %d, want 3", got)
	}
	if got := result.Output(); got == "never" {
		t.Error("the process after the failure ran")
	}
}

func TestPipeKeysItsOutputLikeAPool(t *testing.T) {
	t.Parallel()

	var seen []string
	factory := NewFactory()
	_, err := factory.Pipe(context.Background(), func(pipe *Pipe) {
		withHelperEnv(pipe.As("read").Command(helperArgs("say", "out", "x")...))
		withHelperEnv(pipe.Command(helperArgs("cat")...))
	}, func(stream Stream, buffer string, key string) {
		// A pipe runs one process at a time, so no lock is needed here and the
		// order the keys arrive in is the order the processes ran.
		seen = append(seen, key)
	})
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	sort.Strings(seen)
	if len(seen) != 2 || seen[0] != "0" || seen[1] != "read" {
		t.Errorf("the handler saw the keys %v, want [0 read]", seen)
	}
}

func TestPipeWithNoProcessesAnswersNothing(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	result, err := factory.Pipe(context.Background(), func(*Pipe) {}, nil)
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if result != nil {
		t.Errorf("an empty pipe answered %v, want nothing", result)
	}
}

func TestAsReplacesTheProcessUnderTheSameKey(t *testing.T) {
	t.Parallel()

	factory := NewFactory()
	results, err := factory.Concurrently(context.Background(), func(pool *Pool) {
		withHelperEnv(pool.As("only").Command(helperArgs("say", "out", "first")...))
		withHelperEnv(pool.As("only").Command(helperArgs("say", "out", "second")...))
	}, nil)
	if err != nil {
		t.Fatalf("Concurrently: %v", err)
	}

	collected := results.Collect()
	if len(collected) != 1 {
		t.Fatalf("Collect: %d results, want 1", len(collected))
	}
	if got := collected[0].Output(); got != "second" {
		t.Errorf("the surviving process answered %q, want %q", got, "second")
	}
}

func TestPoolProcessesAreFakedLikeAnyOther(t *testing.T) {
	t.Parallel()

	factory := NewFactory().Fake(FakeHandler{Command: "*", Result: "faked"})
	factory.PreventStrayProcesses()

	results, err := factory.Concurrently(context.Background(), func(pool *Pool) {
		pool.Command("git", "status")
		pool.Command("git", "log")
	}, nil)
	if err != nil {
		t.Fatalf("Concurrently: %v", err)
	}
	for i, result := range results.Collect() {
		if got := result.Output(); got != "faked\n" {
			t.Errorf("result %d answered %q, want %q", i, got, "faked\n")
		}
	}
	factory.AssertRanTimes(t, "git *", 2)
}
