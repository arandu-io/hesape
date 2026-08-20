# Upgrade guide

What changed in a way that stops your code compiling, and what to write instead.

Additions are not listed. A new symbol breaks nothing, and a file that listed
every one of them would be a changelog nobody reads to find the two lines that
matter.

## Before v1.0.0

While the version starts with `v0.`, the API can break. That is what `v0.` means
in Go and it is deliberate — the alternative is freezing a shape before anybody
has built on it. What is not deliberate is breaking it quietly, which is what
this file exists to stop.

Every release from here is compared against the one before it by `apidiff` in
CI. An incompatible change with no entry here fails the build.

The entries below were measured the same way and not remembered: `apidiff`
between every consecutive pair of tags from `v0.1.0` to `v0.11.0`. So each break
is filed under the release it actually shipped in. `v0.4.0`, `v0.6.0` and
`v0.10.0` are absent because nothing broke in them, and `v0.1.0` because it is
the first tag and has nothing before it to compare against.

---

## v0.11.0 — the console takes the gauge registry

| was | is | what to do |
|---|---|---|
| `log.NewConsole(r, editor)` | `log.NewConsole(r, editor, *log.Gauges)` | Pass the registry whatever measures those numbers writes into, from `log.NewGauges()`. Pass `nil` for a console with no gauge section |

`nil` is an argument and not a shortcut: the section is then absent rather than
present and empty, because an empty table on a diagnostic page reads as a number
that failed to arrive.

---

## v0.9.0 — `routing/events` is gone

The package declared `PreparingResponse`, `ResponsePrepared`, `RouteMatched` and
`Routing`. Nothing emitted any of the four, so a listener registered against one
was never called.

| was | is | what to do |
|---|---|---|
| `events.RouteMatched` | `(*routing.Router).Matched(func(*Route, *http.Request))` | It fires after a route matches and before its middleware, and hands over the whole `*Route` instead of the two getters the event's interface exposed |
| `events.Routing` | a middleware | Before dispatch is what middleware is, and the chain already wraps every route |
| `events.PreparingResponse`, `events.ResponsePrepared` | *nothing* | Both carried an `http.ResponseWriter`, which is one value at two moments. The handler streams into that writer, so there is no instant at which a response is prepared and not yet sent |

---

## v0.8.0 — `DefaultPostProcessor` takes the dialect

| was | is | what to do |
|---|---|---|
| `var database.DefaultPostProcessor func() query.Processor` | `func(database.Dialect) query.Processor` | Take the dialect and answer with the processor for it. `DefaultQueryGrammar`, next to it, already had that argument |

Without the dialect the registration cannot pick between processors that differ
by engine, and that difference is the one hardest to notice when it is wrong:
Postgres reads an inserted row's identifier out of a returning clause and MySQL
reads it out of band, so the wrong processor answers with a wrong identifier
rather than an error.

A connector that assigns the variable from an `init` still wins over the shipped
registration. What changed is where a project starts from, not who may move it.

---

## v0.7.0 — a JSON answer is a resource, and `FrameGuard` is gone

### `(*http.Context).JSON`

| was | is | what to do |
|---|---|---|
| `JSON(status int, value any)` | `JSON(status int, resource http.JsonResource)` | Pass a resource — anything with `ToArray() map[string]any` and `With() map[string]any`. Handing it an entity stops compiling, which is the point |

An encoder that took `any` answered with whatever fields the value happened to
have, including the ones somebody adds to the type later without ever reading
the handler: a password hash, an internal note, the identifier of the account a
row belongs to. What may leave is now a list somebody wrote.

The body is the fields under a `data` key, plus whatever `With` returns beside
them. An endpoint that needs another shape builds its own with `http/resources`
and writes it to `Context.Response`.

`resources.JsonResource` is an alias for `http.JsonResource`, so code that names
the type keeps compiling and a resource satisfies both by being what it already
is. The declaration moved because the resource layer imports `http` to build a
response, and the import back would be a cycle. `apidiff` prints a line for
every symbol in `http/resources` whose signature mentions the type; they are all
that one move.

