// Package testing is the assertion vocabulary a test writes against.
//
// It holds the bare assertions ([AssertTrue], [AssertSame], [AssertCount] and
// the rest), the things worth asserting about ([TestResponse] for what a
// handler answered, [AssertableJSONString] for a decoded payload, [TestView]
// for a rendered view, [TestComponent] for a rendered component,
// [PendingCommand] for a console command), and the two pieces around a run
// ([ParallelTesting] for the per-process callbacks,
// [LoggedExceptionCollection] for what the handler recorded).
//
// # Everything reports through T
//
// No assertion here takes *testing.T. They take [T], which is the part of it
// they use, so an assertion can be tested by handing it a recorder instead of
// a real test -- which is how this package's own tests check that a failing
// assertion fails.
//
// Every assertion calls Helper before it reports, so a failure is blamed on
// the line in the test rather than on the line inside the assertion.
//
// # Failure messages carry the context
//
// An assertion on a [TestResponse] appends the status, the headers and the
// body to its message. The answer to "why is this a 500" is in the response,
// and finding it out otherwise means running the test again with a print.
//
// # Subpackages
//
// hesape/testing/constraints holds the reusable matchers, so a comparison used
// by more than one assertion is written once. hesape/testing/fluent holds the
// chained assertion language over a decoded JSON payload.
package testing
