// Package arandutest holds the helpers a test needs and an application must
// not use.
//
// It is a browser (Client), the assertions worth having about what came back
// (Response), the assertions worth having about what landed in the database
// (AssertDatabaseHas, AssertDatabaseMissing, AssertDatabaseCount), and the two
// pieces a test needs to say something about domain events (DrainOutbox,
// Collected).
//
// Every application writes the same six things: send a request, do not follow
// the redirect, keep the cookie, act as somebody, assert about the HTML that
// came back, assert about the row that was written. Laravel calls that
// $this->get(), ->actingAs(), ->assertSee() and ->assertDatabaseHas(), and it
// is in the framework because a project that writes its own writes it slightly
// differently -- and the one that matters, acting as a subject, is the one most
// easily written wrong.
//
// # Nothing here is a second implementation
//
// Everything drives the same code path production drives. There is no
// synchronous mode, no in-memory substitute and no test double for the outbox:
// "sync only in tests" is a second way to do one thing (RULE 9), and the second
// way always leaks into production -- usually on the day somebody copies a test
// into a handler.
//
// The same rule is why acting as somebody is Client.ActingAs and nothing else.
// The subject travels under auth.WithSubject, which is the key the edge
// middleware writes and every policy reads. A package-level ActingAs used to
// live here writing a context key of its own, which no policy, no repository
// and no middleware ever read: it authenticated nothing, and a test written
// against it passed while proving the opposite of what it said. Code that calls
// a service, a job or a seeder directly does not need this package for it --
// auth.WithSubject is the whole answer, and it is the real one.
//
// # The database assertions read without a Grant, on purpose
//
// RULE 17 says no query path skips a policy. These three do, and that is what
// they are for: they check what the Grant-protected path actually wrote, so
// they cannot be the same path or they would prove nothing. They take a table
// name and a match, never an entity, and they never appear outside a _test.go.
//
// Because they read the table and not the tenant's slice of it, a match that
// omits the tenant column proves the row exists somewhere. In a multi-tenant
// application that is rarely the assertion anybody means: put the tenant in the
// match.
//
// # What it answers to
//
// This is the address of Illuminate\Testing in this collection. It is spelled
// arandutest rather than testing because an import segment called "testing"
// shadows the standard library package that every _test.go imports; the
// precedent is net/http/httptest.
//
// The files in the clone at laravel_illuminate/testing:
//
//	TestResponse.php                  -> Response
//	TestResponseAssert.php            -> Response
//	Concerns/AssertsStatusCodes.php   -> Response.Status, Response.OK
//	Constraints/HasInDatabase.php     -> AssertDatabaseHas, AssertDatabaseMissing
//	Constraints/CountInDatabase.php   -> AssertDatabaseCount
//	Constraints/SeeInOrder.php        -> Response.See, Response.DontSee
//	Assert.php                        -> the assertions on Response
//
// The rest of the component does not arrive. AssertableJson, AssertableJsonString
// and Fluent are an assertion language over JSON, and this collection answers
// with HTML fragments (RULE 10). ParallelRunner, ParallelTesting,
// ParallelConsoleOutput and Concerns/RunsInParallel are PHPUnit's process
// harness; go test already runs packages in parallel and t.Parallel is in the
// standard library. Concerns/TestDatabases is the same machinery for cloning a
// schema per process. PendingCommand and TestComponent test the artisan output
// this collection does not have a second set of (docs/31 section 5), TestView
// belongs with the view compiler, LoggedExceptionCollection is the exception
// handler's own recorder, and Constraints/SoftDeletedInDatabase and
// NotSoftDeletedInDatabase describe a column no repository in this collection
// writes. ArraySubset was deprecated in PHPUnit before it was copied here.
package arandutest
