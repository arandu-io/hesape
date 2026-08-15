# Contributing

## Sign your commits

Every commit needs a `Signed-off-by` line:

```
git commit -s -m "what changed and why"
```

That line is the [Developer Certificate of Origin](https://developercertificate.org/):
you are stating that you wrote the change, or that you have the right to submit
it under this project's license. It is not a copyright assignment — you keep
your copyright, and this project can never be relicensed behind your back.

We use DCO rather than a CLA on purpose. A CLA would let the project relicense
later, and the price is that every contributor has to sign a legal document
before their first patch.

## Before you open a pull request

```
gofmt -l .        # no output
go vet ./...
go test -race ./...
```

That covers the root module and nothing else. This repository is seven Go
modules -- the root, plus `redis`, `queue/connectors/redis`, `filesystem/s3` and
the three under `database/connectors` -- and `./...` stops at a module boundary.
The six are where every third-party driver in the collection lives, so they are
the last part of the tree that should go unchecked. Run the pair in each:

```
for mod in $(find . -name go.mod); do
  dir=${mod%/go.mod}
  echo "== $dir"
  (cd "$dir" && go vet ./... && go test -race ./...) || break
done
```

`gofmt` stays out of the loop and loses nothing by it: it walks paths rather than
modules, so `gofmt -l .` at the root already reaches every file in all seven.

The driver suites skip when they cannot reach a server, which is what keeps them
runnable on a machine with nothing installed. CI gives them a real PostgreSQL, a
real MySQL and a real RESP server through `ARANDU_TEST_POSTGRES_DSN`,
`ARANDU_TEST_MYSQL_DSN` and `REDIS_ADDRESS`, and fails when a test skips: `go
test` has no exit code for "everything skipped", and a suite that skipped is a
suite that printed ok.

CI runs all of that, plus a check that no new dependency entered the root module:
it depends on the standard library and `golang.org/x/crypto`, and nothing else.
The six driver modules exist for that reason -- Go has no optional dependency, so
a project that wanted a directory on disk must not carry an S3 client, and a
project on SQLite must not carry pgx. A pull request that adds a require line to
the root `go.mod`, or a second driver to a connector, needs to argue for it
first, in an issue.

## Where a test goes

Beside the code it tests, named `*_test.go`, in the same directory. There is no
`tests/` directory, and that is not style: `go test` attributes coverage per
directory, so a test filed elsewhere leaves the package under test reporting
0% -- and it can only reach what the package exports.

Which package the test declares is a real choice, and it answers one question:

| declare | when |
|---|---|
| `package X_test` | this is the **contract**. The test sees what a caller sees, which is the point |
| `package X` | this is the **implementation**, and the test genuinely needs something the package does not export |

Prefer the first. Take the second only when you use it -- `plans/testpackages.go`
in the arandu-io working tree checks exactly that, by intersecting the
identifiers a test names with what its package declares unexported, and the
checklist runs it across every repository.

A `package main` has no external form: it cannot be imported, so its tests are
internal and that is the end of it.

## What the commit message says

What changed and why. The why is the part that is not in the diff, and it is the
part someone will need in two years.

No AI attribution of any kind: no `Co-Authored-By` for an assistant, no
"generated with" footer. Commits are authored by the people who submit them.

## Architecture decisions

The decisions this project has already made live in `arandu-io/docs`, and every
one that closed a door has an ADR. If your change contradicts one, say so in the
pull request and argue for the change of decision — that is a normal thing to
do, and it is better than a patch that quietly works around it.