`routing.(*ResponseFactory).JSON` still takes `any`, and that is deliberate: it
is a port of the factory method this collection mirrors, and the loose value is
what the original accepts.

### `http/middleware.FrameGuard`

| was | is | what to do |
|---|---|---|
| `middleware.FrameGuard(option string)` | *removed* | Take it out of the chain. `middleware.SecurityHeaders` already sets `X-Frame-Options: DENY` on every response, and the CSP it writes carries `frame-ancestors 'none'` |

---

## v0.5.0 — migrations are a type, and the cursor is signed

### A migration is an interface, not three strings

`database.Migration` was `{ID, Up, Down string}`, and the runner beside it split
the `Up` on semicolons before sending it. Both are gone. What replaces them is
`github.com/arandu-io/hesape/database/migrations`:

```go
type CreateInvoicesTable struct{ migrations.BaseMigration }

func (CreateInvoicesTable) GetName() string {
	return "2026_07_29_000001_create_invoices_table"
}

func (CreateInvoicesTable) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, `CREATE TABLE invoices (...)`, nil)
	return err
}
```

`BaseMigration` answers `GetConnection`, `ShouldRun` and `WithinTransaction`, so
`GetName` and `Up` are what is left to write. `Down` is optional and found by
type assertion, which makes a `Down` with the wrong signature a build failure
instead of a rollback that quietly does nothing.

**Keep the id.** It becomes the string `GetName` returns, unchanged. A migration
that has run in a database and comes back under a different name runs again.

**One statement per call.** Nothing splits on semicolons now, so an `Up` that
held three statements becomes three `conn.Statement` calls, in order.

| was | is | what to do |
|---|---|---|
| `database.Migration` | `migrations.Migration` | An interface, per above. Nothing is left in `database` under that name |
| `database.Migrate(ctx, *DB, []Migration)` | `(*migrations.Migrator).Run` / `.RunPending` | Build one with `migrations.NewMigrator`, and reach a connection with `database.ForMigrations` or `database.MigrationResolver` |
| `database.Rollback(ctx, *DB, []Migration)` | `(*migrations.Migrator).Rollback` | `Options.Steps` and `Options.Batch` say how far back, and neither existed before |
| `database.Status(ctx, *DB, []Migration)` | `(migrations.MigrationRepositoryInterface).GetMigrationBatches` | It answers name to batch, which is the table a status command prints |
| `database.AppliedMigrations(ctx, *DB)` | `(migrations.MigrationRepositoryInterface).GetRan` | Names only, ordered by batch and then by name. For the batch as well, `GetMigrationBatches` |
| `database.Pending(ctx, *DB, []Migration)` | `(*migrations.Migrator).Run` | Pending is computed inside `Run` against the registry, so there is nothing left to ask separately |
| `database.AppliedMigration` | `migrations.MigrationRecord` | Read the migration's id off `Migration`. `ID` still exists and is a different field: the row's ordinal, an `int`. There is no applied-at |
| `database.MigrationsTable` | `migrations.DefaultTable` | **The value changed**, from `arandu_migrations` to `migrations`. A database migrated by an older binary has its records under the old name: pass `arandu_migrations` to `migrations.NewDatabaseMigrationRepository` to go on reading it, or rename the table |

`database.KeyText` stays where it is. It says how a text column that takes part
in a key is spelled, which has nothing to do with who runs the migration.

An application's own migrations reach the `Migrator` through
`migrations.Register` from an `init()` — a package nothing calls cannot be
asked. A module still declares its schema with `Migrations()`, and
`foundation.Migration` is now an alias for `migrations.Migration`, so every
signature that names it changed identity with it: `foundation.Migratable`,
`bus.Migrations`, `notifications.Migrations`, `queue.(*Module).Migrations`,
`queue.(*DatabaseQueue).Migrations` and
`queue/failed.(*DatabaseFailedJobProvider).Migrations`. `apidiff` prints a line
for each; they are the one move above, and returning the new type is the whole
of the fix.

