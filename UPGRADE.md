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

## Unreleased — the HTTP client takes a context, and hashing has one path

### `http/client`: every verb takes a context

```
- ./http/client.(*PendingRequest).Get: changed from func(string, map[string]string) (*Response, error) to func(context.Context, string, map[string]string) (*Response, error)
- ./http/client.(*PendingRequest).Post: changed from func(string, any) (*Response, error) to func(context.Context, string, any) (*Response, error)
- ./http/client.(*PendingRequest).Put, .Patch, .Delete, .Head: the same shape
- ./http/client.(*PendingRequest).Send: changed from func(string, string, map[string]string, any) (*Response, error) to func(context.Context, string, string, map[string]string, any) (*Response, error)
```

The context goes first and everything else is unchanged:

```go
// before
pr.Get(url, query)
pr.Send(method, url, query, data)

// after
pr.Get(ctx, url, query)
pr.Send(ctx, method, url, query, data)
```

Passing `context.Background()` reproduces the old behaviour exactly, and is worth
treating as a smell rather than a fix: it is the shape that made this necessary.
The deadline was built on a background context, so a caller's cancellation never
reached the request. Measured before the change: a caller cancelling at 20 ms
waited the full 60-second client timeout. It now returns in 20 ms with
`context.Canceled`. The wait between retry attempts was a second uncancellable
hold and is now cancellable too.

A nil context is refused rather than swapped for a background one.

**`Retry` no longer repeats a non-idempotent method.** `POST`, `PATCH`, `CONNECT`
and any method outside `GET, HEAD, PUT, DELETE, OPTIONS, TRACE` are sent once,
whatever `times` says — a retried POST is a duplicated write.

```
- ./http/client.(*PendingRequest).RetryNonIdempotentMethods: added
```

Add it only when the endpoint deduplicates. It takes no argument and does nothing
on its own, because a fifth boolean on `Retry` would sit beside `throw`, where one
positional slip buys duplicate writes.

### `encryption.ErrSignature` says which package holds it

No symbol moved, so apidiff reports nothing. The message changed:

```
before: security: the signature is not valid
after:  encryption: the signature is not valid
```

`errors.Is` is unaffected. Only code matching the text notices, and nothing in
this collection does.

It said `security` because half the package identified as the compatibility shim
that is removed at v1.0.0, and half as the home where the key lives. A developer
reading `security: ...` in a log went looking in the package that stops existing.
Every sibling error in the package, and every sibling package, names itself.

`NewSigner` also copies the application key now, as `NewEncrypter` already did.
Keeping the caller's slice fails silently: no error and no log, links simply stop
verifying once the caller reuses the buffer.

### The rate limiter refuses to be wired in a way that always fails open

```
- ./cache.Limit.After, .AfterCallback, .Response, .ResponseCallback: removed
```

Both callbacks were documented as behaviour and **nothing anywhere read them**.
`PerMinute(5).After(onlyFailures)` compiled and counted every request;
`.Response(myPage)` got the default 429. The only test asserted that the setter
stored the value. They do not come back, because each already has one spelling:
`Refuse` for the refusal and `Release` for giving an attempt back.

No caller in any repository used them, so this breaks nobody today.

**`routing/middleware.Throttle` now panics at construction** on a wiring that
could never limit: a zero or negative budget, a limit with no window, a nil
limiter, a nil key function and a nil refusal. `cache.None()` still wires, because
allowing everything is a decision rather than a mistake.

That is a behaviour change apidiff cannot see, and it is the point. Measured
against the previous code with `PerMinute(0)`: **10000 of 10000 requests were
allowed**, none refused, each one logging that the rate limiter could not be
reached — over an error that actually said the limit allows zero attempts. A
security control that silently does not exist, blaming the store. A nil refusal
was worse: the first caller to go over would have panicked instead of getting a
429, possibly weeks later.

Failing open when the store is genuinely unreachable is kept, and is now stated
as a decision rather than inherited: a guard against abuse must not turn a cache
outage into a total one, and the caller whose budget *is* the security control
checks in the handler against the same limiter, where it can fail closed. It is
also now provably loud — the request passes and an error line naming the cause is
written.

