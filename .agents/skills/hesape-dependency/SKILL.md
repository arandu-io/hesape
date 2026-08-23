---
name: hesape-dependency
description: The dependency rule of the hesape collection — one direct third-party dependency in the root module, and a CI gate that refuses the second. Use before running "go get", adding a require line, importing an SDK, a client library, a database or cache driver, a UUID package, an assertion library or a logger; when the request mentions "add a dependency", "use library X", "we need a Redis / S3 / Postgres / MySQL client", "vendor this", "add a JS library", "load it from a CDN", "npm", "node_modules", or "why is this written by hand"; and when a change adds or replaces anything under view/assets. Covers the one dependency the root module has, the gate that reads go.mod and how it was made able to fail, the six driver modules and what belongs in one, and the tests that keep Node, a CDN and an uncredited asset out of the tree.
license: MIT
---

# What is allowed into the graph

The root module of this collection has exactly one direct third-party
dependency.

```sh
cat go.mod
```

```
module github.com/arandu-io/hesape

go 1.26

require golang.org/x/crypto v0.53.0

require golang.org/x/sys v0.46.0 // indirect
```

`golang.org/x/sys` is indirect, through `x/crypto`. A module graph recording
what it always pulled is not a dependency somebody added, and the rule is about
direct ones.

This is not frugality. Every project that imports any package of this collection
carries the whole root module's graph — its builds, its `go.sum`, and its
vulnerability surface. A dependency added here is a dependency added to
everybody.

## The gate, and the story worth knowing

`.github/workflows/ci.yml`, step `no third-party dependency`, reads `go.mod` and
fails a pull request that adds a second direct require.

The version that stood there before read the file for indented lines:

```sh
grep -E '^\s+[a-z]' go.mod | grep -v 'golang.org/x/crypto' | grep -q .
```

`go.mod` has two shapes. A `require` block indents its entries; one `require`
line per dependency does not. That check saw the first shape only, so a
dependency added in the second passed it — which was not reasoned about, it was
proven, by planting one and watching the gate go green. The current check reads
both shapes and skips what is marked indirect.

The lesson generalises past this one file: a gate is only a gate once somebody
has watched it fail. When you add one, plant the thing it forbids and confirm
the exit code before you trust it.

## When the change genuinely needs a driver

It goes in a module of its own. Go has no optional dependency, so a driver in
the root module is a driver in every consumer — and the case that made the point
was the reverse of the obvious one: the skeleton used to carry pgx into every
SQLite-only project, vulnerability surface included.

Six modules exist for that, and each states its own argument in its `go.mod`:

| module | what it carries |
| --- | --- |
| `redis` | `github.com/redis/go-redis/v9` — the RESP cache store, distributed lock and session handler |
| `queue/connectors/redis` | `github.com/redis/go-redis/v9` — the RESP queue connector |
| `filesystem/s3` | nothing. There is no SDK: the protocol is HTTP with a SigV4 signature, written here |
| `database/connectors/pgx` | `github.com/jackc/pgx/v5` |
| `database/connectors/mysql` | `github.com/go-sql-driver/mysql` |
| `database/connectors/sqlite` | `modernc.org/sqlite` |

```sh
find . -name go.mod | wc -l    # 7: the root and the six
```

`filesystem/s3` is the one to read before proposing an SDK. It is a module for
the rule rather than for the weight, and its `go.mod` says why: SigV4 is two
hundred lines against an AWS SDK that brings a hundred modules, its own
credential chain, its own retry policy and its own context rules — and the
algorithm has not changed since 2012 while the SDK's surface changes every
quarter.

A new driver module needs, at minimum:

1. **Its own `go.mod`**, with a comment saying what a project that does not want
   this driver is being spared. Three of the six carry one today —
   `queue/connectors/redis`, `filesystem/s3` and `database/connectors/sqlite`.
   Copy their shape; the argument is what stops the module being folded back in
   by somebody who only sees the inconvenience.
2. **No `replace` pointing at the checkout.** The database and RESP connectors
   all carried one, and for seven releases the version their `go.mod` declared
   was never what anything built against — not in CI, and not on a developer's
   machine. `GOWORK=off` in every job is what keeps a workspace from putting it
   back.
3. **An isolation check.** `database-connectors.yml` runs `go list -deps` in
   each connector and fails if it can reach another driver. If two drivers meet
   in one module the split has stopped paying for itself, and nobody notices
   until `govulncheck` reports an advisory in a project that does not use it.
4. **A CI job of its own**, with a real server where the claim needs one, and a
   check that the suite did not skip. `go test` has no exit code for "everything
   skipped", so a suite that skipped is a suite that printed ok. A module with
   no workflow is built by nothing: `.github/workflows/` currently names the
   root, the three database connectors and the two RESP ones, and `filesystem/s3`
   appears in none of them.

## Assets are a dependency too

There is no Node anywhere in this tree, and nothing may fetch anything at run
time. Three tests hold that, and they are the shape a change to `view/assets/`
has to satisfy:

- **`TestNoNodeAnywhere`** (`view/assets_test.go:23`) walks the whole repository
  and fails on `package.json`, `package-lock.json`, `yarn.lock`,
  `pnpm-lock.yaml`, `bun.lockb`, `node_modules`, `vite.config.js` or
  `vite.config.ts`.
- **`TestNoExternalOrigin`** (`view/assets_test.go:54`) scans every file in
  `view/assets/` for a reference to `cdn`, `unpkg`, `jsdelivr`, `googleapis` or
  `cloudflare`. The Content-Security-Policy the framework sets is
  `script-src 'self'`, so a CDN reference is a script that never loads, and the
  only sign is a console message nobody sees until the page is already broken.
- **`TestEveryEmbeddedAssetIsCredited`** (`view/third_party_test.go:30`) fails
  when a file appears in `view/assets/` with no entry in `view/THIRD_PARTY.md`.
  `go:embed` puts these bytes in every user's executable, so every user of the
  framework becomes a redistributor and owes the copyright notice. Two more
  tests in that file check that the credited versions are the embedded ones
  (`TestTheCreditedVersionsAreTheEmbeddedOnes`) and that the license texts are
  complete rather than named (`TestTheLicenseTextsAreComplete`).

Six files sit in `view/assets/`, five of them named by the single `go:embed`
directive at `view/assets.go:21`. Three are third party — `htmx.min.js`,
`basecoat.bundle.js` and `app.css`, which is Tailwind's output rather than its
input. Three are ours: `app.src.css`, the Tailwind input this project wrote, and
the only two first-party scripts there are:

- **`theme.js`** reads the theme somebody chose out of `localStorage` and
  applies it before the first paint.
- **`ui.js`** is the delegated client behaviour: copy buttons, the theme toggle,
  the combobox, the command palette and the range slider, all dispatched from
  `data-*` attributes read as data.

Those two are the only first-party scripts, and there is no directive framework
beside them for the same reason there is no CDN: compiling an attribute into a
function needs `unsafe-eval`, which the policy does not grant. If a proposal
needs client-side expressions, it needs a different policy, and that is the
conversation to have rather than the dependency to add.

## The check before you open a pull request

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./... && go vet ./... && go test -race ./...
```

Then the six beside the root, which `./...` never reaches:

```sh
for mod in $(find . -name go.mod); do
  dir=${mod%/go.mod}
  (cd "$dir" && GOWORK=off go vet ./... && GOWORK=off go test -race ./...) \
    && echo "ok   $dir" || echo "FAIL $dir"
done
```
