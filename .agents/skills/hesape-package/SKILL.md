---
name: hesape-package
description: Decide where a behaviour lives in the hesape component collection, and whether it should be a package at all. Use when the request is to "add a package", "create a new component", "port Illuminate's X", "add a helper for Y", "where should this code go", or "we need a container / macro / facade / reflection / contracts package"; when a change does not obviously belong to any of the 47 packages already here; or when a proposal arrives for something Laravel has and this collection does not. Also use when the answer is no — a refusal here is written down as a package that exports nothing and carries the argument, and seven of them exist for exactly that. Covers the triage, the naming rule, the doc.go that comes first, the subpackage conventions, and what to do instead of widening a package.
license: MIT
---

# Adding a package, or writing down why there is not one

The collection is 47 top-level packages in the root module, and 153 packages
counting every subdirectory. The default answer to "should this be a new
package" is no, and the second-best answer — after "it belongs in one that
exists" — is a package that exports nothing and says why.

```sh
export GOWORK=off
go list ./... | wc -l    # 153
ls -d */ | wc -l         # 47
```

## The triage

Three questions, in order. They are not invented for this file: each is the
argument one of the seven empty packages already makes, and you can read the
long form in its `doc.go`.

**1. Is this a problem Go has?** Much of what a port would bring across exists
to remedy dangers of a dynamic language.

- A container resolves collaborators at run time because PHP cannot check the
  wiring. Go can: a `New` that takes its collaborators fails at the call site.
  See `container/doc.go`.
- `Macroable` grafts a method onto a type from outside its package because PHP
  has `__call`. Go has no hook for a call that resolves to nothing, and a
  receiver must be declared where its type is. See `macroable/doc.go`.
- Reflection reads a struct back at run time because the type was erased. In Go
  a function value carries its parameter types, and a type assertion asks at the
  call site what a runtime lookup would only ask later. Where the same
  information is needed to emit code, it is read from `go/ast` at build time.
  See `reflection/doc.go`.

If the answer is no, stop here. The output is a `doc.go`, not a package.

**2. Does the concept already have a home?** Three of the seven exist because
the answer was yes and somebody still went looking.

- `contracts` — an interface belongs to the package that consumes it.
  `cache.Store` is in `cache`, `session.Handler` is in `session`. A tree
  gathering copies of them is a second way to say the same thing.
- `conditionable` — `When` and `Unless` are declared by each type whose methods
  return the receiver, because that is the only place a receiver can be
  declared. `collections.Collection[T]` and `query.Builder` have them; nothing
  else earns them.
- `scheduler` — it moved to `console/scheduling`, and the `doc.go` names what
  each symbol became, so an import that has not been updated fails with that
  file open.

**3. Is it a type at all?** `remote` is the case where it was not. Running a
command on another machine is `process` with the ssh binary as the transport;
running it on several is `concurrency.Run` over that; naming a command a person
types is `console.Command`. None of that wanted a host-group type.

## If it lives

**Name it after the Illuminate component.** That is the whole point of the
naming: a developer arriving from Laravel should recognise the vocabulary and
only learn what actually differs. `str`, `collections`, `pipeline`, `bus`,
`notifications` are all the name from there.

**Write the `doc.go` before the code.** Every one of the 47 top-level packages
has one, and it is checked by the fact that `pkg.go.dev` is the only reference
this project publishes. Verify with:

```sh
for d in */; do [ -f "$d/doc.go" ] || echo "MISSING $d"; done   # prints nothing
```

The comment states what the package owns, what is in it, and the signature
decisions a reader would otherwise have to reconstruct. It documents the
package — not a record kept elsewhere, not a comparison with the PHP method,
not a sibling repository.

**Put subpackages where the collection already puts them.** These are the names
in use, and a new one needs a reason:

| subpackage | what it holds |
| --- | --- |
| `X/console` | the `aru` commands the component contributes |
| `X/middleware` | the HTTP middleware it contributes |
| `X/events` | the events it publishes |
| `X/concerns` | behaviour shared by the types in `X`, not exported as an API |
| `X/exceptions` | its error types, where there are enough to crowd the package |
| `X/Xtest`, `X/conformance` | a contract suite every implementation of `X` passes |

**A package that adds routes or commands implements `foundation.Module`** —
`Name()` and `Routes(r *routing.Router)`, plus whichever of the optional
interfaces it needs. A module with no HTTP surface still implements `Routes` and
registers nothing: a one-line no-op says the absence better than a second
interface to check for.

## If it does not live

Write the refusal as a `doc.go` in the package that would have held it, and
leave the directory on disk. The point is that an import going looking for the
concept finds the argument instead of a path that does not resolve.

Such a file has three parts, and all seven have them:

1. **What was expected.** The first line says the package exports nothing, and
   the next says what somebody came here for.
2. **Why it is refused.** In terms of the language, not of taste.
   `macroable/doc.go` names the rule — a receiver must be declared with its
   type — rather than calling macros a bad idea.
3. **What to write instead, by name.** `remote/doc.go` names `process`,
   `concurrency.Run`, `console.Command` and `filesystem.Disk`.
   `scheduler/doc.go` carries a table mapping every old symbol to its
   replacement.

Also say what is given up. `macroable/doc.go` states it outright: a third-party
package can no longer add a method to every `Collection` in the process. A
refusal that only lists the wins reads as advocacy, and gets reopened.

## What this skill is not about

`workbench` and `aru make:package` generate a Go module that an *application*
imports. That is a different thing from a package of this collection, and
nothing in this procedure applies to it.

## Then run the gates

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./... && go vet ./... && go test -race ./...
```