### `str` stops spelling the standard library

```
- ./str.Lower: removed
- ./str.Upper: removed
```

Both were one-line forwards to `strings.ToLower` and `strings.ToUpper`, inside the
package whose stated destination is the standard library first. Call `strings`
directly:

```go
// before
str.Lower(s)

// after
strings.ToLower(s)
```

`Stringable.Lower()` and `Stringable.Upper()` **stay**. They return a
`Stringable` and are links in a fluent chain, which the standard library has no
equivalent for; only their bodies changed.

### The relation surface is reachable, and it was compiling the wrong column

```
- ./database/model.Relation.Match: changed from func(auth.Grant, []any, func(*query.Builder)) (map[any]any, error) to func([]relations.Model, []relations.Model, string) ([]relations.Model, error)
- ./database/model.Relation.GetRelationExistenceQuery: changed shape to the one every relation implements
- ./database/model/relations.BaseRelation: no longer comparable (also HasManyThrough, HasOneOrManyThrough)
```

`model.Relation` had one implementor and it was a test double. The ten real
relations could not satisfy it, so `Builder.With`, `Model.Load`,
`Collection.Load*`, `Has`, `WhereHas`, `WithCount` and `model.Related` were
**unreachable from any application** — in either entity shape. `model.Relation` is
now `relations.Relation` plus the one method the tree has and did not declare.

Its old shape could not have worked even if something had implemented it: keys
were the primary keys, so a has-many on another local key read the wrong column;
morph-to matched its own parents; and a key-to-value dictionary had nothing to say
for a parent that matched nothing, so a childless parent read back as never
loaded rather than loaded and empty.

**A live wrong-results bug went with it.** `BaseRelation.GetRelationExistenceQuery`
called `GetExistenceCompareKey`, which every relation overrides — and Go
dispatches statically, so it reached the base's answer. Every `WhereHas`, `Has`
and `WithCount` over a has-many compiled `where users.id = posts.id`: the parent's
key against the child's own key. Well-formed, it runs, and it returns whichever
rows happen to share an id. Nothing reported it because nothing could reach the
path.

**Relation queries named the tenant twice.** `model.Builder.prepare` owns the
scoping now, and it uses the model's declared column where the relation layer only
knew the default name — wrong for a renamed column, and a column that does not
exist on a shared table. A builder that makes no promise is still filtered, so the
failure mode of this split is a duplicate clause, never a missing one.

The comparability loss is the func field that fixes the static dispatch. Nothing
compares relations with `==`.

### `image`: a ceiling before the decode, and a context through the transformations

```
- ./image.Driver.Process: changed from func([]byte, *ImagePipeline) ([]byte, error) to func(context.Context, []byte, *ImagePipeline) ([]byte, error)
- ./image.Driver.DominantColor, .TransformationHandler: the same shape
- ./image.(*Image).Dimensions, .Width, .Height, .MimeType, .Extension, .ToBytes, .ToString, .ToBase64, .ToDataURI, .HashName: each takes a context first
- ./image.Driver.MaxPixels: added
```

Nothing read the header before decoding, so a crafted file declaring enormous
dimensions was decoded in full — a memory exhaustion whose size the attacker
chooses. Measured with a 72-byte PNG declaring 12000 by 12000: **549.5 MiB
allocated** without the ceiling, **6 KiB** with it, and the refusal names both the
dimensions and the limit.

The default is 33,554,432 pixels, which is a canvas of exactly 128 MiB at four
bytes a pixel — above an 8K frame and every camera in ordinary use.
`ImageManager.MaxPixels(n)` moves it, zero restores the default, and there is no
unlimited setting.

The context reaches the transformations, so a long resize is cancellable rather
than running to the end of a request nobody is waiting for. `Dimensions` reads the
header only and takes one for symmetry; `ToResponse` uses the request's own, so a
browser going away now cancels the work.

The ceiling bounds the decode and not what a transformation is asked to produce:
`Resize(100000, 100000)` still allocates what the caller asked for. That is
caller-chosen rather than attacker-chosen, and it is stated in the package
comment.

