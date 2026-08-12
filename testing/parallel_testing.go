package testing

import (
	"os"
	"strings"
	"sync"
)

// The environment this package reads when no resolver was installed.
//
// TEST_TOKEN is ParaTest's own name for the per-process token, and PHP reads it
// from $_SERVER. The option prefix is PHP's LARAVEL_PARALLEL_TESTING_ with the
// vendor prefix dropped: the shape of the name is the same, and nothing in this
// ecosystem is Laravel.
const (
	parallelTestingTokenEnv     = "TEST_TOKEN"
	parallelTestingOptionPrefix = "PARALLEL_TESTING_"
)

// ParallelTesting answers to Illuminate\Testing\ParallelTesting: the callbacks
// an application registers to be run at the boundaries of a parallel test run
// -- when a worker process starts and ends, when a test case is set up and torn
// down, and when a per-worker test database is created.
//
// In PHP it is a singleton resolved out of the container and reached through
// the ParallelTesting facade, so its callbacks read as static state. There is no
// container here, so it is a plain value: build one with [NewParallelTesting],
// keep it where the test bootstrap can reach it, and pass it. The PHP
// constructor's Container argument is dropped for the same reason.
//
// Every method is safe for concurrent use. PHP has one process per worker and
// no shared memory between them; a Go test binary runs its parallel tests as
// goroutines in one process, so registration and firing can genuinely overlap.
// Callbacks are copied out from under the lock and then called with the lock
// released, so that a callback may register another one without deadlocking.
//
// The callbacks fire only while running in parallel, which here means only that
// a token resolves -- see [ParallelTesting.Token]. PHP guards on a second
// condition, a LARAVEL_PARALLEL_TESTING flag in the environment, which its own
// runner writes; that runner is not part of this package (see the skip list in
// the package doc), so nothing would ever write the flag and the guard would
// only ever be off.
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

// NewParallelTesting answers to ParallelTesting::__construct.
//
// PHP takes the container, because it calls the registered callbacks through
// it to inject their arguments by name. Go calls them directly, so there is
// nothing to take.
func NewParallelTesting() *ParallelTesting {
	return &ParallelTesting{}
}

// ResolveOptionsUsing answers to ParallelTesting::resolveOptionsUsing: install
// the callback that [ParallelTesting.Option] asks for a value.
//
// PHP accepts null to go back to reading the environment; nil does the same.
// The resolver's second result answers PHP's false for an option that was
// never set.
func (p *ParallelTesting) ResolveOptionsUsing(resolver func(option string) (any, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.optionsResolver = resolver
}

// ResolveTokenUsing answers to ParallelTesting::resolveTokenUsing: install the
// callback that [ParallelTesting.Token] asks for the unique process token.
//
// PHP accepts null to go back to reading the environment; nil does the same.
// The resolver's second result answers PHP's false for "no token, so this is
// not a parallel run".
func (p *ParallelTesting) ResolveTokenUsing(resolver func() (string, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokenResolver = resolver
}

// SetUpProcess answers to ParallelTesting::setUpProcess: register a callback to
// run once, at the start of each worker process.
//
// PHP declares the callback's parameters and lets the container fill them in by
// name; the only name it can fill here is "token", so the callback takes it
// directly.
func (p *ParallelTesting) SetUpProcess(callback func(token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpProcessCallbacks = append(p.setUpProcessCallbacks, callback)
}

// SetUpTestCase answers to ParallelTesting::setUpTestCase: register a callback
// to run before each test case.
//
// PHP passes the PHPUnit test case; the callback takes [T], which is this
// package's stand-in for it, plus the token.
func (p *ParallelTesting) SetUpTestCase(callback func(testCase T, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpTestCaseCallbacks = append(p.setUpTestCaseCallbacks, callback)
}

// SetUpTestDatabase answers to ParallelTesting::setUpTestDatabase: register a
// callback to run once per worker, after its own copy of the test database has
// been created. The callback receives the database name and the token.
func (p *ParallelTesting) SetUpTestDatabase(callback func(database string, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setUpTestDatabaseCallbacks = append(p.setUpTestDatabaseCallbacks, callback)
}

// TearDownProcess answers to ParallelTesting::tearDownProcess: register a
// callback to run once, as each worker process ends.
func (p *ParallelTesting) TearDownProcess(callback func(token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tearDownProcessCallbacks = append(p.tearDownProcessCallbacks, callback)
}

// TearDownTestCase answers to ParallelTesting::tearDownTestCase: register a
// callback to run after each test case.
func (p *ParallelTesting) TearDownTestCase(callback func(testCase T, token string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tearDownTestCaseCallbacks = append(p.tearDownTestCaseCallbacks, callback)
}

// CallSetUpProcessCallbacks answers to
// ParallelTesting::callSetUpProcessCallbacks: run the registered "setUp"
// process callbacks, in the order they were registered, if a token resolves.
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

// CallSetUpTestCaseCallbacks answers to
// ParallelTesting::callSetUpTestCaseCallbacks: run the registered "setUp" test
// case callbacks, in the order they were registered, if a token resolves.
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

// CallSetUpTestDatabaseCallbacks answers to
// ParallelTesting::callSetUpTestDatabaseCallbacks: run the registered "setUp"
// test database callbacks, in the order they were registered, if a token
// resolves.
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

// CallTearDownProcessCallbacks answers to
// ParallelTesting::callTearDownProcessCallbacks: run the registered "tearDown"
// process callbacks, in the order they were registered, if a token resolves.
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

// CallTearDownTestCaseCallbacks answers to
// ParallelTesting::callTearDownTestCaseCallbacks: run the registered
// "tearDown" test case callbacks, in the order they were registered, if a
// token resolves.
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

// Option answers to ParallelTesting::option: read a parallel testing option.
//
// PHP answers mixed, and false when the option is unset; Go answers the value
// and whether it was set. With no resolver installed the value comes from the
// environment, under the option's name upper-cased and prefixed -- "recreate
// databases" is read from PARALLEL_TESTING_RECREATE_DATABASES.
func (p *ParallelTesting) Option(option string) (any, bool) {
	p.mu.RLock()
	resolver := p.optionsResolver
	p.mu.RUnlock()

	if resolver == nil {
		return parallelTestingDefaultOption(option)
	}
	return resolver(option)
}

// Token answers to ParallelTesting::token: the token that tells one worker of a
// parallel run from another, and which names its database, its cache prefix and
// its temporary files.
//
// PHP answers string|false; Go answers the token and whether one resolved. With
// no resolver installed it comes from the TEST_TOKEN environment variable, and
// an empty TEST_TOKEN counts as no token -- PHP's empty() reads "" as absent
// too.
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

// whenRunningInParallel answers to ParallelTesting::whenRunningInParallel,
// which is protected in PHP: run the callback only if the tests are running in
// parallel. The token is handed to it because every call site needs it and
// every call site would otherwise resolve it again.
func (p *ParallelTesting) whenRunningInParallel(callback func(token string)) {
	if token, ok := p.inParallel(); ok {
		callback(token)
	}
}

// inParallel answers to ParallelTesting::inParallel, which is protected in PHP:
// whether the current tests are being run in parallel. It answers the token as
// well, since resolving it is how the question is decided.
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
