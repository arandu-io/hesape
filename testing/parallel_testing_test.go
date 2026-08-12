package testing

import (
	"fmt"
	"sync"
	gotesting "testing"
)

// parallelTestingFixed is a token resolver that always answers the same token,
// which is what Illuminate\Testing\Concerns\RunsInParallel installs for each
// worker before it fires the process callbacks.
func parallelTestingFixed(token string) func() (string, bool) {
	return func() (string, bool) { return token, true }
}

// parallelTestingAbsent is a token resolver that answers no token, which is
// what a run that is not parallel looks like.
func parallelTestingAbsent() (string, bool) { return "", false }

func TestParallelTestingCallSetUpProcessCallbacksFiresInOrderWithTheToken(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("3"))

	var calls []string
	p.SetUpProcess(func(token string) { calls = append(calls, "first:"+token) })
	p.SetUpProcess(func(token string) { calls = append(calls, "second:"+token) })

	p.CallSetUpProcessCallbacks()

	want := []string{"first:3", "second:3"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("expected the setUp process callbacks to run as %v, got %v", want, calls)
	}
}

func TestParallelTestingCallSetUpTestCaseCallbacksFiresInOrderWithTheTestCase(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("7"))

	var calls []string
	var cases []T
	p.SetUpTestCase(func(testCase T, token string) {
		calls = append(calls, "first:"+token)
		cases = append(cases, testCase)
	})
	p.SetUpTestCase(func(testCase T, token string) {
		calls = append(calls, "second:"+token)
		cases = append(cases, testCase)
	})

	p.CallSetUpTestCaseCallbacks(t)

	want := []string{"first:7", "second:7"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("expected the setUp test case callbacks to run as %v, got %v", want, calls)
	}
	for i, testCase := range cases {
		if testCase != T(t) {
			t.Fatalf("expected callback %d to receive the test case that was passed in, got %#v", i, testCase)
		}
	}
}

func TestParallelTestingCallSetUpTestDatabaseCallbacksFiresWithTheDatabaseAndToken(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("2"))

	var calls []string
	p.SetUpTestDatabase(func(database string, token string) {
		calls = append(calls, "first:"+database+":"+token)
	})
	p.SetUpTestDatabase(func(database string, token string) {
		calls = append(calls, "second:"+database+":"+token)
	})

	p.CallSetUpTestDatabaseCallbacks("hesape_test_2")

	want := []string{"first:hesape_test_2:2", "second:hesape_test_2:2"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("expected the setUp test database callbacks to run as %v, got %v", want, calls)
	}
}

func TestParallelTestingTearDownCallbacksAreASeparateList(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("1"))

	var calls []string
	p.SetUpProcess(func(token string) { calls = append(calls, "setUpProcess") })
	p.SetUpTestCase(func(testCase T, token string) { calls = append(calls, "setUpTestCase") })
	p.TearDownProcess(func(token string) { calls = append(calls, "tearDownProcess:"+token) })
	p.TearDownTestCase(func(testCase T, token string) { calls = append(calls, "tearDownTestCase:"+token) })

	p.CallTearDownTestCaseCallbacks(t)
	p.CallTearDownProcessCallbacks()

	want := []string{"tearDownTestCase:1", "tearDownProcess:1"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("expected only the tearDown callbacks to run, as %v, got %v", want, calls)
	}
}

func TestParallelTestingCallbacksDoNotFireWithoutAToken(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingAbsent)

	var calls []string
	p.SetUpProcess(func(token string) { calls = append(calls, "setUpProcess") })
	p.SetUpTestCase(func(testCase T, token string) { calls = append(calls, "setUpTestCase") })
	p.SetUpTestDatabase(func(database string, token string) { calls = append(calls, "setUpTestDatabase") })
	p.TearDownProcess(func(token string) { calls = append(calls, "tearDownProcess") })
	p.TearDownTestCase(func(testCase T, token string) { calls = append(calls, "tearDownTestCase") })

	p.CallSetUpProcessCallbacks()
	p.CallSetUpTestCaseCallbacks(t)
	p.CallSetUpTestDatabaseCallbacks("hesape_test")
	p.CallTearDownTestCaseCallbacks(t)
	p.CallTearDownProcessCallbacks()

	if len(calls) != 0 {
		t.Fatalf("expected no callback to run outside a parallel run, got %v", calls)
	}
}

func TestParallelTestingCallbacksWithNoneRegisteredAreHarmless(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("1"))

	p.CallSetUpProcessCallbacks()
	p.CallSetUpTestCaseCallbacks(t)
	p.CallSetUpTestDatabaseCallbacks("hesape_test_1")
	p.CallTearDownTestCaseCallbacks(t)
	p.CallTearDownProcessCallbacks()
}

func TestParallelTestingResolveTokenUsing(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()

	p.ResolveTokenUsing(parallelTestingFixed("9"))
	token, ok := p.Token()
	if !ok || token != "9" {
		t.Fatalf("expected the installed resolver to answer the token \"9\", got %q, resolved %v", token, ok)
	}

	p.ResolveTokenUsing(func() (string, bool) { return "", true })
	token, ok = p.Token()
	if ok || token != "" {
		t.Fatalf("expected an empty token to count as no token, got %q, resolved %v", token, ok)
	}

	p.ResolveTokenUsing(nil)
	if _, ok := p.Token(); ok {
		t.Fatalf("expected no token from the environment, since %s is not set for this test binary", parallelTestingTokenEnv)
	}
}

func TestParallelTestingOption(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()

	asked := ""
	p.ResolveOptionsUsing(func(option string) (any, bool) {
		asked = option
		if option == "recreate databases" {
			return true, true
		}
		return nil, false
	})

	value, ok := p.Option("recreate databases")
	if !ok || value != true {
		t.Fatalf("expected the installed resolver to answer true for \"recreate databases\", got %v, resolved %v", value, ok)
	}
	if asked != "recreate databases" {
		t.Fatalf("expected the resolver to be asked for \"recreate databases\", got %q", asked)
	}

	if value, ok := p.Option("drop databases"); ok || value != nil {
		t.Fatalf("expected no value for an option the resolver does not know, got %v, resolved %v", value, ok)
	}

	p.ResolveOptionsUsing(nil)
	if value, ok := p.Option("recreate databases"); ok || value != nil {
		t.Fatalf("expected no value from the environment, since %sRECREATE_DATABASES is not set for this test binary, got %v",
			parallelTestingOptionPrefix, value)
	}
}

func TestParallelTestingIsSafeForConcurrentUse(t *gotesting.T) {
	t.Parallel()

	p := NewParallelTesting()
	p.ResolveTokenUsing(parallelTestingFixed("4"))

	var mu sync.Mutex
	fired := 0

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.SetUpProcess(func(token string) {
				mu.Lock()
				fired++
				mu.Unlock()
			})
			p.CallSetUpProcessCallbacks()
			p.CallTearDownProcessCallbacks()
			p.Token()
			p.Option("recreate databases")
		}()
	}
	wg.Wait()

	p.CallSetUpProcessCallbacks()

	mu.Lock()
	defer mu.Unlock()
	if fired < 16 {
		t.Fatalf("expected at least the 16 registered callbacks to have fired, got %d", fired)
	}
}