### `support/arr` is removed; `collections/arr` is the one

```
- package github.com/arandu-io/hesape/support/arr: removed
```

Two packages with the same name answered the same question, sharing 59 exported
names with diverging signatures. The one that stays has 85.9% test coverage
against 0.0%, tells a present nil from an absent key, keeps an `int` as an `int`
where the other round-tripped it to `float64` through JSON, and actually removes
an element when asked to forget one through a slice — which the other did not,
silently.

The signatures that change for a caller:

```go
// before -- a default argument
v := arr.Get(m, "k", fallback)

// after -- present and absent are distinguishable
v, ok := arr.Get(m, "k")
```

`Pull`, `First` and `Last` move the same way. `Sort`/`SortDesc` take a projection
rather than a comparator. `ToCssClasses`/`ToCssStyles` are variadic — **a caller
passing a slice must spread it**, and this one compiles either way: without the
spread a class list renders as `[btn btn-primary]`, one class, with nothing to
tell you.

`Arrayer` has no replacement type. Write the interface where you need it:
`interface{ ToArray() map[string]any }`.

**One behaviour is gone on three methods.** `Fluent.Get`, `UriQueryString.Get` and
`ValidatedInput.Input` no longer accept a `func() any` lazy default. Lazy defaults
survive in `Fluent.Value` and everywhere else that uses the support package's own
helper.

**And one widens.** `Dot` now descends into any map or slice kind, where before it
descended only into `map[string]any`. A translation group nesting
`map[string]string` will start yielding wildcard messages it previously dropped.

### A scheduled command no longer goes through a shell

```
- ./console/scheduling.(*CommandBuilder).BuildCommand: changed from func(...) string to func(...) ([]string, error)
- ./database/schema.ProcessFactory: removed
- ./queue.(*Listener).MakeProcess, .RunProcess: re-typed
```

Three packages built a command line and handed it to `exec.CommandContext`
directly, and one of them handed it to a shell: `sh -c "<the line>"`. Any part of
that line coming from data was interpreted, which is an injection surface — and
the package written to prevent exactly that sat unused beside it.

Measured on the old path, with a scheduled command containing `; touch <file>`:
the file **was created**. On the new path it is not, and the argument is echoed
verbatim.

What the shell was carrying moved rather than disappearing: redirection is an
output handler, `sudo -u` is the first four words of the argument list, and the
backgrounded subshell that reported the finish is now a wait beside the run in
the process that started it.

If you were relying on `BuildCommand` to hand you a string, it now hands you the
argument list, which is what a command actually is. A parameter containing a
space, a semicolon or a quote is one argument and stays one argument.

### `hashing`: one way to hash, and it was already the only one running

`HashManager` and everything under it are removed — 32 incompatible lines, all in
`./hashing`. `Make` and `Check` are unchanged and are the whole surface.

```
- ./hashing.HashManager, .Config, .DriverBcrypt, .DriverArgon, .DriverArgon2id: removed
- ./hashing.ArgonHasher, .Argon2IdHasher, .NewArgonHasher, .NewArgon2IdHasher: removed
- ./hashing.AbstractHasher, .ErrDriverNotSupported, .ErrWrongAlgorithm, .ErrValueTooLong: removed
- ./hashing.Hasher.Make/.Check/.NeedsRehash: the variadic Options parameter is gone
- ./hashing.Options.Memory, .Time, .Threads, .Limit, .Verify: removed
```

`HashManager` had no caller outside its own tests. What it did have was three
disagreeing argon2id parameter sets and a bcrypt fallback, while the configuration
this framework publishes says argon2id — a key written and read by nothing but the
manager, whose fallback contradicted the published value.

Two of those disagreements were worse than duplication. `NewArgonHasher` defaulted
to **1024 KiB** of memory where `Make` compiles in **65536**, so a project setting
`hashing.driver=argon` wrote hashes sixty-four times weaker without being told. And
`ForAuth(nil)` — the only non-fake `auth.Hasher` — returned bcrypt at cost 12
rather than argon2id.

