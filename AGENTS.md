# Working in this repository

`hesape` is the component collection of the Arandu framework: 47 top-level
packages spread over seven Go modules, and no application. Nothing here boots a
server and there is no `main`. A change lands in every project that imports the
package it touches, so the bar is a library's bar rather than an application's.

The packages carry the names of Laravel's `Illuminate` components on purpose, so
that a developer arriving from there recognises the vocabulary and only learns
what actually differs. What differs is that a large part of that surface exists
to remedy dangers of a dynamic language. Go does not have them, so the remedy is
refused rather than translated — and the refusal is written down, in the package
that would have held it.

Read `.agents/skills/` before writing code. Each file is a procedure, named by
the situation you are in.

## The gates

Nothing is finished until all four exit zero.

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

Both filters on `gofmt` are load-bearing. `gofmt` is the one tool in the chain
that ignores a build tag, so a view source — excluded from the compiler, and not
valid Go — reaches it and fails on the `@` its directives begin with.
`testdata/` holds fixtures that are invalid on purpose: the two programs under
`database/testdata/` are compiled by a test that requires them to be *refused*.

`./...` stops at a module boundary, and this repository is seven modules. The
root is one; the other six are where every third-party driver in the collection
lives, which makes them the last part of the tree that should go unchecked.

```sh
for mod in $(find . -name go.mod); do
  dir=${mod%/go.mod}
  (cd "$dir" && GOWORK=off go vet ./... && GOWORK=off go test -race ./...) \
    && echo "ok   $dir" || echo "FAIL $dir"
done
```

`GOWORK=off` is not decoration. The six submodules once carried a `replace`
pointing at the checkout, so the version their `go.mod` declared was never what
anything built against. Setting it makes the run build what a consumer
downloads.

`gofmt` stays out of that loop and loses nothing by it: it walks paths rather
than modules, so the `find` above already reaches every file in all seven.

## What does not exist here, and the package that says why

Seven of the 47 export nothing at all. Each is on disk holding a `doc.go` and
nothing else, so that an import going looking for the concept finds the argument
instead of a path that does not resolve. Read the `doc.go` before proposing the
thing again.

| A model reaches for | The package | What is here instead |
| --- | --- | --- |
| a service container, dependency injection | `container` | collaborators as fields, a `New` that takes them, the assembly written out by hand |
| `Macroable`, a method added to a type from outside | `macroable` | a free function, your own type embedding the framework's, or a consumer-declared interface |
| a tree of interfaces | `contracts` | the interface declared in the package that consumes it |
| `Conditionable` as something to embed | `conditionable` | `When` and `Unless`, declared by each type whose API chains |
| reflection over a struct at run time | `reflection` | a type assertion at the call site; `go/ast` at build time where code is emitted |
| SSH tasks, host groups | `remote` | `process` with the ssh binary as the transport, `concurrency.Run` over it |
| a top-level scheduler | `scheduler` | `console/scheduling`; the doc names what each symbol became |

Two more say the same thing one level down: `support/facades` — no global
accessor resolves a service on demand — and `support/traits`, where shared
behaviour sits in `support`, beside the types that use it.

## The three rules everything else follows from

**One direct dependency.** The root module requires `golang.org/x/crypto` and
nothing else. A CI gate reads `go.mod` and fails a pull request that adds a
second. Every third-party driver lives in one of the six modules beside the
root, each with its own `go.mod`, because Go has no optional dependency: a
project that wanted a directory on disk must not carry an S3 client.

**Authorization is a value.** `auth.Grant` has only unexported fields, so it
cannot be written as a struct literal. Every method that reaches stored data —
in `database`, `cache`, `filesystem`, `queue`, `notifications`, `broadcasting` —
takes one, which makes an unauthorized path to the data a build failure. That is
proven by compiling it: `TestRepositoryWithoutGrantDoesNotCompile` in
`database/grant_required_test.go` runs the toolchain over two fixtures and
requires both to be refused, by message.

**The tenant comes from the Grant.** `auth.Tenant(g)`, never from a path
segment, a body, a query or a header. Three things in the collection are
deliberately not scoped by it, and each carries its reason in the file beside
the code.

## Where a change goes

Beside the code it belongs to. Every one of the 47 top-level packages carries a
`doc.go` stating what it owns; read the nearest one before adding a file. A test
goes beside the code it tests and declares `package X_test`, unless it genuinely
needs something the package does not export.

An exported symbol without a doc comment is not finished. `pkg.go.dev` builds
the reference out of them, and that reference is the only documentation this
project publishes. The comment documents the symbol and nothing else: what it
does, what it takes, what it returns, what it guarantees, and the reason a
signature is the shape it is — said in terms of the code, never as a pointer to
a record kept somewhere else.

Comments, identifiers, error messages, log lines, CLI output and test names are
in English.
