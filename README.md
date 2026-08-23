<p align="center">
  <img src=".github/logo.png" alt="Arandu" width="140" height="140">
</p>

<h1 align="center">arandu-io/hesape</h1>

<p align="center">47 packages, one per concern — the collection the framework is built from.</p>

<p align="center">
<a href="https://github.com/arandu-io/hesape/actions/workflows/ci.yml"><img src="https://github.com/arandu-io/hesape/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
<a href="https://pkg.go.dev/github.com/arandu-io/hesape"><img src="https://pkg.go.dev/badge/github.com/arandu-io/hesape.svg" alt="Go Reference"></a>
<a href="https://github.com/arandu-io/hesape/tags"><img src="https://img.shields.io/github/v/tag/arandu-io/hesape?label=version" alt="Latest Version"></a>
<a href="LICENSE.md"><img src="https://img.shields.io/github/license/arandu-io/hesape" alt="License"></a>
</p>


## About `hesape`

`hesape` is Guarani for *illumination*. It is the collection Arandu is built
from: one package per concern — authorization, data access, HTTP, views,
background work, mail, diagnostics — each independently importable:

```go
import (
    "github.com/arandu-io/hesape/auth"
    "github.com/arandu-io/hesape/http"
    "github.com/arandu-io/hesape/mail"
)
```

`github.com/arandu-io/framework` composes these into a running application;
nothing stops a project from importing a package here directly.

## What it delivers

| area | packages | what it does |
|---|---|---|
| authorization & session | `auth`, `session`, `cookie`, `hashing`, `encryption`, `oauth` | `Grant` — unexported fields, issued only by `Authorize` or the named `SystemGrant` escape hatch — plus the session store, CSRF token, password hashing and signed tokens |
| data & storage | `database`, `cache`, `redis`, `filesystem`, `pagination` | one repository shape with no ORM, a cache with pluggable stores, a Redis/RESP adapter for both, tenant-scoped file storage, three paginators (offset, simple, keyset) |
| HTTP & views | `http`, `routing`, `view`, `html` | request/response context over `net/http`, a router with named routes and URL generation, the kyse-to-Go view compiler with HTMX and the delegated client behaviours wired in, an escaped HTML/form builder |
| background work | `queue`, `bus`, `events`, `console/scheduling`, `broadcasting`, `notifications`, `mail` | a job queue where every push carries a `Grant`, batches and chains of jobs, domain events with an outbox, an in-process scheduler (a goroutine, not a system crontab), channel broadcasting over Redis, multi-channel notifications, mail |
| diagnostics & quality | `log`, `exception`, `validation`, `console`, `testing`/`arandutest` | the request Collector this framework exists for, the handler a failed request stops at (and the development error page), a form validator with 108 rules, the vocabulary a project's own commands are written against, a test client with assertions and outbox helpers |
| foundation & utilities | `foundation`, `config`, `collections`, `str`, `support`, `number`, `image`, `jsonschema`, `process`, `pipeline`, `translation` | process composition run once at boot, typed configuration, generic collections, the string transforms a generator, a router and a validator all need, number/currency formatting, declarative image transforms, typed JSON Schema, external process execution, a value piped through a chain of steps, translated strings |

Seven of the 47 export nothing at all, on purpose, and hold only a doc comment
explaining why: a dependency-injection container (`container` — the wiring here
is explicit and hand-written, never resolved), a way to attach a method to a
type from outside its own package (`macroable` — a receiver has to be declared
where its type is, and Go has no hook for a call that resolves to nothing),
reflection over a type's structure (`reflection` — a type assertion asks at the
call site what a runtime lookup would only ask later, and code generation reads
`go/ast` at build time), SSH-driven remote command execution (`remote` — not
built; running a command on another machine is `process` with the ssh binary as
the transport), a home for `When` and `Unless` (`conditionable` — same receiver
rule, so every chainable type declares them itself), a tree of interfaces
(`contracts` — an interface belongs to the package that consumes it), and the
old top-level scheduler (`scheduler` — it moved to `console/scheduling`, and the
doc comment names what each symbol became). The package stays on disk so an
import that goes looking for the concept finds the reason instead of a path that
resolves to nothing.

**Assets are embedded, not fetched** — one `go:embed` directive
(`view/assets.go:21`) bundles HTMX 2.0.4, Tailwind CSS 4.3.3, Basecoat 1.0.2
and Arandu's own `theme.js` and `ui.js`, served from the application's own
origin under `/_arandu/assets/`. Zero CDN: the Content-Security-Policy is
`script-src 'self'`, and a test scans the embedded assets for `cdn`, `unpkg`,
`jsdelivr`, `googleapis` or `cloudflare` and fails the build if it finds one.
There is no directive framework in there either, and that is the same
constraint rather than a taste: compiling an attribute into a function needs
`unsafe-eval`, so client behaviour is delegation on `data-*` attributes that
are read as data and never evaluated. Tailwind
itself is the standalone binary the CLI downloads, checks against a published
SHA-256, and caches — not an npm package. Zero Node anywhere in the tree,
checked in CI.

One direct dependency: `golang.org/x/crypto`, and CI fails a pull request that
adds a second — every third-party driver lives in one of the six modules beside
the root (`redis`, `queue/connectors/redis`, `filesystem/s3` and the three under
`database/connectors`), each with its own `go.mod`, because Go has no optional
dependency. 186,122 lines of production code and 87,439 of test, across 314 test
files, and `go test -race ./...` passes.

## Install

```sh
go get github.com/arandu-io/hesape/auth
```

Every package is fetched the same way, on its own import path.

## The rest of Arandu

`arandu-io/framework` is what assembles these packages into a running
application; `aru` is the command line; `arandu` is the skeleton `aru new`
clones; `examples` is a complete application built on top of all three.

## Learning Arandu

The API reference is generated from the doc comments and lives on
[pkg.go.dev](https://pkg.go.dev/github.com/arandu-io/hesape). Every exported
symbol carries one, and that is deliberate: it is the documentation that cannot
drift from the code, because it sits in the same file.

A guide and a website do not exist yet, and that is a decision rather than a
gap: a guide written against an API that still moves is work done twice, and the
second time is worse — there is wrong documentation published.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Every commit carries a `Signed-off-by`
line, and before a pull request opens the three commands in that file — `gofmt`,
`go vet ./...`, `go test -race ./...` — have to pass in the root module and in
each of the six beside it. CI runs those three and three gates on top of them:
the exported surface diffed against the last release, which fails an
incompatible change that `UPGRADE.md` does not record; `govulncheck`; and a
check that `go.mod` still declares the one dependency.

## Security Vulnerabilities

Please review [our security policy](SECURITY.md) on how to report a
vulnerability. Never open a public issue for one.

## License

Open-sourced software licensed under the [MIT license](LICENSE.md).