**No stored hash becomes unverifiable.** `Check` dispatches on the `$2` prefix
independently of anything removed, and a test carries five hashes written by the
previous code as literals: bcrypt at two costs, argon2i, and argon2id at two
parameter sets. The one change to stored data is that `ForAuth(nil).Make` now
writes argon2id; existing bcrypt columns still authenticate and are flagged for
rewrite on the next sign-in.

`BcryptHasher` and `Options{Rounds}` stay, documented as the import path for
columns another system wrote.

Per-call options go to the constructor:

```go
// before
h.Make(password, hashing.Options{Memory: 65536, Time: 4})

// after -- the parameters are compiled in, deliberately, and there is nothing to pass
hashing.Make(password)
```

### A rollback refuses the migration it cannot undo

No symbol changed, so apidiff reports nothing. The behaviour did.

A migration with no `Down` used to be rolled back like this: nothing ran, the row
recording it as applied was **deleted**, and the line printed said `Success`. The
schema kept the change and the table stopped saying so. The next `aru migrate`
ran the `Up` again and failed on what was already there — two commands away from
the thing that caused it.

Now the record is deleted only when the change was actually undone, and a
migration that reverses nothing has to say which of two things it is:

```go
// The change that genuinely has no inverse. The rollback leaves it applied,
// prints the reason, and carries on with the rest of the batch.
func (BackfillPostViews) Irreversible() string {
    return "the rows it filled in are no longer distinguishable from the rows it did not"
}

// Everything else: write the Down.
func (CreatePostsTable) Down(ctx context.Context, conn migrations.Connection) error {
    return conn.Schema().Drop(ctx, "posts")
}
```

A migration that declares neither stops the rollback with an error naming it. The
migrations already undone in that run stay undone; their records are already
gone. `aru migrate` is unaffected — a migration with no `Down` still applies.

`migrations.IrreversibleMigration` is the interface, tested for the same way
`ReversibleMigration` is: a type assertion, so a wrong signature is a build
failure rather than a rollback that quietly does nothing.

Declaring both `Down` and `Irreversible` is refused rather than resolved. They
are opposite claims about one migration and nothing outside it can tell which
the author meant.

`Reset` is no longer a guarantee of an empty schema: a declared-irreversible
migration keeps its record and its change.

`aru doctor`'s `rollback-does-nothing` still reports a migration that created a
table and wrote no `Down`, and it still leaves a backfill alone. It warns earlier
than this refuses; it does not replace it.

### The string-form resource registration is removed

```
- ./routing.(*Router).Resource: removed
- ./routing.(*Router).Resources: removed
- ./routing.(*Router).ApiResource: removed
- ./routing.(*Router).ApiResources: removed
- ./routing.(*Router).Singleton: removed
- ./routing.(*Router).Singletons: removed
- ./routing.(*Router).ApiSingleton: removed
- ./routing.(*Router).ApiSingletons: removed
- ./routing.(*Router).SetControllerDispatcher: removed
- ./routing.(*Router).GetControllerDispatcher: removed
- ./routing.(*Router).ResourceParameters: removed
- ./routing.(*Router).ResourceVerbs: removed
- ./routing.(*Router).SingularResourceParameters: removed
- ./routing.ControllerDispatcher: removed
- ./routing.ResourceOptions: removed
- ./routing.ResourceRegistrar: removed
- ./routing.NewResourceRegistrar: removed
- ./routing.PendingResourceRegistration: removed
- ./routing.NewPendingResourceRegistration: removed
- ./routing.PendingSingletonResourceRegistration: removed
- ./routing.NewPendingSingletonResourceRegistration: removed
- ./routing.GetParameters: removed
- ./routing.SetParameters: removed
- ./routing.SingularParameters: removed
- ./routing.Verbs: removed
```

Every route these registered answered `500 no controller dispatcher wired`.
`SetControllerDispatcher` is the only thing that could have made them answer, and
nothing called it — not here, not in any module that requires this one. The
routes appeared in the table and in `aru routes`, they matched, and then they
failed at request time.

