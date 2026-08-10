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

## The tree mirrors the clone, directory by directory

`laravel_illuminate/` holds all forty-two Illuminate components, cloned whole.
This tree is generated from it: every directory that holds PHP classes becomes a
Go package at the same path, lowercased, and its doc comment names the files it
answers to.

    laravel_illuminate/auth/Access/Gate.php   ->  hesape/auth/access
    laravel_illuminate/auth/Passwords/        ->  hesape/auth/passwords
    laravel_illuminate/database/Eloquent/     ->  hesape/database/eloquent

What is deliberately not mirrored: `stubs/`, `resources/` and `views/`, which
hold data rather than classes, and the eight components that become no package
at all -- those keep a single directory whose doc comment says why, so somebody
looking for `Container` finds the reason instead of silence.

One package could not be literal: `Support/Defer` is `support/deferpkg`,
because `defer` is a Go keyword.

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