### The pagination cursor is signed

The cursor was base64url of JSON: a client could decode it, move the boundary
row to one it had never been shown, re-encode it and send it back. Base64 is
transport, not a signature.

| was | is | what to do |
|---|---|---|
| `cursor.Encode() string` | `(*pagination.CursorSigner).Encode(cursor) string` | Build one with `pagination.NewCursorSigner(signer, ttl)` over the application key. `Cursor` has no `Encode` any more, so there is no keyless encoder left to reach by accident |
| `pagination.FromEncoded(s) (Cursor, error)` | `(*pagination.CursorSigner).FromEncoded(s)` | The same signer reads it back, and refuses a token this application did not write |
| `pagination.ResolveCurrentCursor(u, name)` | `ResolveCurrentCursor(cs *CursorSigner, u, name)` | Pass the signer first |

`CursorPaginate` needs `Options.Signer` for the same reason, and panics without
one rather than writing links whose reader picks the rows the next query starts
at.

A token issued before this carries no signature and does not verify. There is no
window in which an unsigned one is accepted — stripping the signature is exactly
what forging produces. Read through `ResolveCurrentCursor` such a link is the
first page, like any cursor that does not parse.

### `NotificationTableCommand.MigrationStub`

| was | is | what to do |
|---|---|---|
| `MigrationStub() string` | `MigrationStub() (string, error)` | Handle the error. The SQL is read off `notifications.Migrations` now, by running the migration against a connection that records instead of executing, so it can fail where a constant string could not |

---

## v0.3.0 — `socialite` is `oauth`

A product name from another ecosystem teaches nothing to somebody who has not
used that product. Every component here is named for what it does, and this one
is an OAuth 2 client with a provider catalogue on top.

| was | is | what to do |
|---|---|---|
| `github.com/arandu-io/hesape/socialite` | `github.com/arandu-io/hesape/oauth` | Change the import path. `UserData` and everything on it keep their names |
| `github.com/arandu-io/hesape/socialite/oauthtwo` | `github.com/arandu-io/hesape/oauth/providers` | Change the import path. `Provider`, `NewProvider`, the four provider constructors, `StateStoreInterface`, `CookieStateStore` and `AccessToken` keep their names. The old name was working around a digit in a package name |

No shim is left behind, so an import of the old path fails to resolve instead of
compiling against something stale. The error messages carried the old name too,
and those changed with it.

This is the client half. It is not an authorization server, and it is not
`golang.org/x/oauth2`, which is the token exchange and nothing above it.

---

## v0.2.0 — a middleware that promised Link headers and passed through

| was | is | what to do |
|---|---|---|
| `http/middleware.AddLinkHeadersForPreloadedAssets(limit int)` | *removed* | Take it out of the chain. Nothing else changes: it called `next.ServeHTTP` and did nothing else |

The doc comment said it added `Link` headers for preloaded assets, and no header
ever appeared. Nothing fails when a middleware like that is wired, and the
reason is invisible to whoever wired it, which is worse than the absence.

It could not do the job here either: the original reads its asset list out of a
build manifest, there is no manifest, and the HTTP/2 push it cited as the
motivation was removed from Chrome in 2022.

---

## How this is checked

`apidiff` runs in CI on every pull request, comparing the working tree against
the latest tag, over the whole module. Incompatible changes are printed in the
build log, and the build fails when there are some and this file was not touched
since that tag.

Whole-module and not package by package, deliberately. A loop over the packages
of the working tree cannot see a package that was deleted: it is gone from
`go list ./...`, so nothing asks about it. Two of the breaks above are exactly
that.

The point is not to prevent the break. It is to make it something somebody
decided, in a diff a reviewer can see, instead of something a person finds out
when their build stops.