Nothing replaces the dispatcher, because dispatching a method by its name is
what this collection does not do: there is no container to resolve a controller
out of, and Go has no call-by-name that a compiler checks.

`routing.Resource[C]` is the registration path, and it is unchanged. It takes the
controller value rather than its name, and registers exactly the actions the
value implements — a type assertion against `Indexer[C]`, `Shower[C]` and the
other five, decided when it compiles:

```go
// before -- registered seven routes, each of which answered 500
r.Resource("invoices", "InvoiceController").Only("index", "show").Register()

// after -- registers the actions InvoiceController actually implements
routing.Resource(r, "invoices", InvoiceController{}, adapt)
```

The `Only`/`Except` pair has no replacement and needs none: a controller that
should not answer `destroy` does not implement `Destroy`, and then there is no
route rather than a route that 404s or 500s.

The options with no equivalent on `Resource[C]` — `Shallow`, `Scoped`,
`WithTrashed`, `Names`, `Parameters`, `MiddlewareFor` — went with it. Each was
reachable only through a route that could not answer, so none of them ever ran.

### `view/compilers` is removed; there was never a second compiler

```
- package github.com/arandu-io/hesape/view/compilers: removed
- package github.com/arandu-io/hesape/view/compilers/concerns: removed
```

Nothing imported either one, and nothing in them emitted Go. `Compiler` held a
cache path and a hash; `KyseCompiler` held registries; `ComponentTagCompiler`
expanded component tags into directives that no emitter read. `concerns` was one
`doc.go` saying of itself that nothing there was implemented yet.

The compiler that runs is the one the view build has always called, and it is
unchanged. Building a view goes through the CLI, not through this package, so
there is nothing to rewrite: a caller of these types could not have compiled a
view with them.

`view.Factory` keeps every runtime method a compiled view calls — `StartSection`,
`YieldContent`, `StartPush`, `StartFragment`, `AddLoop` and the rest — and none
of it moved.

### `exception.(*Handler).HandleUncaughtException` is removed

```
- ./exception.(*Handler).HandleUncaughtException: removed
```

It forwarded to `HandleException` and nothing called it. Its doc comment said
"It is Recover that calls this", and `Recover` does not: the recovered panic goes
to the handler's own answer path, which captures the stack frames the error page
needs. A caller that took the comment at its word got the handler stack without
those frames.

```go
// before
h.HandleUncaughtException(w, r, err)

// after
h.HandleException(w, r, err)
```

`HandleException` returns what the handler stack answered, which the removed
method discarded. Panics keep going through `Recover`, unchanged.

## Unreleased — the unsigned verification link is gone

```
- package github.com/arandu-io/hesape/auth/notifications: removed
```

Both halves described a flow this collection does not have.

`ResetPassword` carried a `Token` "minted and stored by the password broker" and
an `Expire` the broker was to fill in — a broker, a repository and a token table
that were removed in the same release. Its own comment said the lifetime is only
a sentence in the message because the token repository is what refuses an old
token.

`VerifyEmail` was the second token model, and it was the unsigned one:
`/verify-email/{id}/{sha1(address)}`. The package's own doc said of it that a
verification link **must** be signed, and that without one *"anybody who knows
somebody's e-mail address can confirm it for them"* — a built-in behaviour
documented as unusable, next to a signed flow that already works.

**Nothing you have issued stops working.** The package had no importer outside its
own test, so no link in anybody's inbox came from it. The live flow signs
`len(id):id|address` and is unchanged; a link minted before this release was
verified against the code after it, byte for byte.

What stays, because none of it is a token model: the `MustVerifyEmail` column and
its check, the listener that sends, and the middleware that gates.

## Unreleased — one password reset flow

`hesape/auth/passwords` and `hesape/auth/console` are removed.

```
- package github.com/arandu-io/hesape/auth/passwords: removed
- package github.com/arandu-io/hesape/auth/console: removed
```

They held a second reset flow, complete and unused: a broker, a manager, and a
database and a cache repository for a token whose hash was stored. The flow this
framework runs is stateless and signed, and it was already the decided one -- the
stored table was written up as debt and then discharged, in writing, before this
removal.

