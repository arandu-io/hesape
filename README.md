# hesape

The components of the [Arandu](https://github.com/arandu-io/framework) framework.

`hesape` is Guarani for *illumination*. It is to Arandu what `Illuminate` is to
Laravel: the collection the framework is made of, one package per component,
under the names a Laravel developer already knows.

```go
import (
    "github.com/arandu-io/hesape/auth"
    "github.com/arandu-io/hesape/httpx"
    "github.com/arandu-io/hesape/mail"
)
```

## The tree is here before the code is

Every package below exists with its doc comment and nothing else. That is
deliberate: the reorganization is specified in full before anything moves,
because the attempt before it fixed things one at a time and one at a time does
not reorganize a structure.

The specification is
[`docs/31-reorganizacao-hesape.md`](https://github.com/arandu-io/docs). It was
written by reading all forty-two Illuminate components against the code — 1,070
surfaces — and it names, for each package, which component it answers to, what
moves into it and from where, and which existing package splits to make it.

The move happens in phases. Each one ends with the whole tree compiling and the
tests passing, so there is no window in which the framework is half moved.

## What is not here

Eight Illuminate components become no package at all, each for a reason written
down: `Conditionable` (Go has `if` in statement position), `Container` (ADR
0001 — the wiring is explicit and written by hand), `Contracts` (an interface
lives in the package that consumes it), `Html` and `Remote` (deleted from
Laravel in 5.1), `Macroable` (a method's receiver must be declared in its own
package), `Reflection` (it is the mechanism this framework's thesis rejects),
and `Workbench` (it is a console generator, so it is `aru`).

Two names differ from Illuminate's, both because the standard library already
has the word: `Http` is `httpx`, and `Testing` is `arandutest` — the precedent
is `net/http/httptest`.

## Licence

MIT. See [LICENSE.md](LICENSE.md).
