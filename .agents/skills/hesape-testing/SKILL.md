---
name: hesape-testing
description: Write, place and run a test in the hesape collection, whose suite is 3,750 test functions across 321 files in seven Go modules. Use when the request is to "add a test", "write tests for this", "cover this case", "the suite is failing", "run the tests", "how do I test X", "should this be a table test", or "where does this test go"; when adding a second implementation of an interface that already has a contract suite; when a test needs a real PostgreSQL, MySQL or RESP server; and when deciding between package X and package X_test. Covers where a file goes and why, which package it declares, the naming and failure-message conventions, the two contract suites, tests that prove a claim by compiling code that must fail, and the command that reaches all seven modules.
license: MIT
---

# Writing a test here

The suite is the reason a change to a library this size can be made at all.

```sh
find . -name '*_test.go' | wc -l                                    # 321
grep -rhcE '^func Test[A-Za-z0-9_]*\(' --include='*_test.go' . | paste -sd+ - | bc   # 3750
```

At that size the only thing that makes a failure useful is that it names the
behaviour that broke. Everything below is in service of that.

## Where the file goes

Beside the code it tests, named `*_test.go`, in the same directory. There is no
`tests/` directory and there will not be one: `go test` attributes coverage per
directory, so a test filed elsewhere leaves the package under test reporting 0%
— and from elsewhere it can only reach what the package exports anyway.

## Which package it declares

This is a real choice, and it answers one question.

| declare | when | count |
| --- | --- | --- |
| `package X_test` | this is the **contract**. The test sees what a caller sees, which is the point | 262 files |
| `package X` | this is the **implementation**, and the test genuinely needs something unexported | 59 files |

Prefer the first, and take the second only when you actually use it — a test
declaring the internal package while naming nothing unexported is a test that
gave up its reason to exist. `CONTRIBUTING.md` names the checker that verifies
exactly that, by intersecting the identifiers a test names with what its package
declares unexported.

A `package main` has no external form: it cannot be imported, so its tests are
internal and that is the end of it.

## The shape

**One function per behaviour, named as a sentence.** The suite has 3,750 test
functions and 110 `t.Run` calls, which is the ratio on purpose: a table with
eight rows reports one failing name, and eight functions report the one that
broke. Reach for `t.Run` when the rows really are the same assertion over
varying input — `TestARefusedSystemGrantSaysWhy` in `auth/policy_test.go:148` is
the shape, three tenants against one message check.

**A doc comment where the name cannot carry the reason.** 897 tests open with
one, 438 of them in the `// TestName: sentence` form. The good ones say what
breaks rather than what is asserted:

```go
// TestNoExternalOrigin: the CSP the framework sets is script-src 'self', so an
// asset pointing at a CDN is a script that never loads -- and the only sign is a
// console message nobody sees until the page is already broken.
```

Where a test exists because a defect shipped, say so and describe the defect.
`database/conformance/conformance.go` and `view/assets_test.go:103` both do, and
those comments are the reason nobody deletes the test as redundant later.

**No assertion library.** No Go file in this repository imports one:
`grep -rn "stretchr/testify" --include='*.go' .` finds nothing and exits 1.
Where that name appears at all it is an indirect line in a driver module's
`go.mod`, pulled in by the driver rather than written against. A failure is
reported with `t.Errorf` / `t.Fatalf` in the standard form, plus the
consequence:

```go
t.Fatalf("error = %v, want ErrForbidden", err)
t.Fatal("the system subject must carry the system role, so audits can find it")
```

`t.Helper()` (151 uses), `t.Cleanup` (85) and `t.TempDir()` (93) are the
standard-library tools this suite leans on. `t.Parallel()` appears 343 times;
use it where the test owns everything it touches.

## Contract suites

When an interface has more than one implementation, the behaviour is tested once
and every implementation is handed to it. Two exist:

- **`cache/cachetest`** — `cachetest.Run(t, factory)` and
  `cachetest.RunLocking(t, factory)`. Every `cache.Store` passes it: the array,
  file and database stores in `cache`, and the RESP store in the `redis` module.
  It lives here and not in each adapter because there is one contract, not one
  per backend, and a copy is the thing that drifts.
- **`database/conformance`** — `conformance.Run(t, dialect, driverName, dsn)`,
  called by all three connector modules. It exists because every test once ran
  against SQLite, which accepts `id TEXT PRIMARY KEY`; MySQL refuses it, so the
  first statement of the first migration failed in every project and nothing
  noticed.

If you are adding a second implementation of something, the new behaviour test
belongs in the contract suite. If you are adding the *first*, consider whether a
contract suite is what you are writing.

## Tests that need a server

A driver test reads its DSN from the environment and skips when it is not there,
so `go test ./...` still passes on a machine with nothing installed:

```
ARANDU_TEST_POSTGRES_DSN   database/connectors/pgx
ARANDU_TEST_MYSQL_DSN      database/connectors/mysql
REDIS_ADDRESS              redis, queue/connectors/redis
```

CI sets all three and then **fails when a test skips**, because `go test` has no
exit code for "everything skipped" and a suite that skipped is a suite that
printed ok. That is not hypothetical: the queue connector ran ten tests against
nothing for as long as it read a variable no runner set, and the two doc
comments saying "CI sets it" were describing a CI that no longer existed.

One RESP image serves all four products, and what keeps that honest is
`TestNothingUsesLuaOrModules` — declared in both `redis/redis_test.go:971` and
`queue/connectors/redis/redis_test.go:327` — which reads the source rather than
talking to a server. Portability is guarded where it can actually be broken.

## Proving a claim that is about the compiler

Some claims here are about what does not compile, and the only proof of one is
an attempted compilation. `TestRepositoryWithoutGrantDoesNotCompile`
(`database/grant_required_test.go:18`) runs `go vet` over two fixtures in
`database/testdata/` and requires each to fail **with a specific message**,
because a fixture that failed for an unrelated reason would prove nothing. It
sets `GOWORK=off` on the child process for the same reason.

Those fixtures are why `testdata/` is excluded from the `gofmt` gate. If you add
one, it is invalid on purpose and the gate must keep skipping it.

## Running

The root module, and the four gates nothing is finished without:

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race -count=1 ./...
```

`./...` stops at a module boundary, so that reaches the root and none of the six
modules beside it — which is exactly where every third-party driver lives:

```sh
for mod in $(find . -name go.mod); do
  dir=${mod%/go.mod}
  (cd "$dir" && GOWORK=off go vet ./... && GOWORK=off go test -race ./...) \
    && echo "ok   $dir" || echo "FAIL $dir"
done
```

`gofmt` stays out of that loop and loses nothing: it walks paths rather than
modules, so the `find` above already covers all seven.

## Two packages that are not for these tests

`arandutest` and `testing` are shipped *to applications* — a browser and
database assertions in the first, the assertion vocabulary in the second. The
collection's own tests use neither: `hesape/testing` is imported by no `_test.go`
in this repository, and `arandutest` only by its own two. Do not reach for them
here.

That is a rule about direction, not a slight. `arandutest` exists so an
application's test drives the same code path production drives — there is no
synchronous mode and no in-memory substitute, because a second way to run
something always leaks into production. A test inside this collection is already
next to the code, so it has nothing to gain from the layer above it.