**What you get instead.** The signed link carries tenant, account id and address,
each length-prefixed so no field can move a boundary, plus a fingerprint of the
stored password hash. Expiry is enforced by the signer. Single use comes from the
fingerprint: changing the password invalidates every link ever minted against the
old one at the same instant, rather than only the one that was redeemed.

**What you give up, and it is deliberate.** There is no way to revoke one link
before it expires without changing the password. That is the price of having no
table, and it was weighed rather than overlooked.

`ClearResetsCommand` goes with the packages. It swept expired rows out of a
store, and there are no rows. It was registered in no console registry, so
nothing loses a command it was running.

## v0.17.0 — the row is the model

The heading is `Unreleased` because these have not been tagged yet. It becomes
the number of the release that carries them, at the moment it is cut.

`database/model.Collection[T]` was `[]*Model[T]` and is `[]*T`. Every terminal
that answered a model answers the row: the application's own struct, with the
columns as fields. Reading one is reading a field, and there is nothing to
unwrap first.

That is one change, and it moved 61 exported signatures. They are all the same
substitution — `*Model[T]` became `*T` — so the table is one row per family and
the full list is at the end of this entry.

| was | is | what to do |
|---|---|---|
| `Collection[T]` was `[]*Model[T]` | `[]*T` | Range over it and read fields. `c.First()`, `c.Find(id)`, `c.All()` and `c.GetDictionary()` hand back rows |
| `First`, `Find`, `Sole`, `FirstOrCreate` and the rest returned `(*Model[T], error)` | `(*T, error)` | Drop the `.Entity`. Where you called a model method on the result, see the two shapes below |
| `Each`, `Cursor`, `Lazy`, `LazyById` handed the callback a `*Model[T]` | a `*T` | Same, in the callback's parameter |
| `Map`, `CountBy`, `MapWithKeys`, `Zip`, `Pad`, `Partition` took a `*Model[T]` | a `*T` | Same, in the callback's parameter |
| `Related(m *Model[T], name)` | `Related(row *T, name)` | Pass the row. It answers `false` for a row that reaches no model, where it used to dereference nothing |
| `m.Is(other *Model[T])`, `m.IsNot(...)` | `m.Is(other *T)` | Pass the row |
| `factories.Factory[T].Make() []T` | `([]*T, error)` | Read the error. What comes back is rows, built through the model, so a made row can then be saved |
| `factories.Factory[T].MakeOne() T` | `(*T, error)` | Same |
| `factories.Factory[T].Create` returned `[]*model.Model[T]` | `[]*T` | Drop the `.Entity` |
| `factories.AfterCreating(func(ctx, g, *model.Model[T]) error)` | `func(ctx, g, *T) error` | The callback receives the stored row |

### The line that replaces `m.Save(...)`, for each of the two entity shapes

A loop that read rows and wrote them back was:

```go
users, err := q.Get(ctx, g)          // []*model.Model[User]
for _, m := range users {
        _ = m.SetAttribute("name", "Ada")
        _, err = m.Save(ctx, g)
}
```

**An entity that embeds `model.Model[T]`** carries its model inside itself, so
the row is the model and Go promotes the methods out of it:

```go
type User struct {
        model.Model[User]

        ID   int64  `db:"id"`
        Name string `db:"name"`
}

users, err := model.Query[User](db).Get(ctx, g)   // []*User
for _, u := range users {
        u.Name = "Ada"
        _, err = u.Save(ctx, g)
}
```

`Save`, `Delete`, `Refresh`, `Load`, `Is` and the rest are reached this way, and
`model.ModelOf(u)` is how to reach the model's configuration — the table, the
key, the tenant column — without the struct exposing them again.

**A plain struct that does not embed it** has no field pointing back at a model,
so there is nothing on the row to call. The columns are all it carries. Write the
update as a statement over the key:

```go
_, err := model.Query[User](db).WhereKey(u.ID).
        Update(ctx, g, map[string]any{"name": "Ada"})
```

