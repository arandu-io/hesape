package testing

import (
	"os"
	"strings"
	"sync"
)

// The environment this package reads when no resolver was installed.
const (
	parallelTestingTokenEnv     = "TEST_TOKEN"
	parallelTestingOptionPrefix = "PARALLEL_TESTING_"
)

// ParallelTesting holds the callbacks an application registers to be run at
// the boundaries of a parallel test run -- when a worker process starts and
// ends, when a test case is set up and torn down, and when a per-worker test
// database is created.
//
// It is a plain value: build one with [NewParallelTesting], keep it where the
// test bootstrap can reach it, and pass it.
//
// Every method is safe for concurrent use. Callbacks are copied out from under
// the lock and then called with the lock released, so that a callback may
// register another one without deadlocking.
//
// The callbacks fire only while running in parallel, which here means only
// that a token resolves -- see [ParallelTesting.Token].
type ParallelTesting struct {
	mu sync.RWMutex

	optionsResolver func(option string) (any, bool)
	tokenResolver   func() (string, bool)

	setUpProcessCallbacks      []func(token string)
	setUpTestCaseCallbacks     []func(testCase T, token string)
	setUpTestDatabaseCallbacks []func(database string, token string)
	tearDownProcessCallbacks   []func(token string)
	tearDownTestCaseCallbacks  []func(testCase T, token string)
}

// NewParallelTesting returns a ParallelTesting with no callbacks and no
// resolvers installed.
func NewParallelTesting() *ParallelTesting {
	return &ParallelTesting{}
}

