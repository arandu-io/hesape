// Package testing mirrors Illuminate\Testing.
//
// The files it answers to, in the clone at
// laravel_illuminate/testing:
//
//	Assert.php
//	AssertableJsonString.php
//	LoggedExceptionCollection.php
//	ParallelConsoleOutput.php
//	ParallelRunner.php
//	ParallelTesting.php
//	ParallelTestingServiceProvider.php
//	PendingCommand.php
//	TestComponent.php
//	TestResponse.php
//	TestResponseAssert.php
//	TestView.php
//
// # Skipped from TestResponseAssert.php and Constraints/ArraySubset.php
//
// Both classes are here -- the assertions of TestResponseAssert are the methods
// of [TestResponse] itself, and ArraySubset is [constraints.ArraySubset]. Two
// method names have no answer, and each one is a name PHPUnit owns rather than
// Illuminate:
//
//	TestResponseAssert::withResponse -- reason 1: it is the constructor of a
//	    class marked @internal whose entire body is __call and __callStatic,
//	    forwarding every name to Assert and catching
//	    ExpectationFailedException to append the response to the message. Go has
//	    no __call, so there is nothing to forward and nothing to construct: a
//	    failing TestResponse assertion adds the same context on the way in,
//	    through TestResponse.messageWithContext, which is where
//	    injectResponseContext ends up.
//	Constraints\ArraySubset::evaluate -- reason 3: evaluate is PHPUnit's
//	    template method, and PHPUnit is not carried here. Its own body is the
//	    comparison, which is [constraints.ArraySubset.Matches] -- the name
//	    PHPUnit's Constraint gives the same question, and the one
//	    [constraints.Constraint] declares -- plus a SebastianBergmann
//	    ComparisonFailure handed to fail(), which is
//	    [constraints.ArraySubset.FailureDescription] here because a Go assertion
//	    reports by returning the message rather than by throwing an object that
//	    carries one.
//
// # Skipped from the parallel testing files
//
// ParallelTesting itself is implemented, as [ParallelTesting]. What is skipped
// around it is the machinery that drives ParaTest through the service
// container and Symfony Console, since go test is the runner here. Each skip,
// with the ADR 0044 reason number:
//
//	ParallelTesting::__construct's $container argument -- reason 2: PHP holds
//	    the container only to call the registered callbacks through it and have
//	    their arguments injected by name. Go calls them directly, so
//	    NewParallelTesting takes nothing.
//	ParallelTestingServiceProvider::boot -- reason 2: it exists to bootstrap the
//	    per-worker test database from a deferred service provider, and there is
//	    no service provider here.
//	ParallelTestingServiceProvider::register -- reason 2: it exists to bind
//	    ParallelTesting into the container as a singleton. Here the value is
//	    built by NewParallelTesting and held by whoever needs it.
//	ParallelConsoleOutput::__construct and ParallelConsoleOutput::write --
//	    reason 3: they wrap a Symfony Console output to drop ParaTest's
//	    "Running phpunit in" and "Configuration read from" lines. This
//	    ecosystem carries neither Symfony Console nor ParaTest, and go test
//	    writes its own output.
//	ParallelRunner::__construct and ParallelRunner::run -- reason 3:
//	    ParallelRunner implements ParaTest's RunnerInterface, which does not
//	    exist here. go test -parallel is the runner.
//	Concerns\RunsInParallel::resolveApplicationUsing and
//	    Concerns\RunsInParallel::resolveRunnerUsing -- reason 2: they install
//	    resolvers for the application container and for the ParaTest runner
//	    that ParallelRunner is built from, and this package carries neither.
//	Concerns\RunsInParallel::execute -- reason 3: it applies PHPUnit's PhpHandler
//	    to the parsed configuration, runs the ParaTest runner and fires the
//	    process callbacks around it. The firing is what
//	    ParallelTesting.CallSetUpProcessCallbacks and
//	    ParallelTesting.CallTearDownProcessCallbacks answer to; the rest is
//	    PHPUnit and ParaTest.
//	Concerns\RunsInParallel::getExitCode -- reason 3: it forwards the exit code
//	    of the ParaTest runner, which does not exist here.
//	Concerns\RunsInParallel::forEachProcess and
//	    Concerns\RunsInParallel::createApplication -- reason 2: they build one
//	    application container per worker process and flush it afterwards, which
//	    is the container lifecycle PHP needs and Go does not have.
//
// # Skipped from PendingCommand.php
//
// PendingCommand is implemented, as [PendingCommand]. Each skip, with the ADR
// 0044 reason number:
//
//	PendingCommand::__destruct -- reason 1: Go has no deterministic destructor.
//	    The PHP runs the command from it when the test described one and never
//	    called run(), which is what makes $hasExecuted worth keeping; a Go test
//	    calls Run or Execute itself.
//	Conditionable's when and unless, Tappable's tap, and Macroable's macro,
//	    mixin and __call, mixed into PendingCommand -- reason 1: __call is a
//	    method table filled at runtime, which a compiled language does not have,
//	    and when, unless and tap are the closure chaining a PHP fluent object
//	    needs to stay one expression. Every method here already returns the
//	    PendingCommand.
//	PendingCommand::mockConsoleOutput and
//	    PendingCommand::createABufferedOutputMock -- reason 3: they build the
//	    Mockery double of OutputStyle that both answers the questions and
//	    records every write, and Mockery is not carried here. What they are for
//	    survives: PendingCommand.consoleInput scripts the answers and
//	    PendingCommand.verifyExpectations reads the questions back out of what
//	    the command printed, which is where a console.IO writes its prompts.
package testing