or give the entity the embedded model, which is what makes the first form work.
The same line divides the relations: `Related` answers `false` on a plain row,
and `Collection.Load`, `LoadMissing`, `LoadAggregate` and
`Builder.EagerLoadRelations` report the new `ErrRowHasNoModel` rather than
attaching a relation to nothing and reporting success.

That last one is a behaviour change and not a compile break, so it is here rather
than in the table: those five used to answer `nil` having loaded nothing at all
for a collection of the plain shape, which is a relation the next line reads and
does not find.

### Every signature the tool reported

`database/model`, on `*Builder[T]`: `Create`, `CreateOrFirst`, `CreateOrRestore`,
`Cursor`, `Each`, `Find`, `FindOrFail`, `FindOrNew`, `FindSole`, `First`,
`FirstOr`, `FirstOrCreate`, `FirstOrFail`, `FirstOrNew`, `FirstWhere`,
`ForceCreate`, `IncrementOrCreate`, `Lazy`, `LazyById`, `RestoreOrCreate`,
`Sole`, `UpdateOrCreate`.

On `*Model[T]`: `Create`, `Find`, `FindOrFail`, `FindOrNew`, `First`,
`FirstOrCreate`, `FirstOrNew`, `ForceCreate`, `Fresh`, `Is`, `IsNot`,
`NewCollection`, `Replicate`, `ReplicateQuietly`, `ResolveRouteBinding`,
`ResolveSoftDeletableRouteBinding`, `UpdateOrCreate`, `WithoutRelations`.

On `Collection[T]`: the type itself, `All`, `Find`, `FindOrFail`, `First`,
`Flip`, `GetDictionary`, `Pad`, `Partition`.

Package functions: `CountBy`, `Map`, `MapWithKeys`, `Related`,
`ResolveChildRouteBinding`, `ResolveSoftDeletableChildRouteBinding`, `Zip`.

`database/model/factories`, on `*Factory[T]`: `AfterCreating`, `Create`,
`CreateOne`, `Make`, `MakeOne`.

### A query built on a `Connection` is renumbered for Postgres

No signature moved, so `apidiff` says nothing about this one. It is here because
the statement your driver receives changed.

Every grammar compiles a placeholder as `?`, Postgres included, and the
translation to `$1, $2` was reached from the instrumented handle, from a
transaction on it and from the migration adapter — and from nowhere on
`database.Connection`, which is what a query builder runs on. So a query built
with `connection.Table(...)` arrived at pgx still carrying `?`, which it does not
accept, on the read and on all three writes.

`Connection` renumbers now, once, for every statement that carries values.
Nothing that worked before stops working: a statement written with `$1` holds no
`?` and is left alone, and one written with `?` was failing at the server.

Two consequences worth knowing:

- A statement carrying **no** bindings is left alone, `Unprepared` included. It
  has no placeholder to number, so a `?` in one is an operator — Postgres spells
  jsonb containment that way — or a character in a literal.
- The query log, `Pretend` and a `QueryException` now carry the statement as it
  was sent rather than as it was written. On Postgres that means `$1` where it
  used to read `?`. A test asserting the portable form against a Postgres
  connection is the one thing here that can stop passing.

`?` as an **operator** in a statement that also carries values is still
renumbered, and that predates this: `where "data" ? 'k' and "tenant_id" = ?`
becomes `where "data" $1 'k' and "tenant_id" = $2` on the instrumented handle as
well. Write that comparison with `jsonb_exists` until it is fixed.

### `*database.DB` gained seven methods

An addition, and listed only for the one way an addition breaks a build: a type
that embeds `*database.DB` — or `*data.DB`, which is an alias of it — alongside
something else declaring `Select`, `Insert`, `Update`, `Delete`, `Statement`,
`GetQueryGrammar` or `GetPostProcessor` now has an ambiguous selector. Name the
one you meant, or declare the method on your own type, where it wins outright.

What it buys is that the handle a module constructor receives satisfies
`database/model.DB`, so `model.Query[Widget](r.db)` compiles with it and every
statement keeps reaching the Collector, keeps being renumbered, and keeps
joining an open `database.Transaction`.

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