// ResolveOptionsUsing installs the callback [ParallelTesting.Option] asks for
// a value.
func (p *ParallelTesting) ResolveOptionsUsing(resolver func(option string) (any, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.optionsResolver = resolver
}

// ResolveTokenUsing installs the callback [ParallelTesting.Token] asks for the
// unique process token.
func (p *ParallelTesting) ResolveTokenUsing(resolver func() (string, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokenResolver = resolver
}

// SetUpProcess registers a callback to run once, at the start of each worker
// process.
func (p *ParallelTesting) SetUpProcess(callback func(token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpProcessCallbacks = append(p.setUpProcessCallbacks, callback)
}

// SetUpTestCase registers a callback to run before each test case.
func (p *ParallelTesting) SetUpTestCase(callback func(testCase T, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpTestCaseCallbacks = append(p.setUpTestCaseCallbacks, callback)
}

// SetUpTestDatabase registers a callback to run once per worker, after its own
// copy of the test database has been created. The callback receives the
// database name and the token.
func (p *ParallelTesting) SetUpTestDatabase(callback func(database string, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpTestDatabaseCallbacks = append(p.setUpTestDatabaseCallbacks, callback)
}

// TearDownProcess registers a callback to run once, as each worker process
// ends.
func (p *ParallelTesting) TearDownProcess(callback func(token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tearDownProcessCallbacks = append(p.tearDownProcessCallbacks, callback)
}

// TearDownTestCase registers a callback to run after each test case.
func (p *ParallelTesting) TearDownTestCase(callback func(testCase T, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tearDownTestCaseCallbacks = append(p.tearDownTestCaseCallbacks, callback)
}

// CallSetUpProcessCallbacks runs the registered process set-up callbacks, in
// the order they were registered, if a token resolves.
func (p *ParallelTesting) CallSetUpProcessCallbacks() {
	p.whenRunningInParallel(func(token string) {
		p.mu.RLock()
		callbacks := parallelTestingCopy(p.setUpProcessCallbacks)
		p.mu.RUnlock()

		for _, callback := range callbacks {
			callback(token)
		}
	})
}

// CallSetUpTestCaseCallbacks runs the registered test case set-up callbacks, in
// the order they were registered, if a token resolves.
func (p *ParallelTesting) CallSetUpTestCaseCallbacks(testCase T) {
	p.whenRunningInParallel(func(token string) {
		p.mu.RLock()
		callbacks := parallelTestingCopy(p.setUpTestCaseCallbacks)
		p.mu.RUnlock()

		for _, callback := range callbacks {
			callback(testCase, token)
		}
	})
}

// CallSetUpTestDatabaseCallbacks runs the registered test database set-up
// callbacks, in the order they were registered, if a token resolves.
func (p *ParallelTesting) CallSetUpTestDatabaseCallbacks(database string) {
	p.whenRunningInParallel(func(token string) {
		p.mu.RLock()
		callbacks := parallelTestingCopy(p.setUpTestDatabaseCallbacks)
		p.mu.RUnlock()

		for _, callback := range callbacks {
			callback(database, token)
		}
	})
}

// CallTearDownProcessCallbacks runs the registered process tear-down
// callbacks, in the order they were registered, if a token resolves.
func (p *ParallelTesting) CallTearDownProcessCallbacks() {
	p.whenRunningInParallel(func(token string) {
		p.mu.RLock()
		callbacks := parallelTestingCopy(p.tearDownProcessCallbacks)
		p.mu.RUnlock()

		for _, callback := range callbacks {
			callback(token)
		}
	})
}

// CallTearDownTestCaseCallbacks runs the registered test case tear-down
// callbacks, in the order they were registered, if a token resolves.
func (p *ParallelTesting) CallTearDownTestCaseCallbacks(testCase T) {
	p.whenRunningInParallel(func(token string) {
		p.mu.RLock()
		callbacks := parallelTestingCopy(p.tearDownTestCaseCallbacks)
		p.mu.RUnlock()

		for _, callback := range callbacks {
			callback(testCase, token)
		}
	})
}

// Option reads a parallel testing option, reporting whether it was set.
//
// With no resolver installed the value comes from the environment, under the
// option's name upper-cased and prefixed -- "recreate databases" is read from
// PARALLEL_TESTING_RECREATE_DATABASES.
func (p *ParallelTesting) Option(option string) (any, bool) {
	p.mu.RLock()
	resolver := p.optionsResolver
	p.mu.RUnlock()

	if resolver == nil {
		return parallelTestingDefaultOption(option)
	}
	return resolver(option)
}

// Token returns the token that tells one worker of a parallel run from
// another, and which names its database, its cache prefix and its temporary
// files. It reports whether one resolved at all.
//
// With no resolver installed it comes from the TEST_TOKEN environment
// variable, and an empty TEST_TOKEN counts as no token.
func (p *ParallelTesting) Token() (string, bool) {
	p.mu.RLock()
	resolver := p.tokenResolver
	p.mu.RUnlock()

	if resolver == nil {
		return parallelTestingDefaultToken()
	}

	token, ok := resolver()
	if token == "" {
		return "", false
	}
	return token, ok
}

// whenRunningInParallel runs the callback only if the tests are running in
// parallel. The token is handed to it because every call site needs it and
// every call site would otherwise resolve it again.
func (p *ParallelTesting) whenRunningInParallel(callback func(token string)) {
	if token, ok := p.inParallel(); ok {
		callback(token)
	}
}

// inParallel reports whether the current tests are being run in parallel. It
// answers the token as well, since resolving it is how the question is decided.
func (p *ParallelTesting) inParallel() (string, bool) {
	return p.Token()
}

// parallelTestingDefaultToken is the token resolver used until
// [ParallelTesting.ResolveTokenUsing] installs one.
func parallelTestingDefaultToken() (string, bool) {
	token := os.Getenv(parallelTestingTokenEnv)
	if token == "" {
		return "", false
	}
	return token, true
}

// parallelTestingDefaultOption is the options resolver used until
// [ParallelTesting.ResolveOptionsUsing] installs one.
func parallelTestingDefaultOption(option string) (any, bool) {
	name := parallelTestingOptionPrefix + strings.ToUpper(strings.ReplaceAll(option, " ", "_"))

	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return nil, false
	}
	return value, true
}

// parallelTestingCopy takes a snapshot of a callback list, so that the list can
// be walked with the lock released and a callback that registers another one
// does not deadlock or read a slice being appended to.
func parallelTestingCopy[C any](callbacks []C) []C {
	if len(callbacks) == 0 {
		return nil
	}
	snapshot := make([]C, len(callbacks))
	copy(snapshot, callbacks)
	return snapshot
}
